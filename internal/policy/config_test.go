package policy_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/util"
)

// Ports the cases of tests/Composer/Test/Policy/*Test.php that build a
// Composer\Config, and tests/Composer/Test/Policy/PolicyConfigTest.php.
// They read the environment, so none runs in parallel.

var policyEnv = []string{
	"COMPOSER_POLICY", "COMPOSER_NO_BLOCKING", "COMPOSER_NO_SECURITY_BLOCKING",
	"COMPOSER_POLICY_ADVISORIES_BLOCK", "COMPOSER_POLICY_MALWARE_BLOCK", "COMPOSER_POLICY_ABANDONED_BLOCK",
	"COMPOSER_SECURITY_BLOCKING_ABANDONED", "COMPOSER_AUDIT_ABANDONED",
}

// clearPolicyEnv is PolicyConfigTest::tearDown, also run before each test.
func clearPolicyEnv(t *testing.T) {
	t.Helper()
	clear := func() {
		for _, name := range policyEnv {
			util.ClearEnv(name)
		}
	}
	clear()
	t.Cleanup(clear)
}

// fromConfig builds a Config (with environment when useEnv), merges
// {"config": cfg} and parses its policy.
func fromConfig(t *testing.T, useEnv bool, cfg *php.Array) (*policy.PolicyConfig, error) {
	t.Helper()
	c := config.New(useEnv, "")
	if cfg != nil {
		if err := c.Merge(php.ArrayOf("config", cfg), config.SourceUnknown); err != nil {
			t.Fatal(err)
		}
	}

	return policy.FromConfig(c)
}

func mustFromConfig(t *testing.T, useEnv bool, cfg *php.Array) *policy.PolicyConfig {
	t.Helper()
	p, err := fromConfig(t, useEnv, cfg)
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func auditCfg(kv ...any) *php.Array  { return php.ArrayOf("audit", php.ArrayOf(kv...)) }
func policyCfg(kv ...any) *php.Array { return php.ArrayOf("policy", php.ArrayOf(kv...)) }

// assertReasons checks m against key/reason pairs (reason nil or string).
func assertReasons(t *testing.T, m *policy.Reasons, kv ...any) {
	t.Helper()
	want := &policy.Reasons{}
	for i := 0; i < len(kv); i += 2 {
		var r *string
		if s, ok := kv[i+1].(string); ok {
			r = &s
		}
		want.Set(kv[i].(string), r)
	}
	policy.AssertDumpEqual(t, want, m)
}

func reasonOf(t *testing.T, m *policy.Reasons, key string) string {
	t.Helper()
	r, ok := m.Get(key)
	if !ok || r == nil {
		t.Fatalf("no reason for %s", key)
	}

	return *r
}

// AbandonedPolicyConfigTest

func TestAbandonedPolicyConfig_LegacyIgnoreAbandonedSimpleArray(t *testing.T) {
	clearPolicyEnv(t)
	abandoned := mustFromConfig(t, true, auditCfg("ignore-abandoned", php.ListOf("vendor/package1", "vendor/package2"))).Abandoned
	assertReasons(t, abandoned.FlatIgnoreForOperation("audit"), "vendor/package1", nil, "vendor/package2", nil)
	assertReasons(t, abandoned.FlatIgnoreForOperation("block"), "vendor/package1", nil, "vendor/package2", nil)
}

func TestAbandonedPolicyConfig_LegacyIgnoreAbandonedDetailedFormat(t *testing.T) {
	clearPolicyEnv(t)
	abandoned := mustFromConfig(t, true, auditCfg("ignore-abandoned", php.ArrayOf(
		"vendor/package1", php.ArrayOf("apply", "audit", "reason", "Report but do not block"),
		"vendor/package2", php.ArrayOf("apply", "block", "reason", "Block but do not report"),
	))).Abandoned
	assertReasons(t, abandoned.FlatIgnoreForOperation("audit"), "vendor/package1", "Report but do not block")
	assertReasons(t, abandoned.FlatIgnoreForOperation("block"), "vendor/package2", "Block but do not report")
}

func TestAbandonedPolicyConfig_GetFlatIgnoreForOperationMergesMultiRuleReasons(t *testing.T) {
	clearPolicyEnv(t)
	abandoned := mustFromConfig(t, true, policyCfg("abandoned", php.ArrayOf("ignore", php.ArrayOf(
		"vendor/multi-abandoned", php.ListOf(
			php.ArrayOf("constraint", "^1.0", "reason", "fork ready"),
			php.ArrayOf("constraint", "^2.0", "reason", "maintained downstream"),
		),
	)))).Abandoned
	reason := reasonOf(t, abandoned.FlatIgnoreForOperation("audit"), "vendor/multi-abandoned")
	if !strings.Contains(reason, "fork ready") || !strings.Contains(reason, "maintained downstream") {
		t.Errorf("got %q", reason)
	}
}

// AdvisoriesPolicyConfigTest

func TestAdvisoriesPolicyConfig_LegacyAuditIgnoreSimpleArray(t *testing.T) {
	clearPolicyEnv(t)
	advisories := mustFromConfig(t, true, auditCfg("ignore", php.ListOf("CVE-2024-1234", "CVE-2024-5678"))).Advisories
	assertReasons(t, advisories.IgnoreListForOperation("audit"), "CVE-2024-1234", nil, "CVE-2024-5678", nil)
	assertReasons(t, advisories.IgnoreListForOperation("block"), "CVE-2024-1234", nil, "CVE-2024-5678", nil)
}

func TestAdvisoriesPolicyConfig_LegacyAuditIgnoreApplyAuditOnly(t *testing.T) {
	clearPolicyEnv(t)
	advisories := mustFromConfig(t, true, auditCfg("ignore", php.ArrayOf(
		"CVE-2024-1234", php.ArrayOf("apply", "audit", "reason", "Only ignore for auditing"),
	))).Advisories
	assertReasons(t, advisories.IgnoreListForOperation("audit"), "CVE-2024-1234", "Only ignore for auditing")
	assertReasons(t, advisories.IgnoreListForOperation("block"))
}

func TestAdvisoriesPolicyConfig_LegacyAuditIgnoreApplyBlockOnly(t *testing.T) {
	clearPolicyEnv(t)
	advisories := mustFromConfig(t, true, auditCfg("ignore", php.ArrayOf(
		"CVE-2024-1234", php.ArrayOf("apply", "block", "reason", "Only ignore for blocking"),
	))).Advisories
	assertReasons(t, advisories.IgnoreListForOperation("audit"))
	assertReasons(t, advisories.IgnoreListForOperation("block"), "CVE-2024-1234", "Only ignore for blocking")
}

func TestAdvisoriesPolicyConfig_LegacyAuditIgnoreMixedFormats(t *testing.T) {
	clearPolicyEnv(t)
	ignore := php.ListOf("CVE-2024-1234")
	ignore.Set("CVE-2024-5678", "Simple reason")
	ignore.Set("CVE-2024-9999", php.ArrayOf("apply", "audit", "reason", "Detailed reason"))
	ignore.Set("CVE-2024-8888", php.ArrayOf("apply", "block"))
	advisories := mustFromConfig(t, true, auditCfg("ignore", ignore)).Advisories
	assertReasons(t, advisories.IgnoreListForOperation("audit"),
		"CVE-2024-1234", nil, "CVE-2024-5678", "Simple reason", "CVE-2024-9999", "Detailed reason")
	assertReasons(t, advisories.IgnoreListForOperation("block"),
		"CVE-2024-1234", nil, "CVE-2024-5678", "Simple reason", "CVE-2024-8888", nil)
}

func TestAdvisoriesPolicyConfig_LegacyAuditIgnoreSeveritySimpleArray(t *testing.T) {
	clearPolicyEnv(t)
	advisories := mustFromConfig(t, true, auditCfg("ignore-severity", php.ListOf("low", "medium"))).Advisories
	assertReasons(t, advisories.IgnoreSeverityForOperation("audit"), "low", nil, "medium", nil)
	assertReasons(t, advisories.IgnoreSeverityForOperation("block"), "low", nil, "medium", nil)
}

func TestAdvisoriesPolicyConfig_LegacyAuditIgnoreSeverityDetailedFormat(t *testing.T) {
	clearPolicyEnv(t)
	advisories := mustFromConfig(t, true, auditCfg("ignore-severity", php.ArrayOf(
		"low", php.ArrayOf("apply", "audit", "reason", "We accept low severity issues"),
		"medium", php.ArrayOf("apply", "block"),
	))).Advisories
	assertReasons(t, advisories.IgnoreSeverityForOperation("audit"), "low", "We accept low severity issues")
	assertReasons(t, advisories.IgnoreSeverityForOperation("block"), "medium", nil)
}

func TestAdvisoriesPolicyConfig_LegacyAuditIgnoreInvalidApplyValue(t *testing.T) {
	clearPolicyEnv(t)
	_, err := fromConfig(t, true, auditCfg("ignore", php.ArrayOf("CVE-2024-1234", php.ArrayOf("apply", "invalid"))))
	var iae *util.InvalidArgumentError
	if !errors.As(err, &iae) || err.Error() != "Invalid 'apply' value for 'CVE-2024-1234': invalid. Expected 'audit', 'block', or 'all'." {
		t.Errorf("got %v", err)
	}
}

func TestAdvisoriesPolicyConfig_GetIgnoreListForOperationMergesMultiRuleReasons(t *testing.T) {
	clearPolicyEnv(t)
	advisories := mustFromConfig(t, true, policyCfg("advisories", php.ArrayOf("ignore", php.ArrayOf(
		"vendor/multi", php.ListOf(
			php.ArrayOf("constraint", "^1.0", "reason", "v1 patched"),
			php.ArrayOf("constraint", "^2.0", "reason", "v2 mitigated"),
		),
	)))).Advisories
	for _, op := range []string{"audit", "block"} {
		reason := reasonOf(t, advisories.IgnoreListForOperation(op), "vendor/multi")
		if !strings.Contains(reason, "v1 patched") || !strings.Contains(reason, "v2 mitigated") {
			t.Errorf("%s: got %q", op, reason)
		}
	}
}

func TestAdvisoriesPolicyConfig_GetIgnoreListForOperationPrefersConcreteReasonOverNull(t *testing.T) {
	clearPolicyEnv(t)
	advisories := mustFromConfig(t, true, policyCfg("advisories", php.ArrayOf("ignore", php.ArrayOf(
		"vendor/mixed", php.ListOf(
			php.ArrayOf("constraint", "^1.0"),
			php.ArrayOf("constraint", "^2.0", "reason", "v2 mitigated"),
		),
	)))).Advisories
	if r := reasonOf(t, advisories.IgnoreListForOperation("audit"), "vendor/mixed"); r != "v2 mitigated" {
		t.Errorf("got %q", r)
	}
}

func TestAdvisoriesPolicyConfig_WithIgnoreSeverityPreservesExistingRulesAndReasons(t *testing.T) {
	clearPolicyEnv(t)
	advisories := mustFromConfig(t, true, policyCfg("advisories", php.ArrayOf("ignore-severity", php.ArrayOf(
		"low", php.ArrayOf("reason", "configured low", "on-block", false, "on-audit", true),
	)))).Advisories
	assertReasons(t, advisories.WithIgnoreSeverity([]string{"low", "medium"}).IgnoreSeverityForOperation("audit"),
		"low", "configured low", "medium", nil)
}

// PolicyConfigTest

func TestPolicyConfig_RejectsReservedCustomListNames(t *testing.T) {
	for _, listName := range []string{
		"ignore-foo", "ignoremalware", "package", "packages", "license", "licence",
		"support", "maintenance", "security", "minimum-release-age",
	} {
		clearPolicyEnv(t)
		_, err := fromConfig(t, false, policyCfg(listName, php.ArrayOf("block", true)))
		var uve *util.UnexpectedValueError
		if !errors.As(err, &uve) || !strings.Contains(err.Error(), listName) {
			t.Errorf("%s: got %v", listName, err)
		}
	}
}

func TestPolicyConfig_AllowsIgnoreUnreachableSiblingKey(t *testing.T) {
	clearPolicyEnv(t)
	p := mustFromConfig(t, false, policyCfg("ignore-unreachable", true))
	if !p.IgnoreUnreachable.Update || p.CustomLists.Len() != 0 {
		t.Errorf("got %+v", p)
	}
}

func TestPolicyConfig_AllowsRegularCustomListName(t *testing.T) {
	clearPolicyEnv(t)
	p := mustFromConfig(t, false, policyCfg("company-policy", php.ArrayOf("block", true)))
	if !p.CustomLists.Has("company-policy") {
		t.Error("missing company-policy")
	}
}

type blockCase struct {
	name, env     string
	block, expect bool
}

var blockCases = []blockCase{{"enable", "1", false, true}, {"disable", "0", true, false}}

func TestPolicyConfig_ComposerPolicyAdvisoriesBlock(t *testing.T) {
	for _, c := range blockCases {
		clearPolicyEnv(t)
		util.PutEnv("COMPOSER_POLICY_ADVISORIES_BLOCK", c.env)
		p := mustFromConfig(t, true, policyCfg("advisories", php.ArrayOf("block", c.block, "audit", "report")))
		if p.Advisories.Block != c.expect || p.Advisories.Audit != policy.AuditReport {
			t.Errorf("%s: got %+v", c.name, p.Advisories)
		}
	}
}

func testAbandonedBlockEnv(t *testing.T, env string) {
	t.Helper()
	for _, c := range blockCases {
		clearPolicyEnv(t)
		util.PutEnv(env, c.env)
		p := mustFromConfig(t, true, policyCfg("abandoned", php.ArrayOf("block", c.block, "audit", "report")))
		if p.Abandoned.Block != c.expect || p.Abandoned.Audit != policy.AuditReport {
			t.Errorf("%s: got %+v", c.name, p.Abandoned)
		}
	}
}

func TestPolicyConfig_ComposerSecurityBlockingAbandoned(t *testing.T) {
	testAbandonedBlockEnv(t, "COMPOSER_SECURITY_BLOCKING_ABANDONED")
}

func TestPolicyConfig_ComposerPolicyAbandonedBlock(t *testing.T) {
	testAbandonedBlockEnv(t, "COMPOSER_POLICY_ABANDONED_BLOCK")
}

func TestPolicyConfig_ComposerPolicyAbandonedBlockTakesPrecedenceOverLegacyAlias(t *testing.T) {
	clearPolicyEnv(t)
	util.PutEnv("COMPOSER_POLICY_ABANDONED_BLOCK", "1")
	util.PutEnv("COMPOSER_SECURITY_BLOCKING_ABANDONED", "0")
	if !mustFromConfig(t, true, nil).Abandoned.Block {
		t.Error("not blocking")
	}
}

func TestPolicyConfig_LegacyAbandonedBlockEnvVarStillWorksWhenCanonicalUnset(t *testing.T) {
	clearPolicyEnv(t)
	util.PutEnv("COMPOSER_SECURITY_BLOCKING_ABANDONED", "1")
	if !mustFromConfig(t, true, nil).Abandoned.Block {
		t.Error("not blocking")
	}
}

func TestPolicyConfig_ComposerSecurityBlockingAbandonedWithAuditConfig(t *testing.T) {
	for _, c := range blockCases {
		clearPolicyEnv(t)
		util.PutEnv("COMPOSER_SECURITY_BLOCKING_ABANDONED", c.env)
		p := mustFromConfig(t, true, auditCfg("block-abandoned", c.block))
		if p.Abandoned.Block != c.expect {
			t.Errorf("%s: got %+v", c.name, p.Abandoned)
		}
	}
}

var abandonedAuditCases = []struct{ name, env, config, expected string }{
	{"report", "report", "fail", "report"},
	{"fail", "fail", "report", "fail"},
}

func TestPolicyConfig_ComposerAuditAbandonedSetsAuditMode(t *testing.T) {
	for _, c := range abandonedAuditCases {
		clearPolicyEnv(t)
		util.PutEnv("COMPOSER_AUDIT_ABANDONED", c.env)
		if got := mustFromConfig(t, true, policyCfg("abandoned", php.ArrayOf("audit", c.config))).Abandoned.Audit; got != c.expected {
			t.Errorf("%s: got %s", c.name, got)
		}
	}
}

func TestPolicyConfig_ComposerAuditAbandonedSetsAuditModeWithAuditConfig(t *testing.T) {
	for _, c := range abandonedAuditCases {
		clearPolicyEnv(t)
		util.PutEnv("COMPOSER_AUDIT_ABANDONED", c.env)
		if got := mustFromConfig(t, true, auditCfg("abandoned", c.config)).Abandoned.Audit; got != c.expected {
			t.Errorf("%s: got %s", c.name, got)
		}
	}
}

func TestPolicyConfig_ComposerPolicyMalwareBlock(t *testing.T) {
	for _, c := range blockCases {
		clearPolicyEnv(t)
		util.PutEnv("COMPOSER_POLICY_MALWARE_BLOCK", c.env)
		p := mustFromConfig(t, true, policyCfg("malware", php.ArrayOf("block", c.block, "audit", "report")))
		if p.Malware.Block != c.expect || p.Malware.Audit != policy.AuditReport {
			t.Errorf("%s: got %+v", c.name, p.Malware)
		}
	}
}

func TestPolicyConfig_BothAbandonedEnvVarsApplyIndependently(t *testing.T) {
	clearPolicyEnv(t)
	util.PutEnv("COMPOSER_SECURITY_BLOCKING_ABANDONED", "1")
	util.PutEnv("COMPOSER_AUDIT_ABANDONED", "report")
	p := mustFromConfig(t, true, nil)
	if !p.Abandoned.Block || p.Abandoned.Audit != policy.AuditReport {
		t.Errorf("got %+v", p.Abandoned)
	}
}

func TestPolicyConfig_AdvisoriesEnvBlockOverridesWhenListExplicitlyDisabled(t *testing.T) {
	clearPolicyEnv(t)
	util.PutEnv("COMPOSER_POLICY_ADVISORIES_BLOCK", "1")
	if !mustFromConfig(t, true, policyCfg("advisories", false)).Advisories.Block {
		t.Error("not blocking")
	}
}

func TestPolicyConfig_MalwareEnvBlockOverridesWhenListExplicitlyDisabled(t *testing.T) {
	clearPolicyEnv(t)
	util.PutEnv("COMPOSER_POLICY_MALWARE_BLOCK", "1")
	if !mustFromConfig(t, true, policyCfg("malware", false)).Malware.Block {
		t.Error("not blocking")
	}
}

func TestPolicyConfig_AbandonedCanonicalEnvBlockOverridesWhenListExplicitlyDisabled(t *testing.T) {
	clearPolicyEnv(t)
	util.PutEnv("COMPOSER_POLICY_ABANDONED_BLOCK", "1")
	if !mustFromConfig(t, true, policyCfg("abandoned", false)).Abandoned.Block {
		t.Error("not blocking")
	}
}

func TestPolicyConfig_AbandonedLegacyEnvBlockOverridesWhenListExplicitlyDisabled(t *testing.T) {
	clearPolicyEnv(t)
	util.PutEnv("COMPOSER_SECURITY_BLOCKING_ABANDONED", "1")
	if !mustFromConfig(t, true, policyCfg("abandoned", false)).Abandoned.Block {
		t.Error("not blocking")
	}
}

func TestPolicyConfig_ComposerAuditAbandonedOverridesWhenAbandonedExplicitlyDisabled(t *testing.T) {
	clearPolicyEnv(t)
	util.PutEnv("COMPOSER_AUDIT_ABANDONED", "fail")
	if got := mustFromConfig(t, true, policyCfg("abandoned", false)).Abandoned.Audit; got != policy.AuditFail {
		t.Errorf("got %s", got)
	}
}

func TestPolicyConfig_WithIgnoreUnreachableOnlyAffectsRequestedScope(t *testing.T) {
	clearPolicyEnv(t)
	p := mustFromConfig(t, false, policyCfg("ignore-unreachable", php.ListOf("update")))
	if p.IgnoreUnreachable != (policy.IgnoreUnreachable{Update: true}) {
		t.Errorf("got %+v", p.IgnoreUnreachable)
	}
	updated, err := p.WithIgnoreUnreachable("audit")
	if err != nil {
		t.Fatal(err)
	}
	if updated.IgnoreUnreachable != (policy.IgnoreUnreachable{Audit: true, Update: true}) {
		t.Errorf("got %+v", updated.IgnoreUnreachable)
	}
}
