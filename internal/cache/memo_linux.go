package cache

import (
	"io/fs"
	"syscall"
)

// fileIdentity tells a file's version apart: its device and inode (a
// cache write renames a new file into place), size, and modification and
// change times.
type fileIdentity struct {
	dev, ino     uint64
	size         int64
	mtime, ctime syscall.Timespec
}

func sysIdentity(fi fs.FileInfo) (fileIdentity, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fileIdentity{}, false
	}

	return fileIdentity{dev: st.Dev, ino: st.Ino, size: st.Size, mtime: st.Mtim, ctime: st.Ctim}, true
}
