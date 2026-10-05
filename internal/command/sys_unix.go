//go:build unix

package command

import (
	"os"

	"golang.org/x/sys/unix"
)

// phpUname is php_uname($mode) for 's' (the OS name) and 'r' (its
// release).
func phpUname() (sysname, release string, ok bool) {
	var u unix.Utsname
	if err := unix.Uname(&u); err != nil {
		return "", "", false
	}

	return unix.ByteSliceToString(u.Sysname[:]), unix.ByteSliceToString(u.Release[:]), true
}

// diskFreeSpace is disk_free_space($dir); ok is false where PHP returns
// false.
func diskFreeSpace(dir string) (float64, bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, false
	}

	return float64(st.Bavail) * float64(st.Bsize), true
}

// isRunningAsRoot is `function_exists('posix_getuid') && posix_getuid() === 0`.
func isRunningAsRoot() bool { return os.Getuid() == 0 }
