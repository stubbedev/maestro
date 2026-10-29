// Ports src/Composer/Advisory/CooldownAuditor.php.

package advisory

import (
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
)

// CooldownAuditor ports Composer\Advisory\CooldownAuditor: it reports the
// installed/locked package versions whose publication is still within the
// configured cooldown period.
type CooldownAuditor struct {
	// Now is what the cooldown is measured against; its zero value is the
	// current time, as new CooldownAuditor() takes it.
	Now time.Time
}

// Collect ports collect: the versions still within the cooldown, as a
// name => {prettyVersion, releaseDate, availableIn, source} map.
func (a *CooldownAuditor) Collect(packages []pkg.PackageInterface, cooldown *policy.CooldownPolicyConfig) *php.Array {
	result := php.NewArray()
	if !cooldown.HasCooldown() {
		return result
	}

	now := a.Now
	if now.IsZero() {
		now = time.Now()
	}

	for _, packageInterface := range packages {
		// Decide on the aliased package, as the block path does
		if alias, ok := packageInterface.(pkg.Alias); ok {
			packageInterface = alias.AliasOf()
		}

		// Mirror the block path's skips: root, platform, dev, ignored, and
		// versions without a verifiable publication date.
		if _, ok := packageInterface.(pkg.RootPackageInterface); ok {
			continue
		}
		if repository.IsPlatformPackage(packageInterface.Name()) || packageInterface.IsDev() || cooldown.IsIgnored(packageInterface, "audit") {
			continue
		}

		releaseDate, ok := cooldown.EffectiveDate(packageInterface)
		if !ok || !cooldown.IsWithinCooldown(releaseDate, now) {
			continue
		}

		source := "time"
		if _, ok := packageInterface.PublishedDate(); ok {
			source = "published-time"
		}

		result.Set(packageInterface.Name(), php.ArrayOf(
			"prettyVersion", packageInterface.PrettyVersion(),
			"releaseDate", releaseDate.Format(dumper.RFC3339),
			"availableIn", cooldown.FormatTimeUntilAvailable(releaseDate, now, false),
			"source", source,
		))
	}

	return result
}
