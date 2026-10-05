// Ports src/Composer/DependencyResolver/PoolOptimizer.php.

package resolver

import (
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
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

	// First pass over all packages to extract information and mark package constraints irremovable
	for _, p := range pool.Packages() {
		// Extract package requirements
		for link := range p.Requires().Values() {
			o.extractConstraints(o.requireConstraintsPerPackage, link.Target(), link.Constraint())
		}
		// Extract package conflicts
		for link := range p.Conflicts().Values() {
			o.extractConstraints(o.conflictConstraintsPerPackage, link.Target(), link.Constraint())
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
	// package name => group hash => dependency hash => package ids
	identicalDefinitionsPerPackage := &repository.NameMap[*repository.NameMap[*repository.NameMap[[]int32]]]{}

	var groupHash strings.Builder
	for _, p := range pool.Packages() {
		// If that package was already marked irremovable, we can skip
		// the entire process for it
		if o.irremovablePackages[p.ID()] {
			continue
		}

		if err := o.markPackageForRemoval(p.ID()); err != nil {
			return err
		}

		dependencyHash := calculateDependencyHash(pool, p)
		version := p.Version()

		// The replaces and conflicts parts of the group hash only depend on
		// the package (and name), so they are computed once rather than for
		// every require constraint.
		replacesPart, replacesDone := "", false

		for _, packageName := range p.Names(false) {
			requireConstraints, ok := o.requireConstraintsPerPackage[packageName]
			if !ok {
				continue
			}

			if !replacesDone {
				replacesPart, replacesDone = replacesHashPart(pool, p), true
			}
			conflictPart := o.conflictHashPart(packageName, version)

			for requireString, requireConstraint := range requireConstraints.All() {
				groupHash.Reset()

				if semver.CompilingMatcher.Match(requireConstraint, semver.OpEQ, version) {
					groupHash.WriteString("require:" + requireString)
				}
				groupHash.WriteString(replacesPart)
				groupHash.WriteString(conflictPart)

				if groupHash.Len() == 0 {
					continue
				}

				groups, ok := identicalDefinitionsPerPackage.Get(packageName)
				if !ok {
					groups = &repository.NameMap[*repository.NameMap[[]int32]]{}
					identicalDefinitionsPerPackage.Set(packageName, groups)
				}
				byDependencies, ok := groups.Get(groupHash.String())
				if !ok {
					byDependencies = &repository.NameMap[[]int32]{}
					groups.Set(groupHash.String(), byDependencies)
				}
				ids, _ := byDependencies.Get(dependencyHash)
				byDependencies.Set(dependencyHash, append(ids, literalOf(p.ID())))
			}
		}
	}

	for packageName, constraintGroups := range identicalDefinitionsPerPackage.All() {
		for _, constraintGroup := range constraintGroups.All() {
			for _, packageIDs := range constraintGroup.All() {
				versions := groupVersions(pool, packageIDs)

				// Only one package in this constraint group has the same requirements, we're not allowed to remove that package
				if len(packageIDs) == 1 {
					o.keepPackageInGroup(pool.PackageByID(int(packageIDs[0])), packageName, versions)

					continue
				}

				// Otherwise we find out which one is the preferred package in this constraint group which is
				// then not allowed to be removed either
				for _, preferredLiteral := range o.policy.SelectPreferredPackages(pool, packageIDs, "") {
					o.keepPackageInGroup(pool.LiteralToPackage(preferredLiteral), packageName, versions)
				}
			}
		}
	}

	return nil
}

// replacesHashPart is the part of a group hash for the replaces of p that
// match its version.
func replacesHashPart(pool *Pool, p pkg.PackageInterface) string {
	var part strings.Builder
	for link := range p.Replaces().Values() {
		if semver.CompilingMatcher.Match(link.Constraint(), semver.OpEQ, p.Version()) {
			// Use the same hash part as the regular require hash because that's what the replacement does
			part.WriteString("require:" + pool.constraintString(link.Constraint()))
		}
	}

	return part.String()
}

// conflictHashPart is the part of a group hash for the conflicts with
// packageName that match version.
func (o *PoolOptimizer) conflictHashPart(packageName, version string) string {
	var part strings.Builder
	for conflictString, conflictConstraint := range o.conflictConstraintsPerPackage[packageName].All() {
		if semver.CompilingMatcher.Match(conflictConstraint, semver.OpEQ, version) {
			part.WriteString("conflict:" + conflictString)
		}
	}

	return part.String()
}

// calculateDependencyHash ports calculateDependencyHash.
func calculateDependencyHash(pool *Pool, p pkg.PackageInterface) string {
	var hash strings.Builder

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
		hash.WriteString(section.key + ":")

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
			subhash[link.Target()] = pool.constraintString(link.Constraint())
		}

		// Sort for best result
		ksortStrings(targets)

		for _, target := range targets {
			hash.WriteString(target + "@" + subhash[target])
		}
	}

	return hash.String()
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
		return &util.LogicError{Site: phperr.At("PoolOptimizer.php", 299), Message: "Attempted removing a package which was previously marked irremovable"}
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
