// Content decoding as libcurl does it for CURLOPT_ENCODING ""
// (lib/content_encoding.c), with the encodings the libcurl PHP links in
// the reference environment supports (built with zlib, brotli and zstd).
// The behaviour on bad, truncated and over-long bodies was checked against
// php-curl with libcurl 8.22 (TestDecodingReader_CurlParity).

package http

import (
	"bufio"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"io"
	"strings"

	"github.com/dsnet/compress/brotli"
	"github.com/klauspost/compress/zstd"
)

// acceptEncoding is the Accept-Encoding header libcurl sends for
// CURLOPT_ENCODING "" (Curl_get_content_encodings): every decoder it was
// built with, in its order. CurlDownloader sets that option, so Composer
// sends this on every request it lets curl decode.
const acceptEncoding = "deflate, gzip, br, zstd"

// zstdMaxWindow is the largest zstd window libcurl's decoder accepts
// (ZSTD_d_windowLogMax defaults to 27: 128 MiB).
const zstdMaxWindow = 1 << 27

// encodingError is a decoding failure as curl reports it: an error number
// (CURLE_BAD_CONTENT_ENCODING, or CURLE_WRITE_ERROR for data after the end
// of a zlib stream) and message.
type encodingError struct {
	errno int
	msg   string
}

func (e *encodingError) Error() string { return e.msg }

// curl_easy_strerror texts, the messages of decoder failures curl reports
// without failf().
const (
	curlBadEncoding = "Unrecognized or bad HTTP Content or Transfer-Encoding"
	curlWriteError  = "Failed writing received data to disk/application"
)

// decodingReader undoes the Content-Encoding header's encodings the way
// Curl_build_unencoding_stack stacks decoders: a comma-separated list,
// names without regard to case, "identity" and "none" skipped, the last
// encoding applied undone first. An unknown name fails once the body has
// data ("Unrecognized content encoding type"), as curl's error writer
// does.
//
// libcurl does not require a compressed stream to be complete: a body that
// ends inside one yields what was decoded so far without an error. Errors
// of the body itself (src) are passed through unchanged. The reader is an
// io.Closer when it decodes anything; Close releases the decoders.
func decodingReader(src io.Reader, header string) io.Reader {
	var names []string

	for name := range strings.SplitSeq(header, ",") {
		name = strings.ToLower(strings.Trim(name, " \t"))
		if name != "" && name != "identity" && name != "none" {
			names = append(names, name)
		}
	}

	if len(names) == 0 {
		return src
	}

	body := &sourceReader{r: src}

	var r io.Reader = body

	for i := len(names) - 1; i >= 0; i-- {
		switch names[i] {
		case "gzip", "x-gzip":
			r = &lazyDecoder{src: r, body: body, open: openGzip, zlib: true}
		case "deflate":
			r = &lazyDecoder{src: r, body: body, open: openDeflate, zlib: true}
		case "br":
			r = &lazyDecoder{src: r, body: body, open: func(r *bufio.Reader) (io.Reader, error) {
				return brotli.NewReader(r, nil)
			}}
		case "zstd":
			r = &lazyDecoder{src: r, body: body, open: func(r *bufio.Reader) (io.Reader, error) {
				return zstd.NewReader(r, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxWindow(zstdMaxWindow), zstd.WithDecoderLowmem(true))
			}}
		default:
			r = &unknownEncoding{src: r}
		}
	}

	return r
}

// openGzip opens a "gzip" body. curl inflates it with inflateInit2(z,
// MAX_WBITS + 32), which detects a gzip or a zlib header from the first
// two bytes and fails right away on anything else.
func openGzip(br *bufio.Reader) (io.Reader, error) {
	head, err := br.Peek(2)
	if err != nil && len(head) == 0 {
		return nil, err
	}

	switch {
	case head[0] == 0x1f && (len(head) == 1 || head[1] == 0x8b):
		zr, err := gzip.NewReader(br)
		if err != nil {
			return nil, err
		}

		// zlib's inflate stops at the end of the first member
		zr.Multistream(false)

		return zr, nil
	case len(head) == 1 && head[0]&0x0f == 8, isZlibHeader(head):
		return zlib.NewReader(br)
	}

	return nil, gzip.ErrHeader
}

// isZlibHeader is whether two bytes start a zlib stream (RFC 1950: deflate
// method, header check).
func isZlibHeader(head []byte) bool {
	return len(head) == 2 && head[0]&0x0f == 8 && (uint16(head[0])<<8|uint16(head[1]))%31 == 0
}

// openDeflate opens a "deflate" body: zlib-wrapped, or raw deflate as some
// servers send it (curl retries with raw inflate on a bad zlib header).
func openDeflate(br *bufio.Reader) (io.Reader, error) {
	head, err := br.Peek(2)
	if err != nil && len(head) == 0 {
		return nil, err
	}

	if isZlibHeader(head) {
		return zlib.NewReader(br)
	}

	return flate.NewReader(br), nil
}

// sourceReader is the body under the decoders; it remembers whether it
// failed, so a decoder's "unexpected EOF" can be told apart from a body
// that was cut off.
type sourceReader struct {
	r   io.Reader
	err error
}

func (s *sourceReader) Read(b []byte) (int, error) {
	n, err := s.r.Read(b)
	if err != nil && !errors.Is(err, io.EOF) {
		s.err = err
	}

	return n, err
}

// lazyDecoder opens its decoder on the first read (curl's writers see the
// body only once it arrives: an empty body decodes to nothing) and maps
// its errors as curl does. The decoder reads through a bufio.Reader, which
// the flate-based ones use as it is (it is an io.ByteReader), so what
// follows the end of a zlib stream stays there to be looked at.
type lazyDecoder struct {
	src  io.Reader
	body *sourceReader
	open func(*bufio.Reader) (io.Reader, error)
	// zlib marks gzip and deflate: zlib's messages, and data after the
	// end of the stream is an error.
	zlib bool

	buf  *bufio.Reader
	dec  io.Reader
	done bool
}

func (d *lazyDecoder) Read(b []byte) (int, error) {
	if d.done {
		return 0, io.EOF
	}

	if d.dec == nil {
		d.buf = bufio.NewReader(d.src)

		dec, err := d.open(d.buf)
		if err != nil {
			return 0, d.fail(err)
		}

		d.dec = dec
	}

	n, err := d.dec.Read(b)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, d.fail(err)
	}

	if errors.Is(err, io.EOF) {
		if d.zlib {
			if err := d.trailing(); err != nil {
				return n, err
			}
		}

		d.done = true
		_ = d.Close()
	}

	return n, err
}

// trailing is curl's check, once zlib reports the end of the stream, that
// the body ends there too: anything after it is CURLE_WRITE_ERROR, a
// second gzip member with its own message.
func (d *lazyDecoder) trailing() error {
	next, _ := d.buf.Peek(2)
	if len(next) == 0 {
		if d.body.err != nil {
			return d.body.err
		}

		return nil
	}

	_ = d.Close()
	d.done = true

	if len(next) == 2 && next[0] == 0x1f && next[1] == 0x8b {
		if _, gz := d.dec.(*gzip.Reader); gz {
			return &encodingError{curleWriteError, "Multi-member gzip response not supported"}
		}
	}

	return &encodingError{curleWriteError, curlWriteError}
}

// Close releases the decoder (zstd's runs goroutines) and those below it.
func (d *lazyDecoder) Close() error {
	if c, ok := d.dec.(interface{ Close() }); ok {
		c.Close()
	}

	if c, ok := d.src.(io.Closer); ok {
		_ = c.Close()
	}

	return nil
}

// fail maps a decoder error: the body's own error as it is, a stream the
// body ended inside as the end of the data, anything else as curl's
// CURLE_BAD_CONTENT_ENCODING.
func (d *lazyDecoder) fail(err error) error {
	_ = d.Close()
	d.done = true

	if d.body.err != nil {
		return d.body.err
	}

	if _, ok := errors.AsType[*encodingError](err); ok {
		return err
	}

	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return io.EOF
	}

	if d.zlib {
		return &encodingError{curleBadContentEncoding, "Error while processing content unencoding: " + zlibMessage(err)}
	}

	return &encodingError{curleBadContentEncoding, curlBadEncoding}
}

// zlibMessage is the zlib error message (z_stream.msg) for a failure of
// Go's decoders: exact for header and checksum errors; zlib names the
// precise defect of corrupt data, which Go's flate does not report.
func zlibMessage(err error) string {
	switch {
	case errors.Is(err, gzip.ErrHeader):
		return "incorrect header check"
	case errors.Is(err, gzip.ErrChecksum), errors.Is(err, zlib.ErrChecksum):
		return "incorrect data check"
	case errors.Is(err, zlib.ErrDictionary):
		return "need dictionary"
	}

	return "invalid block type"
}

// unknownEncoding is curl's error writer for an encoding it does not know.
type unknownEncoding struct{ src io.Reader }

func (u *unknownEncoding) Read([]byte) (int, error) {
	var one [1]byte

	n, err := u.src.Read(one[:])
	if n > 0 {
		return 0, &encodingError{curleBadContentEncoding, "Unrecognized content encoding type"}
	}

	return 0, err
}

// Close closes the decoders below.
func (u *unknownEncoding) Close() error {
	if c, ok := u.src.(io.Closer); ok {
		return c.Close()
	}

	return nil
}
