package pkg

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/php"
)

// Every field of Package and CompletePackage is a skeleton field or one
// adoptRest sets.
func TestSkeleton_FieldsAreClassified(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[Package](), reflect.TypeFor[CompletePackage]()} {
		for f := range typ.Fields() {
			if f.Name == "Package" {
				continue
			}
			skeleton, adopted := slices.Contains(skeletonFields, f.Name), slices.Contains(adoptedFields, f.Name)
			if skeleton == adopted {
				t.Errorf("%s.%s: skeleton %v, adopted %v; it must be one of them", typ.Name(), f.Name, skeleton, adopted)
			}
		}
	}
}

// skeletonMethods are the methods of a CompletePackage that read only its
// skeleton fields (and basePackage's, which a skeleton has too).
var skeletonMethods = []string{
	"Name", "PrettyName", "ID", "SetID", "Repository", "SetRepository", "IsPlatform", "PHPClass",
	"IsDev", "Type", "Stability", "Version", "PrettyVersion", "Requires", "Conflicts", "Provides",
	"Replaces", "DevRequires", "IsDefaultBranch", "Names", "UniqueName", "String", "PrettyString",
	"Equals", "StabilityPriority", "IsAbandoned", "Abandoned", "ReplacementPackage",
}

// fullPackage is a package with every property set.
func fullPackage() *CompletePackage {
	p := NewCompletePackage("a/b", "1.0.0.0", "1.0.0")
	p.SetType("library")
	p.SetTargetDir(Str("t"))
	p.SetExtra(php.ArrayOf("x", 1))
	p.SetBinaries(php.ListOf("bin/x"))
	p.SetInstallationSource(Null[InstallationSource]{S: FromDist, Valid: true})
	p.SetSourceType(Str("git"))
	p.SetSourceURL(Str("https://example.org/a.git"))
	p.SetSourceReference(Str("abc"))
	p.SetSourceMirrors(php.ListOf(php.ArrayOf("url", "m")))
	p.SetDistType(Str("zip"))
	p.SetDistURL(Str("https://example.org/a.zip"))
	p.SetDistReference(Str("abc"))
	p.SetDistSha1Checksum(Str("sum"))
	p.SetDistMirrors(php.ListOf(php.ArrayOf("url", "d")))
	p.SetTransportOptions(php.ArrayOf("o", 1))
	p.SetReleaseDate(time.Unix(1, 0).UTC(), true)
	p.SetSuggests(php.ArrayOf("c/d", "why"))
	p.SetAutoload(php.ArrayOf("psr-4", php.ArrayOf("A\\", "src/")))
	p.SetDevAutoload(php.ArrayOf("psr-4", php.ArrayOf("B\\", "tests/")))
	p.SetIncludePaths(php.ListOf("lib"))
	p.SetPhpExt(php.ArrayOf("extension-name", "x"))
	p.SetNotificationURL("https://example.org/n")
	p.SetScripts(php.ArrayOf("post", php.ListOf("x")))
	p.SetRepositories(php.ListOf(php.ArrayOf("type", "vcs")))
	p.SetLicense(php.ListOf("MIT"))
	p.SetKeywords(php.ListOf("k"))
	p.SetAuthors(php.ListOf(php.ArrayOf("name", "n")))
	p.SetDescription(Str("d"))
	p.SetHomepage(Str("h"))
	p.SetSupport(php.ArrayOf("issues", "i"))
	p.SetFunding(php.ListOf(php.ArrayOf("url", "f")))
	p.SetArchiveName(Str("an"))
	p.SetArchiveExcludes(php.ListOf("ex"))

	return p
}

// testSkeleton is a skeleton of fullPackage, and whether it was loaded.
func testSkeleton() (*CompletePackage, *bool) {
	p := NewCompletePackage("a/b", "1.0.0.0", "1.0.0")
	p.SetType("library")
	loaded := false
	NewSkeletonPackage(p, func() (*CompletePackage, error) {
		loaded = true

		return fullPackage(), nil
	})

	return p, &loaded
}

// A skeleton is loaded by every method but those of skeletonMethods.
func TestSkeleton_MethodsLoadIt(t *testing.T) {
	typ := reflect.TypeFor[*CompletePackage]()
	for m := range typ.Methods() {
		p, loaded := testSkeleton()
		args := []reflect.Value{reflect.ValueOf(p)}
		for i := 1; i < m.Type.NumIn(); i++ {
			args = append(args, reflect.Zero(m.Type.In(i)))
		}
		func() {
			defer func() { _ = recover() }()
			m.Func.Call(args)
		}()
		if want := !slices.Contains(skeletonMethods, m.Name); *loaded != want {
			t.Errorf("%s: loaded %v, want %v", m.Name, *loaded, want)
		}
	}
}

// A loaded skeleton is the full package, its rev aside.
func TestSkeleton_LoadedEqualsFull(t *testing.T) {
	p, loaded := testSkeleton()
	if IsSkeleton(p) != true {
		t.Fatal("not a skeleton")
	}
	p.Rev()
	if !*loaded || IsSkeleton(p) {
		t.Fatal("Rev did not load it")
	}
	full := fullPackage()
	p.rev, full.rev = 0, 0
	p.lazy = nil
	if !reflect.DeepEqual(p, full) {
		t.Errorf("loaded skeleton\n%#v\nfull package\n%#v", p, full)
	}
}

// What is configured later runs on the full package before the skeleton
// takes its properties, for an alias too; on any other package at once.
func TestSkeleton_ConfigureLater(t *testing.T) {
	p, loaded := testSkeleton()
	alias := NewCompleteAliasPackage(p, "1.1.0.0", "1.1.0")
	var order []string
	ConfigureLater(alias, func(q PackageInterface) {
		if q == PackageInterface(p) || q == PackageInterface(alias) {
			t.Error("configured on the skeleton")
		}
		order = append(order, "configure "+q.DistURL().S)
		q.SetDistMirrors(php.ListOf("m"))
	})
	if *loaded || len(order) != 0 {
		t.Fatal("configured at once")
	}
	if got := alias.DistMirrors(); !*loaded || got.Len() != 1 || len(order) != 1 || order[0] != "configure https://example.org/a.zip" {
		t.Fatalf("mirrors %v, order %v", got, order)
	}

	eager := fullPackage()
	ConfigureLater(eager, func(q PackageInterface) { q.SetDistMirrors(nil) })
	if eager.DistMirrors() != nil {
		t.Error("not configured at once")
	}
}
