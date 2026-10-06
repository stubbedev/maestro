//go:build unix

package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

	"golang.org/x/sys/unix"

	"github.com/stubbedev/maestro/internal/archive"
	"github.com/stubbedev/maestro/internal/archive/archivetest"
)

// TestMain doubles as the helper process the multi-process tests start.
func TestMain(m *testing.M) {
	if os.Getenv("MAESTRO_STORE_HELPER") != "" {
		os.Exit(helper())
	}

	os.Exit(m.Run())
}

// helper installs one dist the way a separate maestro process would. It
// works in the directory the test prepared: the store, the archive and the
// destination are fixed names in it.
func helper() int {
	// The dist is named after the archive the link points to, as the
	// test's goroutines name it.
	url, err := os.Readlink("dist.zip")
	if err == nil {
		var s *Store
		if s, err = Open("store", nil); err == nil {
			err = s.Install(Dist{Name: "a/b", Type: "zip", URL: url}, "dist.zip", "pkg", ImportOptions{})
		}
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}

// E builds a store entry.
func E(e archive.Entry, hash ...[32]byte) Entry {
	out := Entry{Entry: e}
	if len(hash) > 0 {
		out.Hash = hash[0]
	}

	return out
}

// setUmask sets the process umask for the test. Tests that use it must
// not run in parallel.
func setUmask(t testing.TB, m int) {
	t.Helper()

	old := unix.Umask(m)
	t.Cleanup(func() { unix.Umask(old) })
}

func openStore(t testing.TB, root string, method Method) *Store {
	t.Helper()

	s, err := Open(root, &Options{Method: method, Archive: archive.Options{Locale: archive.LocaleUTF8}})
	if err != nil {
		t.Fatal(err)
	}

	return s
}

// tempDir is t.TempDir that can also remove the read-only directories
// packages contain.
func tempDir(t testing.TB) string {
	t.Helper()

	dir := t.TempDir()
	t.Cleanup(func() { _ = archivetest.RemoveAll(dir) })

	return dir
}

func writeFile(t testing.TB, dir, name string, data []byte) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

func snapshot(t testing.TB, dir string) archivetest.Tree {
	t.Helper()

	tree, err := archivetest.Snapshot(dir)
	if err != nil {
		t.Fatal(err)
	}

	return tree
}

// sample is a small package with every kind of entry.
func sample() []byte {
	return archivetest.Zip("",
		archivetest.UnixDir("pkg/", 0o755),
		archivetest.UnixFile("pkg/composer.json", 0o644, `{"name":"a/b"}`),
		archivetest.UnixFile("pkg/bin/tool", 0o755, "#!/bin/sh\n"),
		archivetest.UnixFile("pkg/src/A.php", 0o644, "<?php class A {}\n"),
		archivetest.UnixFile("pkg/src/B.php", 0o644, "<?php class A {}\n"),
		archivetest.UnixDir("pkg/ro/", 0o555),
		archivetest.UnixFile("pkg/ro/f", 0o444, "read only"),
		archivetest.UnixDir("pkg/empty/", 0o700),
		archivetest.UnixLink("pkg/link", "src/A.php"),
		archivetest.UnixFile("pkg/zero", 0o600, ""),
	)
}

func TestParseMethod(t *testing.T) {
	for in, want := range map[string]Method{"": Auto, "auto": Auto, " Clone ": Clone, "hardlink": Hardlink, "COPY": Copy} {
		if got, err := ParseMethod(in); err != nil || got != want {
			t.Errorf("ParseMethod(%q) = %v, %v; want %v", in, got, err, want)
		}
	}

	for _, in := range []string{"symlink", "link", "reflink"} {
		if _, err := ParseMethod(in); err == nil || !strings.Contains(err.Error(), MethodEnv) {
			t.Errorf("%s: expected an error naming %s, got %v", in, MethodEnv, err)
		}
	}

	for _, m := range []Method{Auto, Clone, Hardlink, Copy} {
		if got, _ := ParseMethod(m.String()); got != m {
			t.Errorf("%v does not round-trip", m)
		}
	}
}

func TestIndexRoundTrip(t *testing.T) {
	entries := []Entry{
		E(archive.Entry{Kind: archive.Dir, Mode: 0o777, Umask: true}),
		E(archive.Entry{Path: "a", Kind: archive.Dir, Mode: 0o755}),
		E(archive.Entry{Path: "a/f", Kind: archive.File, Mode: 0o644, Size: 3}, sha256.Sum256([]byte("abc"))),
		E(archive.Entry{Path: "a/l", Kind: archive.Symlink, Link: "../x"}),
		E(archive.Entry{Path: "b\xff\x01", Kind: archive.File, Mode: 0o600, Umask: true}),
	}

	data := encodeIndex(entries)

	got, err := decodeIndex(data)
	if err != nil {
		t.Fatal(err)
	}

	if fmt.Sprint(got) != fmt.Sprint(entries) {
		t.Fatalf("round trip:\n got %v\nwant %v", got, entries)
	}

	// Any single flipped bit or truncation is caught by the checksum.
	for i := range data {
		bad := bytes.Clone(data)
		bad[i] ^= 1

		if _, err := decodeIndex(bad); err == nil {
			t.Fatalf("flipping a bit of byte %d went unnoticed", i)
		}
	}

	for n := range data {
		if _, err := decodeIndex(data[:n]); err == nil {
			t.Fatalf("truncation to %d bytes went unnoticed", n)
		}
	}
}

// TestIndexRefusesUnsafeTrees: a well-checksummed index (a tampered cache)
// still cannot describe a tree that escapes the package directory.
func TestIndexRefusesUnsafeTrees(t *testing.T) {
	root := E(archive.Entry{Kind: archive.Dir, Mode: 0o755})

	for name, entries := range map[string][]Entry{
		"no root":            {E(archive.Entry{Path: "a", Kind: archive.Dir})},
		"root not a dir":     {{Kind: archive.File}},
		"parent climb":       {root, E(archive.Entry{Path: "../x", Kind: archive.File})},
		"absolute":           {root, E(archive.Entry{Path: "/etc/passwd", Kind: archive.File})},
		"dot":                {root, E(archive.Entry{Path: "a/./b", Kind: archive.File})},
		"empty component":    {root, E(archive.Entry{Path: "a//b", Kind: archive.File})},
		"nul":                {root, E(archive.Entry{Path: "a\x00b", Kind: archive.File})},
		"under a symlink":    {root, E(archive.Entry{Path: "l", Kind: archive.Symlink, Link: "/"}), E(archive.Entry{Path: "l/x", Kind: archive.File})},
		"under a file":       {root, E(archive.Entry{Path: "f", Kind: archive.File}), E(archive.Entry{Path: "f/x", Kind: archive.File})},
		"parent after child": {root, E(archive.Entry{Path: "d/x", Kind: archive.File}), E(archive.Entry{Path: "d", Kind: archive.Dir})},
		"duplicate":          {root, E(archive.Entry{Path: "f", Kind: archive.File}), E(archive.Entry{Path: "f", Kind: archive.File})},
		"empty link":         {root, E(archive.Entry{Path: "l", Kind: archive.Symlink})},
	} {
		if _, err := decodeIndex(encodeIndex(entries)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestObjectNames(t *testing.T) {
	s := &Store{files: "/s/files"}
	sum := sha256.Sum256([]byte("x"))

	for _, perm := range []fs.FileMode{0o644, 0o755, 0o600, 0, 0o777} {
		path := s.objectPath(&sum, perm)
		dir, name := filepath.Split(path)

		got, gotPerm, ok := parseObjectName(filepath.Base(dir), name)
		if !ok || got != sum || gotPerm != perm {
			t.Errorf("%s: parsed as %x %o %v", path, got, gotPerm, ok)
		}
	}

	if !strings.HasSuffix(s.objectPath(&sum, 0o644), hex.EncodeToString(sum[:])[2:]) {
		t.Error("mode 644 should not be named")
	}

	for _, name := range []string{"zz", strings.Repeat("a", 62) + "-644", strings.Repeat("a", 62) + "-0755", strings.Repeat("a", 62) + "-999", strings.Repeat("A", 62)} {
		if _, _, ok := parseObjectName("aa", name); ok {
			t.Errorf("%q accepted", name)
		}
	}

	if stampTime(&sum) < stampBase || stampTime(&sum) >= stampBase+1<<28 {
		t.Error("stamp out of range")
	}
}

// TestInstall imports a package with every method and checks the tree,
// the hardlinks and the stats.
func TestInstall(t *testing.T) {
	setUmask(t, 0o022)

	unzip := archivetest.NeedInfoZip(t)

	work := tempDir(t)
	zip := writeFile(t, work, "dist.zip", sample())
	want := archivetest.Unzip(t, unzip, zip, 0o022, "C.UTF-8")

	for _, m := range []Method{Auto, Hardlink, Copy} {
		t.Run(m.String(), func(t *testing.T) {
			s := openStore(t, filepath.Join(work, "store-"+m.String()), m)
			d := Dist{Name: "a/b", Type: "zip", URL: "https://example.org/b.zip", Reference: "abc"}
			dst := filepath.Join(work, "vendor-"+m.String(), "a", "b")

			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				t.Fatal(err)
			}

			if err := s.Install(d, zip, dst, ImportOptions{}); err != nil {
				t.Fatal(err)
			}

			if diff := archivetest.Diff(want.Tree, snapshot(t, dst)); len(diff) > 0 {
				t.Fatalf("tree differs:\n%s", archivetest.Short(diff))
			}

			st, err := lstat(filepath.Join(dst, "src", "A.php"))
			if err != nil {
				t.Fatal(err)
			}

			linked := s.device(st.dev).get() == Hardlink
			if m == Auto && !linked && s.device(st.dev).get() != Clone {
				t.Errorf("auto settled on %v on a filesystem with hardlinks", s.device(st.dev).get())
			}

			for _, name := range []string{"composer.json", "bin/tool", "src/A.php", "src/B.php", "ro/f", "zero"} {
				if st, err := lstat(filepath.Join(dst, name)); err != nil || linked != (st.nlink > 1) {
					t.Errorf("%s: method %v but %d links, %v", name, s.device(st.dev).get(), st.nlink, err)
				}
			}

			if m == Hardlink && st.nlink != 3 {
				// The object, src/A.php and src/B.php share one inode.
				t.Errorf("expected 3 links, got %d", st.nlink)
			}

			// A second worktree needs neither the archive nor the network.
			dst2 := filepath.Join(work, "vendor2-"+m.String(), "b")
			if err := os.MkdirAll(filepath.Dir(dst2), 0o755); err != nil {
				t.Fatal(err)
			}

			if err := s.Install(d, "", dst2, ImportOptions{}); err != nil {
				t.Fatal(err)
			}

			if diff := archivetest.Diff(want.Tree, snapshot(t, dst2)); len(diff) > 0 {
				t.Fatalf("warm tree differs:\n%s", archivetest.Short(diff))
			}

			if err := s.Install(Dist{Name: "x/y", Type: "zip"}, "", filepath.Join(work, "nope"), ImportOptions{}); !errors.Is(err, ErrNotFound) {
				t.Errorf("expected ErrNotFound for an unknown dist, got %v", err)
			}

			stats, err := s.Stats()
			if err != nil {
				t.Fatal(err)
			}

			if stats.Releases != 1 || stats.Objects != 5 || stats.ReleaseBytes <= stats.ObjectBytes {
				t.Errorf("unexpected stats %+v", stats)
			}

			if linked != (stats.LinkedBytes > 0) {
				t.Errorf("linked %v but stats %+v", linked, stats)
			}

			if res, err := s.Verify(); err != nil || res.Corrupt != 0 || res.Restamped != 0 || res.Missing != 0 {
				t.Errorf("linked objects failed verification: %+v %v", res, err)
			}
		})
	}
}

// TestCloneMethod checks that an explicit clone fails cleanly where the
// filesystem cannot reflink, and works where it can.
func TestCloneMethod(t *testing.T) {
	work := tempDir(t)
	s := openStore(t, filepath.Join(work, "store"), Clone)
	zip := writeFile(t, work, "dist.zip", sample())

	err := s.Install(Dist{Name: "a/b", Type: "zip"}, zip, filepath.Join(work, "dst"), ImportOptions{})
	if err != nil && !strings.Contains(err.Error(), "reflinks") {
		t.Fatal(err)
	}

	if err != nil {
		if _, serr := os.Stat(filepath.Join(work, "dst")); !errors.Is(serr, fs.ErrNotExist) {
			t.Error("a failed import left its destination behind")
		}

		t.Skipf("no reflinks here: %v", err)
	}
}

// TestUnshared: an unshared import (a Composer plugin) never hardlinks,
// whatever the method, so the package can rewrite its own files in place
// without reaching the store or other projects.
func TestUnshared(t *testing.T) {
	setUmask(t, 0o022)

	work := tempDir(t)
	zip := writeFile(t, work, "dist.zip", sample())

	for _, m := range []Method{Auto, Hardlink, Copy} {
		t.Run(m.String(), func(t *testing.T) {
			s := openStore(t, filepath.Join(work, "store-"+m.String()), m)
			d := Dist{Name: "a/b", Type: "zip"}
			plugin, other := filepath.Join(work, m.String()+"-plugin"), filepath.Join(work, m.String()+"-other")

			if err := s.Install(d, zip, plugin, ImportOptions{Unshared: true}); err != nil {
				t.Fatal(err)
			}

			for _, name := range []string{"composer.json", "bin/tool", "src/A.php", "src/B.php", "ro/f", "zero"} {
				if st, err := lstat(filepath.Join(plugin, name)); err != nil || st.nlink != 1 {
					t.Errorf("%s: %d links, %v", name, st.nlink, err)
				}
			}

			// A shared import of the same release after it links (where the
			// method does), and the unshared one does not move the device
			// off hardlinks.
			if err := s.Install(d, "", other, ImportOptions{}); err != nil {
				t.Fatal(err)
			}

			st, _ := lstat(filepath.Join(other, "composer.json"))
			if m == Hardlink && st.nlink != 2 {
				t.Errorf("shared import after an unshared one: %d links", st.nlink)
			}

			// The plugin rewrites its own file, as phpstan/extension-installer
			// does with file_put_contents.
			if err := os.WriteFile(filepath.Join(plugin, "composer.json"), []byte(`{"generated":true}`), 0o644); err != nil {
				t.Fatal(err)
			}

			if got, _ := os.ReadFile(filepath.Join(other, "composer.json")); string(got) != `{"name":"a/b"}` {
				t.Fatalf("the other project saw the plugin's edit: %q", got)
			}

			if res, err := s.Verify(); err != nil || res.Corrupt != 0 || res.Missing != 0 || res.Restamped != 0 {
				t.Errorf("the store saw the plugin's edit: %+v %v", res, err)
			}
		})
	}
}

// TestInPlaceModification: a tool writing into a hard-linked vendor file in
// place changes the shared inode, so every project linked to it sees the
// edit (pnpm's accepted risk), but the store notices before importing the
// object again: the edit never reaches a project imported afterwards, and
// the release is inserted again from its archive.
func TestInPlaceModification(t *testing.T) {
	setUmask(t, 0o022)

	work := tempDir(t)
	zip := writeFile(t, work, "dist.zip", sample())
	s := openStore(t, filepath.Join(work, "store"), Hardlink)
	d := Dist{Name: "a/b", Type: "zip"}
	first, second := filepath.Join(work, "p1"), filepath.Join(work, "p2")

	if err := s.Install(d, zip, first, ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := s.Install(d, "", second, ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	// Write in place, as an editor or a careless patcher would.
	appendTo(t, filepath.Join(first, "composer.json"), " // patched")

	if got, _ := os.ReadFile(filepath.Join(second, "composer.json")); !strings.HasSuffix(string(got), "patched") {
		t.Fatalf("expected the linked project to share the edit, got %q", got)
	}

	// Without the archive the store cannot restore the content.
	var missing *MissingError
	if err := s.Install(d, "", filepath.Join(work, "p3"), ImportOptions{}); !errors.As(err, &missing) {
		t.Fatalf("expected *MissingError, got %v", err)
	}

	if _, err := os.Stat(filepath.Join(work, "p3")); !errors.Is(err, fs.ErrNotExist) {
		t.Error("a failed import left its destination behind")
	}

	// With it, Install inserts the release again.
	if err := s.Install(d, zip, filepath.Join(work, "p4"), ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	if got, _ := os.ReadFile(filepath.Join(work, "p4", "composer.json")); string(got) != `{"name":"a/b"}` {
		t.Fatalf("modified content spread: %q", got)
	}

	// The edited project keeps its change, and later edits through its
	// old link no longer reach the store.
	if got, _ := os.ReadFile(filepath.Join(first, "composer.json")); !strings.HasSuffix(string(got), "patched") {
		t.Error("the patched project lost its change")
	}

	appendTo(t, filepath.Join(first, "composer.json"), " again")

	if got, _ := os.ReadFile(filepath.Join(work, "p4", "composer.json")); string(got) != `{"name":"a/b"}` {
		t.Fatalf("a later edit through the old link spread: %q", got)
	}

	if res, err := s.Verify(); err != nil || res.Corrupt != 0 || res.Missing != 0 || res.Restamped != 0 {
		t.Errorf("store not healthy after healing: %+v %v", res, err)
	}
}

// TestLinkedMetadataChanges: changes that keep the content (a touch, a
// chmod through a vendor file, which a plain chmod of a hard-linked file
// is) only make the store replace the object by a fresh, correctly
// stamped copy before linking it again.
func TestLinkedMetadataChanges(t *testing.T) {
	setUmask(t, 0o022)

	work := tempDir(t)
	zip := writeFile(t, work, "dist.zip", sample())
	s := openStore(t, filepath.Join(work, "store"), Hardlink)
	d := Dist{Name: "a/b", Type: "zip"}
	first := filepath.Join(work, "p1")

	if err := s.Install(d, zip, first, ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if err := os.Chtimes(filepath.Join(first, "src", "A.php"), now, now); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(filepath.Join(first, "composer.json"), 0o600); err != nil {
		t.Fatal(err)
	}

	second := filepath.Join(work, "p2")
	if err := s.Install(d, "", second, ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	tree := snapshot(t, second)
	if tree["composer.json"].Perm != 0o644 {
		t.Errorf("composer.json imported with mode %v", tree["composer.json"].Perm)
	}

	for _, name := range []string{"src/A.php", "composer.json"} {
		a, _ := lstat(filepath.Join(first, name))
		b, _ := lstat(filepath.Join(second, name))

		if os.SameFile(statFileInfo(t, filepath.Join(first, name)), statFileInfo(t, filepath.Join(second, name))) {
			t.Errorf("%s: linked to the changed inode (%d, %d links)", name, a.nlink, b.nlink)
		}
	}

	if res, err := s.Verify(); err != nil || res.Corrupt != 0 || res.Missing != 0 || res.Restamped != 0 {
		t.Errorf("store not healthy after restamping: %+v %v", res, err)
	}
}

func statFileInfo(t *testing.T, path string) fs.FileInfo {
	t.Helper()

	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}

	return fi
}

// TestLinkedObjectsConcurrent: while a writer changes a store object in
// place through a package file linked to it, concurrent unshared imports
// (clone or copy) either fail with *MissingError or produce the release's
// exact content, never the writer's: the object is checked before and
// after it is read. Each round links a fresh object and races the
// writer's first write against the copies.
func TestLinkedObjectsConcurrent(t *testing.T) {
	setUmask(t, 0o022)

	work := tempDir(t)
	big := strings.Repeat("0123456789abcdef", 1<<14) // 256 KiB, so a copy takes a while
	zip := writeFile(t, work, "dist.zip", archivetest.Zip("", archivetest.UnixFile("pkg/big", 0o644, big)))
	junk := bytes.Repeat([]byte("X"), 4096)

	for _, m := range []Method{Hardlink, Copy} {
		t.Run(m.String(), func(t *testing.T) {
			s := openStore(t, filepath.Join(work, "store-"+m.String()), m)
			d := Dist{Name: "a/b", Type: "zip"}

			for round := range 25 {
				// The release, intact again (Install heals from the archive).
				linked := filepath.Join(work, fmt.Sprintf("%v-linked-%d", m, round))
				if err := s.Install(d, zip, linked, ImportOptions{}); err != nil {
					t.Fatal(err)
				}

				rel, err := s.Lookup(d)
				if err != nil {
					t.Fatal(err)
				}

				e := &rel.Entries()[1]
				legacy := filepath.Join(work, fmt.Sprintf("%v-legacy-%d", m, round))

				if err := os.Link(s.objectPath(&e.Hash, objectPerm(e.Perm(s.umask))), legacy); err != nil {
					t.Fatal(err)
				}

				var wg sync.WaitGroup

				wg.Go(func() {
					f, err := os.OpenFile(legacy, os.O_WRONLY, 0)
					if err != nil {
						t.Error(err)
						return
					}

					defer func() { _ = f.Close() }()

					for off := int64(round%16) * 4096; off < int64(len(big)); off += 16 * 4096 {
						_, _ = f.WriteAt(junk, off)
					}
				})

				for g := range 4 {
					wg.Go(func() {
						dst := filepath.Join(work, fmt.Sprintf("%v-p%d-%d", m, round, g))

						err := s.Materialize(rel, dst, ImportOptions{Unshared: true})
						if _, ok := errors.AsType[*MissingError](err); ok {
							return
						}

						if err != nil {
							t.Error(err)
							return
						}

						if got, _ := os.ReadFile(filepath.Join(dst, "big")); string(got) != big {
							t.Errorf("%s: the writer's content got in", dst)
						}
					})
				}

				wg.Wait()
			}
		})
	}
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestUmaskVariants: one index serves processes with different umasks;
// objects for the new modes are made from the existing content.
func TestUmaskVariants(t *testing.T) {
	work := tempDir(t)
	zip := writeFile(t, work, "dist.zip", archivetest.Zip("",
		archivetest.FatFile("pkg/a.txt", 0x20, "dos"),
		archivetest.UnixFile("pkg/b.txt", 0o644, "unix"),
	))
	d := Dist{Name: "a/b", Type: "zip"}

	setUmask(t, 0o022)

	s1 := openStore(t, filepath.Join(work, "store"), Hardlink)
	if err := s1.Install(d, zip, filepath.Join(work, "p1"), ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	unix.Umask(0o002)

	s2 := openStore(t, filepath.Join(work, "store"), Hardlink)
	if err := s2.Install(d, "", filepath.Join(work, "p2"), ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	tree := snapshot(t, filepath.Join(work, "p2"))
	if tree["a.txt"].Perm != 0o664 || tree["b.txt"].Perm != 0o644 || tree[""].Perm != 0o775 {
		t.Errorf("modes under umask 002: %v", tree)
	}

	if tree := snapshot(t, filepath.Join(work, "p1")); tree["a.txt"].Perm != 0o644 {
		t.Errorf("modes under umask 022: %v", tree)
	}
}

// TestSetgidInherited: directories keep a setgid bit inherited from the
// destination's parent, as with unzip.
func TestSetgidInherited(t *testing.T) {
	setUmask(t, 0o022)

	work := tempDir(t)
	parent := filepath.Join(work, "vendor")

	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(parent, 0o755|fs.ModeSetgid); err != nil {
		t.Skip(err)
	}

	// Linux gives a new directory its parent's setgid bit; BSD kernels
	// (macOS) never do, so there unzip leaves none and neither does maestro.
	probe := filepath.Join(parent, "probe")
	if err := os.Mkdir(probe, 0o755); err != nil {
		t.Fatal(err)
	}

	if fi, err := os.Stat(probe); err != nil || fi.Mode()&fs.ModeSetgid == 0 {
		t.Skip("new directories do not inherit the setgid bit on this system")
	}

	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}

	zip := writeFile(t, work, "dist.zip", sample())
	s := openStore(t, filepath.Join(work, "store"), Copy)

	if err := s.Install(Dist{Name: "a/b", Type: "zip"}, zip, filepath.Join(parent, "b"), ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	for p, n := range snapshot(t, filepath.Join(parent, "b")) {
		if n.Kind == 'd' && n.Perm&fs.ModeSetgid == 0 {
			t.Errorf("%q lost the setgid bit: %v", p, n.Perm)
		}
	}
}

func TestPrune(t *testing.T) {
	work := tempDir(t)
	s := openStore(t, filepath.Join(work, "store"), Copy)
	old := Dist{Name: "a/old", Type: "zip"}
	cur := Dist{Name: "a/cur", Type: "zip"}

	if _, err := s.Insert(old, writeFile(t, work, "old.zip", archivetest.Zip("", archivetest.UnixFile("only-old", 0o644, "old"), archivetest.UnixFile("shared", 0o644, "s")))); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Insert(cur, writeFile(t, work, "cur.zip", archivetest.Zip("", archivetest.UnixFile("only-cur", 0o644, "cur"), archivetest.UnixFile("shared", 0o644, "s")))); err != nil {
		t.Fatal(err)
	}

	format, opts, _ := s.options(&old)
	oldIndex, _ := s.indexPath(&old, format, opts)
	past := time.Now().Add(-48 * time.Hour)

	if err := os.Chtimes(oldIndex, past, past); err != nil {
		t.Fatal(err)
	}

	writeFile(t, s.tmp, "leftover", []byte("x"))

	res, err := s.Prune(24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if res.Releases != 1 || res.Objects != 1 || res.Bytes != 3 {
		t.Errorf("unexpected prune result %+v", res)
	}

	if _, err := s.Lookup(old); !errors.Is(err, ErrNotFound) {
		t.Errorf("pruned release still found: %v", err)
	}

	if err := s.Install(cur, "", filepath.Join(work, "dst"), ImportOptions{}); err != nil {
		t.Fatalf("kept release broken by prune: %v", err)
	}

	if names, _ := os.ReadDir(s.tmp); len(names) != 0 {
		t.Error("temporary files survived")
	}
}

func TestVerify(t *testing.T) {
	work := tempDir(t)
	s := openStore(t, filepath.Join(work, "store"), Copy)
	d := Dist{Name: "a/b", Type: "zip"}

	r, err := s.Insert(d, writeFile(t, work, "dist.zip", sample()))
	if err != nil {
		t.Fatal(err)
	}

	var victim *Entry

	for i := range r.Entries() {
		if e := &r.Entries()[i]; e.Path == "composer.json" {
			victim = e
		}
	}

	obj := s.objectPath(&victim.Hash, victim.Perm(s.umask))
	if err := os.WriteFile(obj, bytes.Repeat([]byte("x"), int(victim.Size)), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := s.Verify()
	if err != nil {
		t.Fatal(err)
	}

	if res.Corrupt != 1 || res.Missing != 1 || res.Releases != 1 {
		t.Errorf("unexpected verify result %+v", res)
	}

	if _, err := os.Stat(obj); !errors.Is(err, fs.ErrNotExist) {
		t.Error("corrupt object kept")
	}
}

// TestRefusedArchive: an archive maestro cannot reproduce is reported as
// an *archive.Error and stores nothing.
func TestRefusedArchive(t *testing.T) {
	work := tempDir(t)
	s := openStore(t, filepath.Join(work, "store"), Copy)
	zip := writeFile(t, work, "dist.zip", archivetest.Zip("", archivetest.UnixFile("/abs", 0o644, "x")))

	_, err := s.Insert(Dist{Name: "a/b", Type: "zip"}, zip)

	var ae *archive.Error
	if !errors.As(err, &ae) || !errors.Is(err, archive.ErrIrreproducible) {
		t.Fatalf("expected an irreproducible archive error, got %v", err)
	}

	if stats, _ := s.Stats(); stats.Releases != 0 || stats.Objects != 0 {
		t.Errorf("a refused archive left %+v", stats)
	}
}

// TestConcurrentInserts: goroutines and processes inserting and importing
// the same and overlapping dists at once all succeed with the same tree.
func TestConcurrentInserts(t *testing.T) {
	setUmask(t, 0o022)

	work := tempDir(t)
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

	// And separate processes, through the test binary's helper mode, each
	// in a directory linking the shared store and its archive.
	procs := make([]*exec.Cmd, 4)

	for p := range procs {
		dir := filepath.Join(work, fmt.Sprintf("proc%d", p))
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.Symlink(root, filepath.Join(dir, "store")); err != nil {
			t.Fatal(err)
		}

		if err := os.Symlink(zips[p%len(zips)], filepath.Join(dir, "dist.zip")); err != nil {
			t.Fatal(err)
		}

		cmd := exec.Command(os.Args[0], "-test.run=^$")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "MAESTRO_STORE_HELPER=1")

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

	for g := range 16 {
		want := snapshot(t, filepath.Join(work, fmt.Sprintf("g%d", g%len(zips))))
		if diff := archivetest.Diff(want, snapshot(t, filepath.Join(work, fmt.Sprintf("g%d", g)))); len(diff) > 0 {
			t.Errorf("g%d differs:\n%s", g, archivetest.Short(diff))
		}
	}

	for p := range procs {
		want := snapshot(t, filepath.Join(work, fmt.Sprintf("g%d", p%len(zips))))
		if diff := archivetest.Diff(want, snapshot(t, filepath.Join(work, fmt.Sprintf("proc%d", p), "pkg"))); len(diff) > 0 {
			t.Errorf("p%d differs:\n%s", p, archivetest.Short(diff))
		}
	}

	s := openStore(t, root, Copy)
	if res, err := s.Verify(); err != nil || res.Corrupt != 0 || res.Missing != 0 || res.Releases != len(zips) {
		t.Errorf("store after concurrent inserts: %+v %v", res, err)
	}

	if names, _ := os.ReadDir(s.tmp); len(names) != 0 {
		t.Errorf("%d temporary files left", len(names))
	}
}

// TestUnshareAndChmod: Chmod of a package file hard-linked to the store
// gives it an inode of its own first, so the store object keeps its mode
// and stamp.
func TestUnshareAndChmod(t *testing.T) {
	setUmask(t, 0o022)

	work := tempDir(t)
	s := openStore(t, filepath.Join(work, "store"), Hardlink)
	dst := filepath.Join(work, "dst")

	if err := s.Install(Dist{Name: "a/b", Type: "zip"}, writeFile(t, work, "dist.zip", sample()), dst, ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(dst, "composer.json")

	// Same mode: nothing to do, the link stays.
	if err := Chmod(bin, 0o644); err != nil {
		t.Fatal(err)
	}

	if st, _ := lstat(bin); st.nlink < 2 {
		t.Fatal("expected a hardlink")
	}

	// Through a symlink, as chmod follows it.
	if err := os.Symlink("composer.json", filepath.Join(dst, "via")); err != nil {
		t.Fatal(err)
	}

	if err := Chmod(filepath.Join(dst, "via"), 0o755); err != nil {
		t.Fatal(err)
	}

	st, _ := lstat(bin)
	if st.nlink != 1 || st.mode != 0o755 {
		t.Errorf("after Chmod: %d links, mode %o", st.nlink, st.mode)
	}

	if got, _ := os.ReadFile(bin); string(got) != `{"name":"a/b"}` {
		t.Errorf("content changed: %q", got)
	}

	if names, _ := filepath.Glob(filepath.Join(dst, ".*maestro-*")); len(names) != 0 {
		t.Errorf("temporary files left: %v", names)
	}

	if res, err := s.Verify(); err != nil || res.Restamped != 0 || res.Corrupt != 0 {
		t.Errorf("the store saw the chmod: %+v %v", res, err)
	}

	// A file of its own is changed in place.
	if err := Unshare(bin); err != nil {
		t.Fatal(err)
	}

	if err := Chmod(bin, 0o600); err != nil {
		t.Fatal(err)
	}

	if st, _ := lstat(bin); st.mode != 0o600 {
		t.Errorf("mode %o", st.mode)
	}
}
