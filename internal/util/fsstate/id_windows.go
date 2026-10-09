//go:build windows

package fsstate

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Known reports whether files have IDs here.
func Known() bool { return true }

// Stat is the ID of the file at path, symlinks followed; false when it
// cannot be read.
func Stat(path string) (ID, bool) { return stat(path, false) }

// Lstat is the ID of the file at path itself, a reparse point's own.
func Lstat(path string) (ID, bool) { return stat(path, true) }

// Fstat is the ID of an open file.
func Fstat(f *os.File) (ID, bool) { return idOf(windows.Handle(f.Fd())) }

// stat is Stat, or Lstat of a reparse point (reparse): the handle is
// opened on the path itself instead of what it points to. Reading a
// file's identity needs no access to its contents, so the handle is
// opened for attributes only and shared every way: a file maestro is
// looking at may be open anywhere else.
func stat(path string, reparse bool) (ID, bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ID{}, false
	}

	flags := uint32(windows.FILE_FLAG_BACKUP_SEMANTICS)
	if reparse {
		flags |= windows.FILE_FLAG_OPEN_REPARSE_POINT
	}

	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		return ID{}, false
	}
	defer func() { _ = windows.CloseHandle(h) }()

	return idOf(h)
}

// fileBasicInfo is FILE_BASIC_INFO, what GetFileInformationByHandleEx
// reports for FileBasicInfo: the file's times, among them a change time
// (which the file information a handle gives does not hold), and its
// attributes. The trailing pad brings the struct to the size the C one
// has (LARGE_INTEGER aligns it to 8): the call rejects anything shorter.
// A file system that keeps no change time (FAT's) reports the FILETIME
// zero for it.
type fileBasicInfo struct {
	CreationTime   windows.Filetime
	LastAccessTime windows.Filetime
	LastWriteTime  windows.Filetime
	ChangeTime     windows.Filetime
	FileAttributes uint32
	_              uint32
}

// idOf is the ID of the file behind h: the volume serial number and the
// file index identify it (a file replaced by a rename gets another;
// stable on NTFS and ReFS, where FAT's collide only across directories),
// the mode its type and whether it is writable (the read-only attribute,
// the only one os.Chmod changes here), and the size and modification and
// change times what changed.
func idOf(h windows.Handle) (ID, bool) {
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(h, &info) != nil {
		return ID{}, false
	}

	mode := uint32(modeRegular | 0o666)
	switch {
	case info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0:
		mode = modeDir | 0o777
	case info.FileAttributes&windows.FILE_ATTRIBUTE_READONLY != 0:
		mode &^= 0o222
	}

	var basic fileBasicInfo
	if windows.GetFileInformationByHandleEx(h, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&basic)), uint32(unsafe.Sizeof(basic))) != nil {
		basic = fileBasicInfo{}
	}

	return ID{
		Dev:   uint64(info.VolumeSerialNumber),
		Ino:   uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow),
		Mode:  mode,
		Size:  int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow),
		Mtime: info.LastWriteTime.Nanoseconds(),
		Ctime: changeTime(basic.ChangeTime),
	}, true
}

// changeTime is t in nanoseconds since the epoch; 0 for the FILETIME
// zero, which a file system without a change time reports and whose
// nanoseconds would overflow int64 into the far future.
func changeTime(t windows.Filetime) int64 {
	if t.HighDateTime == 0 && t.LowDateTime == 0 {
		return 0
	}

	return t.Nanoseconds()
}
