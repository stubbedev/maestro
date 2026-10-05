//go:build unix

package util

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// unlinkPath is unlink(2), which unlike os.Remove never removes a directory.
func unlinkPath(path string) error {
	return unix.Unlink(path)
}

func rmdirPath(path string) error {
	return unix.Rmdir(path)
}

// isReadableAccess is PHP's is_readable: access(2) with R_OK.
func isReadableAccess(path string) bool {
	return unix.Access(path, unix.R_OK) == nil
}

// isExecutable is PHP's is_executable: access(2) with X_OK.
func isExecutable(path string) bool {
	return unix.Access(path, unix.X_OK) == nil
}

func chownLike(path string, fi os.FileInfo) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		_ = os.Lchown(path, int(st.Uid), int(st.Gid))
	}
}

// Junction ports Filesystem::junction, which only exists on Windows.
func (fs *Filesystem) Junction(_, _ string) error {
	return errJunctionUnsupported
}

// IsJunction ports Filesystem::isJunction: always false off Windows.
func IsJunction(string) bool {
	return false
}

// RemoveJunction ports Filesystem::removeJunction: always false off Windows.
func (fs *Filesystem) RemoveJunction(string) (bool, error) {
	return false, nil
}
