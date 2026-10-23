package fsstate

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// pruneDir creates dir's entries, each modified age ago, and returns a
// function listing what is left in it.
func pruneDir(t *testing.T, entries map[string]time.Duration) (string, func() []string) {
	t.Helper()
	dir := t.TempDir()
	now := time.Now()
	for name, age := range entries {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}

	return dir, func() []string {
		t.Helper()
		left, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(left))
		for _, e := range left {
			names = append(names, e.Name())
		}

		return names
	}
}

// Entries past the age limit go, then the least recently modified beyond
// the bound, abandoned temporary files and directories; a temporary file
// still being written stays.
func TestPrune(t *testing.T) {
	dir, left := pruneDir(t, map[string]time.Duration{
		"old":            48 * time.Hour,
		"a":              4 * time.Hour,
		"b":              3 * time.Hour,
		"c":              2 * time.Hour,
		"d":              time.Hour,
		TempPrefix + "1": 2 * TempMaxAge,
		TempPrefix + "2": time.Minute,
	})
	if err := os.Mkdir(filepath.Join(dir, "layout-1"), 0o755); err != nil {
		t.Fatal(err)
	}

	Prune(dir, 2, 24*time.Hour)

	if got, want := left(), []string{TempPrefix + "2", "c", "d"}; !slices.Equal(got, want) {
		t.Errorf("left %q, want %q", got, want)
	}
}

// Without an age limit, a directory within the bound is left alone, and
// one past it keeps the most recent entries.
func TestPrune_NoAgeLimit(t *testing.T) {
	dir, left := pruneDir(t, map[string]time.Duration{"a": 72 * time.Hour, "b": 48 * time.Hour})

	Prune(dir, 2, 0)
	if got, want := left(), []string{"a", "b"}; !slices.Equal(got, want) {
		t.Errorf("within the bound: left %q, want %q", got, want)
	}

	Prune(dir, 1, 0)
	if got, want := left(), []string{"b"}; !slices.Equal(got, want) {
		t.Errorf("past the bound: left %q, want %q", got, want)
	}
}
