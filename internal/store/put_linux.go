package store

import (
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// putObject stores data as the object for sum and perm unless an intact
// one is there already: created under its own name (createObject) when
// there is none, else as writeObject does. fresh reports that this call
// created it.
func (s *Store) putObject(data []byte, sum *[32]byte, perm fs.FileMode) (fresh bool, err error) {
	path := s.objectPath(sum, perm)

	fd, err := s.createObject(path, data, sum, perm)
	if errors.Is(err, fs.ErrExist) {
		return false, s.writeObject(path, data, sum, perm)
	}

	if err != nil {
		return false, err
	}

	return true, closeRaw(fd, path)
}

// createObject creates the object path holding data with mode perm under
// its own name, in one go: open (failing with fs.ErrExist when an object
// is there), write, stamp. The stamp, set last, marks the object complete:
// until then, and after a crash in between, it fails every check as a
// stale object does, and is replaced by the next insert of its content.
func (s *Store) createObject(path string, data []byte, sum *[32]byte, perm fs.FileMode) (int, error) {
	fd := -1

	err := s.intoShard(0, sum, path, func() (err error) {
		fd, err = openRaw(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, uint32(perm))
		return err
	})
	if err != nil {
		return -1, err
	}

	if err = fill(fd, path, data, perm, s.umask, stampTime(sum)); err != nil {
		_ = unix.Close(fd)
		_ = os.Remove(path)

		return -1, err
	}

	return fd, nil
}

// fill writes data to the new file fd (path), gives it the permission
// bits perm where the umask took some away, and the modification time
// mtime.
func fill(fd int, path string, data []byte, perm, umask fs.FileMode, mtime int64) error {
	for len(data) > 0 {
		n, err := unix.Write(fd, data)
		if err == unix.EINTR {
			continue
		}

		if err != nil {
			return &fs.PathError{Op: "write", Path: path, Err: err}
		}

		data = data[n:]
	}

	return stampFd(fd, path, perm, umask, mtime)
}

// stampFd gives the new file fd (path) the permission bits perm where the
// umask took some away, and the modification time mtime.
func stampFd(fd int, path string, perm, umask fs.FileMode, mtime int64) error {
	if chmodAfterCreate(perm, umask) {
		if err := ignoringEINTR(func() error { return unix.Fchmod(fd, uint32(perm)) }); err != nil {
			return &fs.PathError{Op: "chmod", Path: path, Err: err}
		}
	}

	if err := futimens(fd, mtime); err != nil {
		return &fs.PathError{Op: "utimensat", Path: path, Err: err}
	}

	return nil
}

// closeRaw closes fd (path), failures as *fs.PathError.
func closeRaw(fd int, path string) error {
	if err := unix.Close(fd); err != nil {
		return &fs.PathError{Op: "close", Path: path, Err: err}
	}

	return nil
}
