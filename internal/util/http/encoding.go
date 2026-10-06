// Content decoding as libcurl does it for CURLOPT_ENCODING ""
// (lib/content_encoding.c), with the encodings the libcurl PHP links in
// the reference environment supports (built with zlib, brotli and zstd).
// The behaviour on bad, truncated and over-long bodies was checked against
// php-curl with libcurl 8.22 (TestDecodingReader_CurlParity).

package http

import (
	"bufio"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/dsnet/compress/brotli"
	"github.com/klauspost/compress/zstd"
)

// curlAcceptEncoding is the Accept-Encoding header PHP's libcurl sends for
// the CURLOPT_ENCODING CurlDownloader sets: "" makes curl list every
// decoder it was built with, in its order (Curl_get_content_encodings,
// "deflate, gzip, br, zstd" with zlib, brotli and zstd); with curl 8.7.0
// and 8.7.1 Composer asks for "gzip" alone (composer/composer#11913).
func curlAcceptEncoding(info CurlInfo) string {
	if info.Features&curlVersionLibz != 0 && (info.Version == "8.7.0" || info.Version == "8.7.1") {
		return "gzip"
	}

	var names []string

	if info.Features&curlVersionLibz != 0 {
		names = append(names, "deflate", "gzip")
	}

	if info.Features&curlVersionBrotli != 0 {
		names = append(names, "br")
	}

	if info.Features&curlVersionZstd != 0 {
		names = append(names, "zstd")
	}

	return strings.Join(names, ", ")
}

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
	return decodingReaderFor(src, header, curlInfo().Features)
}

// decodingReaderFor is decodingReader with the decoders a libcurl with
// these features has (an encoding it lacks is an unknown one to it).
func decodingReaderFor(src io.Reader, header string, features int64) io.Reader {
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

	for _, name := range slices.Backward(names) {
		switch {
		case (name == "gzip" || name == "x-gzip") && features&curlVersionLibz != 0:
			// inflateInit2(z, MAX_WBITS + 32): gzip or zlib
			r = &lazyDecoder{src: r, body: body, open: openInflater(wrapAuto)}
		case name == "deflate" && features&curlVersionLibz != 0:
			// inflateInit(z), then raw deflate on a data error at the start
			r = &lazyDecoder{src: r, body: body, open: openInflater(wrapZlib)}
		case name == "br" && features&curlVersionBrotli != 0:
			r = &lazyDecoder{src: r, body: body, open: func(r *bufio.Reader) (io.Reader, error) {
				return brotli.NewReader(r, nil)
			}}
		case name == "zstd" && features&curlVersionZstd != 0:
			r = &lazyDecoder{src: r, body: body, open: func(r *bufio.Reader) (io.Reader, error) {
				return zstd.NewReader(r, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxWindow(zstdMaxWindow), zstd.WithDecoderLowmem(true))
			}}
		default:
			r = &unknownEncoding{src: r}
		}
	}

	return r
}

// openInflater opens a gzip or deflate body (inflate.go).
func openInflater(wrap zlibWrap) func(*bufio.Reader) (io.Reader, error) {
	return func(br *bufio.Reader) (io.Reader, error) {
		return newInflater(br, wrap), nil
	}
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
// its errors as curl does.
type lazyDecoder struct {
	src  io.Reader
	body *sourceReader
	open func(*bufio.Reader) (io.Reader, error)

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
		d.done = true
		_ = d.Close()
	}

	return n, err
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

	if zerr, ok := errors.AsType[*zlibError](err); ok {
		// process_zlib_error
		return &encodingError{curleBadContentEncoding, "Error while processing content unencoding: " + zerr.Error()}
	}

	return &encodingError{curleBadContentEncoding, curlBadEncoding}
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
