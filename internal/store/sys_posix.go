//go:build !windows

package store

import (
	"errors"
	"io/fs"
	"os"
)

// statPerm is the permission bits a stat shows for a file created or
// chmodded with perm: perm itself.
func statPerm(perm fs.FileMode) fs.FileMode {
	return perm
}

// chmodAfterCreate reports whether a file created with perm needs a chmod
// to get it: when the umask took bits away.
func chmodAfterCreate(perm, umask fs.FileMode) bool {
	return perm&umask != 0
}

// openShared opens a store file for reading; others may rename or delete
// it meanwhile.
func openShared(path string) (*os.File, error) {
	return os.Open(path)
}

// replaceFile renames oldpath onto newpath, replacing it.
func replaceFile(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

// renameDir moves the assembled package directory onto dst, which rename
// replaces when it is an empty directory.
func renameDir(tmp, dst string) error {
	return os.Rename(tmp, dst)
}

// linkThenRemove renames without replacing where rename cannot: link(2)
// fails if newpath exists.
func linkThenRemove(oldpath, newpath string) error {
	if err := os.Link(oldpath, newpath); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return &fs.PathError{Op: "rename", Path: newpath, Err: fs.ErrExist}
		}

		return err
	}

	return os.Remove(oldpath)
}
