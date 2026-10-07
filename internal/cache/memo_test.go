package cache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/io"
)

// TestReadFileMemo checks that reads of a cached file give its current
// contents whether they come from memory or from the file: unchanged,
// replaced (a cache write), rewritten in place with another size, written
// and removed through the cache.
func TestReadFileMemo(t *testing.T) {
	root := t.TempDir()
	c, err := New(io.NewNullIO(), root, "", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "provider-a.json")
	read := func(want string) {
		t.Helper()
		for range 2 {
			if got, ok, err := c.Read("provider-a.json"); err != nil || !ok || got != want {
				t.Fatalf("Read = %q, %v, %v; want %q", got, ok, err, want)
			}
			if got, ok := c.Peek("provider-a.json"); !ok || got != want {
				t.Fatalf("Peek = %q, %v; want %q", got, ok, want)
			}
			if got, ok := c.Peeker()("provider-a.json"); !ok || got != want {
				t.Fatalf("Peeker = %q, %v; want %q", got, ok, want)
			}
			got, found, err := c.ReadAll([]string{"provider-a.json"}, nil)
			if err != nil || !found[0] || got[0] != want {
				t.Fatalf("ReadAll = %q, %v, %v; want %q", got, found, err, want)
			}
		}
	}

	if err := os.WriteFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	read("one")

	// replaced by another file, as a cache write does
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	read("two")

	// rewritten in place
	if err := os.WriteFile(path, []byte("three!"), 0o600); err != nil {
		t.Fatal(err)
	}
	read("three!")

	if ok, err := c.Write("provider-a.json", "four"); !ok || err != nil {
		t.Fatalf("Write = %v, %v", ok, err)
	}
	read("four")

	if ok, err := c.Remove("provider-a.json"); !ok || err != nil {
		t.Fatalf("Remove = %v, %v", ok, err)
	}
	if got, ok, err := c.Read("provider-a.json"); err != nil || ok || got != "" {
		t.Fatalf("Read after Remove = %q, %v, %v", got, ok, err)
	}
	if _, ok := c.Peek("provider-a.json"); ok {
		t.Fatal("Peek after Remove found the file")
	}
}
