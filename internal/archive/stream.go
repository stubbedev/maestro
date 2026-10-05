package archive

import (
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"io"
	"os"

	"github.com/ulikunitz/xz"
)

// compression is how a tar (or a gzip dist) is wrapped.
type compression uint8

const (
	compressNone compression = iota
	compressGzip
	compressBzip2
	compressXz
)

// stream reopens an archive's decompressed byte stream from the start, once
// for planning and once more for reading content.
type stream struct {
	f      *os.File
	size   int64
	comp   compression
	format Format
	// limit bounds the decompressed stream, so a compression bomb cannot
	// make planning run forever.
	limit int64
}

// open returns the decompressed stream, positioned at its start.
func (s *stream) open() (*countingReader, error) {
	raw := bufio.NewReaderSize(io.NewSectionReader(s.f, 0, s.size), 64<<10)

	var r io.Reader

	switch s.comp {
	case compressNone:
		r = raw
	case compressGzip:
		z, err := gzip.NewReader(raw)
		if err != nil {
			return nil, errorf(s.format, ErrCorrupt, "", "invalid gzip data: %v", err)
		}

		if s.format == Gzip {
			// gzip -cd decompresses every member and fails on anything
			// else after them.
			r = &gzipEnd{z: z, raw: raw, format: s.format}
		} else {
			// PHP's zlib.inflate filter stops at the end of the first
			// member and ignores what follows.
			z.Multistream(false)
			r = &gzipData{z: z, format: s.format}
		}
	case compressBzip2:
		r = bzip2.NewReader(raw)
	case compressXz:
		z, err := xz.NewReader(raw)
		if err != nil {
			return nil, errorf(s.format, ErrCorrupt, "", "invalid xz data: %v", err)
		}

		r = z
	}

	return &countingReader{r: bufio.NewReaderSize(r, 64<<10), limit: s.limit, format: s.format}, nil
}

// gzipData reports invalid gzip data as ErrCorrupt.
type gzipData struct {
	z      *gzip.Reader
	format Format
}

func (g *gzipData) Read(p []byte) (int, error) {
	n, err := g.z.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, errorf(g.format, ErrCorrupt, "", "invalid gzip data: %v", err)
	}

	return n, err
}

// gzipEnd fails if bytes that are not gzip members follow the data it
// decodes, which gzip and PHP's gzread treat differently.
type gzipEnd struct {
	z      *gzip.Reader
	raw    *bufio.Reader
	format Format
}

func (g *gzipEnd) Read(p []byte) (int, error) {
	n, err := g.z.Read(p)
	if errors.Is(err, io.EOF) {
		if _, perr := g.raw.Peek(1); perr == nil {
			return n, errorf(g.format, ErrIrreproducible, "", "trailing data after the gzip stream")
		}

		return n, io.EOF
	}

	if err != nil {
		return n, errorf(g.format, ErrCorrupt, "", "invalid gzip data: %v", err)
	}

	return n, nil
}

// countingReader tracks the offset into the decompressed stream and bounds
// it.
type countingReader struct {
	r      *bufio.Reader
	off    int64
	limit  int64
	format Format
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.off += int64(n)

	if c.off > c.limit {
		return n, errorf(c.format, ErrLimit, "", "decompresses to more than %d bytes", c.limit)
	}

	if err != nil && !errors.Is(err, io.EOF) {
		if _, ok := errors.AsType[*Error](err); !ok {
			err = errorf(c.format, ErrCorrupt, "", "invalid compressed data: %v", err)
		}
	}

	return n, err
}

// full reads exactly len(p) bytes; short reports reaching the end first.
func (c *countingReader) full(p []byte) (short bool, err error) {
	n, err := io.ReadFull(c, p)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true, nil
	}

	if err != nil {
		return false, err
	}

	return n < len(p), nil
}

// skip advances n bytes; short reports reaching the end first.
func (c *countingReader) skip(n int64) (short bool, err error) {
	got, err := io.CopyN(io.Discard, c, n)
	if errors.Is(err, io.EOF) {
		return true, nil
	}

	return got < n, err
}

// atEnd reports whether the stream has no more bytes.
func (c *countingReader) atEnd() (bool, error) {
	_, err := c.r.Peek(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}

	return false, err
}

// detectCompression sniffs the magic bytes PHP's phar looks for.
func detectCompression(f *os.File) (compression, error) {
	var magic [3]byte

	n, err := f.ReadAt(magic[:], 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}

	switch {
	case bytes.Equal(magic[:n], []byte{0x1f, 0x8b, 0x08}):
		return compressGzip, nil
	case bytes.Equal(magic[:n], []byte("BZh")):
		return compressBzip2, nil
	}

	return compressNone, nil
}

// bzip2Concatenated reports whether another bzip2 stream starts after the
// first one: the byte-aligned header of a stream ("BZh" and a block size
// digit) directly followed by a block or end-of-stream magic number, at any
// offset past the start. PHP's bzip2.decompress filter stops after the
// first stream, where Go's reader goes on.
func bzip2Concatenated(f *os.File, size int64) (bool, error) {
	const window = 1 << 20

	buf := make([]byte, window+9)
	carry := 0

	for off := int64(0); off < size; off += window {
		n, err := f.ReadAt(buf[carry:carry+window], off)
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}

		data := buf[:carry+n]

		for i := bytes.Index(data, []byte("BZh")); i >= 0; {
			at := off - int64(carry) + int64(i)
			if at > 0 && i+10 <= len(data) && data[i+3] >= '1' && data[i+3] <= '9' &&
				(bytes.Equal(data[i+4:i+10], []byte("1AY&SY")) || bytes.Equal(data[i+4:i+10], []byte("\x17rE8P\x90"))) {
				return true, nil
			}

			j := bytes.Index(data[i+1:], []byte("BZh"))
			if j < 0 {
				break
			}

			i += 1 + j
		}

		carry = min(9, len(data))
		copy(buf, data[len(data)-carry:])
	}

	return false, nil
}

// streamLimit bounds a decompressed tar: the content limit plus room for a
// header per entry.
func streamLimit(l Limits) int64 {
	return l.MaxTotalSize + int64(l.MaxEntries+16)*1024
}

// readSection hands fn exactly size bytes of the stream, failing if it ends
// first.
func readSection(c *countingReader, i int, e *Entry, size int64, fn FileFunc) error {
	r := &exactReader{r: io.LimitReader(c, size), left: size, entry: e.Path, format: c.format}
	return consume(i, r, fn)
}

// exactReader fails if the stream ends before left bytes were read.
type exactReader struct {
	r      io.Reader
	entry  string
	left   int64
	format Format
}

func (x *exactReader) Read(p []byte) (int, error) {
	n, err := x.r.Read(p)
	x.left -= int64(n)

	if errors.Is(err, io.EOF) && x.left > 0 {
		return n, errorf(x.format, ErrCorrupt, x.entry, "truncated archive")
	}

	return n, err
}
