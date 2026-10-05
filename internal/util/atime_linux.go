//go:build linux || openbsd || dragonfly || solaris || illumos

package util

import (
	"os"
	"syscall"
	"time"
)

func fileAtime(fi os.FileInfo) time.Time {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return time.Unix(st.Atim.Unix())
	}

	return fi.ModTime()
}
