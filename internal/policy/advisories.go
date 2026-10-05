// Ports src/Composer/Policy/AdvisoriesPolicyConfig.php.

package policy

import "github.com/stubbedev/maestro/internal/php"

// AdvisoriesName is AdvisoriesPolicyConfig::NAME.
const AdvisoriesName = "advisories"

// AdvisoriesPolicyConfig is AdvisoriesPolicyConfig: security advisories,
// which can also be ignored by advisory ID and by severity.
type AdvisoriesPolicyConfig struct {
	ListPolicy
	IgnoreID       *OrderedMap[*IgnoreIDRule]
	IgnoreSeverity *OrderedMap[*IgnoreSeverityRule]
}

// NewAdvisoriesPolicyConfig is new AdvisoriesPolicyConfig(...).
func NewAdvisoriesPolicyConfig(block bool, audit string, ignore *IgnoreMap, ignoreID *OrderedMap[*IgnoreIDRule], ignoreSeverity *OrderedMap[*IgnoreSeverityRule]) *AdvisoriesPolicyConfig {
	if ignoreID == nil {
		ignoreID = &OrderedMap[*IgnoreIDRule]{}
	}
	if ignoreSeverity == nil {
		ignoreSeverity = &OrderedMap[*IgnoreSeverityRule]{}
	}

	return &AdvisoriesPolicyConfig{
		Name: AdvisoriesName, Block: block, Audit: audit, Ignore: nonNil(ignore),
		IgnoreID:       ignoreID,
		IgnoreSeverity: ignoreSeverity,
	}
}

// ShouldBlock ports shouldBlock.
func (c *AdvisoriesPolicyConfig) ShouldBlock(blockScope string) bool {
	return c.shouldBlock(blockScope, false)
}

// IgnoreIDForOperation ports getIgnoreIdForOperation: the advisory IDs
// ignored for operation ("block" or "audit") with their reasons.
func (c *AdvisoriesPolicyConfig) IgnoreIDForOperation(operation string) *Reasons {
	result := &Reasons{}
	for id, rule := range c.IgnoreID.All() {
		if appliesTo(operation, rule.OnBlock, rule.OnAudit) {
			result.Set(id, rule.Reason)
		}
	}

	return result
}

// IgnoreListForOperation ports getIgnoreListForOperation: the ignored
// advisory IDs, then the ignored package names, with merged reasons.
func (c *AdvisoriesPolicyConfig) IgnoreListForOperation(operation string) *Reasons {
	result := c.IgnoreIDForOperation(operation)

	for packageName, reason := range c.FlatIgnoreForOperation(operation).All() {
		result.Set(packageName, mergeReason(result, packageName, reason))
	}

	return result
}

// IgnoreSeverityForOperation ports getIgnoreSeverityForOperation.
func (c *AdvisoriesPolicyConfig) IgnoreSeverityForOperation(operation string) *Reasons {
	result := &Reasons{}
	for severity, rule := range c.IgnoreSeverity.All() {
		if appliesTo(operation, rule.OnBlock, rule.OnAudit) {
			result.Set(severity, rule.Reason)
		}
	}

	return result
}

// WithBlockingDisabled ports withBlockingDisabled.
func (c *AdvisoriesPolicyConfig) WithBlockingDisabled() *AdvisoriesPolicyConfig {
	return NewAdvisoriesPolicyConfig(false, c.Audit, c.Ignore, c.IgnoreID, c.IgnoreSeverity)
}

// WithAudit ports withAudit.
func (c *AdvisoriesPolicyConfig) WithAudit(audit string) *AdvisoriesPolicyConfig {
	return NewAdvisoriesPolicyConfig(c.Block, audit, c.Ignore, c.IgnoreID, c.IgnoreSeverity)
}

// WithIgnoreSeverity ports withIgnoreSeverity: severities not ignored yet
// are added as audit-only rules.
func (c *AdvisoriesPolicyConfig) WithIgnoreSeverity(severities []string) *AdvisoriesPolicyConfig {
	ignoreSeverity := c.IgnoreSeverity.Clone()
	for _, severity := range severities {
		if !ignoreSeverity.Has(severity) {
			ignoreSeverity.Set(severity, NewIgnoreSeverityRule(severity, nil, false, true))
		}
	}

	return NewAdvisoriesPolicyConfig(c.Block, c.Audit, c.Ignore, c.IgnoreID, ignoreSeverity)
}

// AdvisoriesPolicyConfigFromRawConfig ports
// AdvisoriesPolicyConfig::fromRawConfig.
func AdvisoriesPolicyConfigFromRawConfig(policyConfig, auditConfig *php.Array) (*AdvisoriesPolicyConfig, error) {
	if _, ok := coalesce(policyConfig, AdvisoriesName); !ok && !isEmpty(auditConfig) {
		packages, ids, err := parseLegacyAuditIgnore(arrayOf(auditConfig, "ignore"))
		if err != nil {
			return nil, err
		}
		severities, err := parseLegacySeverityWithApply(arrayOf(auditConfig, "ignore-severity"))
		if err != nil {
			return nil, err
		}

		return NewAdvisoriesPolicyConfig(boolOr(auditConfig, "block-insecure", true), AuditFail, packages, ids, severities), nil
	}

	raw, _ := coalesce(policyConfig, AdvisoriesName)
	if raw == false {
		return AdvisoriesPolicyConfigDisabled(), nil
	}
	advisoryConfig, _ := raw.(*php.Array)

	ignore, err := ParseIgnoreMap(arrayOf(advisoryConfig, "ignore"))
	if err != nil {
		return nil, err
	}
	ignoreID, err := ParseIgnoreIDMap(arrayOf(advisoryConfig, "ignore-id"))
	if err != nil {
		return nil, err
	}
	ignoreSeverity, err := ParseIgnoreSeverityMap(arrayOf(advisoryConfig, "ignore-severity"))
	if err != nil {
		return nil, err
	}

	return NewAdvisoriesPolicyConfig(
		boolOr(advisoryConfig, "block", true),
		stringOr(advisoryConfig, "audit", AuditFail),
		ignore, ignoreID, ignoreSeverity,
	), nil
}

// AdvisoriesPolicyConfigDisabled is AdvisoriesPolicyConfig::disabled().
func AdvisoriesPolicyConfigDisabled() *AdvisoriesPolicyConfig {
	return NewAdvisoriesPolicyConfig(false, AuditIgnore, nil, nil, nil)
}

// parseLegacySeverityWithApply ports
// AdvisoriesPolicyConfig::parseLegacySeverityWithApply.
func parseLegacySeverityWithApply(config *php.Array) (*OrderedMap[*IgnoreSeverityRule], error) {
	result := &OrderedMap[*IgnoreSeverityRule]{}
	for k, value := range each(config) {
		severity := legacyKey(k, value)
		parsed, err := parseLegacySingleIgnore(k, value)
		if err != nil {
			return nil, err
		}
		result.Set(severity, NewIgnoreSeverityRule(severity, parsed.reason, parsed.onBlock, parsed.onAudit))
	}

	return result, nil
}
