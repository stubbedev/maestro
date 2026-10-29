// Ports src/Composer/DependencyResolver/Pool.php.

package resolver

import (
	"slices"
	"sort"
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
	// Policy maps package names to versions to why a policy removed them.
	Policy *repository.NameMap[*repository.NameMap[*PolicyRemovalReason]]
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

// PolicyRemovedVersions ports getPolicyRemovedVersions: the versions of a
// package that dependency policies removed, with the reason for each, in
// ascending version order. constraint may be nil.
func (p *Pool) PolicyRemovedVersions(packageName string, constraint semver.ConstraintInterface) *repository.NameMap[*PolicyRemovalReason] {
	versions, _ := p.removed.Policy.Get(packageName)
	type entry struct {
		version string
		reason  *PolicyRemovalReason
	}
	filtered := make([]entry, 0, versions.Len())
	for version, reason := range versions.All() {
		if constraint == nil || matchesVersion(constraint, version) {
			filtered = append(filtered, entry{version, reason})
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return semver.VersionCompare(filtered[i].version, filtered[j].version) < 0
	})

	result := &repository.NameMap[*PolicyRemovalReason]{}
	for _, e := range filtered {
		result.Set(e.version, e.reason)
	}

	return result
}

// PolicyRemovalReason ports getPolicyRemovalReason: why a policy removed the
// version, or nil when it did not.
func (p *Pool) PolicyRemovalReason(packageName, version string) *PolicyRemovalReason {
	versions, _ := p.removed.Policy.Get(packageName)
	reason, _ := versions.Get(version)

	return reason
}

// RecordPolicyRemoval ports Pool::recordPolicyRemoval: it records a removal
// under every name of the package, but a replacer never hides the removal of
// the package itself.
func RecordPolicyRemoval(policyRemovedVersions *repository.NameMap[*repository.NameMap[*PolicyRemovalReason]], pk pkg.PackageInterface, reason *PolicyRemovalReason) {
	for _, name := range pk.Names(false) {
		versions, ok := policyRemovedVersions.Get(name)
		if !ok {
			versions = &repository.NameMap[*PolicyRemovalReason]{}
			policyRemovedVersions.Set(name, versions)
		}
		existing, ok := versions.Get(pk.Version())
		if !ok || (existing.PackageName != name && pk.Name() == name) {
			versions.Set(pk.Version(), reason)
		}
	}
}

// AllPolicyRemovedVersions ports getAllPolicyRemovedVersions.
func (p *Pool) AllPolicyRemovedVersions() *repository.NameMap[*repository.NameMap[*PolicyRemovalReason]] {
	return p.removed.Policy
}

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
