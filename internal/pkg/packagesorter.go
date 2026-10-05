// Ports src/Composer/Util/PackageSorter.php. Composer keeps it in
// Composer\Util; it lives here because internal/util sits below the
// package classes.

package pkg

import (
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/semver"
)

// GetMostCurrentVersion ports PackageSorter::getMostCurrentVersion: the
// default branch if there is one, else the highest version (nil for no
// packages).
func GetMostCurrentVersion(packages []PackageInterface) PackageInterface {
	if len(packages) == 0 {
		return nil
	}

	highest := packages[0]
	for _, candidate := range packages {
		if candidate.IsDefaultBranch() {
			return candidate
		}

		if semver.VersionCompare(highest.Version(), candidate.Version()) < 0 {
			highest = candidate
		}
	}

	return highest
}

// SortPackagesAlphabetically ports PackageSorter::sortPackagesAlphabetically
// (a sorted copy).
func SortPackagesAlphabetically(packages []PackageInterface) []PackageInterface {
	out := append([]PackageInterface(nil), packages...)
	php.SortSlice(out, func(a, b PackageInterface) int { return php.Compare(a.Name(), b.Name()) })

	return out
}

// SortPackages ports PackageSorter::sortPackages: packages sorted by
// dependencies weight, so that dependencies come before the packages
// using them; weights (package name => weight) adds to a package's weight.
func SortPackages(packages []PackageInterface, weights map[string]int) []PackageInterface {
	usageList := map[string][]string{}

	for _, p := range packages {
		for _, link := range mergedRequires(p) {
			target := link.Target()
			usageList[target] = append(usageList[target], p.Name())
		}
	}

	computing := map[string]bool{}
	computed := map[string]int{}

	var computeImportance func(name string) int

	computeImportance = func(name string) int {
		// reusing computed importance
		if w, ok := computed[name]; ok {
			return w
		}

		// canceling circular dependency
		if computing[name] {
			return 0
		}

		computing[name] = true
		weight := weights[name]

		for _, user := range usageList[name] {
			weight -= 1 - computeImportance(user)
		}

		delete(computing, name)
		computed[name] = weight

		return weight
	}

	type weighted struct {
		name   string
		weight int
		index  int
	}

	weightedPackages := make([]weighted, len(packages))
	for i, p := range packages {
		weightedPackages[i] = weighted{p.Name(), computeImportance(p.Name()), i}
	}

	php.SortSlice(weightedPackages, func(a, b weighted) int {
		if a.weight != b.weight {
			return a.weight - b.weight
		}

		return php.Strnatcasecmp(a.name, b.name)
	})

	sorted := make([]PackageInterface, len(weightedPackages))
	for i, w := range weightedPackages {
		sorted[i] = packages[w.index]
	}

	return sorted
}

// mergedRequires returns the links SortPackages walks: the requires, and
// for a root package array_merge($requires, $devRequires).
func mergedRequires(p PackageInterface) []*Link {
	requires := p.Requires()

	root, ok := p.(RootPackageInterface)
	if !ok {
		return collectLinks(requires)
	}

	var b LinksBuilder

	next := 0
	for _, links := range [2]Links{requires, root.DevRequires()} {
		for k, l := range links.All() {
			if php.StrKey(k).IsInt() {
				b.Append(php.ToString(int64(next)), l)
				next++
			} else {
				b.Set(k, l)
			}
		}
	}

	return b.links
}
