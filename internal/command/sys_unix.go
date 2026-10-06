//go:build unix

package command

import (
	"os"
	"os/user"
	"strconv"

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

// currentUser is get_current_user(): php_get_current_user names the owner
// of the running script (getpwuid of its st_uid), the composer.phar; for
// maestro that is its executable. "" when it cannot be found, as in PHP.
func currentUser() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}

	return fileOwnerName(exe)
}

// fileOwnerName is the user name of the owner of path, or "".
func fileOwnerName(path string) string {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return ""
	}

	u, err := user.LookupId(strconv.FormatUint(uint64(st.Uid), 10))
	if err != nil {
		return ""
	}

	return u.Username
}
