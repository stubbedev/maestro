//go:build !linux && !darwin

package store

import (
	"io/fs"
	"os"
)

func lstat(path string) (fileStat, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return fileStat{}, err
	}

	return statInfo(info), nil
}

func fstat(f *os.File) (fileStat, error) {
	info, err := f.Stat()
	if err != nil {
		return fileStat{}, err
	}

	return statInfo(info), nil
}

// statInfo is the portable subset: no link count, device or ctime, so
// hardlinks are never chosen and pruning keys on the modification time.
func statInfo(info fs.FileInfo) fileStat {
	return fileStat{
		size:    info.Size(),
		mtime:   info.ModTime().Unix(),
		mtimeNs: int64(info.ModTime().Nanosecond()),
		ctime:   info.ModTime().Unix(),
		nlink:   1,
		mode:    info.Mode() & fs.ModePerm,
		regular: info.Mode().IsRegular(),
		dir:     info.IsDir(),
	}
}

func renameNoReplace(oldpath, newpath string) error {
	return linkThenRemove(oldpath, newpath)
}

func cloneFile(_, _ *os.File) error {
	return errUnsupported
}

func cloneObject(_, _ string, _, _ fs.FileMode, _ stamp) error {
	return errUnsupported
}
