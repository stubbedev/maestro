//go:build !windows

package command

import (
	"io/fs"
	"syscall"
)

// fileOwner is the uid owning the file st describes.
func fileOwner(st fs.FileInfo) (int, bool) {
	s, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}

	return int(s.Uid), true
}
