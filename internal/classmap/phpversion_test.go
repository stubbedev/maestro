package classmap

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPHPVersionOf(t *testing.T) {
	for id, want := range map[int]phpVersion{
		0: php84, -1: php84, 50600: php72, 70205: php72, 70333: php73, 70433: php74, 80030: php80,
		80100: php81, 80234: php82, 80335: php83, 80425: php84, 80511: php85, 80600: php85, 90000: php85,
	} {
		if got := phpVersionOf(id); got != want {
			t.Errorf("phpVersionOf(%d) = %d, want %d", id, got, want)
		}
	}
}

// The classes found in a file follow the parser's PHP, also through the
// parse cache (in memory and on disk): "#[" opens a comment before PHP 8.0,
// and enums are only looked for on PHP >= 8.1.
func TestParser_FollowsPHPVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.php"), []byte("<?php #[Attr] class A {}\nenum E {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cacheFile := filepath.Join(t.TempDir(), "cache.bin")
	want := map[int][]string{70400: nil, 80000: {"A"}, 80100: {"A", "E"}, 0: {"A", "E"}}
	for _, run := range []string{"first", "from the cache file"} {
		cache := NewParseCache()
		cache.UseFile(cacheFile)
		for _, id := range []int{70400, 80000, 80100, 0} {
			for range 2 { // the second scan reads the in-memory cache
				g := NewGenerator(nil).SetParseCache(cache)
				g.Parser = Parser{ShortOpenTag: true, PHPVersionID: id}
				g.Prefetch([]ScanRequest{{Path: dir}})
				if err := g.ScanPaths(dir, nil, Classmap, "", nil); err != nil {
					t.Fatal(err)
				}
				got := g.ClassMap().Classes()
				slices.Sort(got)
				if !slices.Equal(got, want[id]) {
					t.Fatalf("%s run, PHP_VERSION_ID %d: %q, want %q", run, id, got, want[id])
				}
			}
		}
		cache.Save()
	}
}
