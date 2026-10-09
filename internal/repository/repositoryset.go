// Ports src/Composer/Repository/RepositorySet.php without the createPool*
// methods, which are functions of internal/resolver taking the set.

package repository

import (
	"errors"
	"slices"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// RepositorySet flags: RepositorySet::ALLOW_*.
const (
	// AllowUnacceptableStabilities returns packages even though their
	// stability does not match the required stability.
	AllowUnacceptableStabilities = 1
	// AllowShadowedRepositories looks packages up in all repositories,
	// even after they have been found in a higher priority one.
	AllowShadowedRepositories = 2
)

// RootAlias is an inline alias of the root package's requirements, as
// RootPackageLoader collects them: ['package' => ..., 'version' => ...,
// 'alias' => ..., 'alias_normalized' => ...].
type RootAlias struct {
	Package         string
	Version         string
	Alias           string
	AliasNormalized string
}

// AliasTarget is the alias of one version in RepositorySet's root aliases:
// ['alias' => ..., 'alias_normalized' => ...].
type AliasTarget struct {
	Alias           string
	AliasNormalized string
}

// RepositorySet ports Composer\Repository\RepositorySet: the repositories
// packages are looked up in, by priority, with the root package's
// stability settings, aliases and references.
type RepositorySet struct {
	rootAliases                map[string]map[string]AliasTarget
	rootAliasList              []RootAlias
	rootReferences             *php.Array
	repositories               []RepositoryInterface
	acceptableStabilities      *php.Array
	stabilityFlags             *php.Array
	rootRequires               *ConstraintMap
	temporaryConstraints       *ConstraintMap
	locked                     bool
	allowInstalledRepositories bool
}

// NewRepositorySet ports new RepositorySet($minimumStability,
// $stabilityFlags, $rootAliases, $rootReferences, $rootRequires,
// $temporaryConstraints). stabilityFlags maps package names to
// BasePackage::STABILITY_* values and rootReferences package names to
// source references; nil arrays and maps are empty. Platform packages are
// dropped from rootRequires.
func NewRepositorySet(minimumStability string, stabilityFlags *php.Array, rootAliases []RootAlias, rootReferences *php.Array, rootRequires, temporaryConstraints *ConstraintMap) (*RepositorySet, error) {
	minimum, ok := pkg.StabilityValue(minimumStability)
	if !ok {
		return nil, &util.ErrorException{Message: `Undefined array key "` + minimumStability + `"`}
	}
	s := &RepositorySet{
		rootAliases:           RootAliasesPerPackage(rootAliases),
		rootAliasList:         rootAliases,
		rootReferences:        rootReferences,
		acceptableStabilities: php.NewArray(),
		stabilityFlags:        stabilityFlags,
		rootRequires:          &ConstraintMap{},
		temporaryConstraints:  temporaryConstraints,
	}
	if s.rootReferences == nil {
		s.rootReferences = php.NewArray()
	}
	if s.stabilityFlags == nil {
		s.stabilityFlags = php.NewArray()
	}
	if s.temporaryConstraints == nil {
		s.temporaryConstraints = &ConstraintMap{}
	}
	for stability, value := range pkg.Stabilities().All() {
		if php.ToInt(value) <= int64(minimum) {
			s.acceptableStabilities.SetKey(stability, value)
		}
	}
	for name, constraint := range rootRequires.All() {
		if !IsPlatformPackage(name) {
			s.rootRequires.Set(name, constraint)
		}
	}

	return s, nil
}

// RootAliasesPerPackage ports RepositorySet::getRootAliasesPerPackage:
// package name => version => alias.
func RootAliasesPerPackage(aliases []RootAlias) map[string]map[string]AliasTarget {
	normalized := make(map[string]map[string]AliasTarget)
	for _, alias := range aliases {
		if normalized[alias.Package] == nil {
			normalized[alias.Package] = make(map[string]AliasTarget)
		}
		normalized[alias.Package][alias.Version] = AliasTarget{Alias: alias.Alias, AliasNormalized: alias.AliasNormalized}
	}

	return normalized
}

// AllowInstalledRepositories ports allowInstalledRepositories.
func (s *RepositorySet) AllowInstalledRepositories(allow bool) { s.allowInstalledRepositories = allow }

// RootRequires ports getRootRequires: package name => constraint from the
// root package, platform requirements excluded.
func (s *RepositorySet) RootRequires() *ConstraintMap { return s.rootRequires }

// TemporaryConstraints ports getTemporaryConstraints.
func (s *RepositorySet) TemporaryConstraints() *ConstraintMap { return s.temporaryConstraints }

// RootAliases returns the root aliases: package name => version => alias.
func (s *RepositorySet) RootAliases() map[string]map[string]AliasTarget { return s.rootAliases }

// RootAliasList returns the root aliases as the set was given them; in
// that order, they give the PHP array of RootAliases (package name =>
// version => alias, in insertion order).
func (s *RepositorySet) RootAliasList() []RootAlias { return s.rootAliasList }

// RootReferences returns the root references: package name => source
// reference.
func (s *RepositorySet) RootReferences() *php.Array { return s.rootReferences }

// AcceptableStabilities returns the stabilities the minimum stability
// accepts: stability name => BasePackage::STABILITY_*.
func (s *RepositorySet) AcceptableStabilities() *php.Array { return s.acceptableStabilities }

// StabilityFlags returns package name => BasePackage::STABILITY_*.
func (s *RepositorySet) StabilityFlags() *php.Array { return s.stabilityFlags }

// Repositories returns the repositories, by priority.
func (s *RepositorySet) Repositories() []RepositoryInterface { return s.repositories }

// LockForPool does what createPool* do before building a pool: it fails
// when the set holds installed repositories it was not allowed to, then
// locks the set against further repositories.
func (s *RepositorySet) LockForPool() error {
	for _, repo := range s.repositories {
		_, installed := repo.(InstalledRepositoryInterface)
		if _, ok := repo.(*InstalledRepository); ok {
			installed = true
		}
		if installed && !s.allowInstalledRepositories {
			return &util.LogicError{Message: "The pool can not accept packages from an installed repository"}
		}
	}

	s.locked = true

	return nil
}

// AddRepository ports RepositorySet::addRepository: the first repositories
// added have the higher priority; a composite repository adds its
// repositories.
func (s *RepositorySet) AddRepository(repo RepositoryInterface) error {
	if s.locked {
		return &util.RuntimeError{Message: "Pool has already been created from this repository set, it cannot be modified anymore."}
	}

	if composite, ok := AsComposite(repo); ok {
		s.repositories = append(s.repositories, composite.Repositories()...)
	} else {
		s.repositories = append(s.repositories, repo)
	}

	return nil
}

// FindPackages ports RepositorySet::findPackages: the packages providing
// or matching name and constraint (nil: any), in repository priority
// order. flags are the Allow* constants.
func (s *RepositorySet) FindPackages(name string, constraint semver.ConstraintInterface, flags int) ([]pkg.PackageInterface, error) {
	ignoreStability := flags&AllowUnacceptableStabilities != 0
	loadFromAllRepos := flags&AllowShadowedRepositories != 0

	var candidates []pkg.PackageInterface
	if loadFromAllRepos {
		for _, repo := range s.repositories {
			found, err := repo.FindPackages(name, constraint)
			if err != nil {
				// a repository written in PHP left its code through the
				// call (docs/PLUGINS.md §5.12)
				return nil, err
			}
			candidates = append(candidates, found...)
		}
	} else {
		acceptable, stabilityFlags := s.acceptableStabilities, s.stabilityFlags
		if ignoreStability {
			acceptable, stabilityFlags = pkg.Stabilities(), php.NewArray()
		}
		nameMap := NewConstraintMap(name, constraint)
		for _, repo := range s.repositories {
			result, err := repo.LoadPackages(nameMap, acceptable, stabilityFlags, nil)
			if err != nil {
				return nil, err
			}
			candidates = append(candidates, result.Packages...)
			// avoid loading the same package again from other repositories once it has been found
			if slices.Contains(result.NamesFound, name) {
				break
			}
		}
	}

	// when using loadPackages above (!$loadFromAllRepos) the repos already filter for stability so no need to do it again
	if ignoreStability || !loadFromAllRepos {
		return candidates, nil
	}

	var result []pkg.PackageInterface
	for _, candidate := range candidates {
		if s.IsPackageAcceptable(candidate.Names(true), candidate.Stability()) {
			result = append(result, candidate)
		}
	}

	return result, nil
}

// SecurityAdvisoriesResult is getSecurityAdvisories' result.
type SecurityAdvisoriesResult struct {
	// Advisories maps package names, sorted, to their advisories.
	Advisories *NameMap[[]Advisory]
	// UnreachableRepos are the messages of the repositories that could
	// not be reached (with ignoreUnreachable).
	UnreachableRepos []string
}

// GetSecurityAdvisories ports RepositorySet::getSecurityAdvisories: every
// advisory of the packages.
func (s *RepositorySet) GetSecurityAdvisories(packageNames []string, allowPartial, ignoreUnreachable bool) (SecurityAdvisoriesResult, error) {
	constraints := &ConstraintMap{}
	for _, name := range packageNames {
		constraints.Set(name, semver.NewMatchAllConstraint())
	}

	return s.securityAdvisoriesForConstraints(constraints, allowPartial, ignoreUnreachable)
}

// GetMatchingSecurityAdvisories ports
// RepositorySet::getMatchingSecurityAdvisories: the advisories affecting
// the versions of the packages (root aliases left out).
func (s *RepositorySet) GetMatchingSecurityAdvisories(packages []pkg.PackageInterface, allowPartial, ignoreUnreachable bool) (SecurityAdvisoriesResult, error) {
	return s.securityAdvisoriesForConstraints(PackageVersionsConstraintMap(packages), allowPartial, ignoreUnreachable)
}

// PackageVersionsConstraintMap is the package constraint map
// RepositorySet::getMatchingSecurityAdvisories and
// FilterListProviderSet::getMatchingFilterLists build: each package name
// mapped to an OR of "== version" for its versions, root aliases left out.
func PackageVersionsConstraintMap(packages []pkg.PackageInterface) *ConstraintMap {
	type versions struct {
		order []string
	}
	type nameVersion struct {
		versions *versions
		version  string
	}
	byName := &NameMap[*versions]{}
	// one set for all names, sized once (a pool has tens of thousands of
	// versions); a pool keeps a name's versions together, so the name
	// looked up last is tried first
	seen := make(map[nameVersion]struct{}, len(packages))
	lastName := ""
	var last *versions
	for _, p := range packages {
		// ignore root alias versions as they are not actual package versions and should not matter when it comes to vulnerabilities
		if alias, ok := p.(pkg.Alias); ok && alias.IsRootPackageAlias() {
			continue
		}
		// key by version so duplicate versions collapse and the resulting OR constraint stays flat
		// (nesting one MultiConstraint per version produces trees deep enough to blow the stack, see composer/semver#177)
		v := last
		if name := p.Name(); last == nil || name != lastName {
			var ok bool
			if v, ok = byName.Get(name); !ok {
				v = &versions{}
				byName.Set(name, v)
			}
			lastName, last = name, v
		}
		key := nameVersion{v, p.Version()}
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			v.order = append(v.order, key.version)
		}
	}

	constraints := &ConstraintMap{}
	for name, v := range byName.All() {
		constraints.Set(name, semver.CreateMultiConstraint(semver.NewConstraintsOp(semver.OpEQ, v.order), false))
	}

	return constraints
}

// securityAdvisoriesForConstraints ports
// RepositorySet::getSecurityAdvisoriesForConstraints.
func (s *RepositorySet) securityAdvisoriesForConstraints(packageConstraintMap *ConstraintMap, allowPartial, ignoreUnreachable bool) (SecurityAdvisoriesResult, error) {
	merged := &NameMap[[]Advisory]{}
	var unreachable []string
	for _, repo := range s.repositories {
		provider, ok := repo.(AdvisoryProvider)
		if !ok {
			continue
		}
		result, err := s.repoAdvisories(provider, packageConstraintMap, allowPartial)
		if err != nil {
			var transport *util.TransportError
			if !ignoreUnreachable || !errors.As(err, &transport) {
				return SecurityAdvisoriesResult{}, err
			}
			unreachable = append(unreachable, transport.Error())

			continue
		}
		for name, advisories := range result.All() {
			existing, _ := merged.Get(name)
			merged.Set(name, append(existing, advisories...))
		}
	}

	names := merged.Keys()
	php.SortSlice(names, func(a, b string) int { return php.Compare(a, b) })
	sorted := &NameMap[[]Advisory]{}
	for _, name := range names {
		advisories, _ := merged.Get(name)
		sorted.Set(name, advisories)
	}

	return SecurityAdvisoriesResult{Advisories: sorted, UnreachableRepos: unreachable}, nil
}

func (s *RepositorySet) repoAdvisories(provider AdvisoryProvider, packageConstraintMap *ConstraintMap, allowPartial bool) (*NameMap[[]Advisory], error) {
	has, err := provider.HasSecurityAdvisories()
	if err != nil || !has {
		return nil, err
	}
	result, err := provider.SecurityAdvisories(packageConstraintMap, allowPartial)

	return result.Advisories, err
}

// Providers ports RepositorySet::getProviders: the packages providing
// packageName, by name.
func (s *RepositorySet) Providers(packageName string) ([]ProviderInfo, error) {
	var providers []ProviderInfo
	for _, repo := range s.repositories {
		repoProviders, err := repo.Providers(packageName)
		if err != nil {
			return nil, err
		}
		providers = mergeProviders(providers, repoProviders)
	}

	return providers, nil
}

// IsPackageAcceptable ports RepositorySet::isPackageAcceptable: whether a
// package with these names and stability is accepted by the stability
// settings.
func (s *RepositorySet) IsPackageAcceptable(names []string, stability string) bool {
	return version.IsPackageAcceptable(s.acceptableStabilities, s.stabilityFlags, names, stability)
}

// RootAliasesFromArray converts the aliases RootPackage::getAliases (or the
// lock file) holds, a list of ['package' => ..., 'version' => ...,
// 'alias' => ..., 'alias_normalized' => ...].
func RootAliasesFromArray(aliases *php.Array) []RootAlias {
	list := make([]RootAlias, 0, aliases.Len())
	for _, v := range aliases.All() {
		a, ok := v.(*php.Array)
		if !ok {
			continue
		}
		str := func(k string) string { s, _ := a.Get(k); return php.ToString(s) }
		list = append(list, RootAlias{Package: str("package"), Version: str("version"), Alias: str("alias"), AliasNormalized: str("alias_normalized")})
	}

	return list
}
