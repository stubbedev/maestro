// Ports src/Composer/DependencyResolver/PoolOptimizer.php.

package resolver

import (
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// constraintSet is array<string, ConstraintInterface>: constraints by
// their string form, in insertion order.
type constraintSet = repository.NameMap[semver.ConstraintInterface]

// PoolOptimizer ports Composer\DependencyResolver\PoolOptimizer: it
// removes packages from the pool that cannot change the solution.
type PoolOptimizer struct {
	policy Policy

	irremovablePackages           map[int]bool
	requireConstraintsPerPackage  map[string]*constraintSet
	conflictConstraintsPerPackage map[string]*constraintSet
	packagesToRemove              map[int]bool
	aliasesPerPackage             map[int][]pkg.PackageInterface
	removedVersionsByPackage      map[pkg.PackageInterface]*VersionMap

	// expanded memoizes expandDisjunctiveMultiConstraints per constraint
	// object (the loaders share the objects of equal constraint strings).
	expanded map[semver.ConstraintInterface][]semver.ConstraintInterface
}

// NewPoolOptimizer is new PoolOptimizer($policy).
func NewPoolOptimizer(policy Policy) *PoolOptimizer {
	return &PoolOptimizer{policy: policy}
}

// Optimize ports optimize: a new pool without the packages that can be
// optimized away.
func (o *PoolOptimizer) Optimize(request *Request, pool *Pool) (*Pool, error) {
	o.irremovablePackages = map[int]bool{}
	o.requireConstraintsPerPackage = map[string]*constraintSet{}
	o.conflictConstraintsPerPackage = map[string]*constraintSet{}
	o.packagesToRemove = map[int]bool{}
	o.aliasesPerPackage = map[int][]pkg.PackageInterface{}
	o.removedVersionsByPackage = map[pkg.PackageInterface]*VersionMap{}
	o.expanded = map[semver.ConstraintInterface][]semver.ConstraintInterface{}

	if err := o.prepare(request, pool); err != nil {
		return nil, err
	}

	if err := o.optimizeByIdenticalDependencies(pool); err != nil {
		return nil, err
	}

	if err := o.optimizeImpossiblePackagesAway(request, pool); err != nil {
		return nil, err
	}

	optimizedPool := o.applyRemovalsToPool(pool)

	// No need to run this recursively at the moment
	// because the current optimizations cannot provide
	// even more gains when ran again. Might change
	// in the future with additional optimizations.

	o.irremovablePackages = nil
	o.requireConstraintsPerPackage = nil
	o.conflictConstraintsPerPackage = nil
	o.packagesToRemove = nil
	o.aliasesPerPackage = nil
	o.removedVersionsByPackage = nil
	o.expanded = nil

	return optimizedPool, nil
}

func (o *PoolOptimizer) prepare(request *Request, pool *Pool) error {
	irremovableGroups := &repository.NameMap[[]semver.ConstraintInterface]{}

	// Mark fixed or locked packages as irremovable
	for _, p := range request.FixedOrLockedPackages() {
		group, _ := irremovableGroups.Get(p.Name())
		irremovableGroups.Set(p.Name(), append(group, semver.NewConstraintOp(semver.OpEQ, p.Version())))
	}

	// Extract requested package requirements
	for require, constraint := range request.Requires().All() {
		o.extractConstraints(o.requireConstraintsPerPackage, require, constraint)
	}

	// First pass over all packages to extract information and mark package constraints irremovable.
	// The loaders share the link objects of equal requirements between
	// versions; a link's constraints are extracted once (again, they would
	// only set the same strings to equal constraints).
	extractedRequires := map[*pkg.Link]struct{}{}
	extractedConflicts := map[*pkg.Link]struct{}{}
	for _, p := range pool.Packages() {
		// Extract package requirements
		for link := range p.Requires().Values() {
			if _, ok := extractedRequires[link]; !ok {
				extractedRequires[link] = struct{}{}
				o.extractConstraints(o.requireConstraintsPerPackage, link.Target(), link.Constraint())
			}
		}
		// Extract package conflicts
		for link := range p.Conflicts().Values() {
			if _, ok := extractedConflicts[link]; !ok {
				extractedConflicts[link] = struct{}{}
				o.extractConstraints(o.conflictConstraintsPerPackage, link.Target(), link.Constraint())
			}
		}

		// Keep track of alias packages for every package so if either the alias or aliased is kept
		// we keep the others as they are a unit of packages really
		if alias, ok := p.(pkg.Alias); ok {
			id := alias.AliasOf().ID()
			o.aliasesPerPackage[id] = append(o.aliasesPerPackage[id], p)
		}
	}

	irremovableConstraints := make(map[string]semver.ConstraintInterface, irremovableGroups.Len())
	for packageName, constraints := range irremovableGroups.All() {
		if len(constraints) == 1 {
			irremovableConstraints[packageName] = constraints[0]

			continue
		}
		multi, err := semver.NewMultiConstraint(constraints, false)
		if err != nil {
			return err
		}
		irremovableConstraints[packageName] = multi
	}

	// Mark the packages as irremovable based on the constraints
	for _, p := range pool.Packages() {
		constraint, ok := irremovableConstraints[p.Name()]
		if !ok {
			continue
		}

		if semver.CompilingMatcher.Match(constraint, semver.OpEQ, p.Version()) {
			o.markPackageIrremovable(p)
		}
	}

	return nil
}

func (o *PoolOptimizer) markPackageIrremovable(p pkg.PackageInterface) {
	o.irremovablePackages[p.ID()] = true
	if alias, ok := p.(pkg.Alias); ok {
		// recursing here so aliasesPerPackage for the aliasOf can be checked
		// and all its aliases marked as irremovable as well
		o.markPackageIrremovable(alias.AliasOf())
	}

	for _, aliasPackage := range o.aliasesPerPackage[p.ID()] {
		o.irremovablePackages[aliasPackage.ID()] = true
	}
}

func (o *PoolOptimizer) applyRemovalsToPool(pool *Pool) *Pool {
	packages := make([]pkg.PackageInterface, 0, pool.Count()-len(o.packagesToRemove))
	removedVersions := &repository.NameMap[*VersionMap]{}
	for _, p := range pool.Packages() {
		if !o.packagesToRemove[p.ID()] {
			packages = append(packages, p)
		} else {
			versions, ok := removedVersions.Get(p.Name())
			if !ok {
				versions = &VersionMap{}
				removedVersions.Set(p.Name(), versions)
			}
			versions.Set(p.Version(), p.PrettyVersion())
		}
	}

	return NewPool(packages, pool.UnacceptableFixedOrLockedPackages(), &Removed{
		Versions:   removedVersions,
		ByPackage:  o.removedVersionsByPackage,
		Security:   pool.AllSecurityRemovedPackageVersions(),
		Abandoned:  pool.AllAbandonedRemovedPackageVersions(),
		FilterList: pool.AllFilterListRemovedPackageVersions(),
	})
}

func (o *PoolOptimizer) optimizeByIdenticalDependencies(pool *Pool) error {
	// package name => group hash => dependency hash => package ids, each
	// level in insertion order; the hashes are ids of their strings
	// (hashIDs), the names indexes of the names packageHashes returns
	var nameOrder []int32
	perName := map[int32]*orderedIDs[orderedIDs[[]int32]]{}

	// The hashes of each package are computed in parallel (they only read
	// the pool), then filed in pool order, as the PHP loop files them.
	packages := pool.Packages()
	names, hashes := o.packageHashes(packages)
	for i, p := range packages {
		// If that package was already marked irremovable, we can skip
		// the entire process for it
		if o.irremovablePackages[p.ID()] {
			continue
		}

		if err := o.markPackageForRemoval(p.ID()); err != nil {
			return err
		}

		h := &hashes[i]
		for _, group := range h.groups {
			groups, ok := perName[group.name]
			if !ok {
				groups = &orderedIDs[orderedIDs[[]int32]]{}
				perName[group.name] = groups
				nameOrder = append(nameOrder, group.name)
			}
			byDependencies := groups.at(group.hash)
			ids := byDependencies.at(h.dependencyHash)
			*ids = append(*ids, literalOf(p.ID()))
		}
	}

	var groups []identicalGroup
	for _, name := range nameOrder {
		constraintGroups := perName[name]
		for i := range constraintGroups.vals {
			for _, packageIDs := range constraintGroups.vals[i].vals {
				groups = append(groups, identicalGroup{packageName: names[name], packageIDs: packageIDs})
			}
		}
	}
	o.selectGroups(pool, groups)

	for _, group := range groups {
		// Only one package in this constraint group has the same requirements, we're not allowed to remove that package
		if len(group.packageIDs) == 1 {
			o.keepPackageInGroup(pool.PackageByID(int(group.packageIDs[0])), group.packageName, group.versions)

			continue
		}

		// Otherwise we find out which one is the preferred package in this constraint group which is
		// then not allowed to be removed either
		for _, preferredLiteral := range group.preferred {
			o.keepPackageInGroup(pool.LiteralToPackage(preferredLiteral), group.packageName, group.versions)
		}
	}

	return nil
}

// identicalGroup is a group of packages with identical dependencies:
// their ids, the versions groupVersions gives for them and, for a group of
// several packages, the ones the policy prefers.
type identicalGroup struct {
	packageName string
	packageIDs  []int32
	versions    *VersionMap
	preferred   []int32
}

// forkablePolicy is a Policy that can be used on several goroutines
// through forks of it (DefaultPolicy).
type forkablePolicy interface {
	Fork() Policy
}

// selectGroups sets the versions and preferred packages of the groups. They
// only read the pool, so they are computed in parallel when the policy
// can be forked (each fork computes what the policy would).
func (o *PoolOptimizer) selectGroups(pool *Pool, groups []identicalGroup) {
	compute := func(policy Policy, from, to int) {
		for i := from; i < to; i++ {
			group := &groups[i]
			group.versions = groupVersions(pool, group.packageIDs)
			if len(group.packageIDs) > 1 {
				group.preferred = policy.SelectPreferredPackages(pool, group.packageIDs, "")
			}
		}
	}

	forkable, ok := o.policy.(forkablePolicy)
	if !ok {
		compute(o.policy, 0, len(groups))

		return
	}
	first := true
	parallelRanges(len(groups), func() func(from, to int) {
		// the first worker (the only one on a small pool) uses the policy
		policy := o.policy
		if !first {
			policy = forkable.Fork()
		}
		first = false

		return func(from, to int) { compute(policy, from, to) }
	})
}

// packageHashes are the hashes optimizeByIdenticalDependencies files a
// package under, as ids of their strings (hashIDs): its dependency hash,
// and the group hash of each of its names (and their require
// constraints) that has one.
type packageHashes struct {
	dependencyHash int32
	groups         []packageGroup
}

// packageGroup is a group hash of a package for one of its names (an
// index of the names packageHashes returns).
type packageGroup struct {
	name, hash int32
}

// namedMatcher is a constraint of a constraintSet, by its string, as a
// CompilingMatcher function of the version for the == operator.
type namedMatcher struct {
	str     string
	matches func(version string) bool
}

// packageHashes computes the hashes of each package of packages that is
// not irremovable, in parallel; they are the same in any order. It
// returns the names the groups refer to, and the hashes by package.
func (o *PoolOptimizer) packageHashes(packages []pkg.PackageInterface) ([]string, []packageHashes) {
	matchers := func(set *constraintSet) []namedMatcher {
		list := make([]namedMatcher, 0, set.Len())
		for str, constraint := range set.All() {
			list = append(list, namedMatcher{str: "require:" + str, matches: semver.CompilingMatcher.Matcher(constraint, semver.OpEQ)})
		}

		return list
	}
	names := make([]string, 0, len(o.requireConstraintsPerPackage))
	requires := make(map[string]int32, len(o.requireConstraintsPerPackage))
	requireMatchers := make([][]namedMatcher, 0, len(o.requireConstraintsPerPackage))
	for name, set := range o.requireConstraintsPerPackage {
		requires[name] = int32(len(names)) //nolint:gosec // as many as the pool's names
		names = append(names, name)
		requireMatchers = append(requireMatchers, matchers(set))
	}
	conflicts := make(map[string][]namedMatcher, len(o.conflictConstraintsPerPackage))
	for name, set := range o.conflictConstraintsPerPackage {
		list := make([]namedMatcher, 0, set.Len())
		for str, constraint := range set.All() {
			list = append(list, namedMatcher{str: "conflict:" + str, matches: semver.CompilingMatcher.Matcher(constraint, semver.OpEQ)})
		}
		conflicts[name] = list
	}

	ids := newHashIDs()
	out := make([]packageHashes, len(packages))
	newWorker := func() func(from, to int) {
		strs := map[semver.ConstraintInterface]string{}
		constraintString := func(c semver.ConstraintInterface) string {
			s, ok := strs[c]
			if !ok {
				s = c.String()
				strs[c] = s
			}

			return s
		}
		local := ids.local()

		var buf, replacesPart, conflictPart []byte

		return func(from, to int) {
			for i := from; i < to; i++ {
				p := packages[i]
				if o.irremovablePackages[p.ID()] {
					continue
				}

				h := &out[i]
				buf = appendDependencyHash(buf[:0], p, constraintString)
				h.dependencyHash = local.id(buf)
				version := p.Version()

				// The replaces and conflicts parts of the group hash only depend on
				// the package (and name), so they are computed once rather than for
				// every require constraint.
				replacesDone := false

				for _, packageName := range p.Names(false) {
					name, ok := requires[packageName]
					if !ok {
						continue
					}

					if !replacesDone {
						replacesPart, replacesDone = appendReplacesHashPart(replacesPart[:0], p, constraintString), true
					}
					conflictPart = appendConflictHashPart(conflictPart[:0], conflicts[packageName], version)

					for _, require := range requireMatchers[name] {
						buf = buf[:0]
						if require.matches(version) {
							buf = append(buf, require.str...)
						}
						buf = append(buf, replacesPart...)
						buf = append(buf, conflictPart...)

						if len(buf) == 0 {
							continue
						}
						h.groups = append(h.groups, packageGroup{name: name, hash: local.id(buf)})
					}
				}
			}
		}
	}
	parallelRanges(len(packages), newWorker)

	return names, out
}

// appendReplacesHashPart appends the part of a group hash for the
// replaces of p that match its version.
func appendReplacesHashPart(dst []byte, p pkg.PackageInterface, constraintString func(semver.ConstraintInterface) string) []byte {
	for link := range p.Replaces().Values() {
		if semver.CompilingMatcher.Matcher(link.Constraint(), semver.OpEQ)(p.Version()) {
			// Use the same hash part as the regular require hash because that's what the replacement does
			dst = append(dst, "require:"...)
			dst = append(dst, constraintString(link.Constraint())...)
		}
	}

	return dst
}

// appendConflictHashPart appends the part of a group hash for the
// conflicts (of a package name) that match version.
func appendConflictHashPart(dst []byte, conflicts []namedMatcher, version string) []byte {
	for _, conflict := range conflicts {
		if conflict.matches(version) {
			dst = append(dst, conflict.str...)
		}
	}

	return dst
}

// appendDependencyHash appends what calculateDependencyHash returns;
// constraintString is (string) $constraint.
func appendDependencyHash(hash []byte, p pkg.PackageInterface, constraintString func(semver.ConstraintInterface) string) []byte {
	for _, section := range [...]struct {
		key   string
		links pkg.Links
	}{
		{"requires", p.Requires()},
		{"conflicts", p.Conflicts()},
		{"replaces", p.Replaces()},
		{"provides", p.Provides()},
	} {
		if section.links.Len() == 0 {
			continue
		}

		// start new hash section
		hash = append(hash, section.key...)
		hash = append(hash, ':')

		// To get the best dependency hash matches we should use Intervals::compactConstraint() here.
		// However, the majority of projects are going to specify their constraints already pretty
		// much in the best variant possible. In other words, we'd be wasting time here and it would actually hurt
		// performance more than the additional few packages that could be filtered out would benefit the process.
		subhash := make(map[string]string, section.links.Len())
		targets := make([]string, 0, section.links.Len())
		for link := range section.links.Values() {
			if _, ok := subhash[link.Target()]; !ok {
				targets = append(targets, link.Target())
			}
			subhash[link.Target()] = constraintString(link.Constraint())
		}

		// Sort for best result
		ksortStrings(targets)

		for _, target := range targets {
			hash = append(hash, target...)
			hash = append(hash, '@')
			hash = append(hash, subhash[target]...)
		}
	}

	return hash
}

// ksortStrings orders array keys as ksort() does (SORT_REGULAR): byte
// order, unless a key can be numeric.
func ksortStrings(keys []string) {
	numeric := false
	for _, k := range keys {
		if k == "" || strings.IndexByte("0123456789+-. \t\n\r\v\f", k[0]) >= 0 {
			numeric = true

			break
		}
	}
	if !numeric {
		slices.Sort(keys)

		return
	}

	arr := php.NewArray()
	for _, k := range keys {
		arr.Set(k, k)
	}
	php.Ksort(arr, php.SortRegular)
	keys = keys[:0]
	for k := range arr.All() {
		keys = append(keys, k.String())
	}
}

func (o *PoolOptimizer) markPackageForRemoval(id int) error {
	// We are not allowed to remove packages if they have been marked as irremovable
	if o.irremovablePackages[id] {
		return &util.LogicError{Message: "Attempted removing a package which was previously marked irremovable"}
	}

	o.packagesToRemove[id] = true

	return nil
}

// groupVersions is the $versions of keepPackageInGroup: version =>
// pretty version of the group's packages, default branch aliases replaced
// by the branch.
func groupVersions(pool *Pool, packageIDs []int32) *VersionMap {
	versions := &VersionMap{}
	for _, packageID := range packageIDs {
		groupPackage := pool.PackageByID(int(packageID))
		if alias, ok := groupPackage.(pkg.Alias); ok && groupPackage.PrettyVersion() == pkg.DefaultBranchAlias {
			groupPackage = alias.AliasOf()
		}
		versions.Set(groupPackage.Version(), groupPackage.PrettyVersion())
	}

	return versions
}

// keepPackageInGroup ports keepPackageInGroup; versions are the group's
// (groupVersions).
func (o *PoolOptimizer) keepPackageInGroup(p pkg.PackageInterface, packageName string, versions *VersionMap) {
	// Always record versions even if already kept — the package may appear in
	// groups for multiple names (own name + replacement names)
	o.recordRemovedVersionsForPackage(p, packageName, versions)

	// Already marked to keep — version recording done, skip unmarking/alias handling
	if !o.packagesToRemove[p.ID()] {
		return
	}

	o.unmarkPackageForRemoval(p)

	if alias, ok := p.(pkg.Alias); ok {
		aliasOf := alias.AliasOf()
		o.unmarkPackageForRemoval(aliasOf)
		o.recordRemovedVersionsForPackage(aliasOf, packageName, versions)
		for _, aliasPackage := range o.aliasesPerPackage[aliasOf.ID()] {
			o.unmarkPackageForRemoval(aliasPackage)
			o.recordRemovedVersionsForPackage(aliasPackage, packageName, versions)
		}

		return
	}

	for _, aliasPackage := range o.aliasesPerPackage[p.ID()] {
		o.unmarkPackageForRemoval(aliasPackage)
		o.recordRemovedVersionsForPackage(aliasPackage, packageName, versions)
	}
}

func (o *PoolOptimizer) unmarkPackageForRemoval(p pkg.PackageInterface) {
	delete(o.packagesToRemove, p.ID())
}

func (o *PoolOptimizer) recordRemovedVersionsForPackage(p pkg.PackageInterface, packageName string, versions *VersionMap) {
	if !hasName(p, packageName) {
		return
	}

	recorded, ok := o.removedVersionsByPackage[p]
	if !ok {
		recorded = &VersionMap{}
		o.removedVersionsByPackage[p] = recorded
	}
	for version, prettyVersion := range versions.All() {
		recorded.Set(version, prettyVersion)
	}
}

func (o *PoolOptimizer) optimizeImpossiblePackagesAway(request *Request, pool *Pool) error {
	lockedPackages := request.LockedPackages()
	if len(lockedPackages) == 0 {
		return nil
	}

	type indexed struct {
		ids      []int
		packages map[int]pkg.PackageInterface
	}
	packageIndex := map[string]*indexed{}
	for _, p := range pool.Packages() {
		id := p.ID()

		// Do not remove irremovable packages
		if o.irremovablePackages[id] {
			continue
		}
		// Do not remove a package aliased by another package, nor aliases
		if _, aliased := o.aliasesPerPackage[id]; aliased {
			continue
		}
		if _, isAlias := p.(pkg.Alias); isAlias {
			continue
		}
		// Do not remove locked packages
		if request.IsFixedPackage(p) || request.IsLockedPackage(p) {
			continue
		}

		entry, ok := packageIndex[p.Name()]
		if !ok {
			entry = &indexed{packages: map[int]pkg.PackageInterface{}}
			packageIndex[p.Name()] = entry
		}
		if _, ok := entry.packages[id]; !ok {
			entry.ids = append(entry.ids, id)
		}
		entry.packages[id] = p
	}

	for _, p := range lockedPackages {
		// If this locked package is no longer required by root or anything in the pool, it may get uninstalled so do not apply its requirements
		// In a case where a requirement WERE to appear in the pool by a package that would not be used, it would've been unlocked and so not filtered still
		isUnusedPackage := true
		for _, packageName := range p.Names(false) {
			if _, ok := o.requireConstraintsPerPackage[packageName]; ok {
				isUnusedPackage = false

				break
			}
		}

		if isUnusedPackage {
			continue
		}

		for link := range p.Requires().Values() {
			entry, ok := packageIndex[link.Target()]
			if !ok {
				continue
			}

			linkConstraint := link.Constraint()
			for _, id := range entry.ids {
				requiredPkg, ok := entry.packages[id]
				if !ok {
					continue
				}
				if !semver.CompilingMatcher.Match(linkConstraint, semver.OpEQ, requiredPkg.Version()) {
					if err := o.markPackageForRemoval(id); err != nil {
						return err
					}
					delete(entry.packages, id)
				}
			}
		}
	}

	return nil
}

// hasName is in_array($name, $package->getNames(false), true): the name or
// a replaced name.
func hasName(p pkg.PackageInterface, name string) bool {
	if p.Name() == name {
		return true
	}
	for link := range p.Replaces().Values() {
		if link.Target() == name {
			return true
		}
	}

	return false
}

// extractConstraints ports extractRequireConstraintsPerPackage and
// extractConflictConstraintsPerPackage.
func (o *PoolOptimizer) extractConstraints(perPackage map[string]*constraintSet, packageName string, constraint semver.ConstraintInterface) {
	set, ok := perPackage[packageName]
	if !ok {
		set = &constraintSet{}
		perPackage[packageName] = set
	}
	expanded, ok := o.expanded[constraint]
	if !ok {
		expanded = expandDisjunctiveMultiConstraints(constraint)
		o.expanded[constraint] = expanded
	}
	for _, c := range expanded {
		set.Set(c.String(), c)
	}
}

// expandDisjunctiveMultiConstraints ports expandDisjunctiveMultiConstraints.
func expandDisjunctiveMultiConstraints(constraint semver.ConstraintInterface) []semver.ConstraintInterface {
	constraint = semver.Intervals.CompactConstraint(constraint)

	if multi, ok := constraint.(*semver.MultiConstraint); ok && multi.IsDisjunctive() {
		// No need to call ourselves recursively here because Intervals::compactConstraint() ensures that there
		// are no nested disjunctive MultiConstraint instances possible
		return multi.Constraints()
	}

	// Regular constraints and conjunctive MultiConstraints
	return []semver.ConstraintInterface{constraint}
}
