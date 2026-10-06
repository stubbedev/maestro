//go:build windows

package command

import (
	"os/user"
	"strings"

	"golang.org/x/sys/windows"
)

// phpUname is php_uname($mode) for 's' and 'r' on Windows.
func phpUname() (sysname, release string, ok bool) {
	v := windows.RtlGetVersion()

	return "Windows NT", itoa(int(v.MajorVersion)) + "." + itoa(int(v.MinorVersion)), true
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}

	return string(b)
}

// diskFreeSpace is disk_free_space($dir).
func diskFreeSpace(dir string) (float64, bool) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, false
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree); err != nil {
		return 0, false
	}

	return float64(free), true
}

// isRunningAsRoot: posix_getuid does not exist on Windows.
func isRunningAsRoot() bool { return false }

// currentUser is get_current_user(): on Windows PHP names the user running
// it (GetUserNameW, without the domain os/user adds).
func currentUser() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}

	name := u.Username
	if i := strings.LastIndexByte(name, '\\'); i >= 0 {
		name = name[i+1:]
	}

	return name
}
