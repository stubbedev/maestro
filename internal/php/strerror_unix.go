//go:build !windows

package php

import "syscall"

// crtErrno is the errno itself: PHP reads it from the failed system call.
func crtErrno(errno syscall.Errno) int { return int(errno) }

// cErrnoMessage is strerror(n): Go's errno texts are the C library's
// (glibc's, or the BSD libc's on macOS) with the first letter lowered.
func cErrnoMessage(n int) string {
	msg := syscall.Errno(n).Error()
	if msg != "" && msg[0] >= 'a' && msg[0] <= 'z' {
		msg = string(msg[0]-'a'+'A') + msg[1:]
	}

	return msg
}
