//go:build windows

package fsstate

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestDebugID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("one"), 0o666); err != nil {
		t.Fatal(err)
	}

	p, _ := windows.UTF16PtrFromString(path)
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatalf("debug: CreateFile: %v", err)
	}
	defer func() { _ = windows.CloseHandle(h) }()

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		t.Fatalf("debug: GetFileInformationByHandle: %v", err)
	}

	var basic fileBasicInfo
	exErr := windows.GetFileInformationByHandleEx(h, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&basic)), uint32(unsafe.Sizeof(basic)))

	id, ok := Stat(path)
	again, _ := Stat(path)

	t.Errorf("debug: exErr=%v size=%d attrs=%#x vol=%#x idx=%#x create=%v access=%v write=%v change-raw=%#x change-ns=%d change-time=%v",
		exErr, unsafe.Sizeof(basic), info.FileAttributes, info.VolumeSerialNumber,
		uint64(info.FileIndexHigh)<<32|uint64(info.FileIndexLow),
		basic.CreationTime.Nanoseconds(), basic.LastAccessTime.Nanoseconds(), basic.LastWriteTime.Nanoseconds(),
		uint64(basic.ChangeTime.HighDateTime)<<32|uint64(basic.ChangeTime.LowDateTime),
		basic.ChangeTime.Nanoseconds(), time.Unix(0, basic.ChangeTime.Nanoseconds()).UTC())
	t.Errorf("debug: id=%+v ok=%v stable=%v mtime=%v trusted-now=%v trusted-past=%v",
		id, ok, id == again, time.Unix(0, id.Mtime).UTC(),
		id.Trusted(time.Now(), Margin(-time.Hour)),
		ID{Mtime: id.Mtime, Ctime: id.Ctime}.Trusted(time.Now().Add(-time.Minute), 0))
}
