package store

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"strconv"
	"unsafe"

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
//
// It works on raw descriptors: an *os.File would add, per file, the
// poller's registration attempts and a switch back to blocking mode for
// every Fd call (some fifteen syscalls more than the clone needs).
func cloneObject(src, dst string, perm, umask fs.FileMode, want stamp) error {
	in, err := openRaw(src, unix.O_RDONLY, 0)
	if err != nil {
		return err
	}

	defer func() { _ = unix.Close(in) }()

	if err := cloneFd(in, dst, perm, umask, want.mtime); err != nil {
		return err
	}

	// FICLONE locks both inodes: a write through another name of the
	// object came before the clone (or after it, harmlessly), and shows in
	// the stamp.
	var st unix.Stat_t
	if err = unix.Fstat(in, &st); err != nil {
		err = &fs.PathError{Op: "fstat", Path: src, Err: err}
	} else {
		err = want.check(statOf(&st))
	}

	if err != nil {
		_ = os.Remove(dst)
	}

	return err
}

// cloneFd creates dst as a copy-on-write clone of the open file in, with
// permission bits perm and modification time mtime. Filesystems that
// cannot clone, and two different filesystems, fail with errUnsupported,
// leaving nothing behind.
func cloneFd(in int, dst string, perm, umask fs.FileMode, mtime int64) error {
	out, err := openRaw(dst, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, uint32(perm))
	if err != nil {
		return err
	}

	err = unix.IoctlFileClone(out, in)
	if err != nil {
		_ = unix.Close(out)
		_ = os.Remove(dst)

		if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOTTY) || errors.Is(err, unix.EXDEV) ||
			errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EPERM) {
			return errUnsupported
		}

		return &fs.PathError{Op: "clone", Path: dst, Err: err}
	}

	err = stampFd(out, dst, perm, umask, mtime)
	if cerr := closeRaw(out, dst); err == nil {
		err = cerr
	}

	if err != nil {
		_ = os.Remove(dst)
	}

	return err
}

// setMtime gives an open file the modification time t (whole seconds).
func setMtime(f *os.File, t int64) error {
	return futimens(int(f.Fd()), t)
}

// futimens sets the access and modification times of the open file fd to
// t (whole seconds): utimensat(fd, NULL, ...), as glibc's futimens. The
// x/sys Futimes goes through a /proc/self/fd path lookup instead.
func futimens(fd int, t int64) error {
	ts := [2]unix.Timespec{unix.NsecToTimespec(t * 1e9), unix.NsecToTimespec(t * 1e9)}

	for {
		_, _, errno := unix.Syscall6(unix.SYS_UTIMENSAT, uintptr(fd), 0, uintptr(unsafe.Pointer(&ts)), 0, 0, 0) //nolint:gosec // a descriptor is never negative
		switch errno {
		case 0:
			return nil
		case unix.EINTR:
			continue
		}

		return errno
	}
}

// openRaw opens path as os.OpenFile does (close-on-exec, EINTR retried,
// failures as *fs.PathError), returning the descriptor itself.
func openRaw(path string, flag int, perm uint32) (int, error) {
	for {
		fd, err := unix.Open(path, flag|unix.O_CLOEXEC, perm)
		if err == nil {
			return fd, nil
		}

		if err != unix.EINTR {
			return -1, &fs.PathError{Op: "open", Path: path, Err: err}
		}
	}
}

// ignoringEINTR runs fn until it fails with something other than EINTR.
func ignoringEINTR(fn func() error) error {
	for {
		if err := fn(); !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

// widen converts a stat field to int64; the fields are int32 or uint32 on
// some platforms and 64-bit on others.
func widen[T ~int32 | ~int64 | ~uint32 | ~uint64](v T) int64 {
	return int64(v)
}
