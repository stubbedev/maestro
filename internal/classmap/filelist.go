// Ports src/FileList.php.

package classmap

// FileList contains a list of files which were scanned to generate a
// classmap.
type FileList struct {
	files map[string]struct{}
}

// NewFileList returns an empty FileList.
func NewFileList() *FileList {
	return &FileList{files: map[string]struct{}{}}
}

func (l *FileList) Add(path string) {
	l.files[path] = struct{}{}
}

func (l *FileList) Contains(path string) bool {
	_, ok := l.files[path]

	return ok
}
