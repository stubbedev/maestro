//go:build unix

package util

import (
	"os"
	"path/filepath"
	"testing"
)

// SearchPath finds the first executable regular file named like the tool
// in the absolute directories of PATH up to a relative one, and looks
// once per run: a tool that appears later in the run is not seen.
func TestSearchPath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	dirs := make([]string, 4)
	for i := range dirs {
		dirs[i] = filepath.Join(base, string(rune('a'+i)))
		if err := os.Mkdir(dirs[i], 0o755); err != nil {
			t.Fatal(err)
		}
	}

	write := func(dir string, mode os.FileMode) {
		t.Helper()

		if err := os.WriteFile(filepath.Join(dir, "tool"), nil, mode); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.Mkdir(filepath.Join(dirs[0], "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(dirs[1], 0o644)
	write(dirs[2], 0o755)

	path := dirs[0] + string(os.PathListSeparator) + dirs[1] + string(os.PathListSeparator) + dirs[2]

	s := SearchPath(path, "tool")
	if s.Relative || len(s.Hits) != 3 || !s.Hits[0].Dir || s.Hits[1].Executable {
		t.Fatalf("search: %+v", s)
	}

	if got, want := s.Executable(), filepath.Join(dirs[2], "tool"); got != want {
		t.Errorf("executable %q, want %q", got, want)
	}

	// once per run
	write(dirs[3], 0o755)

	withRelative := dirs[3] + string(os.PathListSeparator) + "relative" + string(os.PathListSeparator) + dirs[2]
	if s := SearchPath(path, "other"); len(s.Hits) != 0 {
		t.Errorf("other: %+v", s)
	}

	if err := os.Remove(filepath.Join(dirs[2], "tool")); err != nil {
		t.Fatal(err)
	}

	if s := SearchPath(path, "tool"); s.Executable() != filepath.Join(dirs[2], "tool") {
		t.Errorf("the second search looked again: %+v", s)
	}

	// nothing after a relative directory is decided
	if s := SearchPath(withRelative, "tool"); !s.Relative || s.Executable() != filepath.Join(dirs[3], "tool") || len(s.Hits) != 1 {
		t.Errorf("with a relative directory: %+v", s)
	}
}
