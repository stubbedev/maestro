// Ports src/Composer/Policy/CooldownPolicyConfig.php.

package policy

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// CooldownPolicyConfig is what Composer\Policy\CooldownPolicyConfig is to
// the VersionSelector and the cooldown pool filter.
var _ version.Cooldown = (*CooldownPolicyConfig)(nil)

// CooldownName is CooldownPolicyConfig::NAME.
const CooldownName = "cooldown"

// CooldownPolicyConfig is CooldownPolicyConfig: the cooldown policy, which
// withholds versions published more recently than Period.
// *CooldownPolicyConfig implements version.Cooldown.
type CooldownPolicyConfig struct {
	ListPolicy
	// Period is the cooldown duration in seconds; nil disables it.
	Period *int64
}

// NewCooldownPolicyConfig is new CooldownPolicyConfig(...).
func NewCooldownPolicyConfig(block bool, audit string, ignore *IgnoreMap, period *int64) *CooldownPolicyConfig {
	return &CooldownPolicyConfig{ListPolicy{Name: CooldownName, Block: block, Audit: audit, Ignore: nonNil(ignore)}, periodOf(period)}
}

func periodOf(period *int64) *int64 {
	if period == nil {
		return nil
	}

	p := *period

	return &p
}

// HasCooldown is hasCooldown: whether a cooldown duration is configured.
func (c *CooldownPolicyConfig) HasCooldown() bool {
	return c.Period != nil && *c.Period > 0
}

// ShouldBlock ports shouldBlock: the policy is inert without a duration.
func (c *CooldownPolicyConfig) ShouldBlock(blockScope string) bool {
	if !c.HasCooldown() {
		return false
	}

	return c.shouldBlock(blockScope, false)
}

// Blocks is the public $block.
func (c *CooldownPolicyConfig) Blocks() bool { return c.Block }

// EffectiveDate is getEffectiveDate: the published date, else the release
// date; false is null.
func (c *CooldownPolicyConfig) EffectiveDate(p pkg.PackageInterface) (time.Time, bool) {
	if published, ok := p.PublishedDate(); ok {
		return published, true
	}

	return p.ReleaseDate()
}

// AssertPublishedTimeProvided ports assertPublishedTimeProvided: a Composer
// repository that requires publication times makes a version without one an
// error, as the cooldown cannot tell whether it is new or old.
func (c *CooldownPolicyConfig) AssertPublishedTimeProvided(p pkg.PackageInterface) error {
	if _, ok := p.PublishedDate(); ok {
		return nil
	}

	repository := p.Repository()
	if repository == nil {
		return nil
	}
	requiresPublishedTime, ok := repository.(interface{ RequiresPublishedTime() bool })
	if !ok || !requiresPublishedTime.RequiresPublishedTime() {
		return nil
	}

	return &util.RuntimeError{Message: fmt.Sprintf(
		"%s %s from %s has no published-time, so the cooldown policy configured in \"policy.cooldown\" cannot tell whether it is new or old. "+
			"Make sure the repository provides publication times (run \"composer clear-cache\" if it only recently started to), "+
			"or set \"require-published-time\": false on that repository to fall back to the package-supplied \"time\" field, which offers weaker protection.",
		p.PrettyName(), p.PrettyVersion(), repository.RepoName())}
}

// IsIgnored ports isIgnored: whether p matches a policy.cooldown.ignore rule
// for operation ("block" or "audit").
func (c *CooldownPolicyConfig) IsIgnored(p pkg.PackageInterface, operation string) bool {
	ignore := c.IgnoreForOperation(operation)
	if ignore.Len() == 0 {
		return false
	}

	packageConstraint := semver.NewConstraintOp(semver.OpEQ, p.Version())
	for _, rules := range ignore.All() {
		for _, rule := range rules {
			// only the package's own name, so declaring a replace of an
			// ignored name cannot skip the cooldown
			matched, _ := php.PregIsMatch(rule.PackageNameRegex, p.Name())
			if matched && rule.Constraint.Matches(packageConstraint) {
				return true
			}
		}
	}

	return false
}

// IsWithinCooldown is isWithinCooldown: whether effectiveDate is still too
// recent to be allowed relative to now.
func (c *CooldownPolicyConfig) IsWithinCooldown(effectiveDate, now time.Time) bool {
	if !c.HasCooldown() {
		return false
	}

	cutoffDate := now.Add(-time.Duration(*c.Period) * time.Second)

	return effectiveDate.After(cutoffDate)
}

// FormatTimeUntilAvailable is formatTimeUntilAvailable: e.g. "3 days 4
// hours", or "3d 4h" when short.
func (c *CooldownPolicyConfig) FormatTimeUntilAvailable(effectiveDate, now time.Time, short bool) string {
	availableAt := effectiveDate.Add(time.Duration(*c.Period) * time.Second)
	diff := availableAt.Sub(now)
	if diff < 0 {
		diff = -diff
	}

	days := int(diff.Hours()) / 24
	hours := int(diff.Hours()) % 24
	minutes := int(diff.Minutes()) % 60

	var parts []string
	if days > 0 {
		parts = append(parts, durationPart(days, short, "d", " day"))
	}
	if hours > 0 {
		parts = append(parts, durationPart(hours, short, "h", " hour"))
	}
	if len(parts) == 0 && minutes > 0 {
		parts = append(parts, durationPart(minutes, short, "m", " minute"))
	}

	if result := strings.Join(parts, " "); result != "" {
		return result
	}

	if short {
		return "<1m"
	}

	return "less than a minute"
}

func durationPart(n int, short bool, shortUnit, longUnit string) string {
	if short {
		return strconv.Itoa(n) + shortUnit
	}

	if n > 1 {
		return strconv.Itoa(n) + longUnit + "s"
	}

	return strconv.Itoa(n) + longUnit
}

// WithBlockingDisabled ports withBlockingDisabled.
func (c *CooldownPolicyConfig) WithBlockingDisabled() *CooldownPolicyConfig {
	return NewCooldownPolicyConfig(false, c.Audit, c.Ignore, c.Period)
}

// WithAudit ports withAudit.
func (c *CooldownPolicyConfig) WithAudit(audit string) *CooldownPolicyConfig {
	return NewCooldownPolicyConfig(c.Block, audit, c.Ignore, c.Period)
}

// CooldownPolicyConfigFromRawConfig ports CooldownPolicyConfig::fromRawConfig.
func CooldownPolicyConfigFromRawConfig(policyConfig *php.Array) (*CooldownPolicyConfig, error) {
	cooldownConfig, _ := coalesce(policyConfig, CooldownName)
	envPeriod, envSet, err := getEnvPeriod()
	if err != nil {
		return nil, err
	}

	// Like the other policies' env overrides, the env var re-enables an
	// explicitly disabled policy
	if cooldownConfig == false {
		if !envSet || envPeriod == nil {
			return CooldownPolicyConfigDisabled(), nil
		}
		cooldownConfig = nil
	}

	raw, _ := cooldownConfig.(*php.Array)
	var rawPeriod any
	if v, ok := coalesce(raw, "period"); ok {
		rawPeriod = v
	}
	period, err := ParseDuration(rawPeriod)
	if err != nil {
		return nil, err
	}

	// Environment variable overrides the configured duration but preserves
	// the block/audit/ignore settings from config
	if envSet {
		period = envPeriod
	}

	ignore, err := ParseIgnoreMap(arrayOf(raw, "ignore"))
	if err != nil {
		return nil, err
	}

	return NewCooldownPolicyConfig(
		boolOr(raw, "block", true),
		stringOr(raw, "audit", AuditIgnore),
		ignore,
		period,
	), nil
}

// getEnvPeriod ports getEnvPeriod: the period from
// COMPOSER_POLICY_COOLDOWN_PERIOD, set=false when it is not set, and the
// parsed period (nil for "no cooldown") otherwise.
func getEnvPeriod() (period *int64, set bool, err error) {
	envValue, ok := util.GetEnv("COMPOSER_POLICY_COOLDOWN_PERIOD")
	if !ok || envValue == "" {
		return nil, false, nil
	}

	parsed, err := ParseDuration(envValue)
	if err != nil {
		return nil, true, &util.RuntimeError{
			Message: "Invalid value for COMPOSER_POLICY_COOLDOWN_PERIOD: " + envValue + ". " +
				"Use formats like '7 days', '24 hours', or an integer number of seconds.",
			Prev: err,
		}
	}

	return parsed, true, nil
}

// CooldownPolicyConfigDisabled is CooldownPolicyConfig::disabled().
func CooldownPolicyConfigDisabled() *CooldownPolicyConfig {
	return NewCooldownPolicyConfig(false, AuditIgnore, nil, nil)
}

// ParseDuration ports parseDuration: seconds from an integer, an integer
// string, or an explicit unit duration such as "7 days"; nil for null, ""
// and 0.
func ParseDuration(duration any) (*int64, error) {
	switch v := duration.(type) {
	case nil:
		return nil, nil
	case string:
		if v == "" || v == "0" {
			return nil, nil
		}
	case int64:
		if v == 0 {
			return nil, nil
		}
		if v < 0 {
			return nil, negativeDurationError(duration)
		}

		return &v, nil
	case int:
		return ParseDuration(int64(v))
	}

	// A plain integer is interpreted as a number of seconds.
	value := php.ToString(duration)
	if matched, err := digitsOnly.IsMatch(value); err != nil {
		return nil, err
	} else if matched {
		seconds := php.ToInt(duration)

		return &seconds, nil
	}

	// Reject any other numeric value (negative or fractional) with a clear
	// message.
	if php.IsNumeric(duration) {
		if php.ToFloat(duration) < 0 {
			return nil, negativeDurationError(duration)
		}

		return nil, invalidDurationFormatError(duration)
	}

	// Otherwise only explicit unit durations are accepted.
	if match, err := durationPattern.Match(value); err != nil {
		return nil, err
	} else if match != nil {
		seconds := php.ToInt(match.Get(1)) * durationUnits[match.Get(2)]

		return &seconds, nil
	}

	return nil, invalidDurationFormatError(duration)
}

func negativeDurationError(duration any) error {
	return &util.RuntimeError{Message: fmt.Sprintf(
		"Invalid policy.cooldown.period: duration cannot be negative (%s).", php.ToString(duration))}
}

func invalidDurationFormatError(duration any) error {
	return &util.RuntimeError{Message: fmt.Sprintf(
		"Invalid policy.cooldown.period format: %s. Use an integer number of seconds or a duration like '7 days', '24 hours', '30 minutes' or '1 week'.",
		php.ToString(duration))}
}

var (
	digitsOnly      = php.MustCompile(`{^\d+$}D`)
	durationPattern = php.MustCompile(`{^(\d+)\s*(second|minute|hour|day|week)s?$}D`)
)

var durationUnits = map[string]int64{
	"second": 1,
	"minute": 60,
	"hour":   3600,
	"day":    86400,
	"week":   604800,
}
