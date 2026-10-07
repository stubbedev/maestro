//go:build unix

package store

import (
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// lockFile takes a shared or exclusive advisory lock on f, waiting for it.
func lockFile(f *os.File, exclusive bool) error {
	how := unix.LOCK_SH
	if exclusive {
		how = unix.LOCK_EX
	}

	for {
		err := unix.Flock(int(f.Fd()), how)
		if err != unix.EINTR {
			return err
		}
	}
}

// umaskBySetting reads the umask the only portable way, by setting it and
// putting it back.
func umaskBySetting() fs.FileMode {
	old := unix.Umask(0o022)
	unix.Umask(old)

	return fs.FileMode(old) & fs.ModePerm //nolint:gosec // a umask is 0o777 at most.
}

// linkLimit reports a hardlink refused because the file has as many links
// as the filesystem allows.
func linkLimit(err error) bool {
	return errors.Is(err, unix.EMLINK)
}

// linkUnsupported reports a hardlink the destination cannot have: across
// filesystems, or on one without hardlinks.
func linkUnsupported(err error) bool {
	return errors.Is(err, unix.EXDEV) || errors.Is(err, unix.EPERM) || errors.Is(err, unix.ENOTSUP) ||
		errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOSYS)
}
