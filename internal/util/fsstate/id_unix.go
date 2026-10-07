//go:build unix

package fsstate

import (
	"os"

	"golang.org/x/sys/unix"
)

// Known reports whether files have IDs here.
func Known() bool { return true }

// Stat is the ID of the file at path, symlinks followed; false when it
// cannot be read.
func Stat(path string) (ID, bool) {
	var st unix.Stat_t
	if unix.Stat(path, &st) != nil {
		return ID{}, false
	}

	return idOf(&st), true
}

// Lstat is the ID of the file at path itself, a symlink's own.
func Lstat(path string) (ID, bool) {
	var st unix.Stat_t
	if unix.Lstat(path, &st) != nil {
		return ID{}, false
	}

	return idOf(&st), true
}

// Fstat is the ID of an open file.
func Fstat(f *os.File) (ID, bool) {
	var st unix.Stat_t
	if unix.Fstat(int(f.Fd()), &st) != nil {
		return ID{}, false
	}

	return idOf(&st), true
}

func idOf(st *unix.Stat_t) ID {
	return ID{
		Dev: devOf(st), Ino: uint64(st.Ino), //nolint:unconvert // uint32 on some systems
		Mode: uint32(st.Mode), //nolint:unconvert // uint16 on some systems
		Size: st.Size, Mtime: st.Mtim.Nano(), Ctime: st.Ctim.Nano(),
	}
}
