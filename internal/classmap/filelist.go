// Ports src/FileList.php.

package classmap

// FileList contains a list of files which were scanned to generate a
// classmap. The zero value is an empty list.
type FileList struct {
	files map[string]struct{}
}

// Add records a scanned file (by its realpath).
func (l *FileList) Add(path string) {
	if l.files == nil {
		l.files = make(map[string]struct{})
	}
	l.files[path] = struct{}{}
}

// Contains reports whether the file was scanned.
func (l *FileList) Contains(path string) bool {
	_, ok := l.files[path]

	return ok
}

// Len returns the number of scanned files.
func (l *FileList) Len() int { return len(l.files) }
