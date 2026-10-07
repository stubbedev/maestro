package autoload

import (
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/archive/archivetest"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/store"
)

func TestAheadFiles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		autoload *php.Array
		psr      bool
		target   string
		yes, no  []string
		none     bool
	}{
		{
			name: "classmap", autoload: arr("classmap", list("lib/", "./one.php", `win\dir`), "psr-4", arr(`A\`, "src/")),
			yes: []string{"lib/a.php", "lib/sub/b.inc", "one.php", "win/dir/c.hh"},
			no:  []string{"src/A.php", "lib.php", "libx/a.php", "lib/a.txt"},
		},
		{
			name: "psr", autoload: arr("psr-4", arr(`A\`, list("src/", "more")), "psr-0", arr("B_", "legacy/")), psr: true,
			yes: []string{"src/A.php", "more/x.php", "legacy/B/C.php"},
			no:  []string{"tests/T.php", "srcx/A.php"},
		},
		{name: "psr not scanned", autoload: arr("psr-4", arr(`A\`, "src/")), none: true},
		{name: "whole package", autoload: arr("classmap", list("")), yes: []string{"a.php", "deep/b.php"}, no: []string{"a.txt"}},
		{name: "glob", autoload: arr("classmap", list("src/*/lib")), yes: []string{"anything.php"}},
		{name: "outside", autoload: arr("classmap", list("../other")), yes: []string{"anything.php"}},
		{name: "target-dir", autoload: arr("psr-0", arr("A", "")), psr: true, target: "A/B", yes: []string{"x.php"}},
		{name: "no rules", autoload: arr(), none: true},
		{name: "files only", autoload: arr("files", list("f.php")), psr: true, none: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := newPackage("a/b")
			p.SetAutoload(tc.autoload)
			if tc.target != "" {
				p.SetTargetDir(pkg.Str(tc.target))
			}
			accept, ok := aheadFiles(p, tc.psr)
			if ok == tc.none {
				t.Fatalf("ok %v", ok)
			}
			for _, f := range tc.yes {
				if !accept(f) {
					t.Errorf("%s not taken", f)
				}
			}
			for _, f := range tc.no {
				if accept(f) {
					t.Errorf("%s taken", f)
				}
			}
		})
	}
}

// A dump finds the files of packages the store inserted with the
// generator's Deriver parsed: it writes what a dump without the store
// writes, and keeps no new result with the releases.
func TestGenerator_DumpOfParsedAhead(t *testing.T) {
	zip := archivetest.Zip("",
		archivetest.UnixDir("pkg/", 0o755),
		archivetest.UnixFile("pkg/src/A.php", 0o644, "<?php namespace A; class A {}\n"),
		archivetest.UnixFile("pkg/src/Sub/B.php", 0o644, "<?php namespace A\\Sub; interface B {}\n"),
		archivetest.UnixFile("pkg/src/Wrong.php", 0o644, "<?php namespace Elsewhere; class Wrong {}\n"),
		archivetest.UnixFile("pkg/lib/short.inc", 0o644, "<? class Short {} ?>\n"),
		archivetest.UnixFile("pkg/lib/dup.php", 0o644, "<?php class A {} enum E {}\n"),
		archivetest.UnixFile("pkg/lib/empty.php", 0o644, ""),
		archivetest.UnixFile("pkg/tests/T.php", 0o644, "<?php class T {}\n"),
	)
	pkgOf := func() *pkg.Package {
		p := newPackage("a/b")
		p.SetAutoload(arr("psr-4", arr(`A\`, "src/"), "classmap", list("lib/")))
		p.SetDistType(pkg.Str("zip"))
		p.SetDistURL(pkg.Str("https://example.org/b.zip"))
		p.SetDistReference(pkg.Str("abc"))

		return p
	}
	// project installs the package from a store of its own, which the
	// generator uses with parseAhead, and returns the dump.
	project := func(t *testing.T, parseAhead bool) (e *env, s *store.Store, r *store.Release, dump func()) {
		t.Helper()
		e = setUp(t)
		root := newRoot("root/a")
		root.SetRequires(links(link("root/a", "a/b")))
		p := pkgOf()
		e.packages(p)
		e.generator.Parser.ShortOpenTag = false
		s = openTestStore(t)
		if parseAhead {
			e.generator.UseStore(s)
			e.generator.ParseAhead(true)
		}
		path := filepath.Join(t.TempDir(), "dist.zip")
		if err := os.WriteFile(path, zip, 0o644); err != nil {
			t.Fatal(err)
		}
		dst := e.vendorDir + "/a/b"
		e.mkdir(filepath.Dir(dst))
		d := store.Dist{Name: "a/b", Type: "zip", URL: "https://example.org/b.zip", Reference: "abc"}
		if err := s.Install(d, path, dst, store.ImportOptions{}, e.generator.Deriver(p)); err != nil {
			t.Fatal(err)
		}
		r, err := s.Lookup(d)
		if err != nil {
			t.Fatal(err)
		}

		return e, s, r, func() { e.dump(root, true, "_1") }
	}

	plain, _, _, dump := project(t, false)
	dump()
	want := plain.generatedFiles()

	e, s, r, dump := project(t, true)
	kept, err := s.ReadDerived(r, releaseResults)
	if err != nil || kept == nil {
		t.Fatalf("no results kept with the release: %v", err)
	}
	dump()
	if got := e.generatedFiles(); !maps.Equal(got, want) {
		for name, content := range want {
			if got[name] != content {
				t.Errorf("%s:\n%s\nwant:\n%s", name, got[name], content)
			}
		}
	}
	if after, _ := s.ReadDerived(r, releaseResults); string(after) != string(kept) {
		t.Error("the dump kept new results: it parsed files parsed ahead")
	}
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}

	return s
}
