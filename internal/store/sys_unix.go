//go:build unix

package store

import (
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// setMtime gives an open file the modification time t (whole seconds).
func setMtime(f *os.File, t int64) error {
	tv := []unix.Timeval{{Sec: t}, {Sec: t}}

	return unix.Futimes(int(f.Fd()), tv)
}

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
