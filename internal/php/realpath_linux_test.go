package php

import (
	"os"
	"path/filepath"
	"testing"
)

// The kernel's resolution gives what the component walk gives, for every
// shape of path: links before "..", trailing separators on files and
// directories, missing and dangling paths, loops.
func TestEvalSymlinksAsTheWalk(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"a/b/c", "x"} {
		if err := os.MkdirAll(filepath.Join(dir, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a/f"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		"l":        "a/b",
		"a/rel":    "../x",
		"abs":      dir + "/a/b/c",
		"dangling": "nope",
		"loop1":    "loop2",
		"loop2":    "loop1",
		"lf":       "a/f",
	} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Fatal(err)
		}
	}

	for _, p := range []string{
		"", "/", "/a", "/a/", "/a//b/./c", "/l", "/l/", "/l/../x", "/l/c/..", "/a/rel", "/a/rel/../a/f",
		"/abs", "/abs/..", "/a/f", "/a/f/", "/a/f/..", "/a/f/x", "/lf", "/lf/", "/dangling", "/dangling/..",
		"/loop1", "/missing", "/missing/..", "/a/b/../../x/.",
	} {
		want, wantErr := filepath.EvalSymlinks(dir + p)
		got, err := EvalSymlinks(dir + p)
		if got != want || (err == nil) != (wantErr == nil) {
			t.Errorf("EvalSymlinks(%q) = %q, %v; the walk gives %q, %v", p, got, err, want, wantErr)
		}
	}
}
