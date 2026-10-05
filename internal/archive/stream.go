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

		z.Multistream(s.format == Gzip)
		r = &gzipEnd{z: z, raw: raw, format: s.format}
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

// gzipEnd fails if bytes follow the gzip data it decodes, which gzip and
// PHP's zlib filter treat differently.
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

// streamLimit bounds a decompressed tar: the content limit plus room for a
// header per entry.
func streamLimit(l Limits) int64 {
	return l.MaxTotalSize + int64(l.MaxEntries+16)*1024
}

// readSection hands fn exactly size bytes of the stream, failing if it ends
// first.
func readSection(c *countingReader, e *Entry, size int64, fn func(e *Entry, r io.Reader) error) error {
	r := &exactReader{r: io.LimitReader(c, size), left: size, entry: e.Path, format: c.format}
	return consume(e, r, fn)
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
