package repository

import (
	"reflect"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
)

// Ports tests/Composer/Test/Repository/CompositeRepositoryTest.php,
// FilterRepositoryTest.php, InstalledRepositoryTest.php and
// RepositoryUtilsTest.php.

func TestCompositeRepository_HasPackage(t *testing.T) {
	one := newArrayRepo(t, getPackage(t, "foo", "1"))
	two := newArrayRepo(t, getPackage(t, "bar", "1"))
	repo := must(NewCompositeRepository([]RepositoryInterface{one, two}))

	for _, c := range []struct {
		name, version string
		want          bool
	}{{"foo", "1", true}, {"bar", "1", true}, {"foo", "2", false}, {"bar", "2", false}} {
		if got := must(repo.HasPackage(getPackage(t, c.name, c.version))); got != c.want {
			t.Errorf("%s/%s: %v", c.name, c.version, got)
		}
	}
}

func TestCompositeRepository_FindPackage(t *testing.T) {
	one := newArrayRepo(t, getPackage(t, "foo", "1"))
	two := newArrayRepo(t, getPackage(t, "bar", "1"))
	repo := must(NewCompositeRepository([]RepositoryInterface{one, two}))

	for _, name := range []string{"foo", "bar"} {
		p := must(repo.FindPackage(name, mustConstraint(t, "1")))
		if p == nil || p.Name() != name || p.PrettyVersion() != "1" {
			t.Errorf("%s: %v", name, p)
		}
	}
	if p := must(repo.FindPackage("foo", mustConstraint(t, "2"))); p != nil {
		t.Errorf("foo/2: %v", p)
	}
}

func TestCompositeRepository_FindPackages(t *testing.T) {
	one := newArrayRepo(t, getPackage(t, "foo", "1"), getPackage(t, "foo", "2"), getPackage(t, "bat", "1"))
	two := newArrayRepo(t, getPackage(t, "bar", "1"), getPackage(t, "bar", "2"), getPackage(t, "foo", "3"))
	repo := must(NewCompositeRepository([]RepositoryInterface{one, two}))

	for name, n := range map[string]int{"bat": 1, "bar": 2, "foo": 3} {
		found := must(repo.FindPackages(name, nil))
		if len(found) != n || found[0].Name() != name {
			t.Errorf("%s: %v", name, names(found))
		}
	}
}

func TestCompositeRepository_GetPackages(t *testing.T) {
	one := newArrayRepo(t, getPackage(t, "foo", "1"))
	two := newArrayRepo(t, getPackage(t, "bar", "1"))
	repo := must(NewCompositeRepository([]RepositoryInterface{one, two}))

	packages := must(repo.Packages())
	if !slices.Equal(names(packages), []string{"foo", "bar"}) || packages[0].PrettyVersion() != "1" || packages[1].PrettyVersion() != "1" {
		t.Errorf("%v", names(packages))
	}
}

func TestCompositeRepository_AddRepository(t *testing.T) {
	one := newArrayRepo(t, getPackage(t, "foo", "1"))
	two := newArrayRepo(t, getPackage(t, "bar", "1"), getPackage(t, "bar", "2"), getPackage(t, "bar", "3"))

	repo := must(NewCompositeRepository([]RepositoryInterface{one}))
	if count(t, repo) != 1 {
		t.Fatal("before")
	}
	noErr(t, repo.AddRepository(two))
	if count(t, repo) != 4 {
		t.Fatal("after")
	}
}

func TestCompositeRepository_Count(t *testing.T) {
	one := newArrayRepo(t, getPackage(t, "foo", "1"))
	two := newArrayRepo(t, getPackage(t, "bar", "1"))
	repo := must(NewCompositeRepository([]RepositoryInterface{one, two}))

	if count(t, repo) != 2 {
		t.Fatal("count")
	}
}

func TestCompositeRepository_NoRepositories(t *testing.T) {
	repo := must(NewCompositeRepository(nil))
	if got := must(repo.FindPackages("foo", nil)); len(got) != 0 {
		t.Error("findPackages")
	}
	if got := must(repo.Search("foo", 0, "")); len(got) != 0 {
		t.Error("search")
	}
	if got := must(repo.Packages()); len(got) != 0 {
		t.Error("getPackages")
	}
}

func filterFixture(t *testing.T) *ArrayRepository {
	t.Helper()

	return newArrayRepo(t,
		getPackage(t, "foo/aaa", "1.0.0"),
		getPackage(t, "foo/bbb", "1.0.0"),
		getPackage(t, "bar/xxx", "1.0.0"),
		getPackage(t, "baz/yyy", "1.0.0"),
	)
}

func filterOptions(only, exclude []string, canonical any) *php.Array {
	a := php.NewArray()
	if only != nil {
		a.Set("only", php.StringList(only))
	}
	if exclude != nil {
		a.Set("exclude", php.StringList(exclude))
	}
	if canonical != nil {
		a.Set("canonical", canonical)
	}

	return a
}

func TestFilterRepository_RepoMatching(t *testing.T) {
	all := []string{"foo/aaa", "foo/bbb", "bar/xxx", "baz/yyy"}
	for i, c := range []struct {
		expected []string
		config   *php.Array
	}{
		{[]string{"foo/aaa", "foo/bbb"}, filterOptions([]string{"foo/*"}, nil, nil)},
		{[]string{"foo/aaa", "baz/yyy"}, filterOptions([]string{"foo/aaa", "baz/yyy"}, nil, nil)},
		{[]string{"bar/xxx"}, filterOptions(nil, []string{"foo/*", "baz/yyy"}, nil)},
		// make sure sub-patterns are not matched without wildcard
		{all, filterOptions(nil, []string{"foo/aa", "az/yyy"}, nil)},
		{[]string{}, filterOptions([]string{"foo/aa", "az/yyy"}, nil, nil)},
		// empty "only" means no packages allowed
		{[]string{}, filterOptions([]string{}, nil, nil)},
		// absent "only" means all packages allowed
		{all, filterOptions(nil, nil, nil)},
		// empty or absent "exclude" have the same effect: none
		{all, filterOptions(nil, []string{}, nil)},
		{all, filterOptions(nil, nil, nil)},
	} {
		repo := must(NewFilterRepository(filterFixture(t), c.config))
		if got := names(must(repo.Packages())); !slices.Equal(got, c.expected) {
			t.Errorf("case %d: %v", i, got)
		}
	}
}

func TestFilterRepository_BothFiltersDisallowed(t *testing.T) {
	_, err := NewFilterRepository(filterFixture(t), filterOptions([]string{}, []string{}, nil))
	if !phperr.InstanceOf(err, "InvalidArgumentException") {
		t.Fatalf("%v", err)
	}
}

func TestFilterRepository_SecurityAdvisoriesDisabledInChild(t *testing.T) {
	repo := must(NewFilterRepository(filterFixture(t), filterOptions([]string{"foo/*"}, nil, nil)))

	if must(repo.HasSecurityAdvisories()) {
		t.Error("hasSecurityAdvisories")
	}
	result := must(repo.SecurityAdvisories(NewConstraintMap("foo/aaa", semver.NewMatchAllConstraint()), true))
	if len(result.NamesFound) != 0 || result.Advisories.Len() != 0 {
		t.Errorf("%+v", result)
	}
}

func TestFilterRepository_FiltersDisabledInChild(t *testing.T) {
	repo := must(NewFilterRepository(filterFixture(t), filterOptions([]string{"foo/*"}, nil, nil)))

	if must(repo.HasFilter()) {
		t.Error("hasFilter")
	}
	if got := must(repo.Filter(NewConstraintMap("foo/aaa", semver.NewMatchAllConstraint()), nil)); got.Len() != 0 {
		t.Error("getFilter")
	}
	if got := must(repo.FilterLists()); len(got) != 0 {
		t.Error("getFilterLists")
	}
}

func TestFilterRepository_CanonicalDefaultTrue(t *testing.T) {
	repo := must(NewFilterRepository(filterFixture(t), filterOptions(nil, nil, nil)))
	result := must(repo.LoadPackages(NewConstraintMap("foo/aaa", semver.NewMatchAllConstraint()), pkg.Stabilities(), php.NewArray(), nil))
	if len(result.Packages) != 1 || len(result.NamesFound) != 1 {
		t.Errorf("%+v", result)
	}
}

func TestFilterRepository_NonCanonical(t *testing.T) {
	repo := must(NewFilterRepository(filterFixture(t), filterOptions(nil, nil, false)))
	result := must(repo.LoadPackages(NewConstraintMap("foo/aaa", semver.NewMatchAllConstraint()), pkg.Stabilities(), php.NewArray(), nil))
	if len(result.Packages) != 1 || len(result.NamesFound) != 0 {
		t.Errorf("%+v", result)
	}
}

func TestInstalledRepository_FindPackagesWithReplacersAndProviders(t *testing.T) {
	foo := getPackage(t, "foo", "1")
	foo2 := getPackage(t, "foo", "2")
	one := must(NewInstalledArrayRepository([]pkg.PackageInterface{foo, foo2}))

	bar := getPackage(t, "bar", "1")
	bar2 := getPackage(t, "bar", "2")
	two := must(NewInstalledArrayRepository([]pkg.PackageInterface{bar, bar2}))

	foo.SetReplaces(pkg.LinksOf(pkg.NewLink("foo", "provided", semver.NewMatchAllConstraint(), pkg.TypeUnknown, pkg.NullString{})))
	bar2.SetProvides(pkg.LinksOf(pkg.NewLink("bar", "provided", semver.NewMatchAllConstraint(), pkg.TypeUnknown, pkg.NullString{})))

	repo := must(NewInstalledRepository([]RepositoryInterface{one, two}))

	if got := must(repo.FindPackagesWithReplacersAndProviders("foo", mustConstraint(t, "2"))); !slices.Equal(got, []pkg.PackageInterface{foo2}) {
		t.Errorf("foo 2: %v", got)
	}
	if got := must(repo.FindPackagesWithReplacersAndProviders("bar", mustConstraint(t, "1"))); !slices.Equal(got, []pkg.PackageInterface{bar}) {
		t.Errorf("bar 1: %v", got)
	}
	if got := must(repo.FindPackagesWithReplacersAndProviders("provided", nil)); !slices.Equal(got, []pkg.PackageInterface{foo, bar2}) {
		t.Errorf("provided: %v", got)
	}
}

func TestInstalledRepository_AddRepository(t *testing.T) {
	_, err := NewInstalledRepository([]RepositoryInterface{newArrayRepo(t)})
	if !phperr.InstanceOf(err, "LogicException") {
		t.Fatalf("%v", err)
	}
}

func TestRepositoryUtils_FilterRequiredPackages(t *testing.T) {
	packageA := getPackage(t, "required/a", "1.0.0")
	packageB := getPackage(t, "required/b", "1.0.0")
	configureLinks(t, packageB, map[string][][2]string{"require": {{"required/c", "*"}}})
	packageC := getPackage(t, "required/c", "1.0.0")
	packageCAlias := getAliasPackage(t, packageC, "2.0.0")
	circular := getPackage(t, "required/circular", "1.0.0")
	configureLinks(t, circular, map[string][][2]string{"require": {{"required/circular-b", "*"}}})
	circularB := getPackage(t, "required/circular-b", "1.0.0")
	configureLinks(t, circularB, map[string][][2]string{"require": {{"required/circular", "*"}}})

	byKey := map[string]pkg.PackageInterface{
		"a": packageA, "b": packageB, "c": packageC, "c-alias": packageCAlias, "circular": circular, "circular-b": circularB,
	}
	pkgs := []pkg.PackageInterface{
		getPackage(t, "dummy/pkg", "1.0.0"), getPackage(t, "dummy/pkg2", "2.0.0"),
		packageA, packageB, packageC, packageCAlias, circular, circularB,
	}

	for _, c := range []struct {
		name       string
		links      map[string][][2]string
		expected   []string
		includeDev bool
	}{
		{"no require", nil, nil, false},
		{"require-dev has no effect", map[string][][2]string{"require-dev": {{"required/a", "*"}}}, nil, false},
		{"require-dev works if called with it enabled", map[string][][2]string{"require-dev": {{"required/a", "*"}}}, []string{"a"}, true},
		{"simple require", map[string][][2]string{"require": {{"required/a", "*"}}}, []string{"a"}, false},
		{"require constraint is irrelevant", map[string][][2]string{"require": {{"required/a", "dev-lala"}}}, []string{"a"}, false},
		{"require transitive deps and aliases are included", map[string][][2]string{"require": {{"required/b", "*"}}}, []string{"b", "c", "c-alias"}, false},
		{"circular deps are no problem", map[string][][2]string{"require": {{"required/circular", "*"}}}, []string{"circular", "circular-b"}, false},
	} {
		requirer := getPackage(t, "requirer/pkg", "1.0.0")
		configureLinks(t, requirer, c.links)
		var expected []pkg.PackageInterface
		for _, k := range c.expected {
			expected = append(expected, byKey[k])
		}
		if got := FilterRequiredPackages(pkgs, requirer, c.includeDev); !reflect.DeepEqual(got, expected) {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

// prefetchRecorder is a repository recording the names it was asked to
// prefetch.
type prefetchRecorder struct {
	*ArrayRepository
	names []string
}

func (r *prefetchRecorder) PrefetchPackages(names []string, _, _ *php.Array) {
	r.names = append(r.names, names...)
}

// The wrappers prefetch what their loadPackages would load: a composite
// in every repository, a filter only the names it lets through.
func TestPrefetchPackages_ReachesWhatLoadPackagesLoads(t *testing.T) {
	plain, filtered := &prefetchRecorder{ArrayRepository: filterFixture(t)}, &prefetchRecorder{ArrayRepository: filterFixture(t)}
	filter := must(NewFilterRepository(filtered, filterOptions([]string{"foo/*"}, nil, nil)))
	composite := must(NewCompositeRepository([]RepositoryInterface{plain, filter, filterFixture(t)}))

	PrefetchPackages([]RepositoryInterface{composite}, []string{"foo/aaa", "bar/xxx", "foo/bbb"}, nil, nil)

	if want := []string{"foo/aaa", "bar/xxx", "foo/bbb"}; !slices.Equal(plain.names, want) {
		t.Errorf("the plain repository prefetched %q, want %q", plain.names, want)
	}
	if want := []string{"foo/aaa", "foo/bbb"}; !slices.Equal(filtered.names, want) {
		t.Errorf("the filtered repository prefetched %q, want %q", filtered.names, want)
	}
}
