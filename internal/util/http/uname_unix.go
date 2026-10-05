//go:build unix

package http

import (
	"golang.org/x/sys/unix"
)

// systemUname is uname(2)'s sysname and release, as php_uname reports them.
func systemUname() (sysname, release string) {
	var u unix.Utsname
	if err := unix.Uname(&u); err != nil {
		return "Unknown", "Unknown"
	}

	return unix.ByteSliceToString(u.Sysname[:]), unix.ByteSliceToString(u.Release[:])
}
