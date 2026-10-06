package classmap

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func scanWithCache(t *testing.T, cache *ParseCache, dir string) []string {
	t.Helper()
	g := NewGenerator(nil).SetParseCache(cache)
	g.Prefetch([]ScanRequest{{Path: dir}})
	if err := g.ScanPaths(dir, nil, Classmap, "", nil); err != nil {
		t.Fatal(err)
	}
	classes := g.ClassMap().Classes()
	slices.Sort(classes)

	return classes
}

// A cached parse result is only used for the contents it was found in:
// rewriting a file (same size, modification time put back) is noticed, in
// the run and by the cache file of later runs.
func TestParseCache_FollowsContents(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.php")
	write := func(content string) {
		info, statErr := os.Stat(file)
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if statErr == nil {
			if err := os.Chtimes(file, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
		}
	}
	cacheFile := filepath.Join(t.TempDir(), "cache.bin")

	write("<?php class Foo {}")
	cache := NewParseCache()
	cache.UseFile(cacheFile)
	if got := scanWithCache(t, cache, dir); !slices.Equal(got, []string{"Foo"}) {
		t.Fatalf("first scan: %q", got)
	}
	cache.Save()

	time.Sleep(10 * time.Millisecond) // a new change time
	write("<?php class Bar {}")
	if got := scanWithCache(t, cache, dir); !slices.Equal(got, []string{"Bar"}) {
		t.Fatalf("scan after the rewrite: %q", got)
	}
	cache.Save()

	// a later run, from the file
	for _, content := range []string{"<?php class Foo {}", "<?php class Bar {}", "<?php class Baz {}"} {
		write(content)
		later := NewParseCache()
		later.UseFile(cacheFile)
		want := content[len("<?php class ") : len(content)-len(" {}")]
		if got := scanWithCache(t, later, dir); !slices.Equal(got, []string{want}) {
			t.Fatalf("later run with %q: %q", content, got)
		}
		later.Save()
	}
}

// A later run finds an unchanged file's result through the identity the
// cache file recorded for it, without reading the file; an identity not
// safely older than the cache file (a change within the same timestamp
// tick could hide behind it) is not trusted, and the file is read.
func TestParseCache_IdentityIndex(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.php")
	if err := os.WriteFile(file, []byte("<?php class Foo {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := statKey(file); !ok {
		t.Skip("no file identities on this platform")
	}
	cacheFile := filepath.Join(t.TempDir(), "cache.bin")

	first := NewParseCache()
	first.UseFile(cacheFile)
	if got := scanWithCache(t, first, dir); !slices.Equal(got, []string{"Foo"}) {
		t.Fatalf("first scan: %q", got)
	}
	first.Save()

	margin := statTrustMargin
	t.Cleanup(func() { statTrustMargin = margin })
	scan := func(trust time.Duration) int64 {
		t.Helper()
		statTrustMargin = trust
		before := fileReads.Load()
		if got := scanWithCache(t, readCache(cacheFile), dir); !slices.Equal(got, []string{"Foo"}) {
			t.Fatalf("scan: %q", got)
		}

		return fileReads.Load() - before
	}

	if n := scan(-time.Hour); n != 0 {
		t.Errorf("unchanged file read %d times, want 0", n)
	}
	if n := scan(time.Hour); n != 1 {
		t.Errorf("racily recorded file read %d times, want 1", n)
	}
}

func readCache(path string) *ParseCache {
	c := NewParseCache()
	c.UseFile(path)

	return c
}
