package classmap

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// recordFixture is a directory with a classmap rule (lib/, holding an
// ambiguous class) and a PSR-4 rule (src/, holding a violation).
func recordFixture(t *testing.T) (dir string, scans []RecordScan) {
	t.Helper()
	dir = t.TempDir()
	for path, content := range map[string]string{
		"lib/a.php":     `<?php class Dup {} class A {}`,
		"lib/sub/b.php": `<?php class Dup {}`,
		"src/C.php":     `<?php namespace App; class C {}`,
		"src/Wrong.php": `<?php namespace App; class NotWrong {}`,
		"src/notes.txt": `not scanned`,
	} {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return dir, []RecordScan{
		{Path: filepath.Join(dir, "lib"), Type: Classmap},
		{Path: filepath.Join(dir, "src"), Type: PSR4, Namespace: `App\`},
	}
}

// recorded is the class map of the scans, from the record if it is valid
// (hit), else scanned and recorded.
func recorded(t *testing.T, records, id, anchor string, scans []RecordScan) (m *ClassMap, hit bool) {
	t.Helper()

	return recordedWith(t, records, id, anchor, scans, nil)
}

// recordedWith is recorded, with scans through cache.
func recordedWith(t *testing.T, records, id, anchor string, scans []RecordScan, cache *ParseCache) (m *ClassMap, hit bool) {
	t.Helper()
	rec, ok := NewRecord(records, id, []string{anchor}, DefaultParser, nil, scans)
	if !ok {
		t.Fatal("the scans cannot be recorded")
	}
	if m, ok := rec.Load(); ok {
		return m, true
	}
	g := NewGenerator(nil).AvoidDuplicateScans(nil).SetParseCache(cache)
	g.StartRecording()
	for _, s := range scans {
		if err := g.ScanPaths(s.Path, s.Excluded, s.Type, s.Namespace, nil); err != nil {
			t.Fatal(err)
		}
	}
	g.SaveRecord(rec)

	return g.ClassMap(), false
}

type classMapView struct {
	Map        [][2]string
	Ambiguous  []AmbiguousClass
	Violations []string
}

func viewOf(t *testing.T, m *ClassMap) classMapView {
	t.Helper()
	var v classMapView
	for class, path := range m.Map() {
		v.Map = append(v.Map, [2]string{class, path})
	}
	var err error
	if v.Ambiguous, err = m.AmbiguousClasses(nil); err != nil {
		t.Fatal(err)
	}
	v.Violations = m.PsrViolations()

	return v
}

func trustRecent(t *testing.T) {
	t.Helper()
	margin := statTrustMargin
	t.Cleanup(func() { statTrustMargin = margin })
	statTrustMargin = -time.Hour
}

func TestRecord_KeepsTheClassMap(t *testing.T) {
	trustRecent(t)
	dir, scans := recordFixture(t)
	records := t.TempDir()

	scanned, hit := recorded(t, records, "p", dir, scans)
	if hit {
		t.Fatal("a record was found before any was written")
	}
	want := viewOf(t, scanned)
	if len(want.Ambiguous) != 1 || len(want.Violations) != 1 {
		t.Fatalf("the fixture should have an ambiguous class and a violation: %+v", want)
	}
	got, hit := recorded(t, records, "p", dir, scans)
	if !hit {
		t.Fatal("the record was not used")
	}
	if !reflect.DeepEqual(viewOf(t, got), want) {
		t.Errorf("recorded class map\n%+v\nwant\n%+v", viewOf(t, got), want)
	}
	// the class map taken from a record behaves as a scanned one
	got.ClearPsrViolationsByPath(filepath.Join(dir, "src"))
	got.AddClass("Extra", filepath.Join(dir, "extra.php"))
	if got.PsrViolations() != nil || !got.HasClass("Extra") {
		t.Error("the recorded class map cannot be changed")
	}
}

func TestRecord_NotUsedAfterChanges(t *testing.T) {
	for name, change := range map[string]func(t *testing.T, dir string){
		"rewritten file": func(t *testing.T, dir string) {
			file := filepath.Join(dir, "lib/sub/b.php")
			info, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte(`<?php class Dip {}`), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(file, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
		},
		"new file": func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "lib/sub/new.php"), []byte(`<?php class Fresh {}`), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"removed file": func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, "src/Wrong.php")); err != nil {
				t.Fatal(err)
			}
		},
		"directory symlinked elsewhere": func(t *testing.T, dir string) {
			moved := filepath.Join(t.TempDir(), "lib")
			if err := os.Rename(filepath.Join(dir, "lib"), moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(moved, filepath.Join(dir, "lib")); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			trustRecent(t)
			dir, scans := recordFixture(t)
			records := t.TempDir()
			recorded(t, records, "p", dir, scans)
			time.Sleep(20 * time.Millisecond) // a new change time
			change(t, dir)

			got, hit := recorded(t, records, "p", dir, scans)
			if hit {
				t.Fatal("the record was used after the change")
			}
			// and recorded again
			again, hit := recorded(t, records, "p", dir, scans)
			if !hit || !reflect.DeepEqual(viewOf(t, again), viewOf(t, got)) {
				t.Error("the scan after the change was not recorded")
			}
		})
	}
}

func TestRecord_NotUsedForOtherScans(t *testing.T) {
	trustRecent(t)
	dir, scans := recordFixture(t)
	records := t.TempDir()
	recorded(t, records, "p", dir, scans)

	if _, hit := recorded(t, records, "p", dir, scans[:1]); hit {
		t.Error("the record of other scans was used")
	}
	if _, hit := recorded(t, records, "other", dir, scans); hit {
		t.Error("the record of another id was used")
	}
	excluded := append([]RecordScan(nil), scans...)
	excluded[0].Excluded = DefaultDuplicatesFilter
	if _, ok := NewRecord(records, "p", nil, DefaultParser, nil, excluded); ok {
		t.Error("scans with a matcher of unknown pattern can be recorded")
	}
}

// Identities too close to the record's writing are not trusted.
func TestRecord_NotUsedWhenRacy(t *testing.T) {
	dir, scans := recordFixture(t)
	records := t.TempDir()
	margin := statTrustMargin
	t.Cleanup(func() { statTrustMargin = margin })
	statTrustMargin = time.Hour

	recorded(t, records, "p", dir, scans)
	if _, hit := recorded(t, records, "p", dir, scans); hit {
		t.Error("a record of files changed just before it was used")
	}
}

// A store release's file, taken by its stamp, keeps its record valid
// when the store hard-links it into another project (which changes its
// change time); any other change to it, or a new change time of a file
// without a stamp, still makes the record miss.
func TestRecord_StampedFilesWithoutChangeTime(t *testing.T) {
	const stamp = 1_000_000_000
	for name, tc := range map[string]struct {
		change func(t *testing.T, dir string)
		hit    bool
	}{
		"linked elsewhere": {func(t *testing.T, dir string) {
			if err := os.Link(filepath.Join(dir, "lib/a.php"), filepath.Join(t.TempDir(), "a.php")); err != nil {
				t.Skip("no hardlinks:", err)
			}
		}, true},
		"stamp gone": {func(t *testing.T, dir string) {
			if err := os.Chtimes(filepath.Join(dir, "lib/a.php"), time.Unix(stamp, 1), time.Unix(stamp, 1)); err != nil {
				t.Fatal(err)
			}
		}, false},
		"unstamped file linked elsewhere": {func(t *testing.T, dir string) {
			if err := os.Link(filepath.Join(dir, "lib/sub/b.php"), filepath.Join(t.TempDir(), "b.php")); err != nil {
				t.Skip("no hardlinks:", err)
			}
		}, false},
	} {
		t.Run(name, func(t *testing.T) {
			trustRecent(t)
			dir, scans := recordFixture(t)
			release := &memRelease{name: "a/b", files: []StampedFile{
				stampFile(t, dir, "lib/a.php", `<?php class Dup {} class A {}`, stamp),
			}}
			records := t.TempDir()
			cache := func() *ParseCache {
				c := NewParseCache()
				c.AddReleases([]Release{release})

				return c
			}
			want, _ := recordedWith(t, records, "p", dir, scans, cache())
			if _, hit := recordedWith(t, records, "p", dir, scans, cache()); !hit {
				t.Fatal("the record was not used")
			}
			time.Sleep(20 * time.Millisecond) // a new change time
			tc.change(t, dir)

			got, hit := recordedWith(t, records, "p", dir, scans, cache())
			if hit != tc.hit {
				t.Fatalf("record used: %v, want %v", hit, tc.hit)
			}
			if !reflect.DeepEqual(viewOf(t, got), viewOf(t, want)) {
				t.Errorf("class map\n%+v\nwant\n%+v", viewOf(t, got), viewOf(t, want))
			}
		})
	}
}
