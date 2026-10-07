package fsstate

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteAtomic_ReplacesAndCreatesDirectories(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a", "b", "entry")
	for _, data := range []string{"first", "second, longer"} {
		if err := WriteAtomic(path, []byte(data)); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != data {
			t.Fatalf("read %q, %v; want %q", got, err, data)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("%d files next to the entry, want none", len(entries)-1)
	}
}

// The file ends 0666 less the umask, as os.WriteFile leaves one, not 0600.
func TestWriteAtomic_Mode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix modes")
	}
	t.Parallel()
	dir := t.TempDir()
	want := filepath.Join(dir, "want")
	if err := os.WriteFile(want, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	got := filepath.Join(dir, "got")
	if err := WriteAtomic(got, nil); err != nil {
		t.Fatal(err)
	}
	wi, _ := os.Stat(want)
	gi, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	if gi.Mode().Perm() != wi.Mode().Perm() {
		t.Errorf("mode %v, want %v", gi.Mode().Perm(), wi.Mode().Perm())
	}
}

func TestWriteAtomicFunc_FailureLeavesTheFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "entry")
	if err := os.WriteFile(path, []byte("old"), 0o666); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	err := WriteAtomicFunc(path, func(w io.Writer) error {
		_, _ = w.Write([]byte("partial"))

		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the writer's", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "old" {
		t.Errorf("entry %q after a failed write", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("%d files left, want the entry only", len(entries))
	}
}

func TestWriteAtomicFunc_TempName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var name string
	err := WriteAtomicFunc(filepath.Join(dir, "entry"), func(w io.Writer) error {
		name = filepath.Base(w.(*os.File).Name())

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !IsTemp(name) || IsTemp("entry") {
		t.Errorf("IsTemp(%q) false, or IsTemp(entry) true", name)
	}
}
