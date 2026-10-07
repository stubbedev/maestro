//go:build linux

package vcs

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/store"
)

// gitTree lists a work tree for comparison: path -> mode and content,
// leaving out the files a store import fixes up.
func gitTree(t *testing.T, root string) map[string]string {
	t.Helper()

	tree := map[string]string{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(root, path)
		if rel == ".git/index" || strings.HasPrefix(rel, ".git/logs/") {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		v := info.Mode().String()

		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}

			v += " -> " + target
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			v += " " + string(data)
		}

		tree[rel] = v

		return nil
	})
	noError(t, err)

	return tree
}

func TestGitIntegration_StoreBackedInstall(t *testing.T) {
	gitEnv(t)

	work := t.TempDir()
	t.Chdir(work)

	up := newUpstream(t, filepath.Join(work, "upstream.git"))
	noError(t, os.MkdirAll(filepath.Join(up.dir, "lib", "sub"), 0o755))
	writeFile(t, filepath.Join(up.dir, "lib", "sub", "c.php"), "<?php\n")
	noError(t, os.WriteFile(filepath.Join(up.dir, "run"), []byte("#!/bin/sh\n"), 0o755))
	noError(t, os.Symlink("a.txt", filepath.Join(up.dir, "link")))
	git(t, up.dir, "add", ".")
	git(t, up.dir, "commit", "-q", "-m", "third")
	git(t, up.dir, "tag", "1.1.0")
	v3 := git(t, up.dir, "rev-parse", "HEAD")

	st, err := store.Open(filepath.Join(t.TempDir(), "store"), nil)
	noError(t, err)

	cacheDir := filepath.Join(t.TempDir(), "vcs")
	newDownloader := func() (*GitDownloader, *mio.BufferIO) {
		buffer := bufferIO(t, 0)
		d := realGitDownloader(t, buffer, "cache-vcs-dir", cacheDir)
		d.store = st

		return d, buffer
	}

	p := sourcePackage("1.1.0.0", "1.1.0", v3, "upstream.git")

	// a miss clones with git and stores the checkout
	d, _ := newDownloader()
	first := filepath.Join(work, "vendor1", "pkg")
	noError(t, run(installSteps(d, p, first)...))

	mirror := filepath.Join(cacheDir, "upstream.git") + "/"

	id, ok := d.gitStoreKey(p, "upstream.git", mirror, first)
	if !ok {
		t.Fatal("store bypassed")
	}

	// the key covers the target filesystem's traits (a checkout cloned
	// onto vfat holds core.filemode=false, core.symlinks=false, ...)
	probe := probeFSTraits
	probeFSTraits = func(string) (string, error) { return "false false true false", nil }

	if other, ok := d.gitStoreKey(p, "upstream.git", mirror, first); !ok || other == id {
		t.Errorf("key ignores the filesystem traits: %v %x", ok, other)
	}

	probeFSTraits = probe

	if _, err := st.LookupNamed(id); err != nil {
		t.Fatalf("checkout not stored: %v", err)
	}

	// a hit imports it
	d, buffer := newDownloader()
	second := filepath.Join(work, "vendor2", "pkg")
	before := time.Now().Unix()

	noError(t, run(installSteps(d, p, second)...))

	if out := php.NormalizeEOL(buffer.Output()); !strings.Contains(out, "Cloning "+v3[:10]+" from cache") {
		t.Fatalf("output %q", out)
	}

	// imported files carry their store object's stamp, cloned ones today
	if info, err := os.Stat(filepath.Join(second, "a.txt")); err != nil || info.ModTime().Year() > 2010 {
		t.Fatalf("a.txt not imported from the store: %v %v", info, err)
	}

	if a, b := gitTree(t, first), gitTree(t, second); len(a) != len(b) {
		t.Fatalf("trees differ: %d and %d entries", len(a), len(b))
	} else {
		for path, v := range a {
			if b[path] != v {
				t.Errorf("%s: %.80q, want %.80q", path, b[path], v)
			}
		}
	}

	for _, args := range [][]string{
		{"rev-parse", "HEAD"},
		{"rev-parse", "--abbrev-ref", "HEAD"},
		{"remote", "-v"},
		{"status", "--porcelain"},
		{"ls-files", "-s"},
	} {
		if a, b := git(t, first, args...), git(t, second, args...); a != b {
			t.Errorf("git %v: %q, want %q", args, b, a)
		}
	}

	// the index matches the work tree without a refresh
	if out := git(t, second, "diff-files"); out != "" {
		t.Errorf("diff-files: %q", out)
	}

	// reflogs: the same entries, stamped now
	reflog := readFile(t, filepath.Join(second, ".git", "logs", "HEAD"))
	want := readFile(t, filepath.Join(first, ".git", "logs", "HEAD"))

	if strip := func(s string) string {
		var b strings.Builder

		for line := range strings.SplitSeq(s, "\n") {
			head, tail, _ := strings.Cut(line, "\t")
			if i := strings.LastIndexByte(head, '>'); i >= 0 {
				head = head[:i]
			}

			b.WriteString(head + "\t" + tail + "\n")
		}

		return b.String()
	}; strip(reflog) != strip(want) {
		t.Errorf("reflog %q, want %q", reflog, want)
	}

	for line := range strings.SplitSeq(strings.TrimSpace(reflog), "\n") {
		head, _, _ := strings.Cut(line, "\t")
		fields := strings.Fields(head)

		if ts, err := strconv.ParseInt(fields[len(fields)-2], 10, 64); err != nil || ts < before {
			t.Errorf("reflog time %s before %d", fields[len(fields)-2], before)
		}
	}
}

func TestProbeFSTraits(t *testing.T) {
	gitEnv(t)

	dir := t.TempDir()

	traits, err := probeFSTraits(dir)
	noError(t, err)

	// what git init writes into .git/config on the same directory
	git(t, dir, "init", "-q", "repo")

	want := []string{"true", "true", "false", "false"}
	for i, key := range []string{"core.filemode", "core.symlinks", "core.ignorecase", "core.precomposeunicode"} {
		if out, _ := exec.Command("git", "-C", filepath.Join(dir, "repo"), "config", "--bool", key).Output(); len(out) > 0 { //nolint:noctx // test
			want[i] = strings.TrimSpace(string(out))
		}
	}

	if got := strings.Join(want, " "); traits != got {
		t.Errorf("traits %q, git init says %q", traits, got)
	}

	// the scratch directory is gone
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
		t.Errorf("probe left %v %v", entries, err)
	}
}

// staleWorkTree is a work tree whose index holds other stat data than its
// files, as an imported checkout's does: a repository in a subdirectory
// tree copied with cp -a, its files dated 2001 (so that no entry is racily
// clean). configure runs before the copy.
func staleWorkTree(t *testing.T, configure ...[]string) string {
	t.Helper()

	repo := filepath.Join(t.TempDir(), "repo")
	noError(t, os.MkdirAll(filepath.Join(repo, "src", "deep", "er"), 0o755))
	writeFile(t, filepath.Join(repo, "a.txt"), "a\n")
	writeFile(t, filepath.Join(repo, "src", "deep", "er", "b.php"), "<?php\n")
	writeFile(t, filepath.Join(repo, "src", "deep", "er", "c.php"), "<?php // c\n")
	writeFile(t, filepath.Join(repo, "src", "deep", "d.php"), "<?php // d\n")
	noError(t, os.Symlink("a.txt", filepath.Join(repo, "link")))
	git(t, repo, "init", "-q")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "c")

	for _, args := range configure {
		git(t, repo, args...)
	}

	copied := filepath.Join(t.TempDir(), "copy")

	if out, err := exec.Command("cp", "-a", repo, copied).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}

	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, rel := range []string{"a.txt", "src/deep/er/b.php", "src/deep/er/c.php", "src/deep/d.php"} {
		noError(t, os.Chtimes(filepath.Join(copied, rel), old, old))
	}

	return copied
}

func TestRefreshIndexMatchesGit(t *testing.T) {
	gitEnv(t)

	for name, configure := range map[string][][]string{
		"v2":          nil,
		"v4":          {{"update-index", "--index-version", "4"}},
		"v4 skipHash": {{"config", "index.skipHash", "true"}, {"update-index", "--index-version", "4"}},
		// what clone and reset write under a global feature.manyFiles:
		// v4, skipHash and an empty untracked cache whose ident names the
		// original work tree, not the copy
		"manyFiles":      {{"config", "feature.manyFiles", "true"}, {"reset", "-q", "--hard"}},
		"untrackedCache": {{"config", "core.untrackedCache", "true"}, {"reset", "-q", "--hard"}},
	} {
		t.Run(name, func(t *testing.T) {
			tree := staleWorkTree(t, configure...)
			index := filepath.Join(tree, ".git", "index")
			stale, err := os.ReadFile(index)
			noError(t, err)

			if git(t, tree, "diff-files", "--name-only") == "" {
				t.Fatal("the index is not stale")
			}

			if want := len(configure) == 2 && configure[1][0] == "reset"; bytes.Contains(stale, []byte("UNTR")) != want {
				t.Fatalf("untracked cache in the index: %v", !want)
			}

			noError(t, refreshIndex(tree))

			ours, err := os.ReadFile(index)
			noError(t, err)

			if out := git(t, tree, "diff-files"); out != "" {
				t.Errorf("diff-files after refreshIndex: %q", out)
			}

			noError(t, os.WriteFile(index, stale, 0o644))
			git(t, tree, "update-index", "-q", "--refresh")

			theirs, err := os.ReadFile(index)
			noError(t, err)

			if !bytes.Equal(ours, theirs) {
				t.Errorf("index differs from git's refresh (%d and %d bytes)", len(ours), len(theirs))
			}
		})
	}
}

func TestRefreshIndexFallsBack(t *testing.T) {
	gitEnv(t)

	for name, configure := range map[string][][]string{
		"skip-worktree": {{"update-index", "--skip-worktree", "a.txt"}},
		// git status fills the untracked cache
		"populated untracked cache": {{"config", "core.untrackedCache", "true"}, {"status", "--porcelain"}},
	} {
		tree := staleWorkTree(t, configure...)

		if err := refreshIndex(tree); !errors.Is(err, errIndexFallback) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestRestampReflog(t *testing.T) {
	in := "0000 1111 A U Thor <a@b> 1700000000 +0200\tclone: from /x\n1111 2222 A U Thor <a@b> 1700000001 +0200\n"
	want := "0000 1111 A U Thor <a@b> 42 -0100\tclone: from /x\n1111 2222 A U Thor <a@b> 42 -0100\n"

	got, err := restampReflog([]byte(in), []byte("42 -0100"))
	noError(t, err)

	if string(got) != want {
		t.Fatalf("got %q", got)
	}

	if _, err := restampReflog([]byte("garbage\n"), []byte("42 -0100")); err == nil {
		t.Fatal("garbage accepted")
	}
}
