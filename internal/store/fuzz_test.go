//go:build unix

package store

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/archive"
	"github.com/stubbedev/maestro/internal/archive/archivetest"
)

// FuzzInstall inserts and materializes arbitrary archives in a sandbox:
// nothing may appear outside the package directory, and every file there
// must hold exactly the content its index entry's hash names.
func FuzzInstall(f *testing.F) {
	for _, c := range archivetest.ZipCorpus() {
		f.Add(false, c.Data)
	}

	for _, c := range archivetest.TarCorpus()[:30] {
		f.Add(true, c.Tar)
	}

	f.Fuzz(func(t *testing.T, tar bool, data []byte) {
		sandbox := tempDir(t)
		in := writeFile(t, sandbox, "in", data)
		out := filepath.Join(sandbox, "out")

		if err := os.Mkdir(out, 0o755); err != nil {
			t.Fatal(err)
		}

		s, err := Open(filepath.Join(sandbox, "store"), &Options{
			Method:  Hardlink,
			Archive: archive.Options{Locale: archive.LocaleUTF8, Limits: archive.Limits{MaxEntries: 5000, MaxFileSize: 16 << 20, MaxTotalSize: 32 << 20}},
		})
		if err != nil {
			t.Fatal(err)
		}

		d, format := Dist{Name: "f/z", Type: "zip"}, archive.Zip
		if tar {
			d.Type, format = "tar", archive.Tar
		}

		if archivetest.UnstorableArchive(in, format) {
			// APFS refuses names that are not UTF-8 (EILSEQ): no extractor
			// can install this archive here.
			t.Skip("this file system refuses names that are not UTF-8")
		}

		dst := filepath.Join(out, "pkg")

		r, err := s.Insert(d, in)
		if err == nil {
			err = s.Materialize(r, dst, ImportOptions{})
		}

		if err != nil {
			if _, ok := errors.AsType[*archive.Error](err); !ok {
				t.Fatalf("error of type %T: %v", err, err)
			}

			if names, _ := os.ReadDir(out); len(names) != 0 {
				t.Fatalf("a failed install left %v", names)
			}

			return
		}

		// Only the store, the input and out/pkg exist.
		for _, dir := range []string{sandbox, out} {
			names, _ := os.ReadDir(dir)
			for _, n := range names {
				if !map[string]bool{"store": true, "in": true, "out": true, "pkg": true}[n.Name()] {
					t.Fatalf("%s appeared in %s", n.Name(), dir)
				}
			}
		}

		// The store's objects all hold what their names say.
		if res, err := s.Verify(); err != nil || res.Corrupt != 0 || res.Missing != 0 {
			t.Fatalf("store after install: %+v %v", res, err)
		}

		// Then the package directory (opening up what the archive locked).
		want := map[string]*Entry{}
		for i := range r.Entries() {
			want[r.Entries()[i].Path] = &r.Entries()[i]
		}

		err = filepath.WalkDir(dst, func(path string, de fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			rel, _ := filepath.Rel(dst, path)
			if rel == "." {
				rel = ""
			}

			e, ok := want[filepath.ToSlash(rel)]
			if !ok {
				t.Fatalf("%q is not in the index", rel)
			}

			delete(want, e.Path)

			switch {
			case de.IsDir():
				if e.Kind != archive.Dir {
					t.Fatalf("%q: a directory, indexed as %v", rel, e.Kind)
				}

				_ = os.Chmod(path, 0o755)
			case de.Type()&fs.ModeSymlink != 0:
				target, _ := os.Readlink(path)
				if e.Kind != archive.Symlink || target != e.Link {
					t.Fatalf("%q: symlink to %q, indexed as %v %q", rel, target, e.Kind, e.Link)
				}
			default:
				_ = os.Chmod(path, 0o644)

				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}

				if e.Kind != archive.File || sha256.Sum256(data) != e.Hash || int64(len(data)) != e.Size {
					t.Fatalf("%q does not hold its indexed content", rel)
				}
			}

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		if len(want) != 0 {
			for p := range want {
				t.Errorf("%q indexed but not created", p)
			}
		}
	})
}
