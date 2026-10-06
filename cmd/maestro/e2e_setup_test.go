package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// commit is one commit of a generated git repository.
type commit struct {
	// files are written (path => content; "" deletes the file).
	files map[string]string
	// tag is created on the commit when not empty.
	tag string
	// branch is checked out (created when missing) before committing.
	branch string
}

// gitRepo creates (or extends) a git repository at dir with fixed authors
// and dates, so that both tools see the same commit hashes.
func gitRepo(t *testing.T, dir string, commits ...commit) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	git := func(args ...string) string {
		t.Helper()

		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
			"GIT_AUTHOR_NAME=Maestro E2E", "GIT_AUTHOR_EMAIL=e2e@example.org",
			"GIT_COMMITTER_NAME=Maestro E2E", "GIT_COMMITTER_EMAIL=e2e@example.org",
		)

		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}

		return strings.TrimSpace(string(out))
	}

	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		git("init", "-q", "-b", "main")
	}

	// the dates continue from the repository's last commit
	n := 0
	if out := gitCount(dir); out > 0 {
		n = out
	}

	for _, c := range commits {
		if c.branch != "" {
			if strings.Contains(git("branch", "--list", c.branch), c.branch) {
				git("checkout", "-q", c.branch)
			} else {
				git("checkout", "-q", "-b", c.branch)
			}
		}

		names := make([]string, 0, len(c.files))
		for name := range c.files {
			names = append(names, name)
		}

		slices.Sort(names)

		for _, name := range names {
			path := filepath.Join(dir, name)
			if c.files[name] == "" {
				_ = os.Remove(path)

				continue
			}

			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}

			mode := os.FileMode(0o644)
			if strings.HasPrefix(name, "bin/") {
				mode = 0o755
			}

			if err := os.WriteFile(path, []byte(c.files[name]), mode); err != nil {
				t.Fatal(err)
			}

			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
		}

		n++
		date := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC).Add(time.Duration(n) * time.Hour).Format(time.RFC3339)

		git("add", "-A")

		cmd := exec.Command("git", "commit", "-q", "-m", fmt.Sprintf("commit %d", n))
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
			"GIT_AUTHOR_NAME=Maestro E2E", "GIT_AUTHOR_EMAIL=e2e@example.org",
			"GIT_COMMITTER_NAME=Maestro E2E", "GIT_COMMITTER_EMAIL=e2e@example.org",
			"GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date,
		)

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit: %v\n%s", err, out)
		}

		if c.tag != "" {
			git("tag", c.tag)
		}
	}

	git("checkout", "-q", "main")
}

func gitCount(dir string) int {
	cmd := exec.Command("git", "rev-list", "--all", "--count")
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		return 0
	}

	var n int
	_, _ = fmt.Sscan(string(out), &n)

	return n
}

// zipFile writes a zip archive with fixed timestamps; names ending in "/"
// are directories.
func zipFile(t *testing.T, path string, files map[string]string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}

	slices.Sort(names)

	for _, name := range names {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
		if strings.HasSuffix(name, "/") {
			h.SetMode(0o755 | os.ModeDir)
		} else {
			h.SetMode(0o644)
		}

		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := fw.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// libComposerJSON is the composer.json of a generated package.
func libComposerJSON(name, extra string) string {
	return `{
    "name": "` + name + `",
    "description": "A generated package.",
    "type": "library",
    "license": "MIT",
    "autoload": {"psr-4": {"Acme\\Lib\\": "src/"}}` + extra + `
}
`
}

// archiveEntry is one entry of a generated dist archive.
type archiveEntry struct {
	name    string
	mode    os.FileMode // permission bits; os.ModeSymlink/os.ModeDir for those kinds
	content string      // file content or symlink target
	link    string      // tar hard link target
}

// distEntries is a package tree with what extractors must reproduce:
// executable and private files, an empty directory, a symlink to a file
// and one to a directory, nested directories without their own entries.
func distEntries(prefix string) []archiveEntry {
	return []archiveEntry{
		{name: prefix + "composer.json", mode: 0o644, content: "{\"name\": \"acme/dist\"}\n"},
		{name: prefix + "bin/", mode: os.ModeDir | 0o755},
		{name: prefix + "bin/run", mode: 0o755, content: "#!/bin/sh\necho run\n"},
		{name: prefix + "src/deep/nested/File.php", mode: 0o644, content: "<?php\n"},
		{name: prefix + "private.txt", mode: 0o600, content: "private\n"},
		{name: prefix + "group-writable.txt", mode: 0o664, content: "gw\n"},
		{name: prefix + "empty/", mode: os.ModeDir | 0o755},
		{name: prefix + "link-to-file", mode: os.ModeSymlink | 0o777, content: "src/deep/nested/File.php"},
		{name: prefix + "link-to-dir", mode: os.ModeSymlink | 0o777, content: "src/deep"},
	}
}

func writeZip(t *testing.T, path string, entries []archiveEntry) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)

	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
		h.SetMode(e.mode)

		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}

		if e.mode&os.ModeDir == 0 {
			if _, err := fw.Write([]byte(e.content)); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func tarBytes(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()

	var buf bytes.Buffer

	w := tar.NewWriter(&buf)

	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: int64(e.mode.Perm()), ModTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Format: tar.FormatPAX}

		switch {
		case e.link != "":
			h.Typeflag = tar.TypeLink
			h.Linkname = e.link
		case e.mode&os.ModeSymlink != 0:
			h.Typeflag = tar.TypeSymlink
			h.Linkname = e.content
		case e.mode&os.ModeDir != 0:
			h.Typeflag = tar.TypeDir
		default:
			h.Typeflag = tar.TypeReg
			h.Size = int64(len(e.content))
		}

		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}

		if h.Typeflag == tar.TypeReg {
			if _, err := w.Write([]byte(e.content)); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	w := gzip.NewWriter(&buf)
	w.ModTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// compressWith pipes data through a compressor (bzip2, xz).
func compressWith(t *testing.T, tool string, data []byte) []byte {
	t.Helper()

	cmd := exec.Command(tool, "-c")
	cmd.Stdin = bytes.NewReader(data)

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}

	return out
}

// distArchives writes the dists scenario's archives to <root>/dists.
func distArchives(t *testing.T, root string) {
	t.Helper()

	dir := filepath.Join(root, "dists")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	write := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// one top-level directory (stripped) / several entries at the root
	writeZip(t, filepath.Join(dir, "zip-nested.zip"), distEntries("package-1.0.0/"))
	writeZip(t, filepath.Join(dir, "zip-flat.zip"), distEntries(""))

	tarEntries := append(distEntries("pkg/"), archiveEntry{name: "pkg/hardlink.php", mode: 0o644, link: "pkg/src/deep/nested/File.php"})
	plain := tarBytes(t, tarEntries)

	write("tar-plain.tar", plain)
	write("tar-gz.tar.gz", gzipBytes(t, plain))
	write("tar-bz2.tar.bz2", compressWith(t, "bzip2", plain))
	write("xz.tar.xz", compressWith(t, "xz", plain))
	write("single.php.gz", gzipBytes(t, []byte("<?php\necho 'single';\n")))
	write("tool.phar", []byte("#!/usr/bin/env php\n<?php\necho 'tool';\n"))
}
