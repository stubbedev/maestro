package store

import (
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// putObject stores data as the object for sum and perm unless an intact
// one is there already: created under its own name (createObject) when
// there is none, else as writeObject does.
func (s *Store) putObject(data []byte, sum *[32]byte, perm fs.FileMode) error {
	path := s.objectPath(sum, perm)

	fd, err := s.createObject(path, data, sum, perm)
	if errors.Is(err, fs.ErrExist) {
		return s.writeObject(path, data, sum, perm)
	}

	if err != nil {
		return err
	}

	return closeRaw(fd, path)
}

// putImport stores e's content data and creates the package file dst from
// it. An object it creates is imported from its open descriptor, without
// checking it against its stamp again: no package file is linked to it
// yet, so nothing but the store can have written it. An object that is
// there already is imported as importFile does.
func (s *Store) putImport(dev *device, e *Entry, data []byte, dst string, unshared bool) error {
	perm := e.Perm(s.umask)
	objPerm := objectPerm(perm)
	path := s.objectPath(&e.Hash, objPerm)

	fd, err := s.createObject(path, data, &e.Hash, objPerm)
	if errors.Is(err, fs.ErrExist) {
		if err := s.writeObject(path, data, &e.Hash, objPerm); err != nil {
			return err
		}

		return s.importFile(dev, e, dst, unshared)
	}

	if err != nil {
		return err
	}

	src := newObject{fd: fd, path: path, data: data, umask: s.umask, mtime: stampTime(&e.Hash)}

	err = s.importWith(dev, src, dst, perm, objPerm == perm && !unshared)
	if cerr := closeRaw(fd, path); err == nil {
		err = cerr
	}

	return err
}

// createObject creates the object path holding data with mode perm under
// its own name, in one go: open (failing with fs.ErrExist when an object
// is there), write, stamp. The stamp, set last, marks the object complete:
// until then, and after a crash in between, it fails every check as a
// stale object does, and is replaced by the next insert of its content.
// The descriptor is returned open for reading and writing.
func (s *Store) createObject(path string, data []byte, sum *[32]byte, perm fs.FileMode) (int, error) {
	fd := -1

	err := s.intoShard(0, sum, path, func() (err error) {
		fd, err = openRaw(path, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL, uint32(perm))
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

// newObject is an object this insert has just created, still open.
type newObject struct {
	path  string
	data  []byte
	fd    int
	mtime int64
	umask fs.FileMode
}

func (o newObject) clone(dst string, perm fs.FileMode) error {
	return cloneFd(o.fd, dst, perm, o.umask, o.mtime)
}

func (o newObject) link(dst string) error {
	return os.Link(o.path, dst)
}

func (o newObject) copy(dst string, perm fs.FileMode) error {
	fd, err := openRaw(dst, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, uint32(perm))
	if err != nil {
		return err
	}

	err = fill(fd, dst, o.data, perm, o.umask, o.mtime)
	if cerr := closeRaw(fd, dst); err == nil {
		err = cerr
	}

	if err != nil {
		_ = os.Remove(dst)
	}

	return err
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
