// Ports src/Composer/Policy/IgnoreIdRule.php,
// src/Composer/Policy/IgnorePackageRule.php and
// src/Composer/Policy/IgnoreSeverityRule.php.

package policy

import (
	"fmt"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// IgnoreIDRule is IgnoreIdRule: an advisory ID to ignore.
type IgnoreIDRule struct {
	ID      string
	Reason  *string
	OnBlock bool
	OnAudit bool
}

// NewIgnoreIDRule is new IgnoreIdRule($id, $reason, $onBlock, $onAudit).
func NewIgnoreIDRule(id string, reason *string, onBlock, onAudit bool) *IgnoreIDRule {
	return &IgnoreIDRule{ID: id, Reason: reason, OnBlock: onBlock, OnAudit: onAudit}
}

// ParseIgnoreIDMap ports IgnoreIdRule::parseIgnoreIdMap.
func ParseIgnoreIDMap(config *php.Array) (*OrderedMap[*IgnoreIDRule], error) {
	rules := &OrderedMap[*IgnoreIDRule]{}

	for k, value := range each(config) {
		if k.IsInt() {
			s, ok := value.(string)
			if !ok {
				return nil, &util.UnexpectedValueError{Message: fmt.Sprintf(
					"Invalid ignore-id entry at index %d: expected an advisory ID string, got %s.",
					k.Int(), php.TypeName(value))}
			}
			rules.Set(s, NewIgnoreIDRule(s, nil, true, true))

			continue
		}

		key := k.String()
		switch v := value.(type) {
		case nil:
			rules.Set(key, NewIgnoreIDRule(key, nil, true, true))
		case string:
			rules.Set(key, NewIgnoreIDRule(key, new(v), true, true))
		case *php.Array:
			rules.Set(key, NewIgnoreIDRule(key, reasonOf(v), boolOr(v, "on-block", true), boolOr(v, "on-audit", true)))
		default:
			return nil, &util.UnexpectedValueError{Message: fmt.Sprintf(
				`Invalid ignore-id entry for "%s": value of type %s is not a supported shape.`+
					" Expected null, a reason string, or a rule object.",
				key, php.TypeName(value))}
		}
	}

	return rules, nil
}

// IgnoreSeverityRule is IgnoreSeverityRule: an advisory severity to ignore.
type IgnoreSeverityRule struct {
	Severity string
	Reason   *string
	OnBlock  bool
	OnAudit  bool
}

// NewIgnoreSeverityRule is new IgnoreSeverityRule($severity, $reason, $onBlock, $onAudit).
func NewIgnoreSeverityRule(severity string, reason *string, onBlock, onAudit bool) *IgnoreSeverityRule {
	return &IgnoreSeverityRule{Severity: severity, Reason: reason, OnBlock: onBlock, OnAudit: onAudit}
}

// ParseIgnoreSeverityMap ports IgnoreSeverityRule::parseIgnoreSeverityMap.
func ParseIgnoreSeverityMap(config *php.Array) (*OrderedMap[*IgnoreSeverityRule], error) {
	rules := &OrderedMap[*IgnoreSeverityRule]{}

	for k, value := range each(config) {
		if k.IsInt() {
			s, ok := value.(string)
			if !ok {
				return nil, &util.UnexpectedValueError{Message: fmt.Sprintf(
					"Invalid ignore-severity entry at index %d: expected a severity string, got %s.",
					k.Int(), php.TypeName(value))}
			}
			rules.Set(s, NewIgnoreSeverityRule(s, nil, true, true))

			continue
		}

		key := k.String()
		switch v := value.(type) {
		case nil:
			rules.Set(key, NewIgnoreSeverityRule(key, nil, true, true))
		case string:
			rules.Set(key, NewIgnoreSeverityRule(key, new(v), true, true))
		case *php.Array:
			rules.Set(key, NewIgnoreSeverityRule(key, reasonOf(v), boolOr(v, "on-block", true), boolOr(v, "on-audit", true)))
		default:
			return nil, &util.UnexpectedValueError{Message: fmt.Sprintf(
				`Invalid ignore-severity entry for "%s": value of type %s is not a supported shape.`+
					" Expected null, a reason string, or a rule object.",
				key, php.TypeName(value))}
		}
	}

	return rules, nil
}

// IgnorePackageRule is IgnorePackageRule: a package (name pattern and
// version constraint) to ignore.
type IgnorePackageRule struct {
	PackageName string
	Constraint  semver.ConstraintInterface
	Reason      *string
	OnBlock     bool
	OnAudit     bool
	// PackageNameRegex is BasePackage::packageNameToRegexp($packageName).
	PackageNameRegex string
}

// NewIgnorePackageRule is new IgnorePackageRule(...).
func NewIgnorePackageRule(packageName string, constraint semver.ConstraintInterface, reason *string, onBlock, onAudit bool) *IgnorePackageRule {
	return &IgnorePackageRule{
		PackageName:      packageName,
		Constraint:       constraint,
		Reason:           reason,
		OnBlock:          onBlock,
		OnAudit:          onAudit,
		PackageNameRegex: packageNameToRegexp(packageName),
	}
}

// packageNameToRegexp ports BasePackage::packageNameToRegexp with its
// default wrap; internal/pkg owns the original.
func packageNameToRegexp(allowPattern string) string {
	return "{^" + strings.ReplaceAll(php.PregQuote(allowPattern, ""), `\*`, ".*") + "$}i"
}

// IgnoreMap is the package-level ignore rules of a list, by package name.
type IgnoreMap = OrderedMap[[]*IgnorePackageRule]

func addRule(rules *IgnoreMap, key string, rule *IgnorePackageRule) {
	list, _ := rules.Get(key)
	rules.Set(key, append(list, rule))
}

// ParseIgnoreMap ports IgnorePackageRule::parseIgnoreMap.
func ParseIgnoreMap(config *php.Array) (*IgnoreMap, error) {
	rules := &IgnoreMap{}

	for k, value := range each(config) {
		key := k.String()

		// "vendor/pkg": null
		if value == nil {
			addRule(rules, key, NewIgnorePackageRule(key, semver.NewMatchAllConstraint(), nil, true, true))

			continue
		}

		s, isString := value.(string)
		// Simple string reason: "vendor/pkg": "reason"
		if isString && !k.IsInt() {
			addRule(rules, key, NewIgnorePackageRule(key, semver.NewMatchAllConstraint(), new(s), true, true))

			continue
		}

		// Numeric key with string value: ["vendor/pkg"]
		if k.IsInt() && isString {
			addRule(rules, s, NewIgnorePackageRule(s, semver.NewMatchAllConstraint(), nil, true, true))

			continue
		}

		arr, isArray := value.(*php.Array)
		if isArray && !k.IsInt() {
			// Array of rule objects: "vendor/pkg": [{...}, {...}]
			if first, ok := arr.Get(0); ok && first != nil {
				for _, ruleConfig := range arr.All() {
					ruleArr, ok := ruleConfig.(*php.Array)
					if !ok {
						return nil, &util.UnexpectedValueError{Message: fmt.Sprintf(
							`Invalid ignore rule for "%s": expected an object, got %s.`,
							key, php.TypeName(ruleConfig))}
					}
					rule, err := ruleFromObject(key, ruleArr)
					if err != nil {
						return nil, err
					}
					addRule(rules, key, rule)
				}

				continue
			}

			// Single rule object: "vendor/pkg": {"constraint": "...", ...}
			rule, err := ruleFromObject(key, arr)
			if err != nil {
				return nil, err
			}
			addRule(rules, key, rule)

			continue
		}

		return nil, &util.UnexpectedValueError{Message: fmt.Sprintf(
			`Invalid ignore entry at key "%s": value of type %s is not a supported shape.`+
				" Expected null, a reason string, a rule object, or a list of rule objects.",
			key, php.TypeName(value))}
	}

	return rules, nil
}

// ruleFromObject ports IgnorePackageRule::fromRuleObject.
func ruleFromObject(packageName string, config *php.Array) (*IgnorePackageRule, error) {
	var constraint semver.ConstraintInterface = semver.NewMatchAllConstraint()
	if c, ok := coalesce(config, "constraint"); ok {
		parsed, err := semver.VersionParser{}.ParseConstraints(php.ToString(c))
		if err != nil {
			return nil, err
		}
		constraint = parsed
	}

	return NewIgnorePackageRule(packageName, constraint, reasonOf(config),
		boolOr(config, "on-block", true), boolOr(config, "on-audit", true)), nil
}

// FilterByOperation ports IgnorePackageRule::filterByOperation; operation
// is "block" or "audit".
func FilterByOperation(rules *IgnoreMap, operation string) *IgnoreMap {
	filtered := &IgnoreMap{}
	for packageName, ruleList := range rules.All() {
		for _, rule := range ruleList {
			if rule.appliesTo(operation) {
				addRule(filtered, packageName, rule)
			}
		}
	}

	return filtered
}

func (r *IgnorePackageRule) appliesTo(operation string) bool {
	return appliesTo(operation, r.OnBlock, r.OnAudit)
}

func appliesTo(operation string, onBlock, onAudit bool) bool {
	return (operation == "block" && onBlock) || (operation == "audit" && onAudit)
}
