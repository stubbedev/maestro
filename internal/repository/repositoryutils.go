// Ports src/Composer/Repository/RepositoryUtils.php.

package repository

import (
	"slices"

	"github.com/stubbedev/maestro/internal/pkg"
)

// FilterRequiredPackages ports RepositoryUtils::filterRequiredPackages:
// the packages requirer requires, directly or transitively. Require-dev
// counts only for requirer itself, with includeRequireDev.
func FilterRequiredPackages(packages []pkg.PackageInterface, requirer pkg.PackageInterface, includeRequireDev bool) []pkg.PackageInterface {
	packagesByName := map[string][]int{}
	for index, p := range packages {
		for _, name := range p.Names(true) {
			packagesByName[name] = append(packagesByName[name], index)
		}
	}

	var bucket []pkg.PackageInterface
	collectRequiredPackages(packages, packagesByName, requirer, includeRequireDev, &bucket, map[pkg.PackageInterface]struct{}{})

	return bucket
}

func collectRequiredPackages(packages []pkg.PackageInterface, packagesByName map[string][]int, requirer pkg.PackageInterface, includeRequireDev bool, bucket *[]pkg.PackageInterface, seen map[pkg.PackageInterface]struct{}) {
	requires := requirer.Requires()
	if includeRequireDev {
		requires = MergeLinks(requires, requirer.DevRequires())
	}

	var matches []int
	for name := range requires.All() {
		matches = append(matches, packagesByName[name]...)
	}
	slices.Sort(matches)

	for _, index := range slices.Compact(matches) {
		candidate := packages[index]
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		*bucket = append(*bucket, candidate)
		collectRequiredPackages(packages, packagesByName, candidate, false, bucket, seen)
	}
}

// FlattenRepositories ports RepositoryUtils::flattenRepositories: the
// repositories of composite (and installed) repositories, and with
// unwrapFilterRepos those FilterRepositories wrap, as a flat list.
func FlattenRepositories(repo RepositoryInterface, unwrapFilterRepos bool) []RepositoryInterface {
	var repos []RepositoryInterface
	collectFlattenedRepositories(repo, unwrapFilterRepos, &repos)

	return repos
}

func collectFlattenedRepositories(repo RepositoryInterface, unwrapFilterRepos bool, repos *[]RepositoryInterface) {
	// unwrap filter repos
	if filter, ok := repo.(*FilterRepository); ok && unwrapFilterRepos {
		repo = filter.Repository()
	}

	composite, ok := AsComposite(repo)
	if !ok {
		*repos = append(*repos, repo)

		return
	}

	for _, r := range composite.Repositories() {
		collectFlattenedRepositories(r, unwrapFilterRepos, repos)
	}
}
