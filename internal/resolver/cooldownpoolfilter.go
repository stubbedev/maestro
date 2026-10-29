// Ports src/Composer/DependencyResolver/CooldownPoolFilter.php.

package resolver

import (
	"time"

	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
)

// CooldownPoolFilter ports Composer\DependencyResolver\CooldownPoolFilter:
// it withholds package versions published more recently than the configured
// cooldown period.
type CooldownPoolFilter struct {
	config *policy.CooldownPolicyConfig
	now    time.Time

	effectiveDateCache map[pkg.PackageInterface]effectiveDate
	// unverifiedVersions is repository name => "name (version)" of versions
	// judged without a repository-provided publication date.
	unverifiedVersions *repository.NameMap[[]string]
}

type effectiveDate struct {
	date time.Time
	ok   bool
}

// NewCooldownPoolFilter is new CooldownPoolFilter($config, $now): now
// defaults to the current time.
func NewCooldownPoolFilter(config *policy.CooldownPolicyConfig, now ...time.Time) *CooldownPoolFilter {
	current := time.Now()
	if len(now) > 0 {
		current = now[0]
	}

	return &CooldownPoolFilter{config: config, now: current}
}

// Filter ports filter: it removes the packages that have not yet cleared the
// cooldown.
func (f *CooldownPoolFilter) Filter(pool *Pool, request *Request) (*Pool, error) {
	if !f.config.HasCooldown() || !f.config.Block {
		return pool, nil
	}

	f.effectiveDateCache = map[pkg.PackageInterface]effectiveDate{}
	f.unverifiedVersions = &repository.NameMap[[]string]{}

	var packages []pkg.PackageInterface
	policyRemovedVersions := pool.AllPolicyRemovedVersions()
	if policyRemovedVersions == nil {
		policyRemovedVersions = &repository.NameMap[*repository.NameMap[*PolicyRemovalReason]]{}
	}

	for _, pk := range pool.Packages() {
		// An alias shares the release date of the package it aliases but
		// reports its own stability, so decide on the aliased package to keep
		// the pair consistent
		target := pk
		if a, ok := pk.(pkg.Alias); ok {
			target = a.AliasOf()
		}

		// Skip filtering for packages that should always be allowed through:
		// 1. Root packages
		// 2. Platform packages (php, ext-*, lib-*, etc.)
		// 3. Locked packages kept by a partial update
		// 4. Dev versions (mutable, no stable release date concept)
		// 5. Ignored packages (matching configured policy.cooldown.ignore rules)
		_, isRoot := target.(pkg.RootPackageInterface)
		if isRoot ||
			repository.IsPlatformPackage(target.Name()) ||
			request.IsLockedPackage(pk) ||
			request.IsLockedPackage(target) ||
			target.IsDev() ||
			f.config.IsIgnored(target, "block") {
			packages = append(packages, pk)

			continue
		}

		// A Composer repository has to vouch for the publication date, see the
		// policy config
		if err := f.config.AssertPublishedTimeProvided(target); err != nil {
			return nil, err
		}

		// Nothing vouches for the date of this version, keep track so the user
		// learns the protection is weaker
		if _, ok := target.PublishedDate(); !ok {
			if _, isAlias := pk.(pkg.Alias); !isAlias && target.Repository() != nil {
				repoName := target.Repository().RepoName()
				versions, _ := f.unverifiedVersions.Get(repoName)
				f.unverifiedVersions.Set(repoName, append(versions, pk.PrettyName()+" ("+pk.PrettyVersion()+")"))
			}
		}

		// 6. Packages without release date (conservative - don't block
		// unverifiable)
		releaseDate, ok := f.effectiveDate(target)
		if !ok {
			packages = append(packages, pk)

			continue
		}

		// Check if package is old enough
		if !f.config.IsWithinCooldown(releaseDate, f.now) {
			packages = append(packages, pk)

			continue
		}

		// Package is too new - filter it out and track for error messages
		source := "time"
		if _, ok := target.PublishedDate(); ok {
			source = "published-time"
		}
		RecordPolicyRemoval(policyRemovedVersions, pk, NewCooldownRemovalReason(
			pk.Name(),
			pk.PrettyVersion(),
			releaseDate.Format(dumper.RFC3339),
			f.config.FormatTimeUntilAvailable(releaseDate, f.now, false),
			source,
		))
	}

	return NewPool(packages, pool.UnacceptableFixedOrLockedPackages(), &Removed{
		Versions:  pool.AllRemovedVersions(),
		ByPackage: pool.AllRemovedVersionsByPackage(),
		Policy:    policyRemovedVersions,
	}), nil
}

// UnverifiedVersions ports getUnverifiedVersions: the versions the last
// Filter call had to judge by the package-supplied time field, or by no date
// at all, grouped by the name of the repository they came from.
func (f *CooldownPoolFilter) UnverifiedVersions() *repository.NameMap[[]string] {
	return f.unverifiedVersions
}

// effectiveDate ports effectiveDate: the package's effective date, resolved
// once per Filter call.
func (f *CooldownPoolFilter) effectiveDate(p pkg.PackageInterface) (time.Time, bool) {
	if cached, ok := f.effectiveDateCache[p]; ok {
		return cached.date, cached.ok
	}

	date, ok := f.config.EffectiveDate(p)
	f.effectiveDateCache[p] = effectiveDate{date: date, ok: ok}

	return date, ok
}
