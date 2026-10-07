package policy

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/semver"
)

// Ports tests/Composer/Test/Policy/{IgnoreIdRule,IgnorePackageRule,IgnoreSeverityRule,IgnoreUnreachable}Test.php.

func TestIgnoreIdRule_ParseIgnoreIdMapWithEmptyConfig(t *testing.T) {
	if rules := must(ParseIgnoreIDMap(php.NewArray())); rules.Len() != 0 {
		t.Error("not empty")
	}
}

func TestIgnoreIdRule_ParseIgnoreIdMapWithIntegerKeyAndStringValue(t *testing.T) {
	rules := must(ParseIgnoreIDMap(php.ListOf("CVE-123", "GHSA-456")))
	AssertDumpEqual(t, idRules(NewIgnoreIDRule("CVE-123", nil, true, true), NewIgnoreIDRule("GHSA-456", nil, true, true)), rules)
}

func TestIgnoreIdRule_ParseIgnoreIdMapWithMultipleMixedEntries(t *testing.T) {
	rules := must(ParseIgnoreIDMap(php.ArrayOf(
		"CVE-123", "reason",
		"CVE-456", php.ArrayOf("on-block", false, "reason", "other reason"),
		"CVE-789", nil,
		"CVE-012", php.ArrayOf("on-audit", false),
	)))
	AssertDumpEqual(t, idRules(
		NewIgnoreIDRule("CVE-123", new("reason"), true, true),
		NewIgnoreIDRule("CVE-456", new("other reason"), false, true),
		NewIgnoreIDRule("CVE-789", nil, true, true),
		NewIgnoreIDRule("CVE-012", nil, true, false),
	), rules)
}

func assertUnexpectedValue(t *testing.T, name string, err error) {
	t.Helper()
	if !phperr.InstanceOf(err, "UnexpectedValueException") {
		t.Errorf("%s: got %v, want UnexpectedValueException", name, err)
	}
}

func TestIgnoreIdRule_ParseIgnoreIdMapRejectsUnsupportedShapes(t *testing.T) {
	cases := map[string]*php.Array{
		"integer key with null value":    php.ListOf(nil),
		"integer key with array value":   php.ListOf(php.ArrayOf("id", "CVE-1")),
		"integer key with bool value":    php.ListOf(true),
		"integer key with integer value": php.ListOf(42),
		"string key with bool value":     php.ArrayOf("CVE-1", true),
		"string key with integer value":  php.ArrayOf("CVE-1", 42),
	}
	for name, config := range cases {
		_, err := ParseIgnoreIDMap(config)
		assertUnexpectedValue(t, name, err)
	}
}

func TestIgnorePackageRule_ParseIgnoreMapWithEmptyConfig(t *testing.T) {
	if rules := must(ParseIgnoreMap(php.NewArray())); rules.Len() != 0 {
		t.Error("not empty")
	}
}

func TestIgnorePackageRule_ParseIgnoreMapWithIntegerKeyAndStringValue(t *testing.T) {
	rules := must(ParseIgnoreMap(php.ListOf("vendor/foo", "vendor/bar")))
	AssertDumpEqual(t, ignoreMap(
		"vendor/foo", rule("vendor/foo", all(), nil, true, true),
		"vendor/bar", rule("vendor/bar", all(), nil, true, true),
	), rules)
	r, _ := rules.Get("vendor/foo")
	if _, ok := r[0].Constraint.(*semver.MatchAllConstraint); !ok {
		t.Error("not MatchAllConstraint")
	}
}

func TestIgnorePackageRule_ParseIgnoreMapWithMultipleMixedEntries(t *testing.T) {
	rules := must(ParseIgnoreMap(php.ArrayOf(
		"vendor/foo", "reason",
		"vendor/bar", php.ArrayOf("constraint", "^2.0", "on-block", false, "reason", "other reason"),
		"vendor/baz", nil,
		"vendor/qux", php.ArrayOf("on-audit", false),
	)))
	AssertDumpEqual(t, ignoreMap(
		"vendor/foo", rule("vendor/foo", all(), new("reason"), true, true),
		"vendor/bar", rule("vendor/bar", constraint(t, "^2.0"), new("other reason"), false, true),
		"vendor/baz", rule("vendor/baz", all(), nil, true, true),
		"vendor/qux", rule("vendor/qux", all(), nil, true, false),
	), rules)
	r, _ := rules.Get("vendor/bar")
	if r[0].Constraint.PrettyString() != "^2.0" {
		t.Error(r[0].Constraint.PrettyString())
	}
}

func TestIgnorePackageRule_ParseIgnoreMapWithArrayOfRuleObjects(t *testing.T) {
	rules := must(ParseIgnoreMap(php.ArrayOf(
		"vendor/foo", php.ListOf(
			php.ArrayOf("constraint", "^1.0", "reason", "old version"),
			php.ArrayOf("constraint", "^3.0", "on-block", false),
		),
	)))
	AssertDumpEqual(t, ignoreMap("vendor/foo", []*IgnorePackageRule{
		NewIgnorePackageRule("vendor/foo", constraint(t, "^1.0"), new("old version"), true, true),
		NewIgnorePackageRule("vendor/foo", constraint(t, "^3.0"), nil, false, true),
	}), rules)
}

func TestIgnorePackageRule_ParseIgnoreMapRejectsUnsupportedShapes(t *testing.T) {
	cases := map[string]*php.Array{
		"integer key with array value":   php.ListOf(php.ArrayOf("package", "vendor/foo", "constraint", "^1.0")),
		"integer key with bool value":    php.ListOf(true),
		"string key with bool value":     php.ArrayOf("vendor/foo", true),
		"integer key with integer value": php.ListOf(42),
	}
	for name, config := range cases {
		_, err := ParseIgnoreMap(config)
		assertUnexpectedValue(t, name, err)
	}
}

func TestIgnoreSeverityRule_ParseIgnoreSeverityMapWithEmptyConfig(t *testing.T) {
	if rules := must(ParseIgnoreSeverityMap(php.NewArray())); rules.Len() != 0 {
		t.Error("not empty")
	}
}

func TestIgnoreSeverityRule_ParseIgnoreSeverityMapWithIntegerKeyAndStringValue(t *testing.T) {
	rules := must(ParseIgnoreSeverityMap(php.ListOf("low", "medium")))
	AssertDumpEqual(t, severityRules(NewIgnoreSeverityRule("low", nil, true, true), NewIgnoreSeverityRule("medium", nil, true, true)), rules)
}

func TestIgnoreSeverityRule_ParseIgnoreSeverityMapWithMultipleMixedEntries(t *testing.T) {
	rules := must(ParseIgnoreSeverityMap(php.ArrayOf(
		"low", "reason",
		"medium", php.ArrayOf("on-block", false, "reason", "other reason"),
		"high", nil,
		"critical", php.ArrayOf("on-audit", false),
	)))
	AssertDumpEqual(t, severityRules(
		NewIgnoreSeverityRule("low", new("reason"), true, true),
		NewIgnoreSeverityRule("medium", new("other reason"), false, true),
		NewIgnoreSeverityRule("high", nil, true, true),
		NewIgnoreSeverityRule("critical", nil, true, false),
	), rules)
}

func TestIgnoreSeverityRule_ParseIgnoreSeverityMapRejectsUnsupportedShapes(t *testing.T) {
	cases := map[string]*php.Array{
		"integer key with null value":    php.ListOf(nil),
		"integer key with array value":   php.ListOf(php.ArrayOf("severity", "low")),
		"integer key with bool value":    php.ListOf(true),
		"integer key with integer value": php.ListOf(42),
		"string key with bool value":     php.ArrayOf("low", true),
		"string key with integer value":  php.ArrayOf("low", 42),
	}
	for name, config := range cases {
		_, err := ParseIgnoreSeverityMap(config)
		assertUnexpectedValue(t, name, err)
	}
}

func TestIgnoreUnreachable_FromRawAuditConfig(t *testing.T) {
	got := IgnoreUnreachableFromRawAuditConfig(php.ArrayOf("ignore-unreachable", true))
	if got != (IgnoreUnreachable{Audit: true}) {
		t.Errorf("got %+v", got)
	}
}

func TestIgnoreUnreachable_ForBlockScope(t *testing.T) {
	i := IgnoreUnreachable{false, true, false}
	if !i.ForBlockScope(BlockScopeInstall) || i.ForBlockScope(BlockScopeUpdate) {
		t.Error("wrong")
	}
	i = IgnoreUnreachable{false, false, true}
	if i.ForBlockScope(BlockScopeInstall) || !i.ForBlockScope(BlockScopeUpdate) {
		t.Error("wrong")
	}
}

func TestIgnoreUnreachable_WithOnlyFlipsRequestedScope(t *testing.T) {
	updated := must(IgnoreUnreachable{false, false, true}.With("audit"))
	if updated != (IgnoreUnreachable{true, false, true}) {
		t.Errorf("got %+v", updated)
	}
}

func TestIgnoreUnreachable_WithAcceptsMultipleScopes(t *testing.T) {
	updated := must(IgnoreUnreachableNone().With("audit", "install"))
	if updated != (IgnoreUnreachable{true, true, false}) {
		t.Errorf("got %+v", updated)
	}
}

func assertInvalidArgument(t *testing.T, err error) {
	t.Helper()
	if !phperr.InstanceOf(err, "InvalidArgumentException") {
		t.Errorf("got %v, want InvalidArgumentException", err)
	}
}

func TestIgnoreUnreachable_WithRejectsUnknownScope(t *testing.T) {
	_, err := IgnoreUnreachableNone().With("not-a-scope")
	assertInvalidArgument(t, err)
}

func TestIgnoreUnreachable_WithRequiresAtLeastOneScope(t *testing.T) {
	_, err := IgnoreUnreachableNone().With()
	assertInvalidArgument(t, err)
}
