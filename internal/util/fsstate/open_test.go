package fsstate

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

// OpenFile behaves as os.OpenFile for the files maestro writes and reads:
// exclusive creation, the mode less the umask, reads and writes, and
// failures as *fs.PathError matching the fs errors.
func TestOpenFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "f")

	f, err := OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.WriteString("contents"); err != nil {
		t.Fatal(err)
	}

	if f.Name() != path {
		t.Errorf("name %q, want %q", f.Name(), path)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm()&^0o640 != 0 {
			t.Errorf("mode %v (%v), want 0640 less the umask", info.Mode(), err)
		}
	}

	_, err = OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if pe, ok := errors.AsType[*fs.PathError](err); !ok || pe.Path != path || !errors.Is(err, fs.ErrExist) {
		t.Errorf("exclusive create of an existing file: %v", err)
	}

	if _, err := Open(path + ".missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("open of a missing file: %v", err)
	}

	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	defer r.Close()

	if data, err := io.ReadAll(r); err != nil || string(data) != "contents" {
		t.Errorf("read %q, %v", data, err)
	}
}

func TestPid(t *testing.T) {
	t.Parallel()

	if Pid() != strconv.Itoa(os.Getpid()) {
		t.Errorf("Pid() = %s, want %d", Pid(), os.Getpid())
	}
}
