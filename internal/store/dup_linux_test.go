package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/archive/archivetest"
)

// TestInsertDuplicates installs packages whose files share their content
// many times over into empty stores: the objects created and found at
// once by the workers of one insert.
func TestInsertDuplicates(t *testing.T) {
	setUmask(t, 0o022)

	work := tempDir(t)
	entries := []archivetest.ZipEntry{archivetest.UnixDir("pkg/", 0o755)}

	for d := range 20 {
		dir := fmt.Sprintf("pkg/d%02d/", d)
		entries = append(entries, archivetest.UnixDir(dir, 0o755))

		for f := range 50 {
			entries = append(entries, archivetest.UnixFile(fmt.Sprintf("%sf%02d.php", dir, f), 0o644, fmt.Sprintf("<?php // %d\n", f%3)))
		}
	}

	zip := writeFile(t, work, "dist.zip", archivetest.Zip("", entries...))

	for i := range 30 {
		for _, m := range []Method{Auto, Hardlink, Copy} {
			s := openStore(t, filepath.Join(work, fmt.Sprintf("store-%d-%v", i, m)), m)
			dst := filepath.Join(work, fmt.Sprintf("vendor-%d-%v", i, m))

			if err := s.Install(Dist{Name: "a/b", Type: "zip"}, zip, dst, ImportOptions{}, nil); err != nil {
				t.Fatalf("%d %v: %v", i, m, err)
			}

			if got, _ := os.ReadFile(filepath.Join(dst, "d19", "f49.php")); string(got) != "<?php // 1\n" {
				t.Fatalf("%d %v: %q", i, m, got)
			}
		}
	}
}
