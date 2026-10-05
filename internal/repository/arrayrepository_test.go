package repository

import (
	"reflect"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
)

// Ports tests/Composer/Test/Repository/ArrayRepositoryTest.php.

func newArrayRepo(t *testing.T, packages ...pkg.PackageInterface) *ArrayRepository {
	t.Helper()

	return must(NewArrayRepository(packages))
}

func count(t *testing.T, r RepositoryInterface) int {
	t.Helper()

	return must(r.Count())
}

func TestArrayRepository_AddPackage(t *testing.T) {
	repo := newArrayRepo(t)
	noErr(t, repo.AddPackage(getPackage(t, "foo", "1")))

	if count(t, repo) != 1 {
		t.Fatal("count")
	}
}

func TestArrayRepository_RemovePackage(t *testing.T) {
	p := getPackage(t, "bar", "2")

	repo := newArrayRepo(t)
	noErr(t, repo.AddPackage(getPackage(t, "foo", "1")))
	noErr(t, repo.AddPackage(p))

	if count(t, repo) != 2 {
		t.Fatal("count")
	}

	noErr(t, repo.RemovePackage(getPackage(t, "foo", "1")))

	if count(t, repo) != 1 {
		t.Fatal("count")
	}
	if got := must(repo.Packages()); len(got) != 1 || got[0] != p {
		t.Fatalf("packages %v", got)
	}
}

func TestArrayRepository_HasPackage(t *testing.T) {
	repo := newArrayRepo(t, getPackage(t, "foo", "1"), getPackage(t, "bar", "2"))

	if !must(repo.HasPackage(getPackage(t, "foo", "1"))) {
		t.Error("foo 1")
	}
	if must(repo.HasPackage(getPackage(t, "bar", "1"))) {
		t.Error("bar 1")
	}
}

func TestArrayRepository_FindPackages(t *testing.T) {
	repo := newArrayRepo(t, getPackage(t, "foo", "1"), getPackage(t, "bar", "2"), getPackage(t, "bar", "3"))

	foo := must(repo.FindPackages("foo", nil))
	if len(foo) != 1 || foo[0].Name() != "foo" {
		t.Errorf("foo %v", names(foo))
	}

	bar := must(repo.FindPackages("bar", nil))
	if len(bar) != 2 || bar[0].Name() != "bar" {
		t.Errorf("bar %v", names(bar))
	}
}

func TestArrayRepository_AutomaticallyAddAliasedPackageButNotRemove(t *testing.T) {
	repo := newArrayRepo(t)

	p := getPackage(t, "foo", "1")
	alias := getAliasPackage(t, p, "2")

	noErr(t, repo.AddPackage(alias))

	if count(t, repo) != 2 {
		t.Fatal("count")
	}
	if !must(repo.HasPackage(getPackage(t, "foo", "1"))) || !must(repo.HasPackage(getPackage(t, "foo", "2"))) {
		t.Fatal("hasPackage")
	}

	noErr(t, repo.RemovePackage(alias))

	if count(t, repo) != 1 {
		t.Fatal("count")
	}
}

func TestArrayRepository_Search(t *testing.T) {
	repo := newArrayRepo(t, getPackage(t, "foo", "1"), getPackage(t, "bar", "1"))

	want := []SearchResult{{Name: "foo"}}
	if got := must(repo.Search("foo", SearchFulltext, "")); !reflect.DeepEqual(got, want) {
		t.Errorf("foo %+v", got)
	}
	want = []SearchResult{{Name: "bar"}}
	if got := must(repo.Search("bar", 0, "")); !reflect.DeepEqual(got, want) {
		t.Errorf("bar %+v", got)
	}
	if got := must(repo.Search("foobar", 0, "")); len(got) != 0 {
		t.Errorf("foobar %+v", got)
	}
}

func TestArrayRepository_SearchWithPackageType(t *testing.T) {
	repo := newArrayRepo(t, getPackage(t, "foo", "1"), getPackage(t, "bar", "1"))
	p := getPackage(t, "foobar", "1")
	p.SetType("composer-plugin")
	noErr(t, repo.AddPackage(p))

	if got := must(repo.Search("foo", SearchFulltext, "library")); !reflect.DeepEqual(got, []SearchResult{{Name: "foo"}}) {
		t.Errorf("library %+v", got)
	}
	if got := must(repo.Search("bar", SearchFulltext, "package")); len(got) != 0 {
		t.Errorf("package %+v", got)
	}
	if got := must(repo.Search("foo", 0, "composer-plugin")); !reflect.DeepEqual(got, []SearchResult{{Name: "foobar"}}) {
		t.Errorf("composer-plugin %+v", got)
	}
}

func TestArrayRepository_SearchWithAbandonedPackages(t *testing.T) {
	p1 := getPackage(t, "foo1", "1")
	p1.SetAbandoned(true)
	p2 := getPackage(t, "foo2", "1")
	p2.SetAbandoned("bar")
	repo := newArrayRepo(t, p1, p2)

	want := []SearchResult{{Name: "foo1", Abandoned: true}, {Name: "foo2", Abandoned: "bar"}}
	if got := must(repo.Search("foo", 0, "")); !reflect.DeepEqual(got, want) {
		t.Errorf("%+v", got)
	}
}

// Beyond Composer's tests: loadPackages selection, vendor search,
// providers and the package's repository back-reference.

func TestArrayRepository_LoadPackages(t *testing.T) {
	foo1 := getPackage(t, "foo", "1.0.0")
	foo2 := getPackage(t, "foo", "2.0.0-beta")
	alias := getAliasPackage(t, foo1, "1.1.0")
	bar := getPackage(t, "bar", "1.0.0")
	repo := newArrayRepo(t, foo1, foo2, alias, bar)

	stable := php.ArrayOf("stable", 0)
	m := NewConstraintMap("foo", must(ParseConstraint("^1.1")))
	result := must(repo.LoadPackages(m, stable, php.NewArray(), nil))
	// the alias matches, so the aliased package comes with it
	if want := []pkg.PackageInterface{alias, foo1}; !slices.Equal(result.Packages, want) {
		t.Errorf("packages %v", result.Packages)
	}
	if !slices.Equal(result.NamesFound, []string{"foo"}) {
		t.Errorf("names %v", result.NamesFound)
	}

	m = NewConstraintMap("foo", nil)
	result = must(repo.LoadPackages(m, pkg.Stabilities(), php.NewArray(), AlreadyLoaded{"foo": {"1.0.0.0": foo1}}))
	if want := []pkg.PackageInterface{foo2, alias, foo1}; !slices.Equal(result.Packages, want) {
		t.Errorf("already loaded %v", result.Packages)
	}
	if foo1.Repository() != repo || foo1.IsPlatform() {
		t.Error("repository")
	}
}

func TestArrayRepository_SearchVendorAndProviders(t *testing.T) {
	a := getPackage(t, "acme/a", "1")
	b := getPackage(t, "acme/b", "1")
	b.SetDescription(pkg.Str("Bee"))
	b.SetProvides(pkg.LinksOf(pkg.NewLink("acme/b", "psr/log-implementation", semver.NewMatchAllConstraint(), pkg.TypeProvide, pkg.Str("1.0"))))
	repo := newArrayRepo(t, a, b, getPackage(t, "other/c", "1"))

	if got := must(repo.Search("acm", SearchVendor, "")); !reflect.DeepEqual(got, []SearchResult{{Name: "acme"}}) {
		t.Errorf("vendor %+v", got)
	}
	want := []ProviderInfo{{Name: "acme/b", Description: pkg.Str("Bee"), Type: "library"}}
	if got := must(repo.Providers("psr/log-implementation")); !reflect.DeepEqual(got, want) {
		t.Errorf("providers %+v", got)
	}
	if repo.RepoName() != "array repo (defining 3 packages)" {
		t.Error(repo.RepoName())
	}
}
