// Ports src/Composer/Repository/CompositeRepository.php.

package repository

import (
	"strings"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
)

// CompositeRepository ports Composer\Repository\CompositeRepository: the
// union of several repositories.
type CompositeRepository struct {
	repositories []RepositoryInterface
	// self is the outermost type, for addRepository's override.
	self interface {
		AddRepository(repository RepositoryInterface) error
	}
}

var _ RepositoryInterface = (*CompositeRepository)(nil)

// NewCompositeRepository ports new CompositeRepository($repositories).
func NewCompositeRepository(repositories []RepositoryInterface) (*CompositeRepository, error) {
	r := &CompositeRepository{}
	r.self = r
	if err := r.addRepositories(repositories); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *CompositeRepository) addRepositories(repositories []RepositoryInterface) error {
	for _, repo := range repositories {
		if err := r.self.AddRepository(repo); err != nil {
			return err
		}
	}

	return nil
}

// AsComposite reports `$repo instanceof CompositeRepository` (which
// InstalledRepository is too) and returns its CompositeRepository part.
func AsComposite(repo RepositoryInterface) (*CompositeRepository, bool) {
	switch r := repo.(type) {
	case *CompositeRepository:
		return r, true
	case *InstalledRepository:
		return &r.CompositeRepository, true
	}

	return nil, false
}

// Class returns the PHP class name.
func (r *CompositeRepository) Class() string { return `Composer\Repository\CompositeRepository` }

// RepoName ports CompositeRepository::getRepoName.
func (r *CompositeRepository) RepoName() string {
	return "composite repo (" + r.repoNames() + ")"
}

func (r *CompositeRepository) repoNames() string {
	names := make([]string, len(r.repositories))
	for i, repo := range r.repositories {
		names[i] = repo.RepoName()
	}

	return strings.Join(names, ", ")
}

// Repositories ports getRepositories: the wrapped repositories.
func (r *CompositeRepository) Repositories() []RepositoryInterface { return r.repositories }

// HasPackage ports CompositeRepository::hasPackage.
func (r *CompositeRepository) HasPackage(p pkg.PackageInterface) (bool, error) {
	for _, repo := range r.repositories {
		has, err := repo.HasPackage(p)
		if err != nil || has {
			return has, calledAt(err, 68)
		}
	}

	return false, nil
}

// FindPackage ports CompositeRepository::findPackage.
func (r *CompositeRepository) FindPackage(name string, constraint semver.ConstraintInterface) (pkg.PackageInterface, error) {
	for _, repo := range r.repositories {
		p, err := repo.FindPackage(name, constraint)
		if err != nil || p != nil {
			return p, calledAt(err, 83)
		}
	}

	return nil, nil
}

// FindPackages ports CompositeRepository::findPackages.
func (r *CompositeRepository) FindPackages(name string, constraint semver.ConstraintInterface) ([]pkg.PackageInterface, error) {
	var packages []pkg.PackageInterface
	for _, repo := range r.repositories {
		found, err := repo.FindPackages(name, constraint)
		if err != nil {
			return nil, calledAt(err, 100)
		}
		packages = append(packages, found...)
	}

	return packages, nil
}

// LoadPackages ports CompositeRepository::loadPackages.
func (r *CompositeRepository) LoadPackages(packageNameMap *ConstraintMap, acceptableStabilities, stabilityFlags *php.Array, alreadyLoaded AlreadyLoaded) (LoadResult, error) {
	var result LoadResult
	found := make(map[string]struct{})
	for _, repo := range r.repositories {
		loaded, err := repo.LoadPackages(packageNameMap, acceptableStabilities, stabilityFlags, alreadyLoaded)
		if err != nil {
			return LoadResult{}, calledAt(err, 115)
		}
		result.Packages = append(result.Packages, loaded.Packages...)
		for _, name := range loaded.NamesFound {
			if _, ok := found[name]; !ok {
				found[name] = struct{}{}
				result.NamesFound = append(result.NamesFound, name)
			}
		}
	}

	return result, nil
}

// Search ports CompositeRepository::search without an IO.
func (r *CompositeRepository) Search(query string, mode int, typ string) ([]SearchResult, error) {
	return r.SearchWithIO(query, mode, typ, nil)
}

// SearchWithIO ports CompositeRepository::search: with io, each
// repository's result count is reported in very verbose mode.
func (r *CompositeRepository) SearchWithIO(query string, mode int, typ string, out io.IO) ([]SearchResult, error) {
	var matches []SearchResult
	for _, repo := range r.repositories {
		results, err := repo.Search(query, mode, typ)
		if err != nil {
			return nil, calledAt(err, 134)
		}
		if out != nil {
			out.WriteError("Searched "+repo.RepoName()+", found <info>"+itoa(len(results))+"</info> result(s)", true, io.VeryVerbose)
		}
		matches = append(matches, results...)
	}

	return matches, nil
}

// Packages ports CompositeRepository::getPackages.
func (r *CompositeRepository) Packages() ([]pkg.PackageInterface, error) {
	var packages []pkg.PackageInterface
	for _, repo := range r.repositories {
		found, err := repo.Packages()
		if err != nil {
			return nil, calledAt(err, 152)
		}
		packages = append(packages, found...)
	}

	return packages, nil
}

// Providers ports CompositeRepository::getProviders.
func (r *CompositeRepository) Providers(packageName string) ([]ProviderInfo, error) {
	var results []ProviderInfo
	for _, repo := range r.repositories {
		providers, err := repo.Providers(packageName)
		if err != nil {
			return nil, calledAt(err, 166)
		}
		results = mergeProviders(results, providers)
	}

	return results, nil
}

// mergeProviders is array_merge for provider lists keyed by name: a later
// entry replaces an earlier one of the same name in place.
func mergeProviders(dst, src []ProviderInfo) []ProviderInfo {
	if len(dst) == 0 {
		return append(dst, src...)
	}
	index := make(map[string]int, len(dst)+len(src))
	for i, p := range dst {
		index[p.Name] = i
	}
	for _, p := range src {
		if i, ok := index[p.Name]; ok {
			dst[i] = p
		} else {
			index[p.Name] = len(dst)
			dst = append(dst, p)
		}
	}

	return dst
}

// RemovePackage ports CompositeRepository::removePackage: it removes the
// package from each writable repository.
func (r *CompositeRepository) RemovePackage(p pkg.PackageInterface) error {
	for _, repo := range r.repositories {
		if writable, ok := repo.(WritableRepository); ok {
			if err := writable.RemovePackage(p); err != nil {
				return err
			}
		}
	}

	return nil
}

// Count ports CompositeRepository::count.
func (r *CompositeRepository) Count() (int, error) {
	total := 0
	for _, repo := range r.repositories {
		n, err := repo.Count()
		if err != nil {
			return 0, calledAt(err, 189)
		}
		total += n
	}

	return total, nil
}

// calledAt locates an error a repository's method threw at its call in
// CompositeRepository.php (phperr.Locate): one written in PHP left its
// code through that call, which its trace names as Composer's
// (docs/PLUGINS.md §5.12). maestro's own repositories' errors are left
// alone.
func calledAt(err error, line int) error {
	return phperr.Locate(err, "CompositeRepository.php", line)
}

// AddRepository ports CompositeRepository::addRepository: the
// repositories of a composite repository are added one by one.
func (r *CompositeRepository) AddRepository(repository RepositoryInterface) error {
	if composite, ok := AsComposite(repository); ok {
		for _, repo := range composite.Repositories() {
			if err := r.self.AddRepository(repo); err != nil {
				return err
			}
		}

		return nil
	}
	r.repositories = append(r.repositories, repository)

	return nil
}
