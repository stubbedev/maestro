// Ports src/Composer/Policy/AbandonedPolicyConfig.php.

package policy

import "github.com/stubbedev/maestro/internal/php"

// AbandonedName is AbandonedPolicyConfig::NAME.
const AbandonedName = "abandoned"

// AbandonedPolicyConfig is AbandonedPolicyConfig.
type AbandonedPolicyConfig struct {
	ListPolicy
}

// NewAbandonedPolicyConfig is new AbandonedPolicyConfig(...).
func NewAbandonedPolicyConfig(block bool, audit string, ignore *IgnoreMap) *AbandonedPolicyConfig {
	return &AbandonedPolicyConfig{ListPolicy{Name: AbandonedName, Block: block, Audit: audit, Ignore: nonNil(ignore)}}
}

func nonNil(ignore *IgnoreMap) *IgnoreMap {
	if ignore == nil {
		return &IgnoreMap{}
	}

	return ignore
}

// ShouldBlock ports shouldBlock.
func (c *AbandonedPolicyConfig) ShouldBlock(blockScope string) bool {
	return c.shouldBlock(blockScope, false)
}

// WithBlockingDisabled ports withBlockingDisabled.
func (c *AbandonedPolicyConfig) WithBlockingDisabled() *AbandonedPolicyConfig {
	return NewAbandonedPolicyConfig(false, c.Audit, c.Ignore)
}

// WithAudit ports withAudit.
func (c *AbandonedPolicyConfig) WithAudit(audit string) *AbandonedPolicyConfig {
	return NewAbandonedPolicyConfig(c.Block, audit, c.Ignore)
}

// AbandonedPolicyConfigFromRawConfig ports
// AbandonedPolicyConfig::fromRawConfig.
func AbandonedPolicyConfigFromRawConfig(policyConfig, auditConfig *php.Array) (*AbandonedPolicyConfig, error) {
	if _, ok := coalesce(policyConfig, AbandonedName); !ok && !isEmpty(auditConfig) {
		ignore, err := parseLegacyIgnoreWithApply(arrayOf(auditConfig, "ignore-abandoned"))
		if err != nil {
			return nil, err
		}

		return NewAbandonedPolicyConfig(
			boolOr(auditConfig, "block-abandoned", false),
			stringOr(auditConfig, "abandoned", AuditFail),
			ignore,
		), nil
	}

	raw, _ := coalesce(policyConfig, AbandonedName)
	if raw == false {
		return AbandonedPolicyConfigDisabled(), nil
	}
	abandonedConfig, _ := raw.(*php.Array)

	ignore, err := ParseIgnoreMap(arrayOf(abandonedConfig, "ignore"))
	if err != nil {
		return nil, err
	}

	return NewAbandonedPolicyConfig(
		boolOr(abandonedConfig, "block", false),
		stringOr(abandonedConfig, "audit", AuditFail),
		ignore,
	), nil
}

// AbandonedPolicyConfigDisabled is AbandonedPolicyConfig::disabled().
func AbandonedPolicyConfigDisabled() *AbandonedPolicyConfig {
	return NewAbandonedPolicyConfig(false, AuditIgnore, nil)
}
