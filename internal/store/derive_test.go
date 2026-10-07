package store

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stubbedev/maestro/internal/archive/archivetest"
)

// recordingDeriver notes what Insert hands it.
type recordingDeriver struct {
	mu       sync.Mutex
	files    map[string]string
	inserted []*Release
}

func (d *recordingDeriver) File(path string, data []byte, sum *[32]byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if sha256.Sum256(data) != *sum {
		d.files[path] = "wrong hash"

		return
	}
	d.files[path] = string(data)
}

func (d *recordingDeriver) Inserted(s *Store, r *Release) {
	d.inserted = append(d.inserted, r)
	_ = s.WriteDerived(r, "test", []byte("derived"))
}

// An insert hands a Deriver every file it holds in memory, with its hash,
// then the release once it is in the store; an install the store answers
// without inserting hands it nothing.
func TestInsertDerives(t *testing.T) {
	work := t.TempDir()
	big := strings.Repeat("x", smallFile+1)
	zip := filepath.Join(work, "dist.zip")
	if err := os.WriteFile(zip, archivetest.Zip("",
		archivetest.UnixDir("pkg/", 0o755),
		archivetest.UnixFile("pkg/src/A.php", 0o644, "<?php class A {}\n"),
		archivetest.UnixFile("pkg/zero", 0o600, ""),
		archivetest.UnixFile("pkg/big", 0o644, big),
		archivetest.UnixLink("pkg/link", "src/A.php"),
	), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(work, "store"), nil)
	if err != nil {
		t.Fatal(err)
	}
	d := Dist{Name: "a/b", Type: "zip", URL: "https://example.org/b.zip"}

	dv := &recordingDeriver{files: map[string]string{}}
	if err := s.Install(d, zip, filepath.Join(work, "one"), ImportOptions{}, dv); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"src/A.php": "<?php class A {}\n", "zero": ""}
	if len(dv.files) != len(want) {
		t.Errorf("files %q, want %q", dv.files, want)
	}
	for path, content := range want {
		if dv.files[path] != content {
			t.Errorf("%s: %q, want %q", path, dv.files[path], content)
		}
	}
	if len(dv.inserted) != 1 {
		t.Fatalf("Inserted called %d times, want once", len(dv.inserted))
	}
	if data, err := s.ReadDerived(dv.inserted[0], "test"); err != nil || string(data) != "derived" {
		t.Errorf("derived data %q, %v", data, err)
	}

	again := &recordingDeriver{files: map[string]string{}}
	if err := s.Install(d, zip, filepath.Join(work, "two"), ImportOptions{}, again); err != nil {
		t.Fatal(err)
	}
	if len(again.files) != 0 || len(again.inserted) != 0 {
		t.Errorf("an install from the store derived %q, %d releases", again.files, len(again.inserted))
	}
}
