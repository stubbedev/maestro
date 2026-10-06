// Ports src/Composer/DependencyResolver/PolicyInterface.php and
// DefaultPolicy.php.

package resolver

import (
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
)

// Policy ports Composer\DependencyResolver\PolicyInterface.
type Policy interface {
	// VersionCompare ports versionCompare: a's version compared with
	// operator to b's.
	VersionCompare(a, b pkg.PackageInterface, operator string) bool
	// SelectPreferredPackages ports selectPreferredPackages: the literals
	// to try, best first. requiredPackage "" is null. The result must not
	// be modified.
	SelectPreferredPackages(pool *Pool, literals []int32, requiredPackage string) []int32
}

// DefaultPolicy ports Composer\DependencyResolver\DefaultPolicy.
//
// Its caches are per pool, as in Composer (which keys them by the pool's
// spl_object_id; PHP can reuse the id of a freed pool for a later one,
// which these caches do not imitate).
type DefaultPolicy struct {
	preferStable            bool
	preferLowest            bool
	preferDevOverPrerelease bool
	preferredVersions       map[string]string
	caches                  map[*Pool]*policyCache
}

type policyCache struct {
	// preferred is $preferredPackageResultCachePerPool[$poolId].
	preferred map[string][]int32
	// sorting is $sortingCachePerPool[$poolId]; sortingByKey holds the
	// entries whose PHP key is ambiguous (see sortKey).
	sorting      map[sortKey]int8
	sortingByKey map[string]int8
}

// sortKey identifies a compareByPriority call. PHP keys the cache with
// ('i').$a.'.'.$b.$requiredPackage; that string is unique unless the
// required package name starts with a digit, in which case the exact PHP
// key is used.
type sortKey struct {
	a, b            int32
	ignoreReplace   bool
	requiredPackage string
}

// NewDefaultPolicy is new DefaultPolicy($preferStable, $preferLowest,
// $preferredVersions): preferredVersions maps package names to normalized
// versions (nil: none). COMPOSER_PREFER_DEV_OVER_PRERELEASE is read here.
func NewDefaultPolicy(preferStable, preferLowest bool, preferredVersions map[string]string) *DefaultPolicy {
	return &DefaultPolicy{
		preferStable:            preferStable,
		preferLowest:            preferLowest,
		preferredVersions:       preferredVersions,
		preferDevOverPrerelease: php.ToBool(os.Getenv("COMPOSER_PREFER_DEV_OVER_PRERELEASE")),
		caches:                  map[*Pool]*policyCache{},
	}
}

// Fork returns a policy of the same configuration with caches of its own,
// for use on another goroutine. Its results are the policy's: the caches
// only remember them.
func (p *DefaultPolicy) Fork() Policy {
	fork := *p
	fork.caches = map[*Pool]*policyCache{}

	return &fork
}

// VersionCompare implements Policy.
func (p *DefaultPolicy) VersionCompare(a, b pkg.PackageInterface, operator string) bool {
	if p.preferStable {
		if stabA, stabB := a.Stability(), b.Stability(); stabA != stabB {
			if p.preferLowest && p.preferDevOverPrerelease && stabA != "stable" && stabB != "stable" {
				// When COMPOSER_PREFER_DEV_OVER_PRERELEASE is set and no stable version has been
				// released, "dev" should be considered more stable than "alpha", "beta" or "RC";
				// this allows testing lowest versions with potential fixes applied
				if stabA == "dev" {
					stabA = "stable"
				}
				if stabB == "dev" {
					stabB = "stable"
				}
			}
			valueA, _ := pkg.StabilityValue(stabA)
			valueB, _ := pkg.StabilityValue(stabB)

			return valueA < valueB
		}
	}

	// dev versions need to be compared as branches via matchSpecific's special treatment, the rest can be optimized with compiling matcher
	if (a.IsDev() && strings.HasPrefix(a.Version(), "dev-")) || (b.IsDev() && strings.HasPrefix(b.Version(), "dev-")) {
		op, _ := semver.OperatorConstant(operator)
		constraint := semver.NewConstraintOp(op, b.Version())
		version := semver.NewConstraintOp(semver.OpEQ, a.Version())

		return constraint.MatchSpecific(version, true)
	}

	// CompilingMatcher::match(new Constraint($operator, $b), OP_EQ, $a)
	// compiles to version_compare($a, $b, $operator) for versions that are
	// not branches
	result, _ := semver.VersionCompareOp(a.Version(), b.Version(), operator)

	return result
}

func (p *DefaultPolicy) cache(pool *Pool) *policyCache {
	c, ok := p.caches[pool]
	if !ok {
		c = &policyCache{preferred: map[string][]int32{}, sorting: map[sortKey]int8{}, sortingByKey: map[string]int8{}}
		p.caches[pool] = c
	}

	return c
}

// SelectPreferredPackages implements Policy.
func (p *DefaultPolicy) SelectPreferredPackages(pool *Pool, literals []int32, requiredPackage string) []int32 {
	literals = slices.Clone(literals)
	slices.Sort(literals)

	var key strings.Builder
	for i, l := range literals {
		if i > 0 {
			key.WriteByte(',')
		}
		key.WriteString(strconv.Itoa(int(l)))
	}
	key.WriteString(requiredPackage)
	resultCacheKey := key.String()

	cache := p.cache(pool)
	if cached, ok := cache.preferred[resultCacheKey]; ok {
		return cached
	}

	packages := p.groupLiteralsByName(pool, literals)

	for _, nameLiterals := range packages {
		php.SortSlice(nameLiterals.literals, func(a, b int32) int {
			return p.cachedCompare(cache, pool, a, b, requiredPackage, true)
		})
	}

	var selected []int32
	for _, sortedLiterals := range packages {
		best := p.pruneToBestVersion(pool, sortedLiterals.literals)
		selected = append(selected, p.pruneRemoteAliases(pool, best)...)
	}

	// now sort the result across all packages to respect replaces across packages
	php.SortSlice(selected, func(a, b int32) int {
		return p.cachedCompare(cache, pool, a, b, requiredPackage, false)
	})

	if selected == nil {
		selected = []int32{}
	}
	cache.preferred[resultCacheKey] = selected

	return selected
}

// cachedCompare is selectPreferredPackages' cached compareByPriority.
func (p *DefaultPolicy) cachedCompare(cache *policyCache, pool *Pool, a, b int32, requiredPackage string, ignoreReplace bool) int {
	if requiredPackage != "" && requiredPackage[0] >= '0' && requiredPackage[0] <= '9' {
		key := strconv.Itoa(int(a)) + "." + strconv.Itoa(int(b)) + requiredPackage
		if ignoreReplace {
			key = "i" + key
		}
		if r, ok := cache.sortingByKey[key]; ok {
			return int(r)
		}
		r := p.CompareByPriority(pool, pool.LiteralToPackage(a), pool.LiteralToPackage(b), requiredPackage, ignoreReplace)
		cache.sortingByKey[key] = sign8(r)

		return r
	}

	k := sortKey{a: a, b: b, ignoreReplace: ignoreReplace, requiredPackage: requiredPackage}
	if r, ok := cache.sorting[k]; ok {
		return int(r)
	}
	r := p.CompareByPriority(pool, pool.LiteralToPackage(a), pool.LiteralToPackage(b), requiredPackage, ignoreReplace)
	cache.sorting[k] = sign8(r)

	return r
}

// sign8 stores a comparison result (-1, 0 or 1).
func sign8(r int) int8 {
	switch {
	case r < 0:
		return -1
	case r > 0:
		return 1
	}

	return 0
}

type nameLiterals struct {
	name     string
	literals []int32
}

// groupLiteralsByName ports groupLiteralsByName.
func (p *DefaultPolicy) groupLiteralsByName(pool *Pool, literals []int32) []*nameLiterals {
	var packages []*nameLiterals
	index := map[string]*nameLiterals{}
	for _, literal := range literals {
		packageName := pool.LiteralToPackage(literal).Name()
		group, ok := index[packageName]
		if !ok {
			group = &nameLiterals{name: packageName}
			index[packageName] = group
			packages = append(packages, group)
		}
		group.literals = append(group.literals, literal)
	}

	return packages
}

// CompareByPriority ports compareByPriority.
func (p *DefaultPolicy) CompareByPriority(_ *Pool, a, b pkg.PackageInterface, requiredPackage string, ignoreReplace bool) int {
	// prefer aliases to the original package
	if a.Name() == b.Name() {
		_, aAliased := a.(pkg.Alias)
		_, bAliased := b.(pkg.Alias)
		if aAliased && !bAliased {
			return -1 // use a
		}
		if !aAliased && bAliased {
			return 1 // use b
		}
	}

	if !ignoreReplace {
		// return original, not replaced
		if p.replaces(a, b) {
			return 1 // use b
		}
		if p.replaces(b, a) {
			return -1 // use a
		}

		// for replacers not replacing each other, put a higher prio on replacing
		// packages with the same vendor as the required package
		if requiredVendor, _, found := strings.Cut(requiredPackage, "/"); found {
			aIsSameVendor := strings.HasPrefix(a.Name(), requiredVendor)
			bIsSameVendor := strings.HasPrefix(b.Name(), requiredVendor)

			if bIsSameVendor != aIsSameVendor {
				if aIsSameVendor {
					return -1
				}

				return 1
			}
		}
	}

	// priority equal, sort by package id to make reproducible
	switch {
	case a.ID() == b.ID():
		return 0
	case a.ID() < b.ID():
		return -1
	}

	return 1
}

// replaces ports replaces: whether source replaces target's name.
func (p *DefaultPolicy) replaces(source, target pkg.PackageInterface) bool {
	for link := range source.Replaces().Values() {
		if link.Target() == target.Name() {
			return true
		}
	}

	return false
}

// pruneToBestVersion ports pruneToBestVersion.
func (p *DefaultPolicy) pruneToBestVersion(pool *Pool, literals []int32) []int32 {
	if p.preferredVersions != nil {
		name := pool.LiteralToPackage(literals[0]).Name()
		if preferredVersion, ok := p.preferredVersions[name]; ok {
			var bestLiterals []int32
			for _, literal := range literals {
				if pool.LiteralToPackage(literal).Version() == preferredVersion {
					bestLiterals = append(bestLiterals, literal)
				}
			}
			if len(bestLiterals) > 0 {
				return bestLiterals
			}
		}
	}

	operator := ">"
	if p.preferLowest {
		operator = "<"
	}
	bestLiterals := []int32{literals[0]}
	bestPackage := pool.LiteralToPackage(literals[0])
	for _, literal := range literals[1:] {
		candidate := pool.LiteralToPackage(literal)

		if p.VersionCompare(candidate, bestPackage, operator) {
			bestPackage = candidate
			bestLiterals = []int32{literal}
		} else if p.VersionCompare(candidate, bestPackage, "==") {
			bestLiterals = append(bestLiterals, literal)
		}
	}

	return bestLiterals
}

// pruneRemoteAliases ports pruneRemoteAliases: only root aliases are kept
// when there are any.
func (p *DefaultPolicy) pruneRemoteAliases(pool *Pool, literals []int32) []int32 {
	isRootAlias := func(literal int32) bool {
		a, ok := pool.LiteralToPackage(literal).(pkg.Alias)

		return ok && a.IsRootPackageAlias()
	}
	if !slices.ContainsFunc(literals, isRootAlias) {
		return literals
	}

	var selected []int32
	for _, literal := range literals {
		if isRootAlias(literal) {
			selected = append(selected, literal)
		}
	}

	return selected
}
