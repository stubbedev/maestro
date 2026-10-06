//go:build windows

package store

// The Windows half of store_test.go, which needs a umask, Unix modes and
// symlinks the unprivileged Windows user may not create: the store's
// import methods on NTFS, its stamps under the read-only attribute,
// healing, Chmod, and its lock across goroutines and processes.

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/archive"
	"github.com/stubbedev/maestro/internal/archive/archivetest"
)

// TestMain doubles as the helper process the multi-process test starts.
func TestMain(m *testing.M) {
	if os.Getenv("MAESTRO_STORE_HELPER") != "" {
		os.Exit(helper())
	}

	os.Exit(m.Run())
}

// helper installs one dist the way a separate maestro process would, from
// the store, archive and destination the test names in its environment.
func helper() int {
	zip := os.Getenv("MAESTRO_STORE_ZIP")

	s, err := Open(os.Getenv("MAESTRO_STORE_ROOT"), nil)
	if err == nil {
		err = s.Install(Dist{Name: "a/b", Type: "zip", URL: zip}, zip, os.Getenv("MAESTRO_STORE_DST"), ImportOptions{})
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}

func openStore(t testing.TB, root string, method Method) *Store {
	t.Helper()

	s, err := Open(root, &Options{Method: method, Archive: archive.Options{Locale: archive.LocaleUTF8}})
	if err != nil {
		t.Fatal(err)
	}

	return s
}

func writeFile(t testing.TB, dir, name string, data []byte) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

// sample is a small package with files of every mode Windows can tell
// apart, one content twice, and an empty directory.
func sample() []byte {
	return archivetest.Zip("",
		archivetest.UnixDir("pkg/", 0o755),
		archivetest.UnixFile("pkg/composer.json", 0o644, `{"name":"a/b"}`),
		archivetest.UnixFile("pkg/bin/tool", 0o755, "#!/bin/sh\n"),
		archivetest.UnixFile("pkg/src/A.php", 0o644, "<?php class A {}\n"),
		archivetest.UnixFile("pkg/src/B.php", 0o644, "<?php class A {}\n"),
		archivetest.UnixFile("pkg/ro", 0o444, "read only"),
		archivetest.UnixDir("pkg/empty/", 0o755),
		archivetest.UnixFile("pkg/zero", 0o644, ""),
	)
}

// sampleFiles is sample()'s package tree: file contents, "" for the empty
// directory.
var sampleFiles = map[string]string{
	"composer.json": `{"name":"a/b"}`,
	"bin/tool":      "#!/bin/sh\n",
	"src/A.php":     "<?php class A {}\n",
	"src/B.php":     "<?php class A {}\n",
	"ro":            "read only",
	"zero":          "",
}

func checkTree(t *testing.T, dst string) {
	t.Helper()

	for name, want := range sampleFiles {
		if got, err := os.ReadFile(filepath.Join(dst, name)); err != nil || string(got) != want {
			t.Errorf("%s: %q, %v", name, got, err)
		}
	}

	if fi, err := os.Stat(filepath.Join(dst, "ro")); err != nil || fi.Mode().Perm() != 0o444 {
		t.Errorf("ro is not read-only: %v", err)
	}

	if fi, err := os.Stat(filepath.Join(dst, "empty")); err != nil || !fi.IsDir() {
		t.Errorf("empty directory missing: %v", err)
	}
}

func TestInstall(t *testing.T) {
	work := t.TempDir()
	zip := writeFile(t, work, "dist.zip", sample())

	for _, m := range []Method{Auto, Hardlink, Copy} {
		t.Run(m.String(), func(t *testing.T) {
			s := openStore(t, filepath.Join(work, "store-"+m.String()), m)
			d := Dist{Name: "a/b", Type: "zip", URL: "https://example.org/b.zip", Reference: "abc"}
			dst := filepath.Join(work, "vendor-"+m.String(), "a", "b")

			// An empty destination directory is replaced.
			if err := os.MkdirAll(dst, 0o755); err != nil {
				t.Fatal(err)
			}

			if err := s.Install(d, zip, dst, ImportOptions{}); err != nil {
				t.Fatal(err)
			}

			checkTree(t, dst)

			st, err := lstat(filepath.Join(dst, "src", "A.php"))
			if err != nil {
				t.Fatal(err)
			}

			// NTFS cannot reflink: auto settles on hardlinks.
			linked := s.device(st.dev).get() == Hardlink
			if m != Copy && !linked {
				t.Errorf("%v settled on %v on NTFS", m, s.device(st.dev).get())
			}

			for name := range sampleFiles {
				// The read-only file's object is the same file, so it is
				// linked too.
				if st, err := lstat(filepath.Join(dst, name)); err != nil || linked != (st.nlink > 1) {
					t.Errorf("%s: method %v but %d links, %v", name, s.device(st.dev).get(), st.nlink, err)
				}
			}

			if linked && st.nlink != 3 {
				// The object, src/A.php and src/B.php share one file.
				t.Errorf("expected 3 links, got %d", st.nlink)
			}

			// A second worktree needs no archive.
			dst2 := filepath.Join(work, "vendor2-"+m.String(), "b")
			if err := os.MkdirAll(filepath.Dir(dst2), 0o755); err != nil {
				t.Fatal(err)
			}

			if err := s.Install(d, "", dst2, ImportOptions{}); err != nil {
				t.Fatal(err)
			}

			checkTree(t, dst2)

			if err := s.Install(Dist{Name: "x/y", Type: "zip"}, "", filepath.Join(work, "nope"), ImportOptions{}); !errors.Is(err, ErrNotFound) {
				t.Errorf("expected ErrNotFound for an unknown dist, got %v", err)
			}

			if res, err := s.Verify(); err != nil || res.Corrupt != 0 || res.Restamped != 0 || res.Missing != 0 {
				t.Errorf("objects failed verification: %+v %v", res, err)
			}
		})
	}
}

// TestInPlaceModification: a write through a hard-linked package file
// moves the shared file's modification time, so the store stops using it.
func TestInPlaceModification(t *testing.T) {
	work := t.TempDir()
	zip := writeFile(t, work, "dist.zip", sample())
	s := openStore(t, filepath.Join(work, "store"), Hardlink)
	d := Dist{Name: "a/b", Type: "zip"}
	first := filepath.Join(work, "p1")

	if err := s.Install(d, zip, first, ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(filepath.Join(first, "composer.json"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.WriteString(" // patched")
	if cerr := f.Close(); err == nil {
		err = cerr
	}

	if err != nil {
		t.Fatal(err)
	}

	var missing *MissingError
	if err := s.Install(d, "", filepath.Join(work, "p2"), ImportOptions{}); !errors.As(err, &missing) {
		t.Fatalf("expected *MissingError, got %v", err)
	}

	if _, err := os.Stat(filepath.Join(work, "p2")); !errors.Is(err, fs.ErrNotExist) {
		t.Error("a failed import left its destination behind")
	}

	if err := s.Install(d, zip, filepath.Join(work, "p3"), ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	checkTree(t, filepath.Join(work, "p3"))

	if res, err := s.Verify(); err != nil || res.Corrupt != 0 || res.Missing != 0 || res.Restamped != 0 {
		t.Errorf("store not healthy after healing: %+v %v", res, err)
	}
}

// TestChmod: Windows keeps only the read-only attribute, which hardlinks
// share. A chmod that changes it unshares the package file first; one
// that does not (Composer's chmod of binaries) changes nothing.
func TestChmod(t *testing.T) {
	work := t.TempDir()
	s := openStore(t, filepath.Join(work, "store"), Hardlink)
	dst := filepath.Join(work, "dst")

	if err := s.Install(Dist{Name: "a/b", Type: "zip"}, writeFile(t, work, "dist.zip", sample()), dst, ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	tool := filepath.Join(dst, "bin", "tool")
	if err := Chmod(tool, 0o755); err != nil {
		t.Fatal(err)
	}

	if st, _ := lstat(tool); st.nlink < 2 {
		t.Error("a chmod that changes nothing unshared the file")
	}

	if err := Chmod(tool, 0o555); err != nil {
		t.Fatal(err)
	}

	if st, _ := lstat(tool); st.nlink != 1 || st.mode != 0o444 {
		t.Errorf("after Chmod: %d links, mode %o", st.nlink, st.mode)
	}

	if res, err := s.Verify(); err != nil || res.Restamped != 0 || res.Corrupt != 0 {
		t.Errorf("the store saw the chmod: %+v %v", res, err)
	}
}

// TestLock: the store's lock file excludes Prune's exclusive lock from
// inserts and imports, across handles as across processes.
func TestLock(t *testing.T) {
	s := openStore(t, t.TempDir(), Auto)

	unlock, err := s.lock(true)
	if err != nil {
		t.Fatal(err)
	}

	got := make(chan error, 1)

	go func() {
		u, err := s.lock(false)
		if err == nil {
			u()
		}
		got <- err
	}()

	select {
	case err := <-got:
		t.Fatalf("a shared lock was granted under an exclusive one: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	unlock()

	select {
	case err := <-got:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the shared lock was never granted")
	}

	// Shared locks do not exclude each other.
	u1, err := s.lock(false)
	if err != nil {
		t.Fatal(err)
	}

	defer u1()

	u2, err := s.lock(false)
	if err != nil {
		t.Fatal(err)
	}

	u2()
}

// TestConcurrentInserts: goroutines and processes inserting overlapping
// dists into one store at once, publishing objects without replacing.
func TestConcurrentInserts(t *testing.T) {
	work := t.TempDir()
	root := filepath.Join(work, "store")
	zips := make([]string, 4)

	for i := range zips {
		var entries []archivetest.ZipEntry
		for j := range 200 {
			// Overlapping content between the dists.
			entries = append(entries, archivetest.UnixFile(fmt.Sprintf("pkg/f%03d", j), 0o644, strings.Repeat("x", (i+j)%7*100)))
		}

		zips[i] = writeFile(t, work, fmt.Sprintf("d%d.zip", i), archivetest.Zip("", entries...))
	}

	var wg sync.WaitGroup

	errs := make(chan error, 64)

	for g := range 16 {
		wg.Go(func() {
			s, err := Open(root, &Options{Method: []Method{Auto, Hardlink, Copy}[g%3]})
			if err != nil {
				errs <- err
				return
			}

			i := g % len(zips)
			errs <- s.Install(Dist{Name: "a/b", Type: "zip", URL: zips[i]}, zips[i], filepath.Join(work, fmt.Sprintf("g%d", g)), ImportOptions{})
		})
	}

	procs := make([]*exec.Cmd, 4)

	for p := range procs {
		cmd := exec.Command(os.Args[0], "-test.run=^$")
		cmd.Env = append(os.Environ(), "MAESTRO_STORE_HELPER=1", "MAESTRO_STORE_ROOT="+root,
			"MAESTRO_STORE_ZIP="+zips[p%len(zips)], "MAESTRO_STORE_DST="+filepath.Join(work, fmt.Sprintf("proc%d", p)))

		var out bytes.Buffer

		cmd.Stdout, cmd.Stderr = &out, &out

		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}

		procs[p] = cmd

		t.Cleanup(func() {
			if t.Failed() {
				t.Log(out.String())
			}
		})
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}

	for _, cmd := range procs {
		if err := cmd.Wait(); err != nil {
			t.Error(err)
		}
	}

	same := func(want, got string) {
		t.Helper()

		for j := range 200 {
			name := fmt.Sprintf("f%03d", j)
			a, errA := os.ReadFile(filepath.Join(want, name))
			b, errB := os.ReadFile(filepath.Join(got, name))

			if errA != nil || errB != nil || !bytes.Equal(a, b) {
				t.Errorf("%s/%s differs from %s: %v %v", got, name, want, errA, errB)
				return
			}
		}
	}

	for g := range 16 {
		same(filepath.Join(work, fmt.Sprintf("g%d", g%len(zips))), filepath.Join(work, fmt.Sprintf("g%d", g)))
	}

	for p := range procs {
		same(filepath.Join(work, fmt.Sprintf("g%d", p%len(zips))), filepath.Join(work, fmt.Sprintf("proc%d", p)))
	}

	s := openStore(t, root, Copy)
	if res, err := s.Verify(); err != nil || res.Corrupt != 0 || res.Missing != 0 || res.Releases != len(zips) {
		t.Errorf("store after concurrent inserts: %+v %v", res, err)
	}

	if names, _ := os.ReadDir(s.tmp); len(names) != 0 {
		t.Errorf("%d temporary files left", len(names))
	}
}
