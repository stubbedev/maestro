package fsstate

import (
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"
)

// Stamp is how a file looked when it was stat()ed: its ID where files
// have them (Known), else its description (os.SameFile's identity, size,
// mode and modification time). The zero Stamp is no file.
type Stamp struct {
	ok   bool
	id   ID          // where Known
	info fs.FileInfo // elsewhere
}

// StatStamp stamps the file at path, symlinks followed; false when it
// cannot be stat()ed.
func StatStamp(path string) (Stamp, bool) {
	if Known() {
		id, ok := Stat(path)

		return Stamp{ok: ok, id: id}, ok
	}
	info, err := os.Stat(path)
	if err != nil {
		return Stamp{}, false
	}

	return Stamp{ok: true, info: info}, true
}

// ID is the ID s holds; false where files have none (Known) or for the
// zero Stamp.
func (s Stamp) ID() (ID, bool) {
	if !s.ok || !Known() {
		return ID{}, false
	}

	return s.id, true
}

// IsRegular reports whether s is a regular file.
func (s Stamp) IsRegular() bool {
	if !s.ok {
		return false
	}
	if Known() {
		return s.id.IsRegular()
	}

	return s.info.Mode().IsRegular()
}

// Same reports whether a and b stamp the same file, unchanged in between.
func (a Stamp) Same(b Stamp) bool {
	if !a.ok || !b.ok {
		return false
	}
	if Known() {
		return a.id == b.id
	}

	return os.SameFile(a.info, b.info) && a.info.Size() == b.info.Size() &&
		a.info.Mode() == b.info.Mode() && a.info.ModTime().Equal(b.info.ModTime())
}

// Unchanged reports whether the file at path is the one s stamped,
// unchanged since.
func (s Stamp) Unchanged(path string) bool {
	now, ok := StatStamp(path)

	return ok && s.Same(now)
}

// Snapshot is a regular file's contents and the stamp they were read
// under. The zero Snapshot is a file that could not be read as it was at
// one moment.
type Snapshot struct {
	path    string
	content []byte
	stamp   Stamp
}

// ReadStable reads the regular file at path: stat, read, stat again, and
// keeps the contents only if the file did not change in between. ok is
// false otherwise, or when it is not a regular file or cannot be read.
func ReadStable(path string) (s Snapshot, ok bool) {
	content, stamp, _, ok := readStable(path, os.ReadFile)
	if !ok {
		return Snapshot{}, false
	}

	return Snapshot{path: path, content: content, stamp: stamp}, true
}

// ReadStableString is ReadStable with the contents read straight into a
// string, and seen, a moment before the file was first looked at: what
// the stamp says of the contents can be trusted later when its ID is
// Trusted at seen.
func ReadStableString(path string) (content string, stamp Stamp, seen time.Time, ok bool) {
	return readStable(path, readString)
}

// readStable is stat, read, stat again: the contents read only if the file
// is a regular file that did not change in between.
func readStable[T any](path string, read func(string) (T, error)) (content T, stamp Stamp, seen time.Time, ok bool) {
	seen = time.Now()
	before, ok := StatStamp(path)
	if !ok || !before.IsRegular() {
		return content, Stamp{}, time.Time{}, false
	}
	content, err := read(path)
	if err != nil {
		return content, Stamp{}, time.Time{}, false
	}
	after, ok := StatStamp(path)
	if !ok || !before.Same(after) {
		var zero T

		return zero, Stamp{}, time.Time{}, false
	}

	return content, after, seen, true
}

// readString is os.ReadFile as a string, without copying the bytes read.
func readString(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	var b strings.Builder
	if info, err := f.Stat(); err == nil && info.Size() > 0 {
		b.Grow(int(info.Size()))
	}
	buf, _ := readBuffers.Get().(*[]byte)
	defer readBuffers.Put(buf)
	for {
		n, err := f.Read(*buf)
		b.Write((*buf)[:n])
		if err == io.EOF {
			return b.String(), nil
		} else if err != nil {
			return "", err
		}
	}
}

// readBuffers are readString's buffers, reused.
var readBuffers = sync.Pool{New: func() any {
	buf := make([]byte, 64<<10)

	return &buf
}}

// Content is the file's contents as they were read.
func (s Snapshot) Content() []byte { return s.content }

// Stamp is the stamp the contents were read under.
func (s Snapshot) Stamp() Stamp { return s.stamp }

// Holds reports whether the file holds content, as it was read, and did
// not change since.
func (s Snapshot) Holds(content string) bool {
	return s.stamp.ok && string(s.content) == content && s.stamp.Unchanged(s.path)
}
