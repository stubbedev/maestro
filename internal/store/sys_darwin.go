package store

import (
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

func lstat(path string) (fileStat, error) {
	var st unix.Stat_t

	for {
		err := unix.Lstat(path, &st)
		if err == nil {
			return statOf(&st), nil
		}

		if err != unix.EINTR {
			return fileStat{}, &fs.PathError{Op: "lstat", Path: path, Err: err}
		}
	}
}

func fstat(f *os.File) (fileStat, error) {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return fileStat{}, &fs.PathError{Op: "fstat", Path: f.Name(), Err: err}
	}

	return statOf(&st), nil
}

func statOf(st *unix.Stat_t) fileStat {
	return fileStat{
		size:    st.Size,
		mtime:   st.Mtim.Sec,
		mtimeNs: st.Mtim.Nsec,
		ctime:   st.Ctim.Sec,
		nlink:   uint64(st.Nlink),
		dev:     uint64(st.Dev), //nolint:gosec // device numbers are compared, never computed with.
		mode:    fs.FileMode(st.Mode) & (fs.ModePerm | 0o7000),
		regular: st.Mode&unix.S_IFMT == unix.S_IFREG,
		dir:     st.Mode&unix.S_IFMT == unix.S_IFDIR,
	}
}

func renameNoReplace(oldpath, newpath string) error {
	err := unix.RenamexNp(oldpath, newpath, unix.RENAME_EXCL)

	switch {
	case err == nil:
		return nil
	case errors.Is(err, unix.EEXIST):
		return &fs.PathError{Op: "rename", Path: newpath, Err: fs.ErrExist}
	case errors.Is(err, unix.ENOTSUP), errors.Is(err, unix.EINVAL):
		return linkThenRemove(oldpath, newpath)
	}

	return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: err}
}

func processUmask() fs.FileMode {
	return umaskBySetting()
}

// cloneObject creates dst with clonefile(2) (APFS), which keeps the
// object's mode, checking the object before and after.
func cloneObject(src, dst string, perm, _ fs.FileMode, want stamp) error {
	st, err := lstat(src)
	if err != nil {
		return err
	}

	if err := want.check(st); err != nil {
		return err
	}

	err = unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW)
	if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EXDEV) {
		return errUnsupported
	}

	if err != nil {
		return err
	}

	// A write through another name of the object before or during the
	// clone shows in the stamp.
	if st, err = lstat(src); err == nil {
		err = want.check(st)
	}

	if err == nil && st.mode != perm {
		err = os.Chmod(dst, perm)
	}

	if err != nil {
		_ = os.Remove(dst)
	}

	return err
}
