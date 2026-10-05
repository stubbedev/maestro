//go:build unix

package util

import (
	"strconv"

	"golang.org/x/sys/unix"
)

// getwd is getcwd(3), the physical directory PHP's getcwd() returns.
func getwd() (string, error) {
	return unix.Getwd()
}

func currentUID() string {
	return strconv.Itoa(unix.Getuid())
}

func currentEUID() string {
	return strconv.Itoa(unix.Geteuid())
}
