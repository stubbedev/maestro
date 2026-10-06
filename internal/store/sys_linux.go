package store

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// lstat stats path without following a final symlink and without
// allocating a FileInfo.
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
		mtime:   widen(st.Mtim.Sec),
		mtimeNs: widen(st.Mtim.Nsec),
		ctime:   widen(st.Ctim.Sec),
		nlink:   uint64(widen(st.Nlink)), //nolint:gosec // a link count is never negative
		dev:     st.Dev,
		mode:    fs.FileMode(st.Mode) & (fs.ModePerm | 0o7000),
		regular: st.Mode&unix.S_IFMT == unix.S_IFREG,
		dir:     st.Mode&unix.S_IFMT == unix.S_IFDIR,
	}
}

// renameNoReplace renames oldpath to newpath unless newpath exists, in
// which case it fails with fs.ErrExist.
func renameNoReplace(oldpath, newpath string) error {
	err := unix.Renameat2(unix.AT_FDCWD, oldpath, unix.AT_FDCWD, newpath, unix.RENAME_NOREPLACE)

	switch {
	case err == nil:
		return nil
	case errors.Is(err, unix.EEXIST):
		return &fs.PathError{Op: "rename", Path: newpath, Err: fs.ErrExist}
	case errors.Is(err, unix.EINVAL), errors.Is(err, unix.ENOSYS):
		// The filesystem cannot do it atomically: link and unlink.
		return linkThenRemove(oldpath, newpath)
	}

	return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: err}
}

// processUmask reads the umask from /proc without changing it, which the
// set-and-restore fallback briefly does for every thread.
func processUmask() fs.FileMode {
	data, err := os.ReadFile("/proc/self/status")
	if err == nil {
		if _, rest, ok := bytes.Cut(data, []byte("\nUmask:")); ok {
			line, _, _ := bytes.Cut(rest, []byte("\n"))
			if v, err := strconv.ParseUint(string(bytes.TrimSpace(line)), 8, 32); err == nil {
				return fs.FileMode(v) & fs.ModePerm
			}
		}
	}

	return umaskBySetting()
}

// cloneObject creates dst as a copy-on-write clone of the object at src
// (FICLONE: btrfs, XFS, bcachefs, ZFS 2.2+) carrying the object's stamp
// as its modification time, checking the object after the clone.
// Filesystems that cannot clone, and two different filesystems, fail with
// errUnsupported, leaving nothing behind.
func cloneObject(src, dst string, perm, umask fs.FileMode, want stamp) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}

	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}

	err = unix.IoctlFileClone(int(out.Fd()), int(in.Fd()))
	if err == nil && perm&umask != 0 {
		err = out.Chmod(perm)
	}

	if err == nil {
		err = setMtime(out, want.mtime)
	}

	if cerr := out.Close(); err == nil {
		err = cerr
	}

	if err == nil {
		// FICLONE locks both inodes: a write through another name of the
		// object came before the clone (or after it, harmlessly), and
		// shows in the stamp.
		if err = checkOpen(in, want); err == nil {
			return nil
		}

		_ = os.Remove(dst)

		return err
	}

	_ = os.Remove(dst)

	if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOTTY) || errors.Is(err, unix.EXDEV) ||
		errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EPERM) {
		return errUnsupported
	}

	return err
}

// widen converts a stat field to int64; the fields are int32 or uint32 on
// some platforms and 64-bit on others.
func widen[T ~int32 | ~int64 | ~uint32 | ~uint64](v T) int64 {
	return int64(v)
}
