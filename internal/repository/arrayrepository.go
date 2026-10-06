// Ports src/Composer/Repository/ArrayRepository.php.

package repository

import (
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/semver"
)

// ArrayRepository ports Composer\Repository\ArrayRepository: a repository
// holding its packages in memory.
type ArrayRepository struct {
	// outer is the repository the packages belong to: this one, or the
	// type embedding it.
	outer RepositoryInterface
	// hooks dispatches the overridable initialize() and addPackage().
	hooks arrayHooks

	packages []pkg.PackageInterface
	loaded   bool // $this->packages !== null
	// packageMap caches hasPackage: unique names, nil when invalidated.
	packageMap map[string]struct{}
	rev        uint64
}

// arrayHooks are the methods of ArrayRepository its subclasses override.
type arrayHooks interface {
	initialize() error
	addPackage(p pkg.PackageInterface) error
}

var _ RepositoryInterface = (*ArrayRepository)(nil)

// NewArrayRepository ports new ArrayRepository($packages).
func NewArrayRepository(packages []pkg.PackageInterface) (*ArrayRepository, error) {
	r := &ArrayRepository{}
	r.bind(r, r)
	if err := r.addPackages(packages); err != nil {
		return nil, err
	}

	return r, nil
}

// bind sets the repository packages are attached to and the receiver of
// the overridable methods; embedding types call it first thing.
func (r *ArrayRepository) bind(outer RepositoryInterface, hooks arrayHooks) {
	r.outer = outer
	r.hooks = hooks
}

// addPackages is the loop of ArrayRepository::__construct.
func (r *ArrayRepository) addPackages(packages []pkg.PackageInterface) error {
	for _, p := range packages {
		if err := r.hooks.addPackage(p); err != nil {
			return err
		}
	}

	return nil
}

// RepoName ports ArrayRepository::getRepoName.
func (r *ArrayRepository) RepoName() string {
	n := r.countLoaded()
	s := ""
	if n > 1 {
		s = "s"
	}

	return "array repo (defining " + itoa(n) + " package" + s + ")"
}

// countLoaded is count() for getRepoName, which cannot fail: when
// initializing fails, the packages loaded so far.
func (r *ArrayRepository) countLoaded() int {
	_ = r.ensure()

	return len(r.packages)
}

// Class returns the PHP class name.
func (r *ArrayRepository) Class() string { return `Composer\Repository\ArrayRepository` }

// Rev changes whenever the package list changes.
func (r *ArrayRepository) Rev() uint64 { return r.rev }

func (r *ArrayRepository) ensure() error {
	if r.loaded {
		return nil
	}

	return r.hooks.initialize()
}

// initialize ports ArrayRepository::initialize.
func (r *ArrayRepository) initialize() error {
	r.baseInitialize()

	return nil
}

func (r *ArrayRepository) baseInitialize() {
	r.packages = []pkg.PackageInterface{}
	r.loaded = true
	r.packageMap = nil
	r.rev++
}

// unload is $this->packages = null.
func (r *ArrayRepository) unload() {
	r.packages = nil
	r.loaded = false
	r.packageMap = nil
	r.rev++
}

// LoadPackages ports ArrayRepository::loadPackages.
func (r *ArrayRepository) LoadPackages(packageNameMap *ConstraintMap, acceptableStabilities, stabilityFlags *php.Array, alreadyLoaded AlreadyLoaded) (LoadResult, error) {
	packages, err := r.Packages()
	if err != nil {
		return LoadResult{}, err
	}

	var result []pkg.PackageInterface
	selected := make(map[pkg.PackageInterface]struct{})
	add := func(p pkg.PackageInterface) {
		if _, ok := selected[p]; !ok {
			selected[p] = struct{}{}
			result = append(result, p)
		}
	}
	var namesFound []string
	found := make(map[string]struct{})
	for _, p := range packages {
		name := p.Name()
		constraint, ok := packageNameMap.Get(name)
		if !ok {
			continue
		}
		if versionMatches(constraint, p.Version()) &&
			version.IsPackageAcceptable(acceptableStabilities, stabilityFlags, p.Names(true), p.Stability()) &&
			alreadyLoaded[name][p.Version()] == nil {
			// add selected packages which match stability requirements
			add(p)
			// add the aliased package for packages where the alias matches
			if alias, ok := p.(pkg.Alias); ok {
				add(alias.AliasOf())
			}
		}
		if _, ok := found[name]; !ok {
			found[name] = struct{}{}
			namesFound = append(namesFound, name)
		}
	}

	// add aliases of packages that were selected, even if the aliases did not match
	for _, p := range packages {
		if alias, ok := p.(pkg.Alias); ok {
			if _, ok := selected[alias.AliasOf()]; ok {
				add(p)
			}
		}
	}

	return LoadResult{NamesFound: namesFound, Packages: result}, nil
}

// FindPackage ports ArrayRepository::findPackage.
func (r *ArrayRepository) FindPackage(name string, constraint semver.ConstraintInterface) (pkg.PackageInterface, error) {
	name = strtolower(name)
	packages, err := r.Packages()
	if err != nil {
		return nil, err
	}
	for _, p := range packages {
		if name == p.Name() && versionMatches(constraint, p.Version()) {
			return p, nil
		}
	}

	return nil, nil
}

// FindPackages ports ArrayRepository::findPackages.
func (r *ArrayRepository) FindPackages(name string, constraint semver.ConstraintInterface) ([]pkg.PackageInterface, error) {
	// normalize name
	name = strtolower(name)
	packages, err := r.Packages()
	if err != nil {
		return nil, err
	}
	var result []pkg.PackageInterface
	for _, p := range packages {
		if name == p.Name() && versionMatches(constraint, p.Version()) {
			result = append(result, p)
		}
	}

	return result, nil
}

// searchRegex builds the pattern ArrayRepository::search matches with.
func searchRegex(query string, mode int) (string, error) {
	if mode == SearchFulltext {
		query = php.PregQuote(query, "")
	}
	// vendor/name searches expect the caller to have preg_quoted the query
	parts, err := php.PregSplit(`{\s+}`, query, -1, 0)
	if err != nil {
		return "", err
	}

	return "{(?:" + strings.Join(parts, "|") + ")}i", nil
}

// Search ports ArrayRepository::search.
func (r *ArrayRepository) Search(query string, mode int, typ string) ([]SearchResult, error) {
	packages, err := r.Packages()
	if err != nil {
		return nil, err
	}
	pattern, err := searchRegex(query, mode)
	if err != nil {
		return nil, err
	}
	re, err := php.Compile(pattern)
	if err != nil {
		// Preg::isMatch's PcreException for a pattern that does not compile
		_, err = php.PregIsMatch(pattern, "")

		return nil, err
	}

	var matches []SearchResult
	seen := make(map[string]struct{})
	for _, p := range packages {
		name := p.Name()
		if mode == SearchVendor {
			name = firstSegment(name)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		if typ != "" && p.Type() != typ {
			continue
		}

		matched, err := re.IsMatch(name)
		if err != nil {
			return nil, err
		}
		complete, isComplete := p.(pkg.CompletePackageInterface)
		if !matched && mode == SearchFulltext && isComplete {
			if matched, err = re.IsMatch(keywordsAndDescription(complete)); err != nil {
				return nil, err
			}
		}
		if !matched {
			continue
		}

		seen[name] = struct{}{}
		if mode == SearchVendor {
			matches = append(matches, SearchResult{Name: name})

			continue
		}
		result := SearchResult{Name: p.PrettyName()}
		if isComplete {
			result.Description = complete.Description()
			if complete.IsAbandoned() {
				if replacement := complete.ReplacementPackage(); php.ToBool(replacement.S) {
					result.Abandoned = replacement.S
				} else {
					result.Abandoned = true
				}
			}
		}
		matches = append(matches, result)
	}

	return matches, nil
}

// keywordsAndDescription is implode(' ', (array) $package->getKeywords())
// . ' ' . $package->getDescription().
func keywordsAndDescription(p pkg.CompletePackageInterface) string {
	var b strings.Builder
	i := 0
	for _, k := range p.Keywords().All() {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(php.ToString(k))
		i++
	}
	b.WriteByte(' ')
	b.WriteString(p.Description().S)

	return b.String()
}

// HasPackage ports ArrayRepository::hasPackage.
func (r *ArrayRepository) HasPackage(p pkg.PackageInterface) (bool, error) {
	if r.packageMap == nil {
		packages, err := r.Packages()
		if err != nil {
			return false, err
		}
		r.packageMap = make(map[string]struct{}, len(packages))
		for _, repoPackage := range packages {
			r.packageMap[repoPackage.UniqueName()] = struct{}{}
		}
	}
	_, ok := r.packageMap[p.UniqueName()]

	return ok, nil
}

// AddPackage ports ArrayRepository::addPackage (or the embedding type's
// override).
func (r *ArrayRepository) AddPackage(p pkg.PackageInterface) error { return r.hooks.addPackage(p) }

func (r *ArrayRepository) addPackage(p pkg.PackageInterface) error { return r.addPackageBase(p) }

// addPackageBase is ArrayRepository::addPackage itself (parent::addPackage).
func (r *ArrayRepository) addPackageBase(p pkg.PackageInterface) error {
	if err := r.ensure(); err != nil {
		return err
	}
	if err := p.SetRepository(r.outer); err != nil {
		return err
	}
	r.packages = append(r.packages, p)

	if alias, ok := p.(pkg.Alias); ok {
		aliased := alias.AliasOf()
		if aliased.Repository() == nil {
			if err := r.hooks.addPackage(aliased); err != nil {
				return err
			}
		}
	}

	// invalidate package map cache
	r.packageMap = nil
	r.rev++

	return nil
}

// Providers ports ArrayRepository::getProviders.
func (r *ArrayRepository) Providers(packageName string) ([]ProviderInfo, error) {
	packages, err := r.Packages()
	if err != nil {
		return nil, err
	}
	var result []ProviderInfo
	seen := make(map[string]struct{})
	for _, candidate := range packages {
		name := candidate.Name()
		if _, ok := seen[name]; ok {
			continue
		}
		for link := range candidate.Provides().Values() {
			if packageName == link.Target() {
				info := ProviderInfo{Name: name, Type: candidate.Type()}
				if complete, ok := candidate.(pkg.CompletePackageInterface); ok {
					info.Description = complete.Description()
				}
				seen[name] = struct{}{}
				result = append(result, info)

				break
			}
		}
	}

	return result, nil
}

// CreateAliasPackage ports ArrayRepository::createAliasPackage: an alias
// of the package at the root of p's alias chain, complete when that
// package is.
func CreateAliasPackage(p pkg.PackageInterface, alias, prettyAlias string) pkg.Alias {
	for {
		a, ok := p.(pkg.Alias)
		if !ok {
			break
		}
		p = a.AliasOf()
	}
	if _, ok := pkg.AsCompletePackage(p); ok {
		if complete, ok := p.(pkg.CompletePackageInterface); ok {
			return pkg.NewCompleteAliasPackage(complete, alias, prettyAlias)
		}
	}

	return pkg.NewAliasPackage(p, alias, prettyAlias)
}

// RemovePackage ports ArrayRepository::removePackage.
func (r *ArrayRepository) RemovePackage(p pkg.PackageInterface) error {
	packageID := p.UniqueName()
	packages, err := r.Packages()
	if err != nil {
		return err
	}
	for i, repoPackage := range packages {
		if packageID == repoPackage.UniqueName() {
			// a new array: slices returned by Packages() stay as they
			// were (PHP's getPackages() returns a copy, which callers
			// iterate while removing, as Factory::purgePackages does)
			r.packages = slices.Concat(r.packages[:i:i], r.packages[i+1:])

			// invalidate package map cache
			r.packageMap = nil
			r.rev++

			return nil
		}
	}

	return nil
}

// Packages ports ArrayRepository::getPackages. The slice must not be
// modified.
func (r *ArrayRepository) Packages() ([]pkg.PackageInterface, error) {
	if err := r.ensure(); err != nil {
		return nil, err
	}

	return slices.Clip(r.packages), nil
}

// Count ports ArrayRepository::count.
func (r *ArrayRepository) Count() (int, error) {
	if err := r.ensure(); err != nil {
		return 0, err
	}

	return len(r.packages), nil
}
