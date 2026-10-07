package classmap

import (
	"crypto/sha256"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// releaseParseOf parses the files of dir as the package store would hand
// them to a ReleaseParse while inserting them, and returns what it keeps.
func releaseParseOf(t *testing.T, p Parser, dir string, accept func(string) bool) map[contentKey][]string {
	t.Helper()
	rp := NewReleaseParse(p, accept)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		sum := sha256.Sum256(data)
		rp.File(filepath.ToSlash(rel), data, &sum)

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := rp.Merge(nil)
	results := decodeResults(data, releasesHeader())
	if data != nil && results == nil {
		t.Fatal("the merged results do not decode")
	}

	return results
}

// A file parsed while its release is inserted gets exactly the classes a
// scan's parse of the installed file finds, by every parser; a content
// that does not parse is not kept (the scan reports it).
func TestReleaseParse_IsTheScansParse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	extra := map[string]string{
		"short.php":    "<? class Short {} ?>",
		"empty.php":    "",
		"blank.php":    " \n\t",
		"binary.php":   "\x00\x01\x02",
		"enum.php":     "<?php enum Suit: string { case A = 'a'; }",
		"heredoc.php":  "<?php $x = <<<EOT\n  class NotOne {}\n EOT;\nclass After {}",
		"readonly.php": "<?php readonly class R {}",
	}
	for name, content := range extra {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var sources []string
	for _, root := range []string{"testdata", dir} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() {
				sources = append(sources, path)
			}

			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	all := func(string) bool { return true }
	for _, version := range fuzzVersions {
		for _, short := range []bool{true, false} {
			p := Parser{ShortOpenTag: short, PHPVersionID: version}
			kept := releaseParseOf(t, p, "testdata", all)
			maps.Copy(kept, releaseParseOf(t, p, dir, all))
			for _, path := range sources {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				want, wantErr := p.FindClasses(path)
				got, ok := kept[contentKeyOf(p, data)]
				switch {
				case wantErr != nil && ok:
					t.Errorf("%v %s: kept %q, the scan fails: %v", p, path, got, wantErr)
				case wantErr == nil && !ok:
					t.Errorf("%v %s: not kept, the scan finds %q", p, path, want)
				case !slices.Equal(got, want):
					t.Errorf("%v %s: kept %q, the scan finds %q", p, path, got, want)
				}
			}
		}
	}
}

// The scans of a project whose files were parsed as their release was
// inserted read none of them, and find what they find without.
func TestReleaseParse_ScansReadNothing(t *testing.T) {
	t.Parallel()

	const stamp = 1_000_000_000
	project := t.TempDir()
	files := []StampedFile{
		stampFile(t, project, "src/Foo.php", "<?php class Foo {}", stamp),
		stampFile(t, project, "src/Bar.php", "<?php namespace N; interface Bar {}", stamp+1),
		stampFile(t, project, "src/Empty.php", "", stamp+2),
	}
	rp := NewReleaseParse(DefaultParser, func(path string) bool { return strings.HasPrefix(path, "src/") })
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(project, filepath.FromSlash(f.Path)))
		if err != nil {
			t.Fatal(err)
		}
		rp.File(f.Path, data, &f.Sum)
	}
	data, ok := rp.Merge(nil)
	if !ok {
		t.Fatal("nothing parsed")
	}
	if _, again := rp.Merge(data); again {
		t.Error("merging into the results kept found something new")
	}

	release := &memRelease{name: "a/b", files: files, results: data}
	cache := NewParseCache()
	cache.AddReleases([]Release{release})
	want := scanWithCache(t, NewParseCache(), project)
	if got := scanWithCache(t, cache, project); !slices.Equal(got, want) {
		t.Fatalf("scan: %q, want %q", got, want)
	}
	if n := cache.reads.Load(); n != 0 {
		t.Errorf("the scan read %d files, want 0", n)
	}
	cache.Save()
	if release.writes != 0 {
		t.Error("the scan kept results again")
	}
}
