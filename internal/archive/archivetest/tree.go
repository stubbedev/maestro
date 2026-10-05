// Package archivetest holds what the archive and store tests share: crafted
// archive corpora, runners for the real extractors Composer uses (unzip,
// PharData, GNU tar, gzip), and snapshots of extracted trees to compare
// entry by entry.
package archivetest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Node is one entry of an extracted tree.
type Node struct {
	// Data is a file's content or a symlink's target.
	Data string
	// Perm holds the permission and setuid/setgid/sticky bits.
	Perm fs.FileMode
	// Kind is 'd', 'f' or 'l'.
	Kind byte
}

func (n Node) String() string {
	switch n.Kind {
	case 'l':
		return fmt.Sprintf("symlink -> %q", n.Data)
	case 'd':
		return fmt.Sprintf("dir %v", n.Perm)
	}

	data := n.Data
	if len(data) > 40 {
		data = data[:40] + "..."
	}

	return fmt.Sprintf("file %v %d bytes %q", n.Perm, len(n.Data), data)
}

// Tree maps slash-separated paths relative to the package directory ("" is
// the directory itself) to their nodes.
type Tree map[string]Node

// Snapshot records the tree at dir without following symlinks.
func Snapshot(dir string) (Tree, error) {
	t := Tree{}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		if rel == "." {
			rel = ""
		}

		n := Node{Perm: info.Mode() & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)}

		switch {
		case info.IsDir():
			n.Kind = 'd'
		case info.Mode().IsRegular():
			n.Kind = 'f'

			data, err := readAll(path, info.Mode())
			if err != nil {
				return err
			}

			n.Data = string(data)
		case info.Mode()&fs.ModeSymlink != 0:
			n.Kind, n.Perm = 'l', 0

			if n.Data, err = os.Readlink(path); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s: unexpected file type %v", rel, info.Mode().Type())
		}

		t[filepath.ToSlash(rel)] = n

		return nil
	})

	return t, err
}

// readAll reads a file even when its mode denies the owner reading it.
func readAll(path string, mode fs.FileMode) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil || !errors.Is(err, fs.ErrPermission) {
		return data, err
	}

	if err := os.Chmod(path, mode.Perm()|0o400); err != nil {
		return nil, err
	}

	defer func() { _ = os.Chmod(path, mode) }()

	return os.ReadFile(path)
}

// PackageRoot applies ArchiveDownloader's rule to an extraction directory:
// a single top-level directory, ignoring .DS_Store, is the package. A lone
// symlink to a directory is reported as an error, which is how maestro
// treats it.
func PackageRoot(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	var only fs.DirEntry

	count := 0

	for _, e := range entries {
		if e.Name() != ".DS_Store" {
			only = e
			count++
		}
	}

	if count != 1 {
		return dir, nil
	}

	path := filepath.Join(dir, only.Name())

	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return dir, nil //nolint:nilerr // a dangling link is not a directory.
	}

	if only.Type()&fs.ModeSymlink != 0 {
		return "", errors.New("the only top-level entry is a symlink to a directory")
	}

	return path, nil
}

// Diff lists the differences between two trees, in path order.
func Diff(want, got Tree) []string {
	paths := make([]string, 0, len(want)+len(got))
	for p := range want {
		paths = append(paths, p)
	}

	for p := range got {
		if _, ok := want[p]; !ok {
			paths = append(paths, p)
		}
	}

	slices.Sort(paths)

	var out []string

	for _, p := range paths {
		w, inWant := want[p]
		g, inGot := got[p]

		switch {
		case !inWant:
			out = append(out, fmt.Sprintf("%q: unexpected %v", p, g))
		case !inGot:
			out = append(out, fmt.Sprintf("%q: missing %v", p, w))
		case w != g:
			out = append(out, fmt.Sprintf("%q: want %v, got %v", p, w, g))
		}
	}

	return out
}

// RemoveAll removes dir even where extraction left directories without
// write or search permission. WalkDir visits a directory before listing
// it, so opening it up first lets the walk descend.
func RemoveAll(dir string) error {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, _ error) error {
		if d != nil && d.IsDir() {
			_ = os.Chmod(p, 0o700) //nolint:gosec // directories need search permission.
		}

		return nil
	})

	return os.RemoveAll(dir)
}

// Short abbreviates a list of differences for a test failure.
func Short(diffs []string) string {
	if len(diffs) > 12 {
		diffs = append(diffs[:12:12], fmt.Sprintf("... and %d more", len(diffs)-12))
	}

	return strings.Join(diffs, "\n")
}
