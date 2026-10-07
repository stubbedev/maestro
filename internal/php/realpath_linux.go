package php

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// EvalSymlinks resolves path as PHP's realpath() does: symlinks (there are
// no junctions outside Windows).
//
// An absolute path is resolved by the kernel, in three system calls
// whatever its depth: opened with O_PATH, its descriptor's link in
// /proc/self/fd read back, closed. A path the kernel cannot resolve that
// way, and a relative one, is resolved component by component with
// filepath.EvalSymlinks, as everywhere else; a missing one fails without
// it.
func EvalSymlinks(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return filepath.EvalSymlinks(path)
	}

	real, err := kernelRealpath(path)
	if err == nil {
		return real, nil
	}

	if errors.Is(err, unix.ENOENT) {
		return "", &fs.PathError{Op: "realpath", Path: path, Err: err}
	}

	return filepath.EvalSymlinks(path)
}

// kernelRealpath is the path the kernel resolves path to.
func kernelRealpath(path string) (string, error) {
	var fd int

	err := ignoringEINTR(func() (err error) {
		fd, err = unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
		return err
	})
	if err != nil {
		return "", err
	}

	defer func() { _ = unix.Close(fd) }()

	var buf [unix.PathMax]byte

	n, err := unix.Readlink("/proc/self/fd/"+strconv.Itoa(fd), buf[:])
	if err != nil {
		return "", err
	}

	// A path outside the root ("(unreachable)...") or removed meanwhile
	// ("... (deleted)") is not one.
	real := string(buf[:n])
	if n == len(buf) || !strings.HasPrefix(real, "/") || strings.HasSuffix(real, " (deleted)") {
		return "", unix.EINVAL
	}

	return real, nil
}

// ignoringEINTR runs fn until it fails with something other than EINTR.
func ignoringEINTR(fn func() error) error {
	for {
		if err := fn(); !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}
