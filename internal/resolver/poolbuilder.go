// Ports src/Composer/DependencyResolver/PoolBuilder.php.

package resolver

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

const loadBatchSize = 50

// EventDispatcher is the part of Composer\EventDispatcher\EventDispatcher
// the pool builder uses; *eventdispatcher.EventDispatcher implements it.
type EventDispatcher interface {
	Dispatch(eventName string, event eventdispatcher.Event) (int, error)
}

// PoolBuilderOptions are the arguments of new PoolBuilder() after the
// stability settings and the IO.
type PoolBuilderOptions struct {
	// EventDispatcher dispatches PRE_POOL_CREATE; nil is none.
	EventDispatcher EventDispatcher
	// PoolOptimizer optimizes the built pool; nil is none.
	PoolOptimizer *PoolOptimizer
	// TemporaryConstraints filter the loaded packages by name.
	TemporaryConstraints *repository.ConstraintMap
	// SecurityAdvisoryPoolFilter and FilterListPoolFilter filter the
	// built pool; nil is none.
	SecurityAdvisoryPoolFilter *SecurityAdvisoryPoolFilter
	FilterListPoolFilter       *FilterListPoolFilter
}

type indexedAlias struct {
	index int
	alias pkg.PackageInterface
}

// PoolBuilder ports Composer\DependencyResolver\PoolBuilder: it loads the
// packages a request can need from the repositories.
type PoolBuilder struct {
	acceptableStabilities *php.Array
	stabilityFlags        *php.Array
	rootAliasList         []repository.RootAlias
	rootAliases           map[string]map[string]repository.AliasTarget
	rootReferences        *php.Array
	temporaryConstraints  *repository.ConstraintMap
	eventDispatcher       EventDispatcher
	poolOptimizer         *PoolOptimizer
	io                    io.IO

	aliasMap                          map[pkg.PackageInterface][]indexedAlias
	packagesToLoad                    *repository.ConstraintMap
	loadedPackages                    map[string]semver.ConstraintInterface
	loadedPerRepo                     map[int]repository.AlreadyLoaded
	packages                          []pkg.PackageInterface // by index, nil once unset
	unacceptableFixedOrLockedPackages []pkg.PackageInterface
	updateAllowList                   []string
	updateAllowRegexps                []*php.Regexp
	skippedLoad                       map[string][]pkg.PackageInterface
	ignoredTypes                      []string
	allowedTypes                      []string
	restrictedPackagesList            map[string]bool
	pathRepoUnlocked                  map[string]bool
	maxExtendedReqs                   map[string]bool
	updateAllowWarned                 map[string]bool

	securityAdvisoryPoolFilter *SecurityAdvisoryPoolFilter
	filterListPoolFilter       *FilterListPoolFilter
}

// NewPoolBuilder is new PoolBuilder($acceptableStabilities,
// $stabilityFlags, $rootAliases, $rootReferences, $io, ...):
// acceptableStabilities maps stability names, stabilityFlags package names
// to BasePackage::STABILITY_* values, rootReferences package names to
// source references.
func NewPoolBuilder(acceptableStabilities, stabilityFlags *php.Array, rootAliases []repository.RootAlias, rootReferences *php.Array, out io.IO, opts PoolBuilderOptions) *PoolBuilder {
	if stabilityFlags == nil {
		stabilityFlags = php.NewArray()
	}
	if rootReferences == nil {
		rootReferences = php.NewArray()
	}
	temporaryConstraints := opts.TemporaryConstraints
	if temporaryConstraints == nil {
		temporaryConstraints = &repository.ConstraintMap{}
	}

	return &PoolBuilder{
		acceptableStabilities:      acceptableStabilities,
		stabilityFlags:             stabilityFlags,
		rootAliasList:              rootAliases,
		rootAliases:                repository.RootAliasesPerPackage(rootAliases),
		rootReferences:             rootReferences,
		temporaryConstraints:       temporaryConstraints,
		eventDispatcher:            opts.EventDispatcher,
		poolOptimizer:              opts.PoolOptimizer,
		io:                         out,
		securityAdvisoryPoolFilter: opts.SecurityAdvisoryPoolFilter,
		filterListPoolFilter:       opts.FilterListPoolFilter,
		updateAllowWarned:          map[string]bool{},
		pathRepoUnlocked:           map[string]bool{},
	}
}

// SetIgnoredTypes ports setIgnoredTypes: packages of those types are
// ignored.
func (b *PoolBuilder) SetIgnoredTypes(types []string) { b.ignoredTypes = types }

// SetAllowedTypes ports setAllowedTypes: only packages of those types are
// allowed; nil (PHP null) allows all.
func (b *PoolBuilder) SetAllowedTypes(types []string) { b.allowedTypes = types }

func (b *PoolBuilder) resetState() {
	b.aliasMap = map[pkg.PackageInterface][]indexedAlias{}
	b.packagesToLoad = &repository.ConstraintMap{}
	b.loadedPackages = map[string]semver.ConstraintInterface{}
	b.loadedPerRepo = map[int]repository.AlreadyLoaded{}
	b.packages = nil
	b.unacceptableFixedOrLockedPackages = nil
	b.maxExtendedReqs = map[string]bool{}
	b.skippedLoad = map[string][]pkg.PackageInterface{}
}

// BuildPool ports buildPool.
func (b *PoolBuilder) BuildPool(repositories []repository.RepositoryInterface, request *Request) (*Pool, error) {
	b.resetState()

	b.restrictedPackagesList = nil
	if restricted := request.RestrictedPackages(); restricted != nil {
		b.restrictedPackagesList = make(map[string]bool, len(restricted))
		for _, name := range restricted {
			b.restrictedPackagesList[name] = true
		}
	}

	if len(request.UpdateAllowList()) > 0 {
		b.updateAllowList = request.UpdateAllowList()
		b.updateAllowRegexps = make([]*php.Regexp, len(b.updateAllowList))
		for i, pattern := range b.updateAllowList {
			re, err := php.Compile(pkg.PackageNameToRegexp(pattern, "{^%s$}i"))
			if err != nil {
				return nil, err
			}
			b.updateAllowRegexps[i] = re
		}
		if err := b.warnAboutNonMatchingUpdateAllowList(request); err != nil {
			return nil, err
		}

		if request.LockedRepository() == nil {
			return nil, &util.LogicError{Site: phperr.At("PoolBuilder.php", 219), Message: "No lock repo present and yet a partial update was requested."}
		}

		lockedPackages, err := request.LockedRepository().Packages()
		if err != nil {
			return nil, err
		}
		for _, lockedPackage := range lockedPackages {
			allowed, err := b.isUpdateAllowed(lockedPackage)
			if err != nil {
				return nil, err
			}
			if allowed {
				continue
			}

			// remember which packages we skipped loading remote content for in this partial update
			b.skippedLoad[lockedPackage.Name()] = append(b.skippedLoad[lockedPackage.Name()], lockedPackage)
			for link := range lockedPackage.Replaces().Values() {
				b.skippedLoad[link.Target()] = append(b.skippedLoad[link.Target()], lockedPackage)
			}

			// Path repo packages are never loaded from lock, to force them to always remain in sync
			// unless symlinking is disabled in which case we probably should rather treat them like
			// regular packages. We mark them specially so they can be reloaded fully including update propagation
			// if they do get unlocked, but by default they are unlocked without update propagation.
			if isSymlinkedPathPackage(lockedPackage) {
				b.pathRepoUnlocked[lockedPackage.Name()] = true

				continue
			}

			request.LockPackage(lockedPackage)
		}
	}

	for _, p := range request.FixedOrLockedPackages() {
		// using MatchAllConstraint here because fixed packages do not need to retrigger
		// loading any packages
		b.loadedPackages[p.Name()] = semver.NewMatchAllConstraint()

		// replace means conflict, so if a fixed package replaces a name, no need to load that one, packages would conflict anyways
		for link := range p.Replaces().Values() {
			b.loadedPackages[link.Target()] = semver.NewMatchAllConstraint()
		}

		// TODO in how far can we do the above for conflicts? It's more tricky cause conflicts can be limited to
		// specific versions while replace is a conflict with all versions of the name

		_, fromRoot := p.Repository().(*repository.RootPackageRepository)
		_, fromPlatform := p.Repository().(*repository.PlatformRepository)
		if fromRoot || fromPlatform || version.IsPackageAcceptable(b.acceptableStabilities, b.stabilityFlags, p.Names(true), p.Stability()) {
			if err := b.loadPackage(request, repositories, p, false); err != nil {
				return nil, err
			}
		} else {
			b.unacceptableFixedOrLockedPackages = append(b.unacceptableFixedOrLockedPackages, p)
		}
	}

	for packageName, constraint := range request.Requires().All() {
		// fixed and locked packages have already been added, so if a root require needs one of them, no need to do anything
		if _, ok := b.loadedPackages[packageName]; ok {
			continue
		}

		b.packagesToLoad.Set(packageName, constraint)
		b.maxExtendedReqs[packageName] = true
	}

	// clean up packagesToLoad for anything we manually marked loaded above
	for _, name := range b.packagesToLoad.Keys() {
		if _, ok := b.loadedPackages[name]; ok {
			b.packagesToLoad.Delete(name)
		}
	}

	for b.packagesToLoad.Len() > 0 {
		if err := b.loadPackagesMarkedForLoading(request, repositories); err != nil {
			return nil, phperr.Call(err, `Composer\DependencyResolver\PoolBuilder->loadPackagesMarkedForLoading`, "PoolBuilder.php", 289)
		}
	}

	if b.temporaryConstraints.Len() > 0 {
		b.applyTemporaryConstraints()
	}

	packages := b.packageList()
	if b.eventDispatcher != nil {
		repos := make([]pkg.Repository, len(repositories))
		for i, repo := range repositories {
			repos[i] = repo
		}
		prePoolCreateEvent := eventdispatcher.NewPrePoolCreateEvent(
			eventdispatcher.PrePoolCreate,
			repos,
			request,
			b.acceptableStabilities,
			b.stabilityFlags,
			RootAliasesArray(b.rootAliasList),
			b.rootReferences,
			packages,
			b.unacceptableFixedOrLockedPackages,
		)
		if _, err := b.eventDispatcher.Dispatch(prePoolCreateEvent.Name(), prePoolCreateEvent); err != nil {
			return nil, err
		}
		packages = prePoolCreateEvent.Packages()
		b.unacceptableFixedOrLockedPackages = prePoolCreateEvent.UnacceptableFixedPackages()
	}

	pool := NewPool(packages, b.unacceptableFixedOrLockedPackages, nil)

	b.resetState()

	b.io.Debug("Built pool.", nil)

	// filter vulnerable packages before optimizing the pool otherwise we may end up with inconsistent state where the optimizer took away versions
	// that were not vulnerable and now suddenly the vulnerable ones are removed and we are missing some versions to make it solvable
	pool, err := b.runSecurityAdvisoryFilter(pool, repositories, request)
	if err != nil {
		return nil, err
	}
	if pool, err = b.runFilterListFilter(pool, request); err != nil {
		return nil, err
	}
	if pool, err = b.runOptimizer(request, pool); err != nil {
		return nil, err
	}

	semver.Intervals.Clear()

	return pool, nil
}

// RootAliasesArray returns the root aliases as PoolBuilder's PHP array:
// package name => version => ['alias' => ..., 'alias_normalized' => ...].
func RootAliasesArray(aliases []repository.RootAlias) *php.Array {
	result := php.NewArray()
	for _, alias := range aliases {
		versions, ok := result.GetArray(alias.Package)
		if !ok {
			versions = php.NewArray()
			result.Set(alias.Package, versions)
		}
		versions.Set(alias.Version, php.ArrayOf("alias", alias.Alias, "alias_normalized", alias.AliasNormalized))
	}

	return result
}

// newRootAlias creates the root alias of basePackage: a
// CompleteAliasPackage when it is a CompletePackage.
func newRootAlias(basePackage pkg.PackageInterface, alias repository.AliasTarget) pkg.Alias {
	var aliasPackage pkg.Alias
	if _, isComplete := pkg.AsCompletePackage(basePackage); isComplete {
		complete, _ := basePackage.(pkg.CompletePackageInterface)
		aliasPackage = pkg.NewCompleteAliasPackage(complete, alias.AliasNormalized, alias.Alias)
	} else {
		aliasPackage = pkg.NewAliasPackage(basePackage, alias.AliasNormalized, alias.Alias)
	}
	aliasPackage.SetRootPackageAlias(true)

	return aliasPackage
}

// isSymlinkedPathPackage tells whether p was installed from a path
// repository with symlinking (dist type "path", transport option symlink
// not false).
func isSymlinkedPathPackage(p pkg.PackageInterface) bool {
	if distType := p.DistType(); !distType.Valid || distType.S != "path" {
		return false
	}
	symlink, ok := p.TransportOptions().Get("symlink")

	return !ok || symlink != false
}

// packageList returns $this->packages without the unset entries.
func (b *PoolBuilder) packageList() []pkg.PackageInterface {
	packages := make([]pkg.PackageInterface, 0, len(b.packages))
	for _, p := range b.packages {
		if p != nil {
			packages = append(packages, p)
		}
	}

	return packages
}

// applyTemporaryConstraints is buildPool's filtering by the temporary
// constraints.
func (b *PoolBuilder) applyTemporaryConstraints() {
	for i, p := range b.packages {
		// we check all alias related packages at once, so no need to check individual aliases
		if p == nil {
			continue
		}
		if _, ok := p.(pkg.Alias); ok {
			continue
		}

		for _, packageName := range p.Names(true) {
			constraint, ok := b.temporaryConstraints.Get(packageName)
			if !ok {
				continue
			}

			packageAndAliases := append([]indexedAlias{{index: i, alias: p}}, b.aliasMap[p]...)

			found := false
			for _, packageOrAlias := range packageAndAliases {
				if semver.CompilingMatcher.Match(constraint, semver.OpEQ, packageOrAlias.alias.Version()) {
					found = true
				}
			}

			if !found {
				for _, packageOrAlias := range packageAndAliases {
					b.packages[packageOrAlias.index] = nil
				}
			}
		}
	}
}

func (b *PoolBuilder) markPackageNameForLoading(request *Request, name string, constraint semver.ConstraintInterface) {
	// Skip platform requires at this stage
	if repository.IsPlatformPackage(name) {
		return
	}

	// Root require (which was not unlocked) already loaded the maximum range so no
	// need to check anything here
	if b.maxExtendedReqs[name] {
		return
	}

	// Root requires can not be overruled by dependencies so there is no point in
	// extending the loaded constraint for those.
	// This is triggered when loading a root require which was locked but got unlocked, then
	// we make sure that we load at most the intervals covered by the root constraint.
	if rootRequire, ok := request.Requires().Get(name); ok && !semver.Intervals.IsSubsetOf(constraint, rootRequire) {
		constraint = rootRequire
	}

	// Not yet loaded or already marked for a reload, set the constraint to be loaded
	loaded, isLoaded := b.loadedPackages[name]
	if !isLoaded {
		// Maybe it was already marked before but not loaded yet. In that case
		// we have to extend the constraint (we don't check if they are identical because
		// MultiConstraint::create() will optimize anyway)
		if toLoad, ok := b.packagesToLoad.Get(name); ok {
			// Already marked for loading and this does not expand the constraint to be loaded, nothing to do
			if semver.Intervals.IsSubsetOf(constraint, toLoad) {
				return
			}

			// extend the constraint to be loaded
			constraint = semver.Intervals.CompactConstraint(semver.CreateMultiConstraint([]semver.ConstraintInterface{toLoad, constraint}, false))
		}

		b.packagesToLoad.Set(name, constraint)

		return
	}

	// No need to load this package with this constraint because it is
	// a subset of the constraint with which we have already loaded packages
	if semver.Intervals.IsSubsetOf(constraint, loaded) {
		return
	}

	// We have already loaded that package but not in the constraint that's
	// required. We extend the constraint and mark that package as not being loaded
	// yet so we get the required package versions
	b.packagesToLoad.Set(name, semver.Intervals.CompactConstraint(semver.CreateMultiConstraint([]semver.ConstraintInterface{loaded, constraint}, false)))
	delete(b.loadedPackages, name)
}

// chunk ports array_chunk($map, LOAD_BATCH_SIZE, true).
func chunk(m *repository.ConstraintMap) []*repository.ConstraintMap {
	var batches []*repository.ConstraintMap
	var current *repository.ConstraintMap
	for name, constraint := range m.All() {
		if current == nil || current.Len() == loadBatchSize {
			current = &repository.ConstraintMap{}
			batches = append(batches, current)
		}
		current.Set(name, constraint)
	}

	return batches
}

func (b *PoolBuilder) loadPackagesMarkedForLoading(request *Request, repositories []repository.RepositoryInterface) error {
	for _, name := range b.packagesToLoad.Keys() {
		if b.restrictedPackagesList != nil && !b.restrictedPackagesList[name] {
			b.packagesToLoad.Delete(name)

			continue
		}
		constraint, _ := b.packagesToLoad.Get(name)
		b.loadedPackages[name] = constraint
	}

	// Load packages in chunks of 50 to prevent memory usage build-up due to caches of all sorts
	packageBatches := chunk(b.packagesToLoad)
	b.packagesToLoad = &repository.ConstraintMap{}

	var lockedRepository repository.RepositoryInterface
	if locked := request.LockedRepository(); locked != nil {
		lockedRepository = locked
	}

	for repoIndex, repo := range repositories {
		// these repos have their packages fixed or locked if they need to be loaded so we
		// never need to load anything else from them
		if _, isPlatform := repo.(*repository.PlatformRepository); isPlatform || (lockedRepository != nil && repo == lockedRepository) {
			continue
		}

		if len(packageBatches) == 0 {
			break
		}

		for _, packageBatch := range packageBatches {
			result, err := repo.LoadPackages(packageBatch, b.acceptableStabilities, b.stabilityFlags, b.loadedPerRepo[repoIndex])
			if err != nil {
				return phperr.Call(err, repository.LoadPackagesClass(repo)+"->loadPackages", "PoolBuilder.php", 452)
			}

			for _, name := range result.NamesFound {
				// avoid loading the same package again from other repositories once it has been found
				packageBatch.Delete(name)
			}
			for _, p := range result.Packages {
				loaded := b.loadedPerRepo[repoIndex]
				if loaded == nil {
					loaded = repository.AlreadyLoaded{}
					b.loadedPerRepo[repoIndex] = loaded
				}
				if loaded[p.Name()] == nil {
					loaded[p.Name()] = map[string]pkg.PackageInterface{}
				}
				loaded[p.Name()][p.Version()] = p

				if slices.Contains(b.ignoredTypes, p.Type()) || (b.allowedTypes != nil && !slices.Contains(b.allowedTypes, p.Type())) {
					continue
				}
				if err := b.loadPackage(request, repositories, p, !b.pathRepoUnlocked[p.Name()]); err != nil {
					return err
				}
			}
		}

		merged := &repository.ConstraintMap{}
		for _, batch := range packageBatches {
			for name, constraint := range batch.All() {
				merged.Set(name, constraint)
			}
		}
		packageBatches = chunk(merged)
	}

	return nil
}

func (b *PoolBuilder) loadPackage(request *Request, repositories []repository.RepositoryInterface, p pkg.PackageInterface, propagateUpdate bool) error {
	index := len(b.packages)
	b.packages = append(b.packages, p)

	if alias, ok := p.(pkg.Alias); ok {
		aliasOf := alias.AliasOf()
		b.aliasMap[aliasOf] = append(b.aliasMap[aliasOf], indexedAlias{index: index, alias: p})
	}

	name := p.Name()

	// we're simply setting the root references on all versions for a name here and rely on the solver to pick the
	// right version. It'd be more work to figure out which versions and which aliases of those versions this may
	// apply to
	if reference, ok := b.rootReferences.Get(name); ok && reference != nil {
		// do not modify the references on already locked or fixed packages
		if !request.IsLockedPackage(p) && !request.IsFixedPackage(p) {
			p.SetSourceDistReferences(php.ToString(reference))
		}
	}

	// if propagateUpdate is false we are loading a fixed or locked package, root aliases do not apply as they are
	// manually loaded as separate packages in this case
	//
	// packages in pathRepoUnlocked however need to also load root aliases, they have propagateUpdate set to
	// false because their deps should not be unlocked, but that is irrelevant for root aliases
	if propagateUpdate || b.pathRepoUnlocked[p.Name()] {
		if alias, ok := b.rootAliases[name][p.Version()]; ok {
			basePackage := p
			if a, ok := p.(pkg.Alias); ok {
				basePackage = a.AliasOf()
			}
			aliasPackage := newRootAlias(basePackage, alias)

			newIndex := len(b.packages)
			b.packages = append(b.packages, aliasPackage)
			b.aliasMap[basePackage] = append(b.aliasMap[basePackage], indexedAlias{index: newIndex, alias: aliasPackage})
		}
	}

	for link := range p.Requires().Values() {
		require := link.Target()
		linkConstraint := link.Constraint()

		// if the required package is loaded as a locked package only and hasn't had its deps analyzed
		if _, skipped := b.skippedLoad[require]; skipped {
			// if we're doing a full update or this is a partial update with transitive deps and we're currently
			// looking at a package which needs to be updated we need to unlock the package we now know is a
			// dependency of another package which we are trying to update, and then attempt to load it again
			if propagateUpdate && request.UpdateAllowTransitiveDependencies() {
				skippedRootRequires := b.skippedRootRequires(request, require)

				if request.UpdateAllowTransitiveRootDependencies() || len(skippedRootRequires) == 0 {
					if err := b.unlockPackage(request, repositories, require); err != nil {
						return err
					}
					b.markPackageNameForLoading(request, require, linkConstraint)
				} else {
					b.warnRootRequires(skippedRootRequires)
				}
			} else if _, loaded := b.loadedPackages[require]; b.pathRepoUnlocked[require] && !loaded {
				// if doing a partial update and a package depends on a path-repo-unlocked package which is not referenced by the root, we need to ensure it gets loaded as it was not loaded by the request's root requirements
				// and would not be loaded above if update propagation is not allowed (which happens if the requirer is itself a path-repo-unlocked package) or if transitive deps are not allowed to be unlocked
				b.markPackageNameForLoading(request, require, linkConstraint)
			}
		} else {
			b.markPackageNameForLoading(request, require, linkConstraint)
		}
	}

	// if we're doing a partial update with deps we also need to unlock packages which are being replaced in case
	// they are currently locked and thus prevent this updateable package from being installable/updateable
	if propagateUpdate && request.UpdateAllowTransitiveDependencies() {
		for link := range p.Replaces().Values() {
			replace := link.Target()
			_, loaded := b.loadedPackages[replace]
			_, skipped := b.skippedLoad[replace]
			if !loaded || !skipped {
				continue
			}

			skippedRootRequires := b.skippedRootRequires(request, replace)
			if request.UpdateAllowTransitiveRootDependencies() || len(skippedRootRequires) == 0 {
				if err := b.unlockPackage(request, repositories, replace); err != nil {
					return err
				}
				// the replaced package only needs to be loaded if something else requires it
				b.markPackageNameForLoadingIfRequired(request, replace)
			} else {
				b.warnRootRequires(skippedRootRequires)
			}
		}
	}

	return nil
}

// warnRootRequires warns once per name about root requirements kept at
// their locked version.
func (b *PoolBuilder) warnRootRequires(skippedRootRequires []string) {
	for _, rootRequire := range skippedRootRequires {
		if !b.updateAllowWarned[rootRequire] {
			b.updateAllowWarned[rootRequire] = true
			b.io.WriteError("<warning>Dependency "+rootRequire+" is also a root requirement. Package has not been listed as an update argument, so keeping locked at old version. Use --with-all-dependencies (-W) to include root dependencies.</warning>", true, io.Normal)
		}
	}
}

// isRootRequire ports isRootRequire.
func (b *PoolBuilder) isRootRequire(request *Request, name string) bool {
	return request.Requires().Has(name)
}

// skippedRootRequires ports getSkippedRootRequires.
func (b *PoolBuilder) skippedRootRequires(request *Request, name string) []string {
	skipped, ok := b.skippedLoad[name]
	if !ok {
		return nil
	}

	rootRequires := request.Requires()
	var matches []string

	if rootRequires.Has(name) {
		for _, p := range skipped {
			if name != p.Name() {
				matches = append(matches, p.Name()+" (via replace of "+name+")")
			} else {
				matches = append(matches, p.Name())
			}
		}

		return matches
	}

	for _, packageOrReplacer := range skipped {
		if rootRequires.Has(packageOrReplacer.Name()) {
			matches = append(matches, packageOrReplacer.Name())
		}

		for link := range packageOrReplacer.Replaces().Values() {
			if rootRequires.Has(link.Target()) {
				if name != packageOrReplacer.Name() {
					matches = append(matches, packageOrReplacer.Name()+" (via replace of "+name+")")
				} else {
					matches = append(matches, packageOrReplacer.Name())
				}

				break
			}
		}
	}

	return matches
}

// isUpdateAllowed ports isUpdateAllowed.
func (b *PoolBuilder) isUpdateAllowed(p pkg.PackageInterface) (bool, error) {
	for _, re := range b.updateAllowRegexps {
		if ok, err := re.IsMatch(p.Name()); err != nil || ok {
			return ok, err
		}
	}

	return false, nil
}

func (b *PoolBuilder) warnAboutNonMatchingUpdateAllowList(request *Request) error {
	if request.LockedRepository() == nil {
		return &util.LogicError{Site: phperr.At("PoolBuilder.php", 649), Message: "No lock repo present and yet a partial update was requested."}
	}
	lockedPackages, err := request.LockedRepository().Packages()
	if err != nil {
		return err
	}

patterns:
	for i, pattern := range b.updateAllowList {
		matchedPlatformPackage := false
		re := b.updateAllowRegexps[i]

		// update pattern matches a locked package? => all good
		for _, p := range lockedPackages {
			if ok, err := re.IsMatch(p.Name()); err != nil {
				return err
			} else if ok {
				continue patterns
			}
		}

		// update pattern matches a root require? => all good, probably a new package
		for packageName := range request.Requires().All() {
			ok, err := re.IsMatch(packageName)
			if err != nil {
				return err
			}
			if ok {
				if repository.IsPlatformPackage(packageName) {
					matchedPlatformPackage = true

					continue
				}

				continue patterns
			}
		}

		switch {
		case matchedPlatformPackage:
			b.io.WriteError(`<warning>Pattern "`+pattern+`" listed for update matches platform packages, but these cannot be updated by Composer.</warning>`, true, io.Normal)
		case strings.Contains(pattern, "*"):
			b.io.WriteError(`<warning>Pattern "`+pattern+`" listed for update does not match any locked packages.</warning>`, true, io.Normal)
		default:
			b.io.WriteError(`<warning>Package "`+pattern+`" listed for update is not locked.</warning>`, true, io.Normal)
		}
	}

	return nil
}

// unlockPackage ports unlockPackage: it reverts the decision to use a
// locked package if a partial update with transitive dependencies found
// that this package actually needs to be updated.
func (b *PoolBuilder) unlockPackage(request *Request, repositories []repository.RepositoryInterface, name string) error {
	for _, packageOrReplacer := range slices.Clone(b.skippedLoad[name]) {
		// if we unfixed a replaced package name, we also need to unfix the replacer itself
		// as long as it was not unfixed yet
		replacerName := packageOrReplacer.Name()
		if _, skipped := b.skippedLoad[replacerName]; replacerName == name || !skipped {
			continue
		}

		if request.UpdateAllowTransitiveRootDependencies() || (!b.isRootRequire(request, name) && !b.isRootRequire(request, replacerName)) {
			if err := b.unlockPackage(request, repositories, replacerName); err != nil {
				return err
			}

			if b.isRootRequire(request, replacerName) {
				b.markPackageNameForLoading(request, replacerName, semver.NewMatchAllConstraint())
			} else {
				for _, loadedPackage := range slices.Clone(b.packages) {
					if loadedPackage == nil {
						continue
					}
					if link, ok := loadedPackage.Requires().Get(replacerName); ok {
						b.markPackageNameForLoading(request, replacerName, link.Constraint())
					}
				}
			}
		}
	}

	if b.pathRepoUnlocked[name] {
		for index, p := range slices.Clone(b.packages) {
			if p != nil && p.Name() == name {
				b.removeLoadedPackage(repositories, p, index)
			}
		}
	}

	delete(b.skippedLoad, name)
	delete(b.loadedPackages, name)
	delete(b.maxExtendedReqs, name)
	delete(b.pathRepoUnlocked, name)

	// remove locked package by this name which was already initialized
	for _, lockedPackage := range request.LockedPackages() {
		if _, isAlias := lockedPackage.(pkg.Alias); isAlias || lockedPackage.Name() != name {
			continue
		}
		index := slices.Index(b.packages, lockedPackage)
		if index < 0 {
			continue
		}

		request.UnlockPackage(lockedPackage)
		b.removeLoadedPackage(repositories, lockedPackage, index)

		// make sure that any requirements for this package by other locked or fixed packages are now
		// also loaded, as they were previously ignored because the locked (now unlocked) package already
		// satisfied their requirements
		// and if this package is replacing another that is required by a locked or fixed package, ensure
		// that we load that replaced package in case an update to this package removes the replacement
		for _, fixedOrLockedPackage := range request.FixedOrLockedPackages() {
			if fixedOrLockedPackage == lockedPackage {
				continue
			}

			if _, skipped := b.skippedLoad[fixedOrLockedPackage.Name()]; !skipped {
				continue
			}

			requires := fixedOrLockedPackage.Requires()
			if link, ok := requires.Get(lockedPackage.Name()); ok {
				b.markPackageNameForLoading(request, lockedPackage.Name(), link.Constraint())
			}

			for replace := range lockedPackage.Replaces().Values() {
				_, skipped := b.skippedLoad[replace.Target()]
				if requires.Has(replace.Target()) && skipped {
					if err := b.unlockPackage(request, repositories, replace.Target()); err != nil {
						return err
					}
					// this package is in $requires so no need to call markPackageNameForLoadingIfRequired
					b.markPackageNameForLoading(request, replace.Target(), replace.Constraint())
				}
			}
		}
	}

	return nil
}

func (b *PoolBuilder) markPackageNameForLoadingIfRequired(request *Request, name string) {
	if rootRequire, ok := request.Requires().Get(name); ok {
		b.markPackageNameForLoading(request, name, rootRequire)
	}

	for _, p := range b.packages {
		if p == nil {
			continue
		}
		for link := range p.Requires().Values() {
			if name == link.Target() {
				b.markPackageNameForLoading(request, link.Target(), link.Constraint())
			}
		}
	}
}

// repoIndexOf is array_search($repository, $repositories, true), with
// PHP's false (not found) used as array key 0.
func repoIndexOf(repositories []repository.RepositoryInterface, repo pkg.Repository) int {
	for i, r := range repositories {
		if pkg.Repository(r) == repo {
			return i
		}
	}

	return 0
}

func (b *PoolBuilder) removeLoadedPackage(repositories []repository.RepositoryInterface, p pkg.PackageInterface, index int) {
	repoIndex := repoIndexOf(repositories, p.Repository())

	if loaded := b.loadedPerRepo[repoIndex]; loaded != nil {
		delete(loaded[p.Name()], p.Version())
	}
	b.packages[index] = nil
	if aliases, ok := b.aliasMap[p]; ok {
		for _, entry := range aliases {
			if loaded := b.loadedPerRepo[repoIndex]; loaded != nil {
				delete(loaded[entry.alias.Name()], entry.alias.Version())
			}
			b.packages[entry.index] = nil
		}
		delete(b.aliasMap, p)
	}
}

// numberFormat ports number_format($n): thousands separated by commas.
func numberFormat(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + numberFormat(-n)
	}
	var out strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(c)
	}

	return out.String()
}

// reportFiltered writes the VERY_VERBOSE lines the pool filters and the
// optimizer print when they removed packages.
func (b *PoolBuilder) reportFiltered(what, action string, before time.Time, total, filtered int) {
	b.io.Write(what+" completed in "+strconv.FormatFloat(time.Since(before).Seconds(), 'f', 3, 64)+" seconds", true, io.VeryVerbose)
	percent := int(math.Round(100 / float64(total) * float64(filtered)))
	b.io.Write("<info>Found "+numberFormat(total)+" package versions referenced in your dependency graph. "+numberFormat(filtered)+" ("+strconv.Itoa(percent)+"%) were "+action+".</info>", true, io.VeryVerbose)
}

func (b *PoolBuilder) runOptimizer(request *Request, pool *Pool) (*Pool, error) {
	if b.poolOptimizer == nil {
		return pool, nil
	}

	b.io.Debug("Running pool optimizer.", nil)

	before := time.Now()
	total := pool.Count()

	pool, err := b.poolOptimizer.Optimize(request, pool)
	if err != nil {
		return nil, err
	}

	if filtered := total - pool.Count(); filtered != 0 {
		b.reportFiltered("Pool optimizer", "optimized away", before, total, filtered)
	}

	return pool, nil
}

func (b *PoolBuilder) runFilterListFilter(pool *Pool, request *Request) (*Pool, error) {
	if b.filterListPoolFilter == nil {
		return pool, nil
	}

	b.io.Debug("Running filter list pool filter.", nil)

	before := time.Now()
	total := pool.Count()

	pool, err := b.filterListPoolFilter.Filter(pool, request)
	if err != nil {
		return nil, err
	}

	if filtered := total - pool.Count(); filtered != 0 {
		b.reportFiltered("Filter list pool filter", "filtered away by dependency policies", before, total, filtered)
	}

	return pool, nil
}

func (b *PoolBuilder) runSecurityAdvisoryFilter(pool *Pool, repositories []repository.RepositoryInterface, request *Request) (*Pool, error) {
	if b.securityAdvisoryPoolFilter == nil {
		return pool, nil
	}

	b.io.Debug("Running security advisory pool filter.", nil)

	before := time.Now()
	total := pool.Count()

	pool, err := b.securityAdvisoryPoolFilter.Filter(pool, repositories, request)
	if err != nil {
		return nil, err
	}

	if filtered := total - pool.Count(); filtered != 0 {
		b.reportFiltered("Security advisory pool filter", "filtered away", before, total, filtered)
	}

	return pool, nil
}
