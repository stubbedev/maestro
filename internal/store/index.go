package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/stubbedev/maestro/internal/archive"
)

// Entry is one node of a stored package tree: the extracted entry and,
// for a file, the SHA-256 of its content.
type Entry struct {
	archive.Entry
	Hash [32]byte
}

// ModTime is the modification time, in whole Unix seconds with
// nanoseconds zero, of a file imported from the entry while it holds the
// entry's content: its object's stamp, which hardlinks share and clones
// and copies are given. Any write to the file moves it, so a file whose
// size and modification time match is the entry's content as far as the
// store can tell (see "Hard-linked package files" for the one way an edit
// escapes that).
func (e *Entry) ModTime() int64 {
	return stampTime(&e.Hash)
}

// Release is an extracted dist as the store records it: every entry of its
// package tree, the root first and each entry after its parent directory.
type Release struct {
	entries []Entry
	id      [32]byte
}

// Entries is the release's package tree. It must not be modified.
func (r *Release) Entries() []Entry {
	return r.entries
}

// index file layout, all integers unsigned varints:
//
//	magic "maestro-index-1\n"
//	count
//	count × entry: kind byte, mode (permission bits | 1<<9 when the umask
//	    applies), path length, path, then for a file: size, 32-byte SHA-256;
//	    for a symlink: target length, target
//	SHA-256 of everything before it
const indexMagic = "maestro-index-1\n"

var errCorruptIndex = errors.New("corrupt store index")

func encodeIndex(entries []Entry) []byte {
	size := len(indexMagic) + binary.MaxVarintLen64 + sha256.Size
	for i := range entries {
		size += 1 + 3*binary.MaxVarintLen64 + len(entries[i].Path) + len(entries[i].Link) + 32
	}

	b := make([]byte, 0, size)
	b = append(b, indexMagic...)
	b = binary.AppendUvarint(b, uint64(len(entries)))

	for i := range entries {
		e := &entries[i]
		mode := uint64(e.Mode & fs.ModePerm)

		if e.Umask {
			mode |= 1 << 9
		}

		b = append(b, kindByte(e.Kind))
		b = binary.AppendUvarint(b, mode)
		b = binary.AppendUvarint(b, uint64(len(e.Path)))
		b = append(b, e.Path...)

		switch e.Kind {
		case archive.File:
			b = binary.AppendUvarint(b, uint64(e.Size)) //nolint:gosec // sizes are never negative.
			b = append(b, e.Hash[:]...)
		case archive.Symlink:
			b = binary.AppendUvarint(b, uint64(len(e.Link)))
			b = append(b, e.Link...)
		case archive.Dir:
		}
	}

	sum := sha256.Sum256(b)

	return append(b, sum[:]...)
}

func kindByte(k archive.Kind) byte {
	switch k {
	case archive.Dir:
		return 'd'
	case archive.Symlink:
		return 'l'
	case archive.File:
	}

	return 'f'
}

// decodeIndex parses an index and checks every invariant materializing
// relies on, so that even a tampered index cannot make it write outside
// the package directory: the root comes first, every path is clean and
// relative, each entry's parent is a directory listed before it, and no
// path repeats.
func decodeIndex(data []byte) ([]Entry, error) {
	if len(data) < len(indexMagic)+sha256.Size || !bytes.HasPrefix(data, []byte(indexMagic)) {
		return nil, errCorruptIndex
	}

	body := data[:len(data)-sha256.Size]
	if sum := sha256.Sum256(body); !bytes.Equal(sum[:], data[len(body):]) {
		return nil, fmt.Errorf("%w: checksum mismatch", errCorruptIndex)
	}

	// One string holds every path and target, which entries share.
	d := decoder{b: body[len(indexMagic):], s: string(body[len(indexMagic):])}

	count := d.uvarint()
	if d.err != nil || count == 0 || count > uint64(len(d.b)) {
		return nil, fmt.Errorf("%w: bad entry count", errCorruptIndex)
	}

	entries := make([]Entry, count)
	seen := make(map[string]bool, count) // path -> is a directory

	for i := range entries {
		e := &entries[i]
		kind := d.byte()
		mode := d.uvarint()
		e.Path = d.string()

		switch kind {
		case 'd':
			e.Kind = archive.Dir
		case 'f':
			e.Kind = archive.File
			size := d.uvarint()
			e.Size = int64(size) //nolint:gosec // checked below.

			if size > 1<<62 {
				d.fail()
			}

			copy(e.Hash[:], d.bytes(32))
		case 'l':
			e.Kind = archive.Symlink
			e.Link = d.string()

			if e.Link == "" || strings.IndexByte(e.Link, 0) >= 0 {
				d.fail()
			}
		default:
			d.fail()
		}

		if mode > 0o1777 {
			d.fail()
		}

		e.Mode, e.Umask = fs.FileMode(mode&0o777), mode&(1<<9) != 0

		if d.err != nil {
			return nil, fmt.Errorf("%w: truncated entry %d", errCorruptIndex, i)
		}

		if err := admit(e, i == 0, seen); err != nil {
			return nil, fmt.Errorf("%w: %w", errCorruptIndex, err)
		}
	}

	if len(d.b) != 0 {
		return nil, fmt.Errorf("%w: trailing bytes", errCorruptIndex)
	}

	return entries, nil
}

// admit checks one entry against those before it (seen: path -> whether
// it is a directory).
func admit(e *Entry, first bool, seen map[string]bool) error {
	if first {
		if e.Path != "" || e.Kind != archive.Dir {
			return errors.New("the first entry is not the package directory")
		}

		seen[""] = true

		return nil
	}

	if !archive.ValidPath(e.Path) {
		return fmt.Errorf("invalid path %q", e.Path)
	}

	if !seen[archive.Parent(e.Path)] {
		return fmt.Errorf("%q comes before its directory", e.Path)
	}

	if _, dup := seen[e.Path]; dup {
		return fmt.Errorf("%q is listed twice", e.Path)
	}

	seen[e.Path] = e.Kind == archive.Dir

	return nil
}

// decoder reads an index body, remembering the first failure. s is the
// body as a string, which strings are cut from.
type decoder struct {
	err error
	b   []byte
	s   string
}

func (d *decoder) fail() {
	if d.err == nil {
		d.err = errCorruptIndex
	}

	d.b = nil
}

func (d *decoder) byte() byte {
	if len(d.b) == 0 {
		d.fail()
		return 0
	}

	c := d.b[0]
	d.b = d.b[1:]

	return c
}

func (d *decoder) uvarint() uint64 {
	v, n := binary.Uvarint(d.b)
	if n <= 0 {
		d.fail()
		return 0
	}

	d.b = d.b[n:]

	return v
}

func (d *decoder) bytes(n uint64) []byte {
	if n > uint64(len(d.b)) {
		d.fail()
		return nil
	}

	b := d.b[:n]
	d.b = d.b[n:]

	return b
}

func (d *decoder) string() string {
	n := d.uvarint()
	if n > uint64(len(d.b)) {
		d.fail()
		return ""
	}

	start := len(d.s) - len(d.b)
	d.b = d.b[n:]

	return d.s[start : len(d.s)-len(d.b)]
}
