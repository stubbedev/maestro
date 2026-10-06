// Ports the strerror(errno) texts PHP's filesystem warnings carry.

package php

import (
	"errors"
	"syscall"
)

// CErrno is a C library errno PHP sets itself rather than taking it from
// a failed system call, as chdir() does with ENOTDIR for a file.
type CErrno int

// ENOTDIR is 20 in glibc, the BSD libc and the Microsoft C runtime alike.
const ENOTDIR CErrno = 20

func (e CErrno) Error() string { return cErrnoMessage(int(e)) }

// Strerror renders an OS error as PHP's warnings do: the C library's
// strerror() of the errno behind err. Errors that carry no errno render
// as their own text.
func Strerror(err error) string {
	if _, msg, ok := errnoOf(err); ok {
		return msg
	}

	return err.Error()
}

// Errno returns the C library errno PHP sees for err (as the "(errno N)"
// of a chdir() warning shows it), and whether err carries one.
func Errno(err error) (int, bool) {
	n, _, ok := errnoOf(err)

	return n, ok
}

func errnoOf(err error) (int, string, bool) {
	if e, ok := errors.AsType[CErrno](err); ok {
		return int(e), e.Error(), true
	}

	errno, ok := errors.AsType[syscall.Errno](err)
	if !ok {
		return 0, "", false
	}

	n := crtErrno(errno)

	return n, cErrnoMessage(n), true
}
