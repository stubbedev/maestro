//go:build linux || darwin

package classmap

import (
	"os"

	"golang.org/x/sys/unix"
)

// statKey is the fileKey of the file at path (symlinks followed).
func statKey(path string) (fileKey, bool) {
	var st unix.Stat_t
	if unix.Stat(path, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG {
		return fileKey{}, false
	}

	return keyOf(&st), true
}

// fstatKey is the fileKey of an open file.
func fstatKey(f *os.File) (fileKey, bool) {
	var st unix.Stat_t
	if unix.Fstat(int(f.Fd()), &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG {
		return fileKey{}, false
	}

	return keyOf(&st), true
}
