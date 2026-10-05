//go:build darwin || freebsd || netbsd

package util

import (
	"os"
	"syscall"
	"time"
)

func fileAtime(fi os.FileInfo) time.Time {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return time.Unix(st.Atimespec.Unix())
	}

	return fi.ModTime()
}
