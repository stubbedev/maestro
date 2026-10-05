// Ports src/Composer/Policy/ListPolicyConfig.php.

package policy

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// ListPolicyConfig is the abstract ListPolicyConfig: the configuration of
// one policy list. Its implementations are *AdvisoriesPolicyConfig,
// *MalwarePolicyConfig, *AbandonedPolicyConfig and *CustomListPolicyConfig.
type ListPolicyConfig interface {
	// List returns the fields and methods all lists share.
	List() *ListPolicy
	// ShouldBlock ports shouldBlock: whether blocking applies to the
	// given BlockScope*.
	ShouldBlock(blockScope string) bool
}

// ListPolicy holds the fields of ListPolicyConfig.
type ListPolicy struct {
	Name string
	// Block reports whether the list blocks matching versions during
	// update/require, and during install for lists supporting it.
	Block bool
	// Audit is one of the Audit* values: how composer audit treats
	// matches from this list.
	Audit string
	// Ignore holds the package-level ignore rules.
	Ignore *IgnoreMap
}

// List returns l.
func (l *ListPolicy) List() *ListPolicy { return l }

// shouldBlock ports ListPolicyConfig::shouldBlock for a list whose
// supportsInstallBlockScope() is supportsInstall.
func (l *ListPolicy) shouldBlock(blockScope string, supportsInstall bool) bool {
	if !l.Block {
		return false
	}

	return blockScope != BlockScopeInstall || supportsInstall
}

// IgnoreForOperation ports getIgnoreForOperation; operation is "block" or
// "audit".
func (l *ListPolicy) IgnoreForOperation(operation string) *IgnoreMap {
	return FilterByOperation(l.Ignore, operation)
}

// FlatIgnoreForOperation ports getFlatIgnoreForOperation: the package names
// ignored for operation, with their merged reasons.
func (l *ListPolicy) FlatIgnoreForOperation(operation string) *Reasons {
	result := &Reasons{}
	for packageName, rules := range l.Ignore.All() {
		for _, rule := range rules {
			if rule.appliesTo(operation) {
				result.Set(packageName, mergeReason(result, packageName, rule.Reason))
			}
		}
	}

	return result
}

// mergeReason ports ListPolicyConfig::mergeReason.
func mergeReason(m *Reasons, key string, newReason *string) *string {
	existing, ok := m.Get(key)
	if !ok {
		return newReason
	}

	if newReason == nil {
		return existing
	}
	if existing == nil {
		return newReason
	}
	if *existing == *newReason {
		return existing
	}

	// Avoid re-appending if newReason is already part of a previously merged value
	for part := range strings.SplitSeq(*existing, ";") {
		if php.Trim(part) == *newReason {
			return existing
		}
	}

	merged := *existing + "; " + *newReason

	return &merged
}

// legacyIgnore is one entry of the legacy ignore formats, parsed by
// parseLegacySingleIgnore.
type legacyIgnore struct {
	reason  *string
	onBlock bool
	onAudit bool
}

// legacyKey is is_int($key) ? (string) $value : $key.
func legacyKey(k php.Key, value any) string {
	if k.IsInt() {
		return php.ToString(value)
	}

	return k.String()
}

// parseLegacyAuditIgnore ports ListPolicyConfig::parseLegacyAuditIgnore:
// the legacy audit.ignore format mixing advisory IDs and package names
// (those containing a /).
func parseLegacyAuditIgnore(config *php.Array) (*IgnoreMap, *OrderedMap[*IgnoreIDRule], error) {
	packages := &IgnoreMap{}
	ids := &OrderedMap[*IgnoreIDRule]{}

	for k, value := range each(config) {
		id := legacyKey(k, value)
		parsed, err := parseLegacySingleIgnore(k, value)
		if err != nil {
			return nil, nil, err
		}

		if strings.Contains(id, "/") {
			addRule(packages, id, NewIgnorePackageRule(id, semver.NewMatchAllConstraint(), parsed.reason, parsed.onBlock, parsed.onAudit))
		} else {
			ids.Set(id, NewIgnoreIDRule(id, parsed.reason, parsed.onBlock, parsed.onAudit))
		}
	}

	return packages, ids, nil
}

// parseLegacyIgnoreWithApply ports
// ListPolicyConfig::parseLegacyIgnoreWithApply.
func parseLegacyIgnoreWithApply(config *php.Array) (*IgnoreMap, error) {
	result := &IgnoreMap{}
	for k, value := range each(config) {
		packageName := legacyKey(k, value)
		parsed, err := parseLegacySingleIgnore(k, value)
		if err != nil {
			return nil, err
		}
		result.Set(packageName, []*IgnorePackageRule{NewIgnorePackageRule(packageName, semver.NewMatchAllConstraint(), parsed.reason, parsed.onBlock, parsed.onAudit)})
	}

	return result, nil
}

// parseLegacySingleIgnore ports ListPolicyConfig::parseLegacySingleIgnore:
// the legacy format with "apply": "audit|block|all".
func parseLegacySingleIgnore(k php.Key, value any) (legacyIgnore, error) {
	parsed := legacyIgnore{onBlock: true, onAudit: true}

	switch v := value.(type) {
	case string:
		if !k.IsInt() {
			parsed.reason = new(v)
		}
	case *php.Array:
		apply, ok := coalesce(v, "apply")
		if !ok {
			apply = "all"
		}
		parsed.reason = reasonOf(v)
		applyStr, isString := apply.(string)
		if !isString || !slices.Contains([]string{"audit", "block", "all"}, applyStr) {
			shown := applyStr
			if !isString {
				shown = php.TypeName(apply)
			}

			return parsed, &util.InvalidArgumentError{Message: fmt.Sprintf(
				"Invalid 'apply' value for '%s': %s. Expected 'audit', 'block', or 'all'.", k.String(), shown)}
		}
		parsed.onBlock = applyStr == "block" || applyStr == "all"
		parsed.onAudit = applyStr == "audit" || applyStr == "all"
	}

	return parsed, nil
}
