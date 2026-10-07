//go:build unix

package fsstate

import (
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// OpenFile opens path as os.OpenFile does (close-on-exec, EINTR retried,
// failures as *fs.PathError), for a regular file or directory: the
// descriptor is opened directly and handed to os.NewFile, which leaves it
// blocking and out of the runtime's poller. os.OpenFile tries to register
// every file with the poller (an epoll_ctl a regular file refuses) and
// switches it back to blocking mode (two fcntl calls), and each Fd call
// switches it again; this costs one fcntl. Not for pipes, FIFOs or
// devices, which want the poller.
func OpenFile(path string, flag int, perm fs.FileMode) (*os.File, error) {
	for {
		fd, err := unix.Open(path, flag|unix.O_CLOEXEC, sysMode(perm))
		if err == nil {
			return os.NewFile(uintptr(fd), path), nil
		}

		if err != unix.EINTR {
			return nil, &fs.PathError{Op: "open", Path: path, Err: err}
		}
	}
}

// sysMode is the mode bits open(2) takes for perm, as os.OpenFile passes
// them.
func sysMode(perm fs.FileMode) uint32 {
	mode := uint32(perm.Perm())
	if perm&fs.ModeSetuid != 0 {
		mode |= unix.S_ISUID
	}

	if perm&fs.ModeSetgid != 0 {
		mode |= unix.S_ISGID
	}

	if perm&fs.ModeSticky != 0 {
		mode |= unix.S_ISVTX
	}

	return mode
}
