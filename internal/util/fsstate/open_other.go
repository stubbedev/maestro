//go:build !unix

package fsstate

import (
	"io/fs"
	"os"
)

// OpenFile is os.OpenFile: only unix systems have the extra cost its
// unix version avoids.
func OpenFile(path string, flag int, perm fs.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag, perm)
}
