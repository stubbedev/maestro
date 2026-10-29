//go:build unix

package util

import (
	"os"
	"syscall"
)

// fileOwner is a file's owner and group as PHP's stat() reads them.
func fileOwner(fi os.FileInfo) (uid, gid int, ok bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}

	return int(st.Uid), int(st.Gid), true
}
