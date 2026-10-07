//go:build unix

package store

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/archive/archivetest"
)

// readdirOrder is dir's entries in readdir() order.
func readdirOrder(t *testing.T, dir string) []string {
	t.Helper()

	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = f.Close() }()

	names, err := f.Readdirnames(-1)
	if err != nil {
		t.Fatal(err)
	}

	return names
}

// TestImportKeepsDirectoryOrder: a directory's files are created in the
// release's order, whether the install inserts the release or imports it
// from the store, so readdir() lists them as it lists files created one by
// one in that order (on btrfs, the creation order; Composer and the dump
// follow readdir order).
func TestImportKeepsDirectoryOrder(t *testing.T) {
	setUmask(t, 0o022)

	work := tempDir(t)
	entries := []archivetest.ZipEntry{archivetest.UnixDir("pkg/", 0o755), archivetest.UnixDir("pkg/a/", 0o755), archivetest.UnixDir("pkg/b/", 0o755)}

	var names []string

	for f := range 300 {
		name := fmt.Sprintf("f%03d.php", (f*7919)%300)
		names = append(names, name)
		entries = append(entries,
			archivetest.UnixFile("pkg/a/"+name, 0o644, fmt.Sprintf("<?php // a %d\n", f)),
			archivetest.UnixFile("pkg/b/"+name, 0o644, fmt.Sprintf("<?php // %d\n", f%5)))
	}

	zip := writeFile(t, work, "dist.zip", archivetest.Zip("", entries...))

	// the order of the same names created one by one in sorted order, as
	// the release lists them
	ref := filepath.Join(work, "ref")
	if err := os.Mkdir(ref, 0o755); err != nil {
		t.Fatal(err)
	}

	sorted := slices.Sorted(slices.Values(names))
	for _, name := range sorted {
		if err := os.WriteFile(filepath.Join(ref, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	want := readdirOrder(t, ref)

	for i := range 10 {
		for _, m := range []Method{Auto, Copy} {
			s := openStore(t, filepath.Join(work, fmt.Sprintf("store-%d-%v", i, m)), m)
			d := Dist{Name: "a/b", Type: "zip"}

			for _, dst := range []string{fmt.Sprintf("cold-%d-%v", i, m), fmt.Sprintf("warm-%d-%v", i, m)} {
				dst = filepath.Join(work, dst)
				if err := s.Install(d, zip, dst, ImportOptions{}, nil); err != nil {
					t.Fatal(err)
				}

				for _, dir := range []string{"a", "b"} {
					if got := readdirOrder(t, filepath.Join(dst, dir)); !slices.Equal(got, want) {
						t.Fatalf("%s/%s: readdir order differs from files created in the release's order", filepath.Base(dst), dir)
					}
				}
			}
		}
	}
}
