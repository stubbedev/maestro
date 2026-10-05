// Ports tests/Composer/Test/Util/PackageSorterTest.php.

package pkg_test

import (
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
)

func sorterPackage(name string, requires ...string) pkg.PackageInterface {
	p := pkg.NewPackage(name, "1.0.0.0", "1.0.0")

	var b pkg.LinksBuilder
	for _, r := range requires {
		b.Set(r, pkg.NewLink(p.Name(), r, semver.NewMatchAllConstraint(), pkg.TypeUnknown, pkg.NullString{}))
	}

	p.SetRequires(b.Build())

	return p
}

func TestPackageSorter_SortingDoesNothingWithNoDependencies(t *testing.T) {
	packages := []pkg.PackageInterface{sorterPackage("foo/bar1"), sorterPackage("foo/bar2"), sorterPackage("foo/bar3"), sorterPackage("foo/bar4")}

	if got := pkg.SortPackages(packages, nil); !slices.Equal(got, packages) {
		t.Errorf("got %v", got)
	}
}

func TestPackageSorter_SortingOrdersDependenciesHigherThanPackage(t *testing.T) {
	for name, c := range map[string]struct {
		packages []pkg.PackageInterface
		expected []string
		weights  map[string]int
	}{
		"one package is dep": {
			[]pkg.PackageInterface{sorterPackage("foo/bar1", "foo/bar4"), sorterPackage("foo/bar2", "foo/bar4"), sorterPackage("foo/bar3", "foo/bar4"), sorterPackage("foo/bar4")},
			[]string{"foo/bar4", "foo/bar1", "foo/bar2", "foo/bar3"},
			nil,
		},
		"one package has more deps": {
			[]pkg.PackageInterface{sorterPackage("foo/bar1", "foo/bar2"), sorterPackage("foo/bar2", "foo/bar4"), sorterPackage("foo/bar3", "foo/bar4"), sorterPackage("foo/bar4")},
			[]string{"foo/bar4", "foo/bar2", "foo/bar1", "foo/bar3"},
			nil,
		},
		"package is required by many, but requires one other": {
			[]pkg.PackageInterface{
				sorterPackage("foo/bar1", "foo/bar3"), sorterPackage("foo/bar2", "foo/bar3"), sorterPackage("foo/bar3", "foo/bar4"),
				sorterPackage("foo/bar4"), sorterPackage("foo/bar5", "foo/bar3"), sorterPackage("foo/bar6", "foo/bar3"),
			},
			[]string{"foo/bar4", "foo/bar3", "foo/bar1", "foo/bar2", "foo/bar5", "foo/bar6"},
			nil,
		},
		"one package has many requires": {
			[]pkg.PackageInterface{
				sorterPackage("foo/bar1", "foo/bar2"), sorterPackage("foo/bar2"), sorterPackage("foo/bar3", "foo/bar4"),
				sorterPackage("foo/bar4"), sorterPackage("foo/bar5", "foo/bar2"), sorterPackage("foo/bar6", "foo/bar2"),
			},
			[]string{"foo/bar2", "foo/bar4", "foo/bar1", "foo/bar3", "foo/bar5", "foo/bar6"},
			nil,
		},
		"circular deps sorted alphabetically if weighted equally": {
			[]pkg.PackageInterface{
				sorterPackage("foo/bar1", "circular/part1"), sorterPackage("foo/bar2", "circular/part2"),
				sorterPackage("circular/part1", "circular/part2"), sorterPackage("circular/part2", "circular/part1"),
			},
			[]string{"circular/part1", "circular/part2", "foo/bar1", "foo/bar2"},
			nil,
		},
		"equal weight sorted alphabetically": {
			[]pkg.PackageInterface{sorterPackage("foo/bar10", "foo/dep"), sorterPackage("foo/bar2", "foo/dep"), sorterPackage("foo/baz", "foo/dep"), sorterPackage("foo/dep")},
			[]string{"foo/dep", "foo/bar2", "foo/bar10", "foo/baz"},
			nil,
		},
		"pre-weighted packages bumped to top incl their deps": {
			[]pkg.PackageInterface{sorterPackage("foo/bar", "foo/dep"), sorterPackage("foo/bar2", "foo/dep2"), sorterPackage("foo/dep"), sorterPackage("foo/dep2")},
			[]string{"foo/dep", "foo/bar", "foo/dep2", "foo/bar2"},
			map[string]int{"foo/bar": -1000},
		},
	} {
		var got []string
		for _, p := range pkg.SortPackages(c.packages, c.weights) {
			got = append(got, p.Name())
		}

		if !slices.Equal(got, c.expected) {
			t.Errorf("%s: got %q, want %q", name, got, c.expected)
		}
	}
}
