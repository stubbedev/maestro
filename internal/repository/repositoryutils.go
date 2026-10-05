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
	return filterRequiredPackages(packages, requirer, includeRequireDev, nil)
}

func filterRequiredPackages(packages []pkg.PackageInterface, requirer pkg.PackageInterface, includeRequireDev bool, bucket []pkg.PackageInterface) []pkg.PackageInterface {
	requires := requirer.Requires()
	if includeRequireDev {
		requires = MergeLinks(requires, requirer.DevRequires())
	}
	for _, candidate := range packages {
		if slices.ContainsFunc(candidate.Names(true), requires.Has) && !slices.Contains(bucket, candidate) {
			bucket = append(bucket, candidate)
			bucket = filterRequiredPackages(packages, candidate, false, bucket)
		}
	}

	return bucket
}

// FlattenRepositories ports RepositoryUtils::flattenRepositories: the
// repositories of composite (and installed) repositories, and with
// unwrapFilterRepos those FilterRepositories wrap, as a flat list.
func FlattenRepositories(repo RepositoryInterface, unwrapFilterRepos bool) []RepositoryInterface {
	// unwrap filter repos
	if filter, ok := repo.(*FilterRepository); ok && unwrapFilterRepos {
		repo = filter.Repository()
	}

	composite, ok := AsComposite(repo)
	if !ok {
		return []RepositoryInterface{repo}
	}

	var repos []RepositoryInterface
	for _, r := range composite.Repositories() {
		repos = append(repos, FlattenRepositories(r, unwrapFilterRepos)...)
	}

	return repos
}
