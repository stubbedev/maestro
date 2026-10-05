// Ports src/Composer/DependencyResolver/Rule.php, GenericRule.php,
// Rule2Literals.php and MultiConflictRule.php.

package resolver

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// The reasons of a rule (Rule::RULE_*), with the reason data each carries.
const (
	// RuleRootRequire: *RootRequire.
	RuleRootRequire = 2
	// RuleFixed: the fixed pkg.PackageInterface.
	RuleFixed = 3
	// RulePackageConflict: the conflict *pkg.Link.
	RulePackageConflict = 6
	// RulePackageRequires: the require *pkg.Link.
	RulePackageRequires = 7
	// RulePackageSameName: the (replaced) package name, a string.
	RulePackageSameName = 10
	// RuleLearned: the index into the solver's learned pool, an int.
	RuleLearned = 12
	// RulePackageAlias: the alias pkg.PackageInterface.
	RulePackageAlias = 13
	// RulePackageInverseAlias: the aliased pkg.PackageInterface.
	RulePackageInverseAlias = 14
	// RuleLockedFilterListRemoved: the locked pkg.PackageInterface.
	RuleLockedFilterListRemoved = 15
)

// The PHP classes a rule is an instance of.
const (
	kindGeneric uint8 = iota
	kindRule2Literals
	kindMultiConflict
)

// RootRequire is the reason data of a RuleRootRequire rule:
// ['packageName' => ..., 'constraint' => ...].
type RootRequire struct {
	PackageName string
	Constraint  semver.ConstraintInterface
}

// Rule ports Composer\DependencyResolver\Rule and its subclasses
// GenericRule, Rule2Literals (two literals, both kept in order) and
// MultiConflictRule (at most one of three or more packages). Literals are
// package ids, negative for "not installed".
type Rule struct {
	literals   []int32
	reasonData any
	// next chains the rules of one hash bucket in a RuleSet.
	next     *Rule
	kind     uint8
	reason   uint8
	typ      uint8
	disabled bool
}

// low8 keeps the 8 bits Rule's PHP bitfield has for a reason or a type.
func low8(v int) uint8 { return uint8(v & 0xff) }

// literalOf is a package id as a literal: ids are positions in a pool,
// far below 2^31.
func literalOf(id int) int32 { return int32(id) } //nolint:gosec // see above

// NewGenericRule is new GenericRule($literals, $reason, $reasonData): the
// literals are copied and sorted.
func NewGenericRule(literals []int32, reason int, reasonData any) *Rule {
	r := &Rule{literals: slices.Clone(literals), reasonData: reasonData, kind: kindGeneric, reason: low8(reason), typ: 255}
	slices.Sort(r.literals)

	return r
}

// NewRule2Literals is new Rule2Literals($literal1, $literal2, $reason, $reasonData).
func NewRule2Literals(literal1, literal2 int32, reason int, reasonData any) *Rule {
	if literal1 > literal2 {
		literal1, literal2 = literal2, literal1
	}

	return &Rule{literals: []int32{literal1, literal2}, reasonData: reasonData, kind: kindRule2Literals, reason: low8(reason), typ: 255}
}

// NewMultiConflictRule is new MultiConflictRule($literals, $reason,
// $reasonData): the literals (at least 3) are copied and sorted.
func NewMultiConflictRule(literals []int32, reason int, reasonData any) (*Rule, error) {
	if len(literals) < 3 {
		return nil, &util.RuntimeError{Message: "multi conflict rule requires at least 3 literals"}
	}
	r := &Rule{literals: slices.Clone(literals), reasonData: reasonData, kind: kindMultiConflict, reason: low8(reason), typ: 255}
	slices.Sort(r.literals)

	return r, nil
}

// Literals ports getLiterals. The slice must not be modified.
func (r *Rule) Literals() []int32 { return r.literals }

// IsMultiConflict reports `instanceof MultiConflictRule`.
func (r *Rule) IsMultiConflict() bool { return r.kind == kindMultiConflict }

// Reason ports getReason.
func (r *Rule) Reason() int { return int(r.reason) }

// ReasonData ports getReasonData (see the Rule* constants for its type).
func (r *Rule) ReasonData() any { return r.reasonData }

// RootRequire returns the reason data of a RuleRootRequire rule.
func (r *Rule) RootRequire() *RootRequire { rr, _ := r.reasonData.(*RootRequire); return rr }

// Link returns the reason data of a RulePackageRequires or
// RulePackageConflict rule.
func (r *Rule) Link() *pkg.Link { l, _ := r.reasonData.(*pkg.Link); return l }

// Package returns the reason data of a RuleFixed,
// RuleLockedFilterListRemoved, RulePackageAlias or RulePackageInverseAlias
// rule.
func (r *Rule) Package() pkg.PackageInterface { p, _ := r.reasonData.(pkg.PackageInterface); return p }

// RequiredPackage ports getRequiredPackage; ok is false for null.
func (r *Rule) RequiredPackage() (name string, ok bool) {
	switch r.reason {
	case RuleRootRequire:
		return r.RootRequire().PackageName, true
	case RuleFixed, RuleLockedFilterListRemoved:
		return r.Package().Name(), true
	case RulePackageRequires:
		return r.Link().Target(), true
	}

	return "", false
}

// SetType ports setType: only the low 8 bits are kept.
func (r *Rule) SetType(typ int) { r.typ = low8(typ) }

// Type ports getType.
func (r *Rule) Type() int { return int(r.typ) }

// Disable ports disable. A MultiConflictRule cannot be disabled.
func (r *Rule) Disable() error {
	if r.kind == kindMultiConflict {
		return &util.RuntimeError{Message: "Disabling multi conflict rules is not possible. Please contact composer at https://github.com/composer/composer to let us debug what lead to this situation."}
	}
	r.disabled = true

	return nil
}

// Enable ports enable.
func (r *Rule) Enable() { r.disabled = false }

// IsDisabled ports isDisabled.
func (r *Rule) IsDisabled() bool { return r.disabled }

// IsEnabled ports isEnabled.
func (r *Rule) IsEnabled() bool { return !r.disabled }

// IsAssertion ports isAssertion: a GenericRule with a single literal.
func (r *Rule) IsAssertion() bool { return r.kind == kindGeneric && len(r.literals) == 1 }

// hash stands for getHash: rules of different classes never share a
// bucket, as their PHP hashes differ.
func (r *Rule) hash() uint64 {
	h := uint64(14695981039346656037) ^ uint64(r.kind)
	for _, l := range r.literals {
		h = (h ^ uint64(uint32(l))) * 1099511628211 //nolint:gosec // the bits of l, for hashing
	}

	return h
}

// Equals ports equals with each class's semantics: a MultiConflictRule
// only equals another one, the others compare the literals.
func (r *Rule) Equals(other *Rule) bool {
	if r.kind == kindMultiConflict && other.kind != kindMultiConflict {
		return false
	}

	return slices.Equal(r.literals, other.literals)
}

// String ports __toString.
func (r *Rule) String() string {
	var b strings.Builder
	if r.disabled {
		b.WriteString("disabled")
	}
	b.WriteByte('(')
	if r.kind == kindMultiConflict {
		b.WriteString("multi(")
	}
	for i, l := range r.literals {
		if i != 0 {
			b.WriteByte('|')
		}
		b.WriteString(strconv.Itoa(int(l)))
	}
	if r.kind == kindMultiConflict {
		b.WriteByte(')')
	}
	b.WriteByte(')')

	return b.String()
}

// IsCausedByLock ports isCausedByLock.
func (r *Rule) IsCausedByLock(request *Request, pool *Pool) (bool, error) {
	var target string
	var constraint semver.ConstraintInterface
	switch r.reason {
	case RulePackageRequires:
		target, constraint = r.Link().Target(), r.Link().Constraint()
	case RuleRootRequire:
		target, constraint = r.RootRequire().PackageName, r.RootRequire().Constraint
	default:
		return false, nil
	}
	if repository.IsPlatformPackage(target) || request.LockedRepository() == nil {
		return false, nil
	}
	packages, err := request.LockedRepository().Packages()
	if err != nil {
		return false, err
	}
	for _, p := range packages {
		if p.Name() != target {
			continue
		}
		if pool.IsUnacceptableFixedOrLockedPackage(p) {
			return true, nil
		}
		if !matchesVersion(constraint, p.Version()) {
			return true, nil
		}
		// required package was locked but has been unlocked and still matches
		if r.reason == RulePackageRequires && !request.IsLockedPackage(p) {
			return true, nil
		}

		break
	}

	return false, nil
}

// SourcePackage ports getSourcePackage.
func (r *Rule) SourcePackage(pool *Pool) (pkg.PackageInterface, error) {
	switch r.reason {
	case RulePackageConflict:
		package1 := deduplicateDefaultBranchAlias(pool.LiteralToPackage(r.literals[0]))
		package2 := deduplicateDefaultBranchAlias(pool.LiteralToPackage(r.literals[1]))
		// swap literals if they are not in the right order with package2 being the conflicter
		if r.Link().Source() == package1.Name() {
			package2 = package1
		}

		return package2, nil
	case RulePackageRequires:
		return deduplicateDefaultBranchAlias(pool.LiteralToPackage(r.literals[0])), nil
	}

	return nil, &util.LogicError{Message: "Not implemented"}
}

// PrettyString ports getPrettyString.
func (r *Rule) PrettyString(ctx *PrettyContext) (string, error) {
	pool := ctx.Pool
	literals := r.literals
	switch r.reason {
	case RuleRootRequire:
		rr := r.RootRequire()
		packages := pool.WhatProvides(rr.PackageName, rr.Constraint)
		if len(packages) == 0 {
			return "No package found to satisfy root composer.json require " + rr.PackageName + " " + rr.Constraint.PrettyString(), nil
		}

		var nonAlias pkg.PackageInterface
		nonAliasCount := 0
		for _, p := range packages {
			if _, ok := p.(pkg.Alias); !ok {
				nonAlias = p
				nonAliasCount++
			}
		}
		if nonAliasCount == 1 && ctx.Request.IsLockedPackage(nonAlias) {
			return nonAlias.PrettyName() + " is locked to version " + nonAlias.PrettyVersion() + " and an update of this package was not requested.", nil
		}

		return "Root composer.json requires " + rr.PackageName + " " + rr.Constraint.PrettyString() + " -> satisfiable by " + ctx.packageList(packages, rr.Constraint, false) + ".", nil

	case RuleFixed:
		p := deduplicateDefaultBranchAlias(r.Package())
		if ctx.Request.IsLockedPackage(p) {
			return p.PrettyName() + " is locked to version " + p.PrettyVersion() + " and an update of this package was not requested.", nil
		}

		return p.PrettyName() + " is present at version " + p.PrettyVersion() + " and cannot be modified by Composer", nil

	case RuleLockedFilterListRemoved:
		p := deduplicateDefaultBranchAlias(r.Package())

		return p.PrettyName() + " " + p.PrettyVersion() + " was removed by a dependency policy (e.g. malware) and cannot be installed.", nil

	case RulePackageConflict:
		return r.conflictPrettyString(pool)

	case RulePackageRequires:
		sourcePackage := deduplicateDefaultBranchAlias(pool.LiteralToPackage(literals[0]))
		link := r.Link()
		requires := make([]pkg.PackageInterface, 0, len(literals)-1)
		for _, literal := range literals[1:] {
			requires = append(requires, pool.LiteralToPackage(literal))
		}

		text := link.PrettyString(sourcePackage)
		if len(requires) > 0 {
			return text + " -> satisfiable by " + ctx.packageList(requires, link.Constraint(), false) + ".", nil
		}
		reason, err := ctx.MissingPackageReason(link.Target(), link.Constraint())
		if err != nil {
			return "", err
		}

		return text + " -> " + reason[1], nil

	case RulePackageSameName:
		return r.sameNamePrettyString(ctx)

	case RuleLearned:
		return r.learnedPrettyString(ctx)

	case RulePackageAlias:
		aliasPackage := pool.LiteralToPackage(literals[0])
		// avoid returning content like "9999999-dev is an alias of dev-master" as it is useless
		if aliasPackage.Version() == pkg.DefaultBranchAlias {
			return "", nil
		}
		p := deduplicateDefaultBranchAlias(pool.LiteralToPackage(literals[1]))

		return aliasPackage.PrettyString() + " is an alias of " + p.PrettyString() + " and thus requires it to be installed too.", nil

	case RulePackageInverseAlias:
		// inverse alias rules work the other way around than above
		aliasPackage := pool.LiteralToPackage(literals[1])
		// avoid returning content like "9999999-dev is an alias of dev-master" as it is useless
		if aliasPackage.Version() == pkg.DefaultBranchAlias {
			return "", nil
		}
		p := deduplicateDefaultBranchAlias(pool.LiteralToPackage(literals[0]))

		return aliasPackage.PrettyString() + " is an alias of " + p.PrettyString() + " and must be installed with it.", nil
	}

	var b strings.Builder
	b.WriteByte('(')
	for i, literal := range literals {
		if i != 0 {
			b.WriteByte('|')
		}
		b.WriteString(pool.LiteralToPrettyString(literal, ctx.InstalledMap))
	}
	b.WriteByte(')')

	return b.String(), nil
}

func (r *Rule) conflictPrettyString(pool *Pool) (string, error) {
	package1 := deduplicateDefaultBranchAlias(pool.LiteralToPackage(r.literals[0]))
	package2 := deduplicateDefaultBranchAlias(pool.LiteralToPackage(r.literals[1]))
	conflictTarget := package1.PrettyString()
	link := r.Link()

	// swap literals if they are not in the right order with package2 being the conflicter
	if link.Source() == package1.Name() {
		package1, package2 = package2, package1
		prettyConstraint, err := link.PrettyConstraint()
		if err != nil {
			return "", err
		}
		conflictTarget = package1.PrettyName() + " " + prettyConstraint
	}

	// if the conflict is not directly against the package but something it provides/replaces,
	// we try to find that link to display a better message
	if link.Target() != package1.Name() {
		provideType := ""
		var provided *pkg.Link
		for provide := range package1.Provides().Values() {
			if provide.Target() == link.Target() {
				provideType, provided = "provides", provide

				break
			}
		}
		for replace := range package1.Replaces().Values() {
			if replace.Target() == link.Target() {
				provideType, provided = "replaces", replace

				break
			}
		}

		if provideType != "" {
			prettyConstraint, err := link.PrettyConstraint()
			if err != nil {
				return "", err
			}
			providedConstraint, err := provided.PrettyConstraint()
			if err != nil {
				return "", err
			}
			conflictTarget = link.Target() + " " + prettyConstraint + " (" + package1.PrettyString() + " " + provideType + " " + link.Target() + " " + providedConstraint + ")"
		}
	}

	return package2.PrettyString() + " conflicts with " + conflictTarget + ".", nil
}

func (r *Rule) sameNamePrettyString(ctx *PrettyContext) (string, error) {
	pool := ctx.Pool
	packageNames := &repository.NameMap[bool]{}
	for _, literal := range r.literals {
		packageNames.Set(pool.LiteralToPackage(literal).Name(), true)
	}
	replacedName, _ := r.reasonData.(string)

	if packageNames.Len() <= 1 {
		list := ctx.packageListOfLiterals(r.literals, nil, true)

		return "You can only install one version of a package, so only one of these can be installed: " + list + ".", nil
	}

	var reason string
	if !packageNames.Has(replacedName) {
		quantity := "all"
		if len(r.literals) == 2 {
			quantity = "both"
		}
		reason = "They " + quantity + " replace " + replacedName + " and thus cannot coexist."
	} else {
		var replacerNames []string
		for name := range packageNames.All() {
			if name != replacedName {
				replacerNames = append(replacerNames, name)
			}
		}
		if len(replacerNames) == 1 {
			reason = replacerNames[0] + " replaces "
		} else {
			reason = "[" + strings.Join(replacerNames, ", ") + "] replace "
		}
		reason += replacedName + " and thus cannot coexist with it."
	}

	var installedPackages, removablePackages []pkg.PackageInterface
	for _, literal := range r.literals {
		if ctx.InstalledMap[int(abs32(literal))] {
			installedPackages = append(installedPackages, pool.LiteralToPackage(literal))
		} else {
			removablePackages = append(removablePackages, pool.LiteralToPackage(literal))
		}
	}

	if len(installedPackages) > 0 && len(removablePackages) > 0 {
		return ctx.packageList(removablePackages, nil, true) + " cannot be installed as that would require removing " + ctx.packageList(installedPackages, nil, true) + ". " + reason, nil
	}

	list := ctx.packageListOfLiterals(r.literals, nil, true)

	return "Only one of these can be installed: " + list + ". " + reason, nil
}

func (r *Rule) learnedPrettyString(ctx *PrettyContext) (string, error) {
	pool := ctx.Pool
	const learnedString = " (conflict analysis result)"

	if len(r.literals) == 1 {
		return "Conclusion: " + pool.LiteralToPrettyString(r.literals[0], ctx.InstalledMap) + learnedString, nil
	}

	groups := &repository.NameMap[[]pkg.PackageInterface]{}
	for _, literal := range r.literals {
		p := pool.LiteralToPackage(literal)
		var group string
		if ctx.InstalledMap[p.ID()] {
			group = "remove"
			if literal > 0 {
				group = "keep"
			}
		} else {
			group = "don't install"
			if literal > 0 {
				group = "install"
			}
		}
		existing, _ := groups.Get(group)
		groups.Set(group, append(existing, deduplicateDefaultBranchAlias(p)))
	}

	ruleTexts := make([]string, 0, groups.Len())
	for group, packages := range groups.All() {
		list := ctx.packageList(packages, nil, false)
		oneOf := ""
		if len(packages) > 1 {
			oneOf = " one of"
		}
		ruleTexts = append(ruleTexts, group+oneOf+" "+list)
	}

	return "Conclusion: " + strings.Join(ruleTexts, " | ") + learnedString, nil
}

// deduplicateDefaultBranchAlias ports Rule::deduplicateDefaultBranchAlias.
func deduplicateDefaultBranchAlias(p pkg.PackageInterface) pkg.PackageInterface {
	if a, ok := p.(pkg.Alias); ok && a.PrettyVersion() == pkg.DefaultBranchAlias {
		return a.AliasOf()
	}

	return p
}

func abs32(l int32) int32 {
	if l < 0 {
		return -l
	}

	return l
}
