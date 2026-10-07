package fsstate

import (
	"io/fs"
	"os"
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
	before, ok := StatStamp(path)
	if !ok || !before.IsRegular() {
		return Snapshot{}, false
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, false
	}
	after, ok := StatStamp(path)
	if !ok || !before.Same(after) {
		return Snapshot{}, false
	}

	return Snapshot{path: path, content: content, stamp: after}, true
}

// Content is the file's contents as they were read.
func (s Snapshot) Content() []byte { return s.content }

// Stamp is the stamp the contents were read under.
func (s Snapshot) Stamp() Stamp { return s.stamp }

// Holds reports whether the file holds content, as it was read, and did
// not change since.
func (s Snapshot) Holds(content string) bool {
	return s.stamp.ok && string(s.content) == content && s.stamp.Unchanged(s.path)
}
