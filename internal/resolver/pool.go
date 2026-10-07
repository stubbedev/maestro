// Ports src/Composer/DependencyResolver/Pool.php.

package resolver

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
)

// VersionMap is array<string, string>: normalized version => pretty
// version, in insertion order.
type VersionMap = repository.NameMap[string]

// Removed holds the versions the optimizer and the pool filters took out
// of a pool, for the problem messages (the optional arguments of new
// Pool()).
type Removed struct {
	// Versions maps package names to the versions the optimizer removed.
	Versions *repository.NameMap[*VersionMap]
	// ByPackage maps the packages the optimizer kept to the versions of
	// their identical-dependency groups.
	ByPackage map[pkg.PackageInterface]*VersionMap
	// Security maps package names to versions to the advisories that
	// removed them.
	Security *repository.NameMap[*repository.NameMap[[]repository.Advisory]]
	// Abandoned maps package names to the versions removed as abandoned.
	Abandoned *repository.NameMap[*VersionMap]
	// FilterList maps package names to versions to the filter list
	// entries that removed them.
	FilterList *repository.NameMap[*repository.NameMap[[]*repository.FilterListEntry]]
}

// Pool ports Composer\DependencyResolver\Pool: the packages the solver
// chooses from. Package ids are their positions, starting at 1.
type Pool struct {
	packages                          []pkg.PackageInterface
	packageByName                     map[string][]pkg.PackageInterface
	providerCache                     map[providerKey][]pkg.PackageInterface
	constraintStrings                 map[semver.ConstraintInterface]string
	unacceptableFixedOrLockedPackages []pkg.PackageInterface
	removed                           Removed
}

type providerKey struct {
	name       string
	constraint string
	null       bool
}

// NewPool is new Pool($packages, $unacceptableFixedOrLockedPackages, ...):
// it gives the packages the ids 1..n. removed may be nil.
func NewPool(packages, unacceptableFixedOrLockedPackages []pkg.PackageInterface, removed *Removed) *Pool {
	p := &Pool{
		packageByName:                     make(map[string][]pkg.PackageInterface, len(packages)),
		providerCache:                     make(map[providerKey][]pkg.PackageInterface),
		constraintStrings:                 make(map[semver.ConstraintInterface]string),
		unacceptableFixedOrLockedPackages: unacceptableFixedOrLockedPackages,
	}
	if removed != nil {
		p.removed = *removed
	}
	p.setPackages(packages)

	return p
}

// matchesVersion is $constraint->matches(new Constraint('==', $version)).
func matchesVersion(constraint semver.ConstraintInterface, version string) bool {
	return constraint.Matches(semver.NewConstraintOp(semver.OpEQ, version))
}

// RemovedVersions ports getRemovedVersions: the versions of name the
// optimizer removed that match constraint.
func (p *Pool) RemovedVersions(name string, constraint semver.ConstraintInterface) *VersionMap {
	result := &VersionMap{}
	versions, ok := p.removed.Versions.Get(name)
	if !ok {
		return result
	}
	for version, prettyVersion := range versions.All() {
		if matchesVersion(constraint, version) {
			result.Set(version, prettyVersion)
		}
	}

	return result
}

// AllRemovedVersions ports getAllRemovedVersions.
func (p *Pool) AllRemovedVersions() *repository.NameMap[*VersionMap] { return p.removed.Versions }

// RemovedVersionsByPackage ports getRemovedVersionsByPackage (keyed by the
// package object rather than its spl_object_id).
func (p *Pool) RemovedVersionsByPackage(pk pkg.PackageInterface) *VersionMap {
	if v, ok := p.removed.ByPackage[pk]; ok {
		return v
	}

	return &VersionMap{}
}

// AllRemovedVersionsByPackage ports getAllRemovedVersionsByPackage.
func (p *Pool) AllRemovedVersionsByPackage() map[pkg.PackageInterface]*VersionMap {
	return p.removed.ByPackage
}

// IsSecurityRemovedPackageVersion ports isSecurityRemovedPackageVersion;
// constraint may be nil.
func (p *Pool) IsSecurityRemovedPackageVersion(packageName string, constraint semver.ConstraintInterface) bool {
	versions, _ := p.removed.Security.Get(packageName)
	for version := range versions.All() {
		if constraint != nil && matchesVersion(constraint, version) {
			return true
		}
	}

	return false
}

// SecurityAdvisoryIdentifiersForPackageVersion ports
// getSecurityAdvisoryIdentifiersForPackageVersion.
func (p *Pool) SecurityAdvisoryIdentifiersForPackageVersion(packageName string, constraint semver.ConstraintInterface) []string {
	versions, _ := p.removed.Security.Get(packageName)
	for version, advisories := range versions.All() {
		if constraint != nil && matchesVersion(constraint, version) {
			ids := make([]string, len(advisories))
			for i, advisory := range advisories {
				ids[i] = advisory.Partial().AdvisoryID
			}

			return ids
		}
	}

	return nil
}

// IsAbandonedRemovedPackageVersion ports isAbandonedRemovedPackageVersion.
func (p *Pool) IsAbandonedRemovedPackageVersion(packageName string, constraint semver.ConstraintInterface) bool {
	versions, _ := p.removed.Abandoned.Get(packageName)
	for version := range versions.All() {
		if constraint != nil && matchesVersion(constraint, version) {
			return true
		}
	}

	return false
}

// AllSecurityRemovedPackageVersions ports getAllSecurityRemovedPackageVersions.
func (p *Pool) AllSecurityRemovedPackageVersions() *repository.NameMap[*repository.NameMap[[]repository.Advisory]] {
	return p.removed.Security
}

// AllAbandonedRemovedPackageVersions ports getAllAbandonedRemovedPackageVersions.
func (p *Pool) AllAbandonedRemovedPackageVersions() *repository.NameMap[*VersionMap] {
	return p.removed.Abandoned
}

// IsFilterListRemovedPackageVersion ports isFilterListRemovedPackageVersion.
func (p *Pool) IsFilterListRemovedPackageVersion(packageName string, constraint semver.ConstraintInterface) bool {
	versions, _ := p.removed.FilterList.Get(packageName)
	for version := range versions.All() {
		if constraint != nil && matchesVersion(constraint, version) {
			return true
		}
	}

	return false
}

// AllFilterListRemovedPackageVersions ports getAllFilterListRemovedPackageVersions.
func (p *Pool) AllFilterListRemovedPackageVersions() *repository.NameMap[*repository.NameMap[[]*repository.FilterListEntry]] {
	return p.removed.FilterList
}

// FilterListEntryForPackageVersion ports getFilterListEntryForPackageVersion:
// list name => "flagged as malware reported by ..." / "filtered by ...".
func (p *Pool) FilterListEntryForPackageVersion(packageName string, constraint semver.ConstraintInterface) *repository.NameMap[string] {
	lists := &repository.NameMap[[]string]{}
	seen := map[*repository.FilterListEntry]bool{}
	versions, _ := p.removed.FilterList.Get(packageName)
	for version, entries := range versions.All() {
		if constraint == nil || !matchesVersion(constraint, version) {
			continue
		}
		for _, entry := range entries {
			if seen[entry] {
				continue
			}
			seen[entry] = true
			var b strings.Builder
			if entry.Source.Valid && phpTruthy(entry.Source.S) {
				b.WriteString(" reported by " + entry.Source.S)
			}
			if entry.URL.Valid && phpTruthy(entry.URL.S) {
				b.WriteString(" (see " + entry.URL.S + ")")
			}
			if entry.Reason.Valid && phpTruthy(entry.Reason.S) {
				b.WriteString(" reason: " + entry.Reason.S)
			}
			list, _ := lists.Get(entry.ListName)
			lists.Set(entry.ListName, append(list, b.String()))
		}
	}

	result := &repository.NameMap[string]{}
	for listName, listEntries := range lists.All() {
		action := "filtered by "
		if listName == "malware" {
			action = "flagged as "
		}
		result.Set(listName, action+listName+strings.Join(listEntries, ", "))
	}

	return result
}

// phpTruthy is (bool) $string.
func phpTruthy(s string) bool { return s != "" && s != "0" }

func (p *Pool) setPackages(packages []pkg.PackageInterface) {
	p.packages = make([]pkg.PackageInterface, 0, len(packages))
	for i, pk := range packages {
		p.packages = append(p.packages, pk)
		pk.SetID(i + 1)
		for _, provided := range pk.Names(true) {
			p.packageByName[provided] = append(p.packageByName[provided], pk)
		}
	}
}

// Packages ports getPackages. The slice must not be modified.
func (p *Pool) Packages() []pkg.PackageInterface { return p.packages }

// PackageByID ports packageById.
func (p *Pool) PackageByID(id int) pkg.PackageInterface { return p.packages[id-1] }

// Count ports count().
func (p *Pool) Count() int { return len(p.packages) }

// constraintString is (string) $constraint, memoized per constraint object.
func (p *Pool) constraintString(c semver.ConstraintInterface) string {
	if s, ok := p.constraintStrings[c]; ok {
		return s
	}
	s := c.String()
	p.constraintStrings[c] = s

	return s
}

// WhatProvides ports whatProvides: the packages named name or providing or
// replacing it that match constraint (nil: any). The slice must not be
// modified.
func (p *Pool) WhatProvides(name string, constraint semver.ConstraintInterface) []pkg.PackageInterface {
	key := providerKey{name: name, null: constraint == nil}
	if constraint != nil {
		key.constraint = p.constraintString(constraint)
	}
	if cached, ok := p.providerCache[key]; ok {
		return cached
	}
	result := p.computeWhatProvides(name, constraint)
	p.providerCache[key] = result

	return result
}

func (p *Pool) computeWhatProvides(name string, constraint semver.ConstraintInterface) []pkg.PackageInterface {
	candidates := p.packageByName[name]
	if len(candidates) == 0 {
		return []pkg.PackageInterface{}
	}
	// the candidates of the name itself are matched with the checker
	// CompilingMatcher::match uses (its cached results are what the checker
	// gives), looked up once for them all
	var matcher func(version string) bool
	matches := make([]pkg.PackageInterface, 0, len(candidates))
	for _, candidate := range candidates {
		var ok bool
		if candidate.Name() == name && constraint != nil {
			if matcher == nil {
				matcher = semver.CompilingMatcher.Matcher(constraint, semver.OpEQ)
			}
			ok = matcher(candidate.Version())
		} else {
			ok = p.Match(candidate, name, constraint)
		}
		if ok {
			matches = append(matches, candidate)
		}
	}

	return matches
}

// LiteralToPackage ports literalToPackage.
func (p *Pool) LiteralToPackage(literal int32) pkg.PackageInterface {
	if literal < 0 {
		literal = -literal
	}

	return p.packages[literal-1]
}

// LiteralToPrettyString ports literalToPrettyString; installedMap holds
// the ids of the installed packages.
func (p *Pool) LiteralToPrettyString(literal int32, installedMap map[int]bool) string {
	pk := p.LiteralToPackage(literal)
	var prefix string
	if installedMap[pk.ID()] {
		prefix = "remove"
		if literal > 0 {
			prefix = "keep"
		}
	} else {
		prefix = "don't install"
		if literal > 0 {
			prefix = "install"
		}
	}

	return prefix + " " + pk.PrettyString()
}

// Match ports match: whether candidate is name, or provides or replaces
// it, in a version matching constraint (nil: any).
func (p *Pool) Match(candidate pkg.PackageInterface, name string, constraint semver.ConstraintInterface) bool {
	if candidate.Name() == name {
		return constraint == nil || semver.CompilingMatcher.Match(constraint, semver.OpEQ, candidate.Version())
	}

	provides := candidate.Provides()
	replaces := candidate.Replaces()

	// aliases create multiple replaces/provides for one target so they can not use the shortcut below
	if replaces.Has("0") || provides.Has("0") {
		for link := range provides.Values() {
			if link.Target() == name && (constraint == nil || constraint.Matches(link.Constraint())) {
				return true
			}
		}
		for link := range replaces.Values() {
			if link.Target() == name && (constraint == nil || constraint.Matches(link.Constraint())) {
				return true
			}
		}

		return false
	}

	if link, ok := provides.Get(name); ok && (constraint == nil || constraint.Matches(link.Constraint())) {
		return true
	}
	if link, ok := replaces.Get(name); ok && (constraint == nil || constraint.Matches(link.Constraint())) {
		return true
	}

	return false
}

// IsUnacceptableFixedOrLockedPackage ports isUnacceptableFixedOrLockedPackage.
func (p *Pool) IsUnacceptableFixedOrLockedPackage(pk pkg.PackageInterface) bool {
	return slices.Contains(p.unacceptableFixedOrLockedPackages, pk)
}

// UnacceptableFixedOrLockedPackages ports getUnacceptableFixedOrLockedPackages.
func (p *Pool) UnacceptableFixedOrLockedPackages() []pkg.PackageInterface {
	return p.unacceptableFixedOrLockedPackages
}

// String ports __toString.
func (p *Pool) String() string {
	var b strings.Builder
	b.WriteString("Pool:\n")
	for _, pk := range p.packages {
		id := strconv.Itoa(pk.ID())
		b.WriteString("- " + strings.Repeat(" ", max(0, 6-len(id))) + id + ": " + pk.Name() + "\n")
	}

	return b.String()
}
