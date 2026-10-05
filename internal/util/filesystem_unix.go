//go:build unix

package util

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// dirSeparators are '/' and DIRECTORY_SEPARATOR.
const dirSeparators = "/"

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

// chownLike gives path the owner of fi, as PHP's cross-device rename()
// does; only failures other than EPERM count.
func chownLike(path string, fi os.FileInfo) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}

	if err := os.Chown(path, int(st.Uid), int(st.Gid)); err != nil && !errors.Is(err, syscall.EPERM) {
		return err
	}

	return nil
}

// errJunctionUnsupported is the LogicException Filesystem::junction throws
// off Windows.
var errJunctionUnsupported = &LogicError{Message: `Function Composer\Util\Filesystem is not available on non-Windows platform`}

// Junction ports Filesystem::junction, which only exists on Windows.
func (fs *Filesystem) Junction(_, _ string) error {
	return errJunctionUnsupported
}

// IsJunction ports Filesystem::isJunction: always false off Windows.
func IsJunction(string) bool {
	return false
}

// RemoveJunction ports Filesystem::removeJunction: always false off Windows.
func RemoveJunction(string) (bool, error) {
	return false, nil
}
