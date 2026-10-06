//go:build windows

package store

import (
	"errors"
	"io/fs"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// setMtime gives an open file the modification time t (whole seconds).
func setMtime(f *os.File, t int64) error {
	ft := windows.NsecToFiletime(t * 1e9)

	return windows.SetFileTime(windows.Handle(f.Fd()), nil, nil, &ft)
}

// lockFile takes a shared or exclusive lock on all of f, waiting for it.
// Closing the file releases it, as with flock.
func lockFile(f *os.File, exclusive bool) error {
	var flags uint32
	if exclusive {
		flags = windows.LOCKFILE_EXCLUSIVE_LOCK
	}

	return windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, ^uint32(0), ^uint32(0), new(windows.Overlapped))
}

// processUmask is 0: Windows has no umask, and its files only know the
// read-only attribute.
func processUmask() fs.FileMode {
	return 0
}

// statPerm is the permission bits a stat shows for a file created or
// chmodded with perm: Windows keeps only the read-only attribute, which Go
// reports as 0444, and 0666 without it.
func statPerm(perm fs.FileMode) fs.FileMode {
	if perm&0o200 != 0 {
		return 0o666
	}

	return 0o444
}

// chmodAfterCreate reports whether a file created with perm needs a chmod
// to get it: a read-only one, as the read-only attribute CreateFile is
// asked for does not always stick (Wine and some network shares drop it).
func chmodAfterCreate(perm, _ fs.FileMode) bool {
	return perm&0o200 == 0
}

// linkLimit reports a hardlink refused because the file has as many links
// as NTFS allows (1024).
func linkLimit(err error) bool {
	return errors.Is(err, windows.ERROR_TOO_MANY_LINKS)
}

// linkUnsupported reports a hardlink the destination cannot have: across
// volumes, or on a filesystem without hardlinks (FAT, exFAT, some shares).
func linkUnsupported(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE) || errors.Is(err, windows.ERROR_INVALID_FUNCTION) ||
		errors.Is(err, windows.ERROR_NOT_SUPPORTED)
}

// lstat opens path itself (not a symlink's target) just to ask for what
// GetFileInformationByHandle knows: the link count and volume serial
// number, which os.Lstat leaves out on Windows.
func lstat(path string) (fileStat, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fileStat{}, &fs.PathError{Op: "lstat", Path: path, Err: err}
	}

	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return fileStat{}, &fs.PathError{Op: "lstat", Path: path, Err: err}
	}

	defer func() { _ = windows.CloseHandle(h) }()

	st, err := statHandle(h)
	if err != nil {
		return fileStat{}, &fs.PathError{Op: "lstat", Path: path, Err: err}
	}

	return st, nil
}

func fstat(f *os.File) (fileStat, error) {
	st, err := statHandle(windows.Handle(f.Fd()))
	if err != nil {
		return fileStat{}, &fs.PathError{Op: "fstat", Path: f.Name(), Err: err}
	}

	return st, nil
}

// statHandle is the store's fileStat of an open handle. The modes are
// Go's: 0666 or 0444 (read-only), directories 0777 or 0555. There is no
// change time to key pruning on; the modification time stands in.
func statHandle(h windows.Handle) (fileStat, error) {
	var d windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &d); err != nil {
		return fileStat{}, err
	}

	ns := d.LastWriteTime.Nanoseconds()
	st := fileStat{
		size:    int64(d.FileSizeHigh)<<32 | int64(d.FileSizeLow),
		mtime:   ns / 1e9,
		mtimeNs: ns % 1e9,
		nlink:   uint64(d.NumberOfLinks),
		dev:     uint64(d.VolumeSerialNumber),
		mode:    0o666,
	}
	st.ctime = st.mtime

	reparse := d.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
	st.dir = d.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 && !reparse
	st.regular = !st.dir && !reparse

	if st.dir {
		st.mode = 0o777
	}

	if d.FileAttributes&windows.FILE_ATTRIBUTE_READONLY != 0 {
		st.mode &^= 0o222
	}

	return st, nil
}

// renameNoReplace is MoveFileEx without MOVEFILE_REPLACE_EXISTING, which
// fails with ERROR_ALREADY_EXISTS when newpath exists.
func renameNoReplace(oldpath, newpath string) error {
	from, err := windows.UTF16PtrFromString(oldpath)
	if err != nil {
		return err
	}

	to, err := windows.UTF16PtrFromString(newpath)
	if err != nil {
		return err
	}

	if err := windows.MoveFileEx(from, to, 0); err != nil {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: err}
	}

	return nil
}

// openShared opens a store file for reading the way POSIX opens any file:
// letting other processes rename or delete it meanwhile (os.Open on
// Windows leaves out FILE_SHARE_DELETE, so a reader would make every
// writer's rename onto the file fail).
func openShared(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}

	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}

	return os.NewFile(uintptr(h), path), nil
}

// replaceFile renames oldpath onto newpath, replacing it. Windows refuses
// while another process holds newpath open without FILE_SHARE_DELETE (or
// is replacing it too), which lasts moments: the rename is retried for up
// to two seconds.
func replaceFile(oldpath, newpath string) error {
	var err error

	for wait := time.Millisecond; wait < 2*time.Second; wait *= 2 {
		if err = os.Rename(oldpath, newpath); err == nil ||
			!errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return err
		}

		time.Sleep(wait)
	}

	return err
}

// renameDir moves the assembled package directory onto dst, which may be
// an empty directory: MoveFileEx replaces no directory, so an empty dst is
// removed first.
func renameDir(tmp, dst string) error {
	if err := os.Remove(dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	return os.Rename(tmp, dst)
}

func cloneObject(_, _ string, _, _ fs.FileMode, _ stamp) error {
	return errUnsupported
}
