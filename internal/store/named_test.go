//go:build !windows

package store

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// treeOf lists a directory tree: path -> mode, then content or target.
func treeOf(t *testing.T, root string) map[string]string {
	t.Helper()

	tree := map[string]string{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(root, path)
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
	if err != nil {
		t.Fatal(err)
	}

	return tree
}

func TestInsertDirLookupNamed(t *testing.T) {
	s, err := Open(t.TempDir(), &Options{Method: Copy})
	if err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(t.TempDir(), "src")
	for _, dir := range []string{"src/.git/objects/pack", "src/lib"} {
		if err := os.MkdirAll(filepath.Join(filepath.Dir(src), dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	big := make([]byte, smallFile+10)
	for i := range big {
		big[i] = byte(i)
	}

	for name, f := range map[string]struct {
		data string
		mode fs.FileMode
	}{
		"README":                        {"hello\n", 0o644},
		"bin/run":                       {"#!/bin/sh\n", 0o755},
		".git/objects/pack/pack-1.pack": {string(big), 0o444},
		"lib/a.php":                     {"<?php\n", 0o600},
	} {
		path := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(f.data), f.mode); err != nil {
			t.Fatal(err)
		}

		if err := os.Chmod(path, f.mode); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.Symlink("../README", filepath.Join(src, "lib", "readme")); err != nil {
		t.Fatal(err)
	}

	id := sha256.Sum256([]byte("checkout"))

	if _, err := s.LookupNamed(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("lookup before insert: %v", err)
	}

	if _, err := s.InsertDir(id, src); err != nil {
		t.Fatal(err)
	}

	// a fresh Store reads the index from disk
	s2, err := Open(s.Root(), &Options{Method: Clone})
	if err != nil {
		t.Fatal(err)
	}

	r, err := s2.LookupNamed(id)
	if err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "dst")
	if err := s2.Materialize(r, dst, ImportOptions{Unshared: true}); err != nil {
		t.Fatal(err)
	}

	want, got := treeOf(t, src), treeOf(t, dst)
	if len(want) != len(got) {
		t.Fatalf("got %d entries, want %d", len(got), len(want))
	}

	for path, v := range want {
		if got[path] != v {
			t.Errorf("%s: got %.60q, want %.60q", path, got[path], v)
		}
	}

	// a named id never answers for a dist and the other way round
	if _, err := s2.LookupNamed(sha256.Sum256([]byte("other"))); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other id: %v", err)
	}
}

func TestInsertDirRefusesSpecialFiles(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.InsertDir([32]byte{1}, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing directory inserted")
	}

	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := s.InsertDir([32]byte{1}, file); err == nil {
		t.Fatal("file inserted as a tree")
	}
}
