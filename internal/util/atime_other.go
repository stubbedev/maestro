//go:build !(darwin || freebsd || netbsd || linux || openbsd || dragonfly || solaris || illumos || windows)

package util

import (
	"os"
	"time"
)

func fileAtime(fi os.FileInfo) time.Time {
	return fi.ModTime()
}
