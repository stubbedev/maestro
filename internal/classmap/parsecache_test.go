package classmap

import (
	"crypto/sha256"
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

// memRelease is a Release kept in memory.
type memRelease struct {
	name    string
	files   []StampedFile
	results []byte
	writes  int
}

func (r *memRelease) Name() string                 { return r.name }
func (r *memRelease) Files() []StampedFile         { return r.files }
func (r *memRelease) ReadResults() ([]byte, error) { return r.results, nil }
func (r *memRelease) WriteResults(data []byte) error {
	r.results = append([]byte(nil), data...)
	r.writes++

	return nil
}

// stampFile writes a release file into the package directory dir as the
// package store imports it: with the modification time its stamp says.
func stampFile(t *testing.T, dir, rel, content string, stamp int64) StampedFile {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, time.Unix(stamp, 0), time.Unix(stamp, 0)); err != nil {
		t.Fatal(err)
	}

	return StampedFile{Path: rel, Size: int64(len(content)), ModTime: stamp, Sum: sha256.Sum256([]byte(content))}
}

// Files of a package store release are known by their stamps: the first
// scan parses them and keeps the results with the release; any later
// scan, in this project or another one installing the release, reads
// none of them. A file that no longer shows its stamp is read.
func TestParseCache_Releases(t *testing.T) {
	const stamp = 1_000_000_000
	project := t.TempDir()
	files := []StampedFile{
		stampFile(t, project, "src/Foo.php", "<?php class Foo {}", stamp),
		stampFile(t, project, "src/Bar.php", "<?php class Bar {}", stamp+1),
	}
	release := &memRelease{name: "a/b", files: files}
	scan := func(dir string, want ...string) int64 {
		t.Helper()
		cache := NewParseCache()
		cache.AddReleases([]Release{release})
		before := fileReads.Load()
		if got := scanWithCache(t, cache, dir); !slices.Equal(got, want) {
			t.Fatalf("scan of %s: %q, want %q", dir, got, want)
		}
		cache.Save()

		return fileReads.Load() - before
	}

	if n := scan(project, "Bar", "Foo"); n != 2 {
		t.Errorf("first scan read %d files, want 2", n)
	}
	if release.writes != 1 {
		t.Fatalf("results kept %d times, want once", release.writes)
	}
	if n := scan(project, "Bar", "Foo"); n != 0 {
		t.Errorf("second scan read %d files, want 0", n)
	}
	if release.writes != 1 {
		t.Errorf("unchanged results kept again")
	}

	// another project with the same release
	other := t.TempDir()
	stampFile(t, other, "src/Foo.php", "<?php class Foo {}", stamp)
	stampFile(t, other, "src/Bar.php", "<?php class Bar {}", stamp+1)
	if n := scan(other, "Bar", "Foo"); n != 0 {
		t.Errorf("scan of another project read %d files, want 0", n)
	}

	// edited: same size, new modification time
	if err := os.WriteFile(filepath.Join(other, "src", "Foo.php"), []byte("<?php class Baz {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := scan(other, "Bar", "Baz"); n != 1 {
		t.Errorf("scan after an edit read %d files, want 1", n)
	}

	// the stamp at another path, and a stamp with a fraction of a second
	moved := t.TempDir()
	stampFile(t, moved, "lib/Foo.php", "<?php class Foo {}", stamp)
	fraction := stampFile(t, moved, "src/Bar.php", "<?php class Qux {}", stamp+1)
	if err := os.Chtimes(filepath.Join(moved, "src", "Bar.php"), time.Unix(stamp+1, 5), time.Unix(stamp+1, 5)); err != nil {
		t.Fatal(err)
	}
	_ = fraction
	if n := scan(moved, "Foo", "Qux"); n != 2 {
		t.Errorf("scan of moved and touched files read %d files, want 2", n)
	}
}

// Two release files of the same size, stamp and path but different
// contents leave the file unknown: it is read.
func TestParseCache_ReleasesAmbiguousStamp(t *testing.T) {
	const stamp = 1_000_000_000
	dir := t.TempDir()
	foo := stampFile(t, dir, "src/A.php", "<?php class Foo {}", stamp)
	bar := foo
	bar.Sum = sha256.Sum256([]byte("<?php class Bar {}"))
	cache := NewParseCache()
	cache.AddReleases([]Release{&memRelease{name: "a", files: []StampedFile{foo}}, &memRelease{name: "b", files: []StampedFile{bar}}})
	before := fileReads.Load()
	if got := scanWithCache(t, cache, dir); !slices.Equal(got, []string{"Foo"}) {
		t.Fatalf("scan: %q", got)
	}
	if n := fileReads.Load() - before; n != 1 {
		t.Errorf("ambiguous file read %d times, want 1", n)
	}
}

// Kept results of another format or binary, or damaged, are ignored.
func TestParseCache_ReleaseResultsDecoding(t *testing.T) {
	header := releasesHeader()
	results := map[contentKey][]string{
		{sum: sha256.Sum256([]byte("a")), parser: DefaultParser.key()}: {"A", `N\B`},
		{sum: sha256.Sum256([]byte("b")), parser: parserKey{true, 3}}:  nil,
	}
	data := encodeResults(header, results)
	got := decodeResults(data, header)
	if len(got) != len(results) {
		t.Fatalf("decoded %d results, want %d", len(got), len(results))
	}
	for k, v := range results {
		if !slices.Equal(got[k], v) {
			t.Errorf("result %x: %q, want %q", k.sum[:4], got[k], v)
		}
	}
	for _, bad := range [][]byte{nil, []byte("maestro classmap release 0\n"), data[:len(data)-1], append(append([]byte(nil), data...), 1)} {
		if decodeResults(bad, header) != nil {
			t.Errorf("decoded %q", bad)
		}
	}
}

func TestHasPathSuffix(t *testing.T) {
	for _, c := range []struct {
		path, rel string
		want      bool
	}{
		{"/v/a/b/src/Foo.php", "src/Foo.php", true},
		{`C:\v\a\b\src\Foo.php`, "src/Foo.php", true},
		{"/v/a/b/xsrc/Foo.php", "src/Foo.php", false},
		{"src/Foo.php", "src/Foo.php", false},
		{"/v/src/Foo.phps", "src/Foo.php", false},
		{"/v/src\\Foo.php", "src\\Foo.php", true},
	} {
		if got := hasPathSuffix(c.path, c.rel); got != c.want {
			t.Errorf("hasPathSuffix(%q, %q) = %t", c.path, c.rel, got)
		}
	}
}

// A run that met identities too recent to trust saves the cache file
// again, so that a later run can trust them (git rewrites an index with
// racily clean entries); without that they would never become trusted.
func TestParseCache_RacyEntriesRewriteTheFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.php"), []byte("<?php class Foo {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := statKey(filepath.Join(dir, "a.php")); !ok {
		t.Skip("no file identities on this platform")
	}
	cacheFile := filepath.Join(t.TempDir(), "cache.bin")
	first := readCache(cacheFile)
	scanWithCache(t, first, dir)
	first.Save()

	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(cacheFile, old, old); err != nil {
		t.Fatal(err)
	}
	margin := statTrustMargin
	t.Cleanup(func() { statTrustMargin = margin })
	statTrustMargin = time.Minute

	// the file's times are not a minute older than the cache file's (an
	// hour in the past now): racy, read again, and the cache file saved
	later := readCache(cacheFile)
	scanWithCache(t, later, dir)
	later.Save()
	info, err := os.Stat(cacheFile)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().After(old) {
		t.Error("the cache file was not saved again after a racy entry")
	}
}
