// Ports src/Composer/DependencyResolver/RuleSetGenerator.php.

package resolver

import (
	"slices"

	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// ignoreNothing is PlatformRequirementFilterFactory::ignoreNothing().
type ignoreNothing struct{}

func (ignoreNothing) IsIgnored(string) bool { return false }

// ruleAllocator hands out rules and literal slices from shared blocks, so
// that generating hundreds of thousands of rules does not allocate each
// one separately.
type ruleAllocator struct {
	rules    []Rule
	literals []int32
}

const (
	ruleBlock    = 1024
	literalBlock = 16384
)

func (a *ruleAllocator) rule() *Rule {
	if len(a.rules) == 0 {
		a.rules = make([]Rule, ruleBlock)
	}
	r := &a.rules[0]
	a.rules = a.rules[1:]

	return r
}

func (a *ruleAllocator) lits(n int) []int32 {
	if n > literalBlock/4 {
		return make([]int32, n)
	}
	if len(a.literals) < n {
		a.literals = make([]int32, literalBlock)
	}
	l := a.literals[:n:n]
	a.literals = a.literals[n:]

	return l
}

// newRule builds a rule of kind from sorted literals.
func (a *ruleAllocator) newRule(kind uint8, literals []int32, reason int, reasonData any) *Rule {
	r := a.rule()
	*r = Rule{literals: literals, reasonData: reasonData, kind: kind, reason: low8(reason), typ: 255}

	return r
}

// RuleSetGenerator ports Composer\DependencyResolver\RuleSetGenerator.
type RuleSetGenerator struct {
	policy               Policy
	pool                 *Pool
	rules                *RuleSet
	addedMap             []bool
	addedNegative        []int
	addedList            []pkg.PackageInterface
	addedPackagesByNames map[string][]pkg.PackageInterface
	addedNames           []string
	alloc                ruleAllocator
	queue                []pkg.PackageInterface
}

// NewRuleSetGenerator is new RuleSetGenerator($policy, $pool).
func NewRuleSetGenerator(policy Policy, pool *Pool) *RuleSetGenerator {
	return &RuleSetGenerator{policy: policy, pool: pool, rules: NewRuleSet()}
}

func packageID(p pkg.PackageInterface) int32 { return literalOf(p.ID()) }

// createRequireRule ports createRequireRule: nil for a self fulfilling
// rule.
func (g *RuleSetGenerator) createRequireRule(p pkg.PackageInterface, providers []pkg.PackageInterface, reason int, reasonData any) *Rule {
	// self fulfilling rule?
	if slices.Contains(providers, p) {
		return nil
	}
	literals := g.alloc.lits(len(providers) + 1)
	literals[0] = -packageID(p)
	for i, provider := range providers {
		literals[i+1] = packageID(provider)
	}
	slices.Sort(literals)

	return g.alloc.newRule(kindGeneric, literals, reason, reasonData)
}

// createInstallOneOfRule ports createInstallOneOfRule.
func (g *RuleSetGenerator) createInstallOneOfRule(packages []pkg.PackageInterface, reason int, reasonData any) *Rule {
	literals := g.alloc.lits(len(packages))
	for i, p := range packages {
		literals[i] = packageID(p)
	}
	slices.Sort(literals)

	return g.alloc.newRule(kindGeneric, literals, reason, reasonData)
}

// createRule2Literals ports createRule2Literals: nil for a self conflict.
func (g *RuleSetGenerator) createRule2Literals(issuer, provider pkg.PackageInterface, reason int, reasonData any) *Rule {
	// ignore self conflict
	if issuer == provider {
		return nil
	}

	return g.rule2Literals(-packageID(issuer), -packageID(provider), reason, reasonData)
}

func (g *RuleSetGenerator) rule2Literals(l1, l2 int32, reason int, reasonData any) *Rule {
	literals := g.alloc.lits(2)
	literals[0], literals[1] = min(l1, l2), max(l1, l2)

	return g.alloc.newRule(kindRule2Literals, literals, reason, reasonData)
}

// createMultiConflictRule ports createMultiConflictRule.
func (g *RuleSetGenerator) createMultiConflictRule(packages []pkg.PackageInterface, reason int, reasonData any) *Rule {
	if len(packages) == 2 {
		return g.rule2Literals(-packageID(packages[0]), -packageID(packages[1]), reason, reasonData)
	}
	literals := g.alloc.lits(len(packages))
	for i, p := range packages {
		literals[i] = -packageID(p)
	}
	slices.Sort(literals)

	return g.alloc.newRule(kindMultiConflict, literals, reason, reasonData)
}

func (g *RuleSetGenerator) addRule(typ int, newRule *Rule) {
	if newRule == nil {
		return
	}
	// the types used here are always valid
	_ = g.rules.Add(newRule, typ)
}

func (g *RuleSetGenerator) isAdded(p pkg.PackageInterface) bool {
	id := p.ID()
	if id < 0 {
		return slices.Contains(g.addedNegative, id)
	}

	return id < len(g.addedMap) && g.addedMap[id]
}

// markAdded sets $addedMap[$package->id]: a package that is not in the
// pool keeps a stale id from another one, which PHP uses all the same.
func (g *RuleSetGenerator) markAdded(p pkg.PackageInterface) {
	id := p.ID()
	if id < 0 {
		g.addedNegative = append(g.addedNegative, id)

		return
	}
	if id >= len(g.addedMap) {
		g.addedMap = append(g.addedMap, make([]bool, id+1-len(g.addedMap))...)
	}
	g.addedMap[id] = true
}

// addRulesForPackage ports addRulesForPackage.
func (g *RuleSetGenerator) addRulesForPackage(p pkg.PackageInterface, filter version.PlatformRequirementFilter) {
	ignoreList, _ := filter.(version.IgnoreListPlatformRequirementFilter)

	g.queue = append(g.queue[:0], p)
	for head := 0; head < len(g.queue); head++ {
		p := g.queue[head]
		if g.isAdded(p) {
			continue
		}
		g.markAdded(p)
		g.addedList = append(g.addedList, p)

		if alias, ok := p.(pkg.Alias); !ok {
			for _, name := range p.Names(false) {
				if _, seen := g.addedPackagesByNames[name]; !seen {
					g.addedNames = append(g.addedNames, name)
				}
				g.addedPackagesByNames[name] = append(g.addedPackagesByNames[name], p)
			}
		} else {
			aliasOf := alias.AliasOf()
			g.queue = append(g.queue, aliasOf)
			g.addRule(TypePackage, g.createRequireRule(p, []pkg.PackageInterface{aliasOf}, RulePackageAlias, p))

			// aliases must be installed with their main package, so create a rule the other way around as well
			g.addRule(TypePackage, g.createRequireRule(aliasOf, []pkg.PackageInterface{p}, RulePackageInverseAlias, aliasOf))

			// if alias package has no self.version requires, its requirements do not
			// need to be added as the aliased package processing will take care of it
			if !alias.HasSelfVersionRequires() {
				continue
			}
		}

		for link := range p.Requires().Values() {
			constraint := link.Constraint()
			if filter.IsIgnored(link.Target()) {
				continue
			} else if ignoreList != nil {
				constraint = ignoreList.FilterConstraint(link.Target(), constraint)
			}

			possibleRequires := g.pool.WhatProvides(link.Target(), constraint)

			g.addRule(TypePackage, g.createRequireRule(p, possibleRequires, RulePackageRequires, link))

			g.queue = append(g.queue, possibleRequires...)
		}
	}
}

// addConflictRules ports addConflictRules.
func (g *RuleSetGenerator) addConflictRules(filter version.PlatformRequirementFilter) {
	for _, p := range g.addedList {
		for link := range p.Conflicts().Values() {
			// even if conflict ends up being with an alias, there would be at least one actual package by this name
			if _, ok := g.addedPackagesByNames[link.Target()]; !ok {
				continue
			}

			if filter.IsIgnored(link.Target()) {
				continue
			}
			// IgnoreListPlatformRequirementFilter::filterConstraint($target,
			// $constraint, false) returns the constraint unchanged
			conflicts := g.pool.WhatProvides(link.Target(), link.Constraint())

			for _, conflict := range conflicts {
				// define the conflict rule for regular packages, for alias packages it's only needed if the name
				// matches the conflict exactly, otherwise the name match is by provide/replace which means the
				// package which this is an alias of will conflict anyway, so no need to create additional rules
				if _, isAlias := conflict.(pkg.Alias); !isAlias || conflict.Name() == link.Target() {
					g.addRule(TypePackage, g.createRule2Literals(p, conflict, RulePackageConflict, link))
				}
			}
		}
	}

	for _, name := range g.addedNames {
		if packages := g.addedPackagesByNames[name]; len(packages) > 1 {
			g.addRule(TypePackage, g.createMultiConflictRule(packages, RulePackageSameName, name))
		}
	}
}

// addRulesForRequest ports addRulesForRequest.
func (g *RuleSetGenerator) addRulesForRequest(request *Request, filter version.PlatformRequirementFilter) error {
	for _, p := range request.FixedPackages() {
		// Locked package was dropped from this pool by a policy filter list
		// (e.g. malware). Solver::checkForFilterListRemovedLockedPackages
		// surfaces a SolverProblemsException for it; here we simply skip
		// adding rules for the now-missing pool member.
		//
		// We must dispatch on the filter-list-removed map directly rather
		// than `$package->id === -1` because the package object retains a
		// stale positive id from any prior Pool it was part of (Pool only
		// re-assigns ids for packages it actually contains, so removed
		// entries keep their previous id).
		if request.IsLockedPackage(p) && g.pool.IsFilterListRemovedPackageVersion(p.Name(), semver.NewConstraintOp(semver.OpEQ, p.Version())) {
			continue
		}

		if p.ID() == -1 {
			// fixed package was not added to the pool as it did not pass the stability requirements, this is fine
			if g.pool.IsUnacceptableFixedOrLockedPackage(p) {
				continue
			}

			// otherwise, looks like a bug
			return &util.LogicError{Message: "Fixed package " + p.PrettyString() + " was not added to solver pool."}
		}

		g.addRulesForPackage(p, filter)

		g.addRule(TypeRequest, g.createInstallOneOfRule([]pkg.PackageInterface{p}, RuleFixed, p))
	}

	ignoreList, _ := filter.(version.IgnoreListPlatformRequirementFilter)
	for packageName, constraint := range request.Requires().All() {
		if filter.IsIgnored(packageName) {
			continue
		} else if ignoreList != nil {
			constraint = ignoreList.FilterConstraint(packageName, constraint)
		}

		packages := g.pool.WhatProvides(packageName, constraint)
		if len(packages) > 0 {
			for _, p := range packages {
				g.addRulesForPackage(p, filter)
			}

			g.addRule(TypeRequest, g.createInstallOneOfRule(packages, RuleRootRequire, &RootRequire{PackageName: packageName, Constraint: constraint}))
		}
	}

	return nil
}

// addRulesForRootAliases ports addRulesForRootAliases.
func (g *RuleSetGenerator) addRulesForRootAliases(filter version.PlatformRequirementFilter) {
	for _, p := range g.pool.Packages() {
		// ensure that rules for root alias packages and aliases of packages which were loaded are also loaded
		// even if the alias itself isn't required, otherwise a package could be installed without its alias which
		// leads to unexpected behavior
		if alias, ok := p.(pkg.Alias); ok && !g.isAdded(p) && (alias.IsRootPackageAlias() || g.isAdded(alias.AliasOf())) {
			g.addRulesForPackage(p, filter)
		}
	}
}

// RulesFor ports getRulesFor; filter nil ignores nothing.
func (g *RuleSetGenerator) RulesFor(request *Request, filter version.PlatformRequirementFilter) (*RuleSet, error) {
	if filter == nil {
		filter = ignoreNothing{}
	}
	g.addedMap = make([]bool, g.pool.Count()+1)
	g.addedPackagesByNames = make(map[string][]pkg.PackageInterface)

	if err := g.addRulesForRequest(request, filter); err != nil {
		return nil, err
	}
	g.addRulesForRootAliases(filter)
	g.addConflictRules(filter)

	// Remove references to packages
	g.addedMap, g.addedNegative, g.addedList, g.addedPackagesByNames, g.addedNames, g.queue = nil, nil, nil, nil, nil, nil
	rules := g.rules
	g.rules = NewRuleSet()

	return rules, nil
}
