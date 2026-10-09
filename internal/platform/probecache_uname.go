//go:build linux || darwin

// Ports nothing: unameString, php_uname()'s fields as the snapshot
// records them.

package platform

import (
	"bytes"

	"golang.org/x/sys/unix"
)

func unameString() string {
	var u unix.Utsname
	if unix.Uname(&u) != nil {
		return ""
	}

	field := func(b []byte) string { return string(bytes.TrimRight(b, "\x00")) }

	return field(u.Sysname[:]) + "\x00" + field(u.Nodename[:]) + "\x00" + field(u.Release[:]) + "\x00" + field(u.Version[:]) + "\x00" + field(u.Machine[:])
}
