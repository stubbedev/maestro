//go:build unix

package util

import (
	"strconv"

	"golang.org/x/sys/unix"
)

func currentUID() string {
	return strconv.Itoa(unix.Getuid())
}

func currentEUID() string {
	return strconv.Itoa(unix.Geteuid())
}
