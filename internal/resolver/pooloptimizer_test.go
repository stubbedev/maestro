package resolver

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
)

// Ports tests/Composer/Test/DependencyResolver/PoolOptimizerTest.php.

// When a package replaces another name, it appears in identical-dependency groups for both
// its own name and the replacement name. The kept package's removedVersionsByPackage should
// contain versions from all name groups it participates in.
func TestPoolOptimizer_RemovedVersionsByPackageIncludesVersionsFromReplacementNameGroups(t *testing.T) {
	request := NewRequest(newLockArrayRepository(t))

	replaceConstraint := parseConstraints(t, "^1.0")
	requireName(t, request, "package/a", parseConstraints(t, "^1.0"))
	requireName(t, request, "package/b", parseConstraints(t, "^1.0"))

	replacing := func(name, version string) *pkg.CompletePackage {
		p := getPackage(t, name, version)
		p.SetReplaces(pkg.LinksOf(newLink(name, "package/b", replaceConstraint, pkg.TypeReplace)))

		return p
	}
	// package/a 1.0.0 and 1.1.0 both replace package/b
	packageA100 := replacing("package/a", "1.0.0")
	packageA110 := replacing("package/a", "1.1.0")
	// package/c 1.5.0 and 1.6.0 also replace package/b (same constraint, same deps = same group under 'package/b')
	packageC150 := replacing("package/c", "1.5.0")
	packageC160 := replacing("package/c", "1.6.0")

	pool := NewPool([]pkg.PackageInterface{packageA100, packageA110, packageC150, packageC160}, nil, nil)
	optimizedPool, err := NewPoolOptimizer(NewDefaultPolicy(false, false, nil)).Optimize(request, pool)
	if err != nil {
		t.Fatal(err)
	}

	removedVersions := optimizedPool.RemovedVersionsByPackage(packageA110)
	for _, p := range []*pkg.CompletePackage{packageA100, packageA110, packageC150, packageC160} {
		if !removedVersions.Has(p.Version()) {
			t.Errorf("Should contain %s %s", p.Name(), p.PrettyVersion())
		}
	}
}

func TestPoolOptimizer_KeepingAnAliasRecordsRemovedVersionsForAliasOfAndSiblingAliases(t *testing.T) {
	request := NewRequest(newLockArrayRepository(t))
	requireName(t, request, "package/a", parseConstraints(t, "^1.1"))

	package110 := getPackage(t, "package/a", "1.1.0")
	package110Alias := getAliasPackage(t, package110, "1.1.x-dev")
	package110SiblingAlias := getAliasPackage(t, package110, "1.1.x-dev")
	package111 := getPackage(t, "package/a", "1.1.1")
	package111Alias := getAliasPackage(t, package111, "1.1.x-dev")

	pool := NewPool([]pkg.PackageInterface{package110, package110Alias, package110SiblingAlias, package111, package111Alias}, nil, nil)
	optimizedPool, err := NewPoolOptimizer(NewDefaultPolicy(false, false, nil)).Optimize(request, pool)
	if err != nil {
		t.Fatal(err)
	}

	expected := [][2]string{
		{package110.Version(), package110.PrettyVersion()},
		{package110Alias.Version(), package110Alias.PrettyVersion()},
		{package111.Version(), package111.PrettyVersion()},
	}
	for _, p := range []pkg.PackageInterface{package110Alias, package110, package110SiblingAlias} {
		var got [][2]string
		for v, pretty := range optimizedPool.RemovedVersionsByPackage(p).All() {
			got = append(got, [2]string{v, pretty})
		}
		if !slices.Equal(got, expected) {
			t.Errorf("%s: got %v, want %v", p, got, expected)
		}
	}
}

func TestPoolOptimizer_PoolOptimizer(t *testing.T) {
	testPoolOptimizerFixtures(t)
}

// TestPoolOptimizer_PoolOptimizerParallel runs the fixtures with the
// package hashes computed on several goroutines whatever the pool size.
func TestPoolOptimizer_PoolOptimizerParallel(t *testing.T) {
	defer func(n, chunk int) { minParallelPackages, parallelChunk = n, chunk }(minParallelPackages, parallelChunk)
	minParallelPackages, parallelChunk = 1, 1

	testPoolOptimizerFixtures(t)
}

func testPoolOptimizerFixtures(t *testing.T) {
	files := fixtureFiles(t, "testdata/Fixtures/pooloptimizer")
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	sections := map[string]bool{"TEST": true, "REQUEST": true, "POOL-BEFORE": true, "POOL-AFTER": true}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			testData := readTestFile(t, file, sections)
			requestData := jsonArray(t, testData["REQUEST"])
			packagesBefore := loadPackages(t, jsonArray(t, testData["POOL-BEFORE"]))
			expectedPackages := loadPackages(t, jsonArray(t, testData["POOL-AFTER"]))

			request := NewRequest(newLockArrayRepository(t))
			arrayLoader := loader.NewArrayLoader(nil, false)
			for _, data := range subArray(requestData, "locked").All() {
				request.LockPackage(loadPackage(t, arrayLoader, data.(*php.Array)))
			}
			for _, data := range subArray(requestData, "fixed").All() {
				request.FixPackage(loadPackage(t, arrayLoader, data.(*php.Array)))
			}
			for name, constraint := range subArray(requestData, "require").All() {
				requireName(t, request, name.String(), parseConstraints(t, php.ToString(constraint)))
			}

			preferStable, _ := requestData.Get("preferStable")
			preferLowest, _ := requestData.Get("preferLowest")

			pool := NewPool(packagesBefore, nil, nil)
			pool, err := NewPoolOptimizer(NewDefaultPolicy(php.ToBool(preferStable), php.ToBool(preferLowest), nil)).Optimize(request, pool)
			if err != nil {
				t.Fatal(err)
			}

			if got, want := reducePackagesInfoForComparison(pool.Packages()), reducePackagesInfoForComparison(expectedPackages); !slices.Equal(got, want) {
				t.Fatalf("%s\ngot:  %q\nwant: %q", testData["TEST"], got, want)
			}
		})
	}
}

func reducePackagesInfoForComparison(packages []pkg.PackageInterface) []string {
	var info []string
	for _, p := range packages {
		s := p.Name() + "@" + p.Version()
		if alias, ok := p.(pkg.Alias); ok {
			s += " (alias of " + alias.AliasOf().Version() + ")"
		}
		info = append(info, s)
	}
	slices.Sort(info)

	return info
}

func loadPackages(t testing.TB, packagesData *php.Array) []pkg.PackageInterface {
	t.Helper()
	arrayLoader := loader.NewArrayLoader(nil, false)
	var packages []pkg.PackageInterface
	for _, data := range packagesData.All() {
		p := loadPackage(t, arrayLoader, data.(*php.Array))
		packages = append(packages, p)
		if alias, ok := p.(pkg.Alias); ok {
			packages = append(packages, alias.AliasOf())
		}
	}

	return packages
}
