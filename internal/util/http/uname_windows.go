//go:build windows

package http

import (
	"strconv"

	"golang.org/x/sys/windows"
)

// systemUname is what php_uname('s') and php_uname('r') report on Windows.
func systemUname() (sysname, release string) {
	v := windows.RtlGetVersion()

	return "Windows NT", strconv.Itoa(int(v.MajorVersion)) + "." + strconv.Itoa(int(v.MinorVersion))
}
