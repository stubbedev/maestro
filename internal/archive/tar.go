// Reproduces TarDownloader's `(new PharData($file))->extractTo($dir, null,
// true)` from PHP 8.4's ext/phar: phar.c (phar_open_from_fp's format
// detection), tar.c (phar_parse_tarfile, phar_is_tar, phar_tar_number,
// phar_tar_checksum) and phar_object.c (extract_helper, phar_extract_file),
// with main/streams/plain_wrapper.c's recursive mkdir.

package archive

import (
	"bytes"
	"io/fs"
	"os"
	"slices"
	"strings"
)

// Tar header field offsets (tar.h's tar_header).
const (
	tarBlock       = 512
	tarOffMode     = 100
	tarOffSize     = 124
	tarOffChecksum = 148
	tarOffType     = 156
	tarOffLink     = 157
	tarOffMagic    = 257
	tarOffPrefix   = 345
	// sizeof(old_tar_header): the pre-POSIX header phar checksums.
	tarOldHeaderLen = 257
)

// tarFile locates one file's data in the decompressed stream.
type tarFile struct {
	off  int64
	size int64
}

// tarContent streams a planned tar's file data from a fresh pass over the
// decompressed archive.
type tarContent struct {
	s     *stream
	files []tarFile
}

func (t *tarContent) readFiles(a *Archive, fn FileFunc) error {
	c, err := t.s.open()
	if err != nil {
		return err
	}

	for _, i := range a.fileOrder(func(src int) int64 { return t.files[src].off }) {
		e := &a.entries[i]
		f := t.files[e.src]

		if short, err := c.skip(f.off - c.off); err != nil {
			return err
		} else if short {
			return errorf(t.s.format, ErrCorrupt, e.Path, "truncated archive")
		}

		if err := readSection(c, i, e, f.size, fn); err != nil {
			return err
		}
	}

	return nil
}

// pharTar plans PharData::extractTo.
type pharTar struct {
	b     *builder
	c     *countingReader
	names map[string]struct{}
	files []tarFile
}

func planPharTar(f *os.File, size int64, b *builder) (contentReader, error) {
	comp, err := detectCompression(f)
	if err != nil {
		return nil, err
	}

	if comp == compressBzip2 {
		if multi, err := bzip2Concatenated(f, size); err != nil || multi {
			if err == nil {
				err = errorf(Tar, ErrIrreproducible, "", "concatenated bzip2 streams")
			}

			return nil, err
		}
	}

	s := &stream{f: f, size: size, comp: comp, format: Tar, limit: streamLimit(b.limits)}

	c, err := s.open()
	if err != nil {
		return nil, err
	}

	p := &pharTar{b: b, c: c, names: map[string]struct{}{}}
	if err := p.parse(comp); err != nil {
		return nil, err
	}

	return &tarContent{s: s, files: p.files}, nil
}

func (p *pharTar) fail(kind error, entry, reason string, args ...any) *Error {
	return errorf(Tar, kind, entry, reason, args...)
}

// parse follows phar_open_from_fp's detection, then phar_parse_tarfile,
// placing every entry as phar_extract_file would.
func (p *pharTar) parse(comp compression) error {
	var buf [tarBlock]byte

	if short, err := p.c.full(buf[:]); err != nil {
		return err
	} else if short {
		// Neither a tar nor anything else phar could open.
		return p.fail(ErrIrreproducible, "", "not a tar archive (shorter than one header)")
	}

	switch {
	case comp != compressNone && (bytes.HasPrefix(buf[:], []byte{0x1f, 0x8b, 0x08}) || bytes.HasPrefix(buf[:], []byte("BZh"))):
		// phar would decompress again.
		return p.fail(ErrIrreproducible, "", "nested compression")
	case bytes.HasPrefix(buf[:], []byte("PK\x03\x04")):
		return p.fail(ErrIrreproducible, "", "a zip archive in a tar dist")
	case bytes.HasPrefix(buf[:], []byte("<?php")) || !tarChecksumOK(&buf):
		return p.fail(ErrIrreproducible, "", "not a tar archive")
	}

	old := !bytes.HasPrefix(buf[tarOffMagic:], []byte("ustar"))
	longName, longKey, haveLong := "", "", false

	for {
		hdr := &buf
		pos := p.c.off
		sum1 := tarNumber(hdr[tarOffChecksum : tarOffChecksum+8])

		if sum1 == 0 && tarSum(hdr[:]) == 0 {
			return nil
		}

		copy(hdr[tarOffChecksum:tarOffChecksum+8], "        ")

		sum2 := tarSum(hdr[:tarBlock])
		if old {
			sum2 = tarSum(hdr[:tarOldHeaderLen])
			if sum2 != sum1 && tarSum(hdr[:]) == sum1 {
				// A ustar header without the magic.
				sum2, old = sum1, false
			}
		}

		typ := hdr[tarOffType]

		size, err := p.size(hdr)
		if err != nil {
			return err
		}

		skip := int64(0)

		switch {
		case !old && (typ == 'g' || typ == 'x'):
			// pax headers are skipped, their records ignored.
			skip = tarPad(size)
		case !haveLong && typ == 'L':
			name, key, err := p.longName(size)
			if err != nil {
				return err
			}

			longName, longKey, haveLong = name, key, true

			if short, err := p.c.full(buf[:]); err != nil {
				return err
			} else if short {
				return p.fail(ErrCorrupt, "", "is a corrupted tar file (truncated)")
			}

			continue
		default:
			name, key := longName, longKey
			if !haveLong {
				name = tarHeaderName(hdr, old)
				key = name
			}

			haveLong = false

			if sum1 != sum2 {
				return p.fail(ErrCorrupt, name, "is a corrupted tar file (checksum mismatch of file \"%s\")", name)
			}

			if err := p.entry(hdr, key, name, typ, old, pos, size); err != nil {
				return err
			}

			if typ == 0 || typ == '0' {
				skip = tarPad(size)
			}
		}

		if skip > 0 {
			if short, err := p.c.skip(skip); err != nil {
				return err
			} else if short {
				return p.fail(ErrCorrupt, "", "is a corrupted tar file (truncated)")
			}
		}

		if end, err := p.c.atEnd(); err != nil || end {
			return err
		}

		if short, err := p.c.full(buf[:]); err != nil {
			return err
		} else if short {
			return p.fail(ErrCorrupt, "", "is a corrupted tar file (truncated)")
		}
	}
}

// size reads the header's size field as phar does, refusing sizes phar's
// 32-bit arithmetic would wrap.
func (p *pharTar) size(hdr *[tarBlock]byte) (int64, error) {
	field := hdr[tarOffSize : tarOffSize+12]
	if field[0]&0x80 != 0 {
		return 0, p.fail(ErrIrreproducible, "", "base-256 size field")
	}

	size := tarNumber64(field)
	if size >= 1<<32-tarBlock {
		return 0, p.fail(ErrLimit, "", "entry of %d bytes", size)
	}

	return size, nil
}

// longName reads a ././@LongLink name: the bytes up to the first NUL, which
// is all phar's C-string handling ever sees of it, and its manifest key.
func (p *pharTar) longName(size int64) (name, key string, err error) {
	if size == 0 || size > maxPath+1 {
		return "", "", p.fail(ErrIrreproducible, "", "long name of %d bytes", size)
	}

	data := make([]byte, tarPad(size))
	if short, err := p.c.full(data); err != nil {
		return "", "", err
	} else if short {
		return "", "", p.fail(ErrCorrupt, "", "is a corrupted tar file (truncated)")
	}

	raw, rest, _ := bytes.Cut(data[:size], []byte{0})
	if len(bytes.Trim(rest, "\x00")) != 0 {
		return "", "", p.fail(ErrIrreproducible, string(raw), "long name with bytes after its NUL")
	}

	// The manifest key keeps every byte, NULs included.
	return string(raw), string(data[:size]), nil
}

// tarHeaderName is the name phar builds from a header: prefix "/" name for
// ustar headers with a prefix, the name field otherwise, without one
// trailing slash.
func tarHeaderName(hdr *[tarBlock]byte, old bool) string {
	name := cString(hdr[:100])
	if !old && hdr[tarOffPrefix] != 0 {
		name = cString(hdr[tarOffPrefix:tarOffPrefix+155]) + "/" + name
	}

	return strings.TrimSuffix(name, "/")
}

// entry adds one manifest entry, whose manifest key is key, and places it
// as phar_extract_file would.
func (p *pharTar) entry(hdr *[tarBlock]byte, key, name string, typ byte, old bool, pos, size int64) error {
	mode := tarNumber(hdr[tarOffMode : tarOffMode+8])
	isDir := typ == '5' || (old && typ == 0 && mode&0o170000 == 0o040000)

	switch typ {
	case 0, '0', '5':
	case '1':
		// A hard link must name an earlier entry, or phar refuses the archive.
		link := cString(hdr[tarOffLink : tarOffLink+100])
		if _, ok := p.names[link]; !ok {
			return p.fail(ErrCorrupt, name, "is a corrupted tar file - hard link to non-existent file \"%s\"", link)
		}
	case '2':
	default:
		return p.fail(ErrIrreproducible, name, "unsupported tar entry type %q", typ)
	}

	if size > 0 && typ != 0 && typ != '0' {
		// phar would not skip the data and read it as the next header.
		return p.fail(ErrIrreproducible, name, "entry of type %q with data", typ)
	}

	if strings.HasPrefix(name, ".phar") && (len(name) == 5 || name[5] == '/' || name[5] == '\\') {
		return p.fail(ErrIrreproducible, name, "phar metadata entry")
	}

	if _, dup := p.names[key]; dup {
		// phar would keep the first entry's place with the last one's data.
		return p.fail(ErrIrreproducible, name, "duplicate entry")
	}

	p.names[key] = struct{}{}

	path, ok := virtualPath(name)
	if !ok {
		return p.fail(ErrIrreproducible, name, "Cannot extract \"%s\", internal error", name)
	}

	perm := fs.FileMode(mode) & fs.ModePerm

	// The directory holding the entry is created when missing: with the
	// entry's own mode for a directory entry, 0777 otherwise.
	dirMode := fs.FileMode(0o777)
	if isDir {
		dirMode = perm
	}

	dir, err := p.ensureDir(Parent(path), dirMode, name)
	if err != nil || isDir {
		// A directory entry never creates the directory itself.
		return err
	}

	switch n := p.b.get(path); {
	case dir.Kind != Dir || dir.Mode&0o300 != 0o300:
		return p.fail(ErrIrreproducible, name, "Cannot extract \"%s\", could not open for writing", name)
	case n != nil:
		// Another name for the same path (or a directory there).
		return p.fail(ErrIrreproducible, name, "duplicate entry")
	}

	if typ == '1' || typ == '2' {
		// Links become empty files carrying the link's own mode.
		size = 0
	}

	err = p.b.add(Entry{Path: path, Kind: File, Mode: perm, Size: size, src: len(p.files)})
	p.files = append(p.files, tarFile{off: pos, size: size})

	return err
}

// ensureDir returns what is at dir, first creating it and its missing
// parents with mode (under the umask) when nothing is, as PHP's recursive
// mkdir does, and refusing what would fail.
func (p *pharTar) ensureDir(dir string, mode fs.FileMode, name string) (*Entry, error) {
	if n := p.b.get(dir); n != nil {
		return n, nil
	}

	var missing []string

	at := dir
	for p.b.get(at) == nil {
		missing = append(missing, at)
		at = Parent(at)
	}

	if top := p.b.get(at); top.Kind != Dir || top.Mode&0o300 != 0o300 || (len(missing) > 1 && mode&0o300 != 0o300) {
		return nil, p.fail(ErrIrreproducible, name, "Cannot extract \"%s\", could not create directory \"%s\"", name, dir)
	}

	for _, path := range slices.Backward(missing) {
		if err := p.b.add(Entry{Path: path, Kind: Dir, Mode: mode, Umask: true}); err != nil {
			return nil, err
		}
	}

	return p.b.get(dir), nil
}

// virtualPath is virtual_file_ex(CWD_EXPAND) against "/": empty and "."
// components vanish and ".." climbs, never above the root. ok is false when
// nothing remains.
func virtualPath(name string) (string, bool) {
	parts := make([]string, 0, strings.Count(name, "/")+1)

	for c := range strings.SplitSeq(name, "/") {
		switch c {
		case "", ".":
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		default:
			parts = append(parts, c)
		}
	}

	if len(parts) == 0 {
		return "", false
	}

	return strings.Join(parts, "/"), true
}

// tarChecksumOK is phar_is_tar's test: the stored checksum equals the sum
// of the header with its checksum field blanked.
func tarChecksumOK(hdr *[tarBlock]byte) bool {
	h := *hdr
	copy(h[tarOffChecksum:tarOffChecksum+8], "        ")

	return tarNumber(hdr[tarOffChecksum:tarOffChecksum+8]) == tarSum(h[:])
}

// tarNumber is phar_tar_number: leading spaces, then octal digits, in 32
// bits.
func tarNumber(b []byte) uint32 {
	return uint32(tarNumber64(b)) //nolint:gosec // phar's uint32 arithmetic wraps the same way.
}

func tarNumber64(b []byte) int64 {
	i := 0
	for i < len(b) && b[i] == ' ' {
		i++
	}

	var n int64
	for ; i < len(b) && b[i] >= '0' && b[i] <= '7'; i++ {
		n = n<<3 | int64(b[i]-'0')
	}

	return n
}

// tarSum is phar_tar_checksum: the unsigned sum of the bytes.
func tarSum(b []byte) uint32 {
	var s uint32
	for _, c := range b {
		s += uint32(c)
	}

	return s
}

func tarPad(n int64) int64 {
	return (n + tarBlock - 1) &^ (tarBlock - 1)
}

func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}

	return string(b)
}
