// Ports src/Composer/Repository/FilterRepository.php.

package repository

import (
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// FilterRepository ports Composer\Repository\FilterRepository: it limits
// a repository to the packages its "only" or "exclude" patterns allow,
// and with canonical false lets lower priority repositories provide the
// same packages.
type FilterRepository struct {
	repo      RepositoryInterface
	only      *php.Regexp
	exclude   *php.Regexp
	canonical bool
}

var (
	_ RepositoryInterface = (*FilterRepository)(nil)
	_ AdvisoryProvider    = (*FilterRepository)(nil)
	_ FilterListProvider  = (*FilterRepository)(nil)
)

// NewFilterRepository ports new FilterRepository($repo, $options): options
// may hold "only" and "exclude" (lists of package name patterns) and
// "canonical" (a bool).
func NewFilterRepository(repo RepositoryInterface, options *php.Array) (*FilterRepository, error) {
	r := &FilterRepository{repo: repo, canonical: true}
	var err error
	if r.only, err = filterOption(repo, options, "only"); err != nil {
		return nil, err
	}
	if r.exclude, err = filterOption(repo, options, "exclude"); err != nil {
		return nil, err
	}
	if r.exclude != nil && r.only != nil {
		return nil, &util.InvalidArgumentError{Message: `Only one of "only" and "exclude" can be specified for repository ` + repo.RepoName()}
	}
	if v, _ := options.Get("canonical"); v != nil {
		canonical, ok := v.(bool)
		if !ok {
			return nil, &util.InvalidArgumentError{Message: `"canonical" key for repository ` + repo.RepoName() + " should be a boolean"}
		}
		r.canonical = canonical
	}

	return r, nil
}

// filterOption compiles BasePackage::packageNamesToRegexp($options[$key]);
// nil when the option is unset.
func filterOption(repo RepositoryInterface, options *php.Array, key string) (*php.Regexp, error) {
	v, _ := options.Get(key)
	if v == nil {
		return nil, nil
	}
	names, ok := v.(*php.Array)
	if !ok {
		return nil, &util.InvalidArgumentError{Message: `"` + key + `" key for repository ` + repo.RepoName() + " should be an array"}
	}

	return php.Compile(pkg.PackageNamesToRegexp(stringValues(names), "{^(?:%s)$}iD"))
}

// Class returns the PHP class name.
func (r *FilterRepository) Class() string { return `Composer\Repository\FilterRepository` }

// RepoName ports FilterRepository::getRepoName: the wrapped repository's.
func (r *FilterRepository) RepoName() string { return r.repo.RepoName() }

// Repository ports getRepository: the wrapped repository.
func (r *FilterRepository) Repository() RepositoryInterface { return r.repo }

// HasPackage ports FilterRepository::hasPackage.
func (r *FilterRepository) HasPackage(p pkg.PackageInterface) (bool, error) {
	return r.repo.HasPackage(p)
}

// FindPackage ports FilterRepository::findPackage.
func (r *FilterRepository) FindPackage(name string, constraint semver.ConstraintInterface) (pkg.PackageInterface, error) {
	if allowed, err := r.isAllowed(name); err != nil || !allowed {
		return nil, err
	}

	return r.repo.FindPackage(name, constraint)
}

// FindPackages ports FilterRepository::findPackages.
func (r *FilterRepository) FindPackages(name string, constraint semver.ConstraintInterface) ([]pkg.PackageInterface, error) {
	if allowed, err := r.isAllowed(name); err != nil || !allowed {
		return nil, err
	}

	return r.repo.FindPackages(name, constraint)
}

// allowedNames returns the entries of m whose names are allowed.
func allowedNames[V any](r *FilterRepository, m *NameMap[V]) (*NameMap[V], error) {
	allowed := &NameMap[V]{}
	for name, v := range m.All() {
		ok, err := r.isAllowed(name)
		if err != nil {
			return nil, err
		}
		if ok {
			allowed.Set(name, v)
		}
	}

	return allowed, nil
}

// LoadPackages ports FilterRepository::loadPackages.
func (r *FilterRepository) LoadPackages(packageNameMap *ConstraintMap, acceptableStabilities, stabilityFlags *php.Array, alreadyLoaded AlreadyLoaded) (LoadResult, error) {
	allowed, err := allowedNames(r, packageNameMap)
	if err != nil {
		return LoadResult{}, err
	}
	if allowed.Len() == 0 {
		return LoadResult{}, nil
	}

	result, err := r.repo.LoadPackages(allowed, acceptableStabilities, stabilityFlags, alreadyLoaded)
	if err != nil {
		return LoadResult{}, err
	}
	if !r.canonical {
		result.NamesFound = nil
	}

	return result, nil
}

// Search ports FilterRepository::search.
func (r *FilterRepository) Search(query string, mode int, typ string) ([]SearchResult, error) {
	found, err := r.repo.Search(query, mode, typ)
	if err != nil {
		return nil, err
	}
	var result []SearchResult
	for _, p := range found {
		allowed, err := r.isAllowed(p.Name)
		if err != nil {
			return nil, err
		}
		if allowed {
			result = append(result, p)
		}
	}

	return result, nil
}

// Packages ports FilterRepository::getPackages.
func (r *FilterRepository) Packages() ([]pkg.PackageInterface, error) {
	packages, err := r.repo.Packages()
	if err != nil {
		return nil, err
	}
	var result []pkg.PackageInterface
	for _, p := range packages {
		allowed, err := r.isAllowed(p.Name())
		if err != nil {
			return nil, err
		}
		if allowed {
			result = append(result, p)
		}
	}

	return result, nil
}

// Providers ports FilterRepository::getProviders.
func (r *FilterRepository) Providers(packageName string) ([]ProviderInfo, error) {
	providers, err := r.repo.Providers(packageName)
	if err != nil {
		return nil, err
	}
	var result []ProviderInfo
	for _, p := range providers {
		allowed, err := r.isAllowed(p.Name)
		if err != nil {
			return nil, err
		}
		if allowed {
			result = append(result, p)
		}
	}

	return result, nil
}

// Count ports FilterRepository::count.
func (r *FilterRepository) Count() (int, error) {
	n, err := r.repo.Count()
	if err != nil || n == 0 {
		return 0, err
	}
	packages, err := r.Packages()

	return len(packages), err
}

// HasSecurityAdvisories ports FilterRepository::hasSecurityAdvisories.
func (r *FilterRepository) HasSecurityAdvisories() (bool, error) {
	provider, ok := r.repo.(AdvisoryProvider)
	if !ok {
		return false, nil
	}

	return provider.HasSecurityAdvisories()
}

// SecurityAdvisories ports FilterRepository::getSecurityAdvisories.
func (r *FilterRepository) SecurityAdvisories(packageConstraintMap *ConstraintMap, allowPartial bool) (AdvisoryResult, error) {
	provider, ok := r.repo.(AdvisoryProvider)
	if !ok {
		return AdvisoryResult{Advisories: &NameMap[[]Advisory]{}}, nil
	}
	allowed, err := allowedNames(r, packageConstraintMap)
	if err != nil {
		return AdvisoryResult{}, err
	}

	return provider.SecurityAdvisories(allowed, allowPartial)
}

// HasFilter ports FilterRepository::hasFilter.
func (r *FilterRepository) HasFilter() (bool, error) {
	provider, ok := r.repo.(FilterListProvider)
	if !ok {
		return false, nil
	}

	return provider.HasFilter()
}

// Filter ports FilterRepository::getFilter.
func (r *FilterRepository) Filter(packageConstraintMap *ConstraintMap, configuredLists []string) (*NameMap[[]*FilterListEntry], error) {
	provider, ok := r.repo.(FilterListProvider)
	if !ok {
		return &NameMap[[]*FilterListEntry]{}, nil
	}
	allowed, err := allowedNames(r, packageConstraintMap)
	if err != nil {
		return nil, err
	}

	return provider.Filter(allowed, configuredLists)
}

// FilterLists ports FilterRepository::getFilterLists.
func (r *FilterRepository) FilterLists() ([]string, error) {
	provider, ok := r.repo.(FilterListProvider)
	if !ok {
		return nil, nil
	}

	return provider.FilterLists()
}

// isAllowed ports FilterRepository::isAllowed.
func (r *FilterRepository) isAllowed(name string) (bool, error) {
	if r.only != nil {
		return r.only.IsMatch(name)
	}
	if r.exclude == nil {
		return true, nil
	}
	matched, err := r.exclude.IsMatch(name)

	return !matched, err
}
