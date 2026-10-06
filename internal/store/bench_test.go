//go:build unix

package store

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/archive/archivetest"
)

// benchDir is where benchmarks put their stores and package directories:
// a temporary directory, so TMPDIR selects the filesystem measured.
func benchDir(b *testing.B) string {
	b.Helper()

	return tempDir(b)
}

// benchPackage is a zip shaped like a large PHP package: 3,000 files of a
// few kilobytes in 150 directories.
func benchPackage(b *testing.B, dir string) (string, int64) {
	b.Helper()

	entries := []archivetest.ZipEntry{archivetest.UnixDir("vendor-pkg-1a2b3c/", 0o755)}
	total := int64(0)

	for d := range 150 {
		entries = append(entries, archivetest.UnixDir(fmt.Sprintf("vendor-pkg-1a2b3c/src/D%03d/", d), 0o755))

		for f := range 20 {
			body := fmt.Sprintf("<?php\n\nnamespace Vendor\\D%03d;\n\nfinal class C%02d\n{\n%s}\n", d, f, strings.Repeat("    public function m(): int { return 42; }\n", 20+(d*f)%80))
			total += int64(len(body))
			entries = append(entries, archivetest.UnixFile(fmt.Sprintf("vendor-pkg-1a2b3c/src/D%03d/C%02d.php", d, f), 0o644, body))
		}
	}

	return writeFile(b, dir, "bench.zip", archivetest.Zip("", entries...)), total
}

// BenchmarkImport materializes a 3,000-file package from a warm store.
func BenchmarkImport(b *testing.B) {
	for _, m := range []Method{Clone, Hardlink, Copy} {
		b.Run(m.String(), func(b *testing.B) {
			work := benchDir(b)
			zip, total := benchPackage(b, work)
			s := openStore(b, filepath.Join(work, "store"), m)
			d := Dist{Name: "bench/pkg", Type: "zip"}

			r, err := s.Insert(d, zip)
			if err != nil {
				b.Fatal(err)
			}

			if err := s.Materialize(r, filepath.Join(work, "probe"), ImportOptions{}); err != nil {
				b.Skipf("%v unavailable here: %v", m, err)
			}

			b.SetBytes(total)
			b.ResetTimer()

			n := 0
			for b.Loop() {
				n++
				if err := s.Materialize(r, filepath.Join(work, fmt.Sprint("dst", n)), ImportOptions{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkInsertCold extracts the 3,000-file package into an empty store.
func BenchmarkInsertCold(b *testing.B) {
	work := benchDir(b)
	zip, total := benchPackage(b, work)
	d := Dist{Name: "bench/pkg", Type: "zip"}

	b.SetBytes(total)

	n := 0
	for b.Loop() {
		n++
		s := openStore(b, filepath.Join(work, fmt.Sprint("store", n)), Auto)

		if _, err := s.Insert(d, zip); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkInsertWarm inserts the package again into a store that has
// every file: one stat per file.
func BenchmarkInsertWarm(b *testing.B) {
	work := benchDir(b)
	zip, total := benchPackage(b, work)
	s := openStore(b, filepath.Join(work, "store"), Auto)

	if _, err := s.Insert(Dist{Name: "bench/pkg", Type: "zip"}, zip); err != nil {
		b.Fatal(err)
	}

	b.SetBytes(total)

	n := 0
	for b.Loop() {
		n++
		if _, err := s.Insert(Dist{Name: "bench/pkg", Type: "zip", Reference: strconv.Itoa(n)}, zip); err != nil {
			b.Fatal(err)
		}
	}
}
