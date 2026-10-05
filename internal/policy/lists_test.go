package policy

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/filterlist/source"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/semver"
)

// Ports tests/Composer/Test/Policy/{Abandoned,Advisories,CustomList,Malware}PolicyConfigTest.php,
// except the cases building a Config (config_test.go).

// must panics on err, failing the test.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}

	return v
}

func constraint(t *testing.T, s string) semver.ConstraintInterface {
	t.Helper()

	return must(semver.VersionParser{}.ParseConstraints(s))
}

func all() semver.ConstraintInterface { return semver.NewMatchAllConstraint() }

func ignoreMap(entries ...any) *IgnoreMap {
	m := &IgnoreMap{}
	for i := 0; i < len(entries); i += 2 {
		m.Set(entries[i].(string), entries[i+1].([]*IgnorePackageRule))
	}

	return m
}

func rule(name string, c semver.ConstraintInterface, reason *string, onBlock, onAudit bool) []*IgnorePackageRule {
	return []*IgnorePackageRule{NewIgnorePackageRule(name, c, reason, onBlock, onAudit)}
}

//go:fix inline

func defaultPolicyConfigs(name string) []*php.Array {
	return []*php.Array{php.NewArray(), php.ArrayOf(name, true), php.ArrayOf(name, php.NewArray())}
}

func TestAbandonedPolicyConfig_DefaultConfig(t *testing.T) {
	for _, policyConfig := range defaultPolicyConfigs("abandoned") {
		AssertDumpEqual(t, NewAbandonedPolicyConfig(false, AuditFail, nil),
			must(AbandonedPolicyConfigFromRawConfig(policyConfig, php.NewArray())))
	}
}

func TestAbandonedPolicyConfig_FromRawConfig(t *testing.T) {
	raw := php.ArrayOf("abandoned", php.ArrayOf(
		"block", true,
		"audit", "report",
		"ignore", php.ArrayOf(
			"acme/abandoned", "flagged by mistake",
			"acme/abandoned2", php.ArrayOf("constraint", "1.0"),
		),
	))
	AssertDumpEqual(t,
		NewAbandonedPolicyConfig(true, AuditReport, ignoreMap(
			"acme/abandoned", rule("acme/abandoned", all(), new("flagged by mistake"), true, true),
			"acme/abandoned2", rule("acme/abandoned2", constraint(t, "1.0"), nil, true, true),
		)),
		must(AbandonedPolicyConfigFromRawConfig(raw, php.NewArray())))
}

func TestAbandonedPolicyConfig_FromAuditConfig(t *testing.T) {
	audit := php.ArrayOf(
		"block-abandoned", true,
		"abandoned", "report",
		"ignore-abandoned", php.ArrayOf(
			"acme/abandoned", "flagged by mistake",
			"acme/abandoned2", php.ArrayOf("apply", "block"),
		),
	)
	AssertDumpEqual(t,
		NewAbandonedPolicyConfig(true, AuditReport, ignoreMap(
			"acme/abandoned", rule("acme/abandoned", all(), new("flagged by mistake"), true, true),
			"acme/abandoned2", rule("acme/abandoned2", all(), nil, true, false),
		)),
		must(AbandonedPolicyConfigFromRawConfig(php.NewArray(), audit)))
}

func TestAbandonedPolicyConfig_PolicyAbandonedSetIgnoresLegacyAuditAbandonedKeys(t *testing.T) {
	policyConfig := php.ArrayOf("abandoned", php.ArrayOf("block", true, "audit", "report"))
	audit := php.ArrayOf("block-abandoned", false, "abandoned", "ignore", "ignore-abandoned", php.ArrayOf("acme/abandoned", "should be ignored"))
	abandoned := must(AbandonedPolicyConfigFromRawConfig(policyConfig, audit))
	if !abandoned.Block || abandoned.Audit != AuditReport || abandoned.Ignore.Len() != 0 {
		t.Errorf("got %+v", abandoned)
	}
}

func TestAbandonedPolicyConfig_PolicyAbandonedFalseIgnoresLegacyAuditAbandonedKeys(t *testing.T) {
	policyConfig := php.ArrayOf("abandoned", false)
	audit := php.ArrayOf("block-abandoned", true, "abandoned", "fail", "ignore-abandoned", php.ArrayOf("acme/abandoned", "should be ignored"))
	AssertDumpEqual(t, AbandonedPolicyConfigDisabled(), must(AbandonedPolicyConfigFromRawConfig(policyConfig, audit)))
}

func TestAdvisoriesPolicyConfig_DefaultConfig(t *testing.T) {
	for _, policyConfig := range defaultPolicyConfigs("advisories") {
		AssertDumpEqual(t, NewAdvisoriesPolicyConfig(true, AuditFail, nil, nil, nil),
			must(AdvisoriesPolicyConfigFromRawConfig(policyConfig, php.NewArray())))
	}
}

func idRules(rules ...*IgnoreIDRule) *OrderedMap[*IgnoreIDRule] {
	m := &OrderedMap[*IgnoreIDRule]{}
	for _, r := range rules {
		m.Set(r.ID, r)
	}

	return m
}

func severityRules(rules ...*IgnoreSeverityRule) *OrderedMap[*IgnoreSeverityRule] {
	m := &OrderedMap[*IgnoreSeverityRule]{}
	for _, r := range rules {
		m.Set(r.Severity, r)
	}

	return m
}

func TestAdvisoriesPolicyConfig_FromRawConfig(t *testing.T) {
	raw := php.ArrayOf("advisories", php.ArrayOf(
		"block", true,
		"audit", "report",
		"ignore", php.ArrayOf(
			"acme/abandoned", "flagged by mistake",
			"acme/abandoned2", php.ArrayOf("constraint", "1.0"),
		),
		"ignore-severity", php.ArrayOf(
			"low", php.ArrayOf("reason", "reason", "on-block", false, "on-audit", false),
			"high", "ignore",
		),
		"ignore-id", php.ArrayOf(
			"CVE-2024-1234", "flagged by mistake",
			"CVE-2024-1235", php.ArrayOf("on-block", false),
		),
	))
	AssertDumpEqual(t,
		NewAdvisoriesPolicyConfig(true, AuditReport, ignoreMap(
			"acme/abandoned", rule("acme/abandoned", all(), new("flagged by mistake"), true, true),
			"acme/abandoned2", rule("acme/abandoned2", constraint(t, "1.0"), nil, true, true),
		), idRules(
			NewIgnoreIDRule("CVE-2024-1234", new("flagged by mistake"), true, true),
			NewIgnoreIDRule("CVE-2024-1235", nil, false, true),
		), severityRules(
			NewIgnoreSeverityRule("low", new("reason"), false, false),
			NewIgnoreSeverityRule("high", new("ignore"), true, true),
		)),
		must(AdvisoriesPolicyConfigFromRawConfig(raw, php.NewArray())))
}

func TestAdvisoriesPolicyConfig_ShouldBlockNeverAppliesToInstallScope(t *testing.T) {
	advisories := NewAdvisoriesPolicyConfig(true, AuditFail, nil, nil, nil)
	if !advisories.ShouldBlock(BlockScopeUpdate) || advisories.ShouldBlock(BlockScopeInstall) {
		t.Error("wrong")
	}
}

func TestAdvisoriesPolicyConfig_ShouldBlockReturnsFalseWhenBlockIsOff(t *testing.T) {
	advisories := NewAdvisoriesPolicyConfig(false, AuditFail, nil, nil, nil)
	if advisories.ShouldBlock(BlockScopeUpdate) || advisories.ShouldBlock(BlockScopeInstall) {
		t.Error("wrong")
	}
}

func TestAdvisoriesPolicyConfig_FromAuditConfig(t *testing.T) {
	audit := php.ArrayOf(
		"block", true,
		"ignore", php.ArrayOf(
			"acme/abandoned", "flagged by mistake",
			"acme/abandoned2", php.ArrayOf("apply", "block"),
			"CVE-2024-1234", "flagged by mistake",
			"CVE-2024-1235", php.ArrayOf("apply", "audit"),
		),
		"ignore-severity", php.ArrayOf("low", php.ArrayOf("apply", "block")),
	)
	AssertDumpEqual(t,
		NewAdvisoriesPolicyConfig(true, AuditFail, ignoreMap(
			"acme/abandoned", rule("acme/abandoned", all(), new("flagged by mistake"), true, true),
			"acme/abandoned2", rule("acme/abandoned2", all(), nil, true, false),
		), idRules(
			NewIgnoreIDRule("CVE-2024-1234", new("flagged by mistake"), true, true),
			NewIgnoreIDRule("CVE-2024-1235", nil, false, true),
		), severityRules(
			NewIgnoreSeverityRule("low", nil, true, false),
		)),
		must(AdvisoriesPolicyConfigFromRawConfig(php.NewArray(), audit)))
}

func TestAdvisoriesPolicyConfig_WithIgnoreSeverityAddsAuditScopedRulesForNewSeverities(t *testing.T) {
	updated := AdvisoriesPolicyConfigDisabled().WithIgnoreSeverity([]string{"low", "medium"})
	assertReasons(t, updated.IgnoreSeverityForOperation("audit"), "low", nil, "medium", nil)
	assertReasons(t, updated.IgnoreSeverityForOperation("block"))
}

func TestAdvisoriesPolicyConfig_PolicyAdvisoriesSetIgnoresLegacyAuditAdvisoriesKeys(t *testing.T) {
	policyConfig := php.ArrayOf("advisories", php.ArrayOf("block", false, "audit", "report"))
	audit := php.ArrayOf(
		"block-insecure", true,
		"ignore", php.ArrayOf("CVE-2024-1234", "should be ignored"),
		"ignore-severity", php.ArrayOf("low", "should be ignored"),
	)
	advisories := must(AdvisoriesPolicyConfigFromRawConfig(policyConfig, audit))
	if advisories.Block || advisories.Audit != AuditReport || advisories.Ignore.Len() != 0 ||
		advisories.IgnoreID.Len() != 0 || advisories.IgnoreSeverity.Len() != 0 {
		t.Errorf("got %+v", advisories)
	}
}

func TestAdvisoriesPolicyConfig_PolicyAdvisoriesFalseIgnoresLegacyAuditAdvisoriesKeys(t *testing.T) {
	policyConfig := php.ArrayOf("advisories", false)
	audit := php.ArrayOf(
		"block-insecure", true,
		"ignore", php.ArrayOf("CVE-2024-1234", "should be ignored"),
		"ignore-severity", php.ArrayOf("low", "should be ignored"),
	)
	AssertDumpEqual(t, AdvisoriesPolicyConfigDisabled(), must(AdvisoriesPolicyConfigFromRawConfig(policyConfig, audit)))
}

// assertReasons checks m against key/reason pairs (reason nil or string).
func assertReasons(t *testing.T, m *Reasons, kv ...any) {
	t.Helper()
	want := &Reasons{}
	for i := 0; i < len(kv); i += 2 {
		var r *string
		if s, ok := kv[i+1].(string); ok {
			r = &s
		}
		want.Set(kv[i].(string), r)
	}
	AssertDumpEqual(t, want, m)
}

func TestCustomListPolicyConfig_DefaultConfig(t *testing.T) {
	for _, listConfig := range []any{php.NewArray(), true} {
		AssertDumpEqual(t, NewCustomListPolicyConfig("test", true, AuditFail, nil, nil),
			must(CustomListPolicyConfigFromRawConfig("test", listConfig)))
	}
}

func TestCustomListPolicyConfig_ShouldBlockNeverAppliesToInstallScope(t *testing.T) {
	custom := NewCustomListPolicyConfig("company-policy", true, AuditFail, nil, nil)
	if !custom.ShouldBlock(BlockScopeUpdate) || custom.ShouldBlock(BlockScopeInstall) {
		t.Error("wrong")
	}
}

func TestCustomListPolicyConfig_FromRawConfig(t *testing.T) {
	raw := php.ArrayOf(
		"block", false,
		"audit", "report",
		"ignore", php.ArrayOf(
			"acme/test", "flagged by mistake",
			"acme/test2", php.ArrayOf("constraint", "1.0"),
		),
		"sources", php.ListOf(php.ArrayOf("type", "url", "url", "https://example.com")),
	)
	AssertDumpEqual(t,
		NewCustomListPolicyConfig("test", false, AuditReport, ignoreMap(
			"acme/test", rule("acme/test", all(), new("flagged by mistake"), true, true),
			"acme/test2", rule("acme/test2", constraint(t, "1.0"), nil, true, true),
		), []*source.URLSource{{ListName: "test", URL: "https://example.com"}}),
		must(CustomListPolicyConfigFromRawConfig("test", raw)))
}

func TestCustomListPolicyConfig_FromRawConfigRejectsNonHttpsSourceUrl(t *testing.T) {
	for name, url := range map[string]string{
		"http":              "http://insecure.example.org/list.json",
		"ftp":               "ftp://example.org/list.json",
		"file":              "file:///etc/list.json",
		"protocol-relative": "//example.org/list.json",
	} {
		_, err := CustomListPolicyConfigFromRawConfig("company-policy",
			php.ArrayOf("sources", php.ListOf(php.ArrayOf("type", "url", "url", url))))
		if err == nil || !strings.Contains(err.Error(), `must start with "https://"`) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestMalwarePolicyConfig_DefaultConfig(t *testing.T) {
	for _, policyConfig := range defaultPolicyConfigs("malware") {
		AssertDumpEqual(t, NewMalwarePolicyConfig(true, AuditFail, BlockScopeAll, nil, nil),
			must(MalwarePolicyConfigFromRawConfig(policyConfig)))
	}
}

func TestMalwarePolicyConfig_FromRawConfig(t *testing.T) {
	raw := php.ArrayOf("malware", php.ArrayOf(
		"block", false,
		"block-scope", "update",
		"audit", "report",
		"ignore", php.ArrayOf(
			"acme/malware", "flagged by mistake",
			"acme/malware2", php.ArrayOf("constraint", "1.0"),
		),
		"ignore-source", php.ListOf("untrusted"),
	))
	AssertDumpEqual(t,
		NewMalwarePolicyConfig(false, AuditReport, BlockScopeUpdate, ignoreMap(
			"acme/malware", rule("acme/malware", all(), new("flagged by mistake"), true, true),
			"acme/malware2", rule("acme/malware2", constraint(t, "1.0"), nil, true, true),
		), []string{"untrusted"}),
		must(MalwarePolicyConfigFromRawConfig(raw)))
}

func TestMalwarePolicyConfig_ShouldBlockReturnsFalseWhenBlockIsOff(t *testing.T) {
	malware := NewMalwarePolicyConfig(false, AuditFail, BlockScopeAll, nil, nil)
	if malware.ShouldBlock(BlockScopeUpdate) || malware.ShouldBlock(BlockScopeInstall) {
		t.Error("wrong")
	}
}

func TestMalwarePolicyConfig_ShouldBlockHonoursConfiguredBlockScope(t *testing.T) {
	cases := []struct {
		name              string
		configured, query string
		expected          bool
	}{
		{"all + update", BlockScopeAll, BlockScopeUpdate, true},
		{"all + install", BlockScopeAll, BlockScopeInstall, true},
		{"update + update", BlockScopeUpdate, BlockScopeUpdate, true},
		{"update + install", BlockScopeUpdate, BlockScopeInstall, false},
		{"install + update", BlockScopeInstall, BlockScopeUpdate, false},
		{"install + install", BlockScopeInstall, BlockScopeInstall, true},
	}
	for _, c := range cases {
		malware := NewMalwarePolicyConfig(true, AuditFail, c.configured, nil, nil)
		if got := malware.ShouldBlock(c.query); got != c.expected {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}
