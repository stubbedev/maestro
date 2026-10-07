// Ports the createPool, createPoolWithAllPackages, createPoolForPackage and
// createPoolForPackages methods of src/Composer/Repository/RepositorySet.php,
// which live here because the pool is the resolver's (docs/PORTING.md).

package resolver

import (
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
)

// The VersionSelector looks packages up in a RepositorySet.
var _ version.RepositorySet = (*repository.RepositorySet)(nil)

// CreatePoolOptions are createPool's optional arguments.
type CreatePoolOptions struct {
	// EventDispatcher dispatches PRE_POOL_CREATE; nil is none.
	EventDispatcher EventDispatcher
	// PoolOptimizer optimizes the pool; nil is none.
	PoolOptimizer *PoolOptimizer
	// IgnoredTypes are package types not to load.
	IgnoredTypes []string
	// AllowedTypes are the only package types to load: null (the zero
	// value) allows all, [] loads no package at all.
	AllowedTypes               php.Nullable[[]string]
	SecurityAdvisoryPoolFilter *SecurityAdvisoryPoolFilter
	FilterListPoolFilter       *FilterListPoolFilter
}

// CreatePool ports RepositorySet::createPool: the pool of the packages the
// request can need.
func CreatePool(set *repository.RepositorySet, request *Request, out io.IO, opts CreatePoolOptions) (*Pool, error) {
	poolBuilder := NewPoolBuilder(set.AcceptableStabilities(), set.StabilityFlags(), set.RootAliasList(), set.RootReferences(), out, PoolBuilderOptions{
		EventDispatcher:            opts.EventDispatcher,
		PoolOptimizer:              opts.PoolOptimizer,
		TemporaryConstraints:       set.TemporaryConstraints(),
		SecurityAdvisoryPoolFilter: opts.SecurityAdvisoryPoolFilter,
		FilterListPoolFilter:       opts.FilterListPoolFilter,
	})
	poolBuilder.SetIgnoredTypes(opts.IgnoredTypes)
	poolBuilder.SetAllowedTypes(opts.AllowedTypes)

	if err := set.LockForPool(); err != nil {
		return nil, err
	}

	pool, err := poolBuilder.BuildPool(set.Repositories(), request)

	return pool, err
}

// CreatePoolWithAllPackages ports RepositorySet::createPoolWithAllPackages:
// a pool of every package of the repositories, with the root aliases. It
// is meant for the install from a lock file, where all packages are
// needed.
func CreatePoolWithAllPackages(set *repository.RepositorySet) (*Pool, error) {
	if err := set.LockForPool(); err != nil {
		return nil, err
	}

	rootAliases := set.RootAliases()
	var packages []pkg.PackageInterface
	for _, repo := range set.Repositories() {
		repoPackages, err := repo.Packages()
		if err != nil {
			return nil, err
		}
		for _, p := range repoPackages {
			packages = append(packages, p)

			if alias, ok := rootAliases[p.Name()][p.Version()]; ok {
				for {
					a, isAlias := p.(pkg.Alias)
					if !isAlias {
						break
					}
					p = a.AliasOf()
				}
				packages = append(packages, newRootAlias(p, alias))
			}
		}
	}

	return NewPool(packages, nil, nil), nil
}

// CreatePoolForPackage ports RepositorySet::createPoolForPackage;
// lockedRepo may be nil.
func CreatePoolForPackage(set *repository.RepositorySet, packageName string, lockedRepo *repository.LockArrayRepository) (*Pool, error) {
	// TODO unify this with above in some simpler version without "request"?
	return CreatePoolForPackages(set, []string{packageName}, lockedRepo)
}

// CreatePoolForPackages ports RepositorySet::createPoolForPackages: a pool
// with only the named packages loaded.
func CreatePoolForPackages(set *repository.RepositorySet, packageNames []string, lockedRepo *repository.LockArrayRepository) (*Pool, error) {
	request := NewRequest(lockedRepo)

	var allowedPackages []string
	for _, packageName := range packageNames {
		if repository.IsPlatformPackage(packageName) {
			return nil, &util.LogicError{Message: "createPoolForPackage(s) can not be used for platform packages, as they are never loaded by the PoolBuilder which expects them to be fixed. Use createPoolWithAllPackages or pass in a proper request with the platform packages you need fixed in it."}
		}

		if err := request.RequireName(packageName, nil); err != nil {
			return nil, err
		}
		allowedPackages = append(allowedPackages, php.Strtolower(packageName))
	}

	if len(allowedPackages) > 0 {
		request.RestrictPackages(allowedPackages)
	}

	return CreatePool(set, request, io.NewNullIO(), CreatePoolOptions{})
}
