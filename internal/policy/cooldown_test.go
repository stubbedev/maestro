package policy

import (
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// Ports tests/Composer/Test/Policy/CooldownPolicyConfigTest.php.

// clearCooldownEnv clears the cooldown variables before and after a test:
// the tests read the environment, so none runs in parallel.
func clearCooldownEnv(t *testing.T) {
	t.Helper()
	clear := func() {
		util.ClearEnv("COMPOSER_POLICY_COOLDOWN_PERIOD")
		util.ClearEnv("COMPOSER_POLICY_COOLDOWN_BLOCK")
	}
	clear()
	t.Cleanup(clear)
}

func TestCooldownPolicyConfig_ParseDurationNull(t *testing.T) {
	for _, duration := range []any{nil, "", int64(0), "0"} {
		if got, err := ParseDuration(duration); err != nil || got != nil {
			t.Errorf("%v: got %v, %v", duration, got, err)
		}
	}
}

func TestCooldownPolicyConfig_ParseDurationInteger(t *testing.T) {
	for _, c := range []struct {
		duration any
		want     int64
	}{{int64(3600), 3600}, {"86400", 86400}} {
		got, err := ParseDuration(c.duration)
		if err != nil || got == nil || *got != c.want {
			t.Errorf("%v: got %v, %v", c.duration, got, err)
		}
	}
}

func TestCooldownPolicyConfig_ParseDurationHumanReadable(t *testing.T) {
	for _, c := range []struct {
		duration string
		want     int64
	}{
		{"90 seconds", 90},
		{"1 minute", 60},
		{"30 minutes", 1800},
		{"1 hour", 3600},
		{"1 day", 86400},
		{"7 days", 604800},
		{"1 week", 604800},
		{"2 weeks", 1209600},
		{"1week", 604800},
	} {
		got, err := ParseDuration(c.duration)
		if err != nil || got == nil || *got != c.want {
			t.Errorf("%q: got %v, %v", c.duration, got, err)
		}
	}
}

func TestCooldownPolicyConfig_ParseDurationInvalidFormatThrows(t *testing.T) {
	_, err := ParseDuration("not a duration")
	if err == nil || !strings.Contains(err.Error(), "Invalid policy.cooldown.period format") {
		t.Errorf("got %v", err)
	}
}

func TestCooldownPolicyConfig_ParseDurationInvalidFormatProviderThrows(t *testing.T) {
	for name, duration := range map[string]string{
		"relative tomorrow":      "tomorrow",
		"relative next week":     "next week",
		"relative next month":    "next month",
		"relative today":         "today",
		"relative midnight":      "midnight",
		"unsupported unit month": "1 month",
		"unsupported unit year":  "1 year",
		"past relative":          "2 days ago",
		"trailing words":         "7 days extra",
		"fractional seconds":     "1.5",
		"capitalized unit":       "7 Days",
		"uppercase unit":         "1 WEEK",
		"surrounding whitespace": "  2 hours  ",
		"whitespace only":        "   ",
		"trailing newline":       "7 days\n",
	} {
		_, err := ParseDuration(duration)
		if err == nil || !strings.Contains(err.Error(), "Invalid policy.cooldown.period format") {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestCooldownPolicyConfig_ParseDurationNegativeThrows(t *testing.T) {
	for name, duration := range map[string]any{"negative int": int64(-3600), "negative numeric string": "-3600"} {
		_, err := ParseDuration(duration)
		if err == nil || !strings.Contains(err.Error(), "cannot be negative") {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestCooldownPolicyConfig_DefaultIsInactiveWithoutAge(t *testing.T) {
	for name, policyConfig := range map[string]*php.Array{
		"no cooldown key": php.NewArray(),
		"cooldown false":  php.ArrayOf("cooldown", false),
		"cooldown empty":  php.ArrayOf("cooldown", php.NewArray()),
		"cooldown true":   php.ArrayOf("cooldown", true),
	} {
		cooldown := must(CooldownPolicyConfigFromRawConfig(policyConfig))
		if cooldown.Period != nil || cooldown.HasCooldown() {
			t.Errorf("%s: got %+v", name, cooldown)
		}
		// block defaults to true, but with no period configured the policy is inert
		if cooldown.ShouldBlock(BlockScopeUpdate) {
			t.Errorf("%s: blocking", name)
		}
	}
}

func TestCooldownPolicyConfig_FromRawConfig(t *testing.T) {
	raw := php.ArrayOf("cooldown", php.ArrayOf(
		"period", "7 days",
		"block", true,
		"audit", "report",
		"ignore", php.ArrayOf(
			"acme/pkg", "pinned by us",
			"acme/pkg2", php.ArrayOf("constraint", "1.0"),
		),
	))
	AssertDumpEqual(t,
		NewCooldownPolicyConfig(true, AuditReport, ignoreMap(
			"acme/pkg", rule("acme/pkg", all(), new("pinned by us"), true, true),
			"acme/pkg2", rule("acme/pkg2", constraint(t, "1.0"), nil, true, true),
		), new(int64(604800))),
		must(CooldownPolicyConfigFromRawConfig(raw)))
}

func TestCooldownPolicyConfig_IntegerAge(t *testing.T) {
	cooldown := must(CooldownPolicyConfigFromRawConfig(php.ArrayOf("cooldown", php.ArrayOf("period", int64(3600)))))
	if cooldown.Period == nil || *cooldown.Period != 3600 || !cooldown.HasCooldown() {
		t.Errorf("got %+v", cooldown)
	}
}

func TestCooldownPolicyConfig_ShouldBlock(t *testing.T) {
	active := NewCooldownPolicyConfig(true, AuditIgnore, nil, new(int64(604800)))
	if !active.ShouldBlock(BlockScopeUpdate) {
		t.Error("not blocking update")
	}
	// cooldown is update/require-only, never blocks at install scope
	if active.ShouldBlock(BlockScopeInstall) {
		t.Error("blocking install")
	}

	blockOff := NewCooldownPolicyConfig(false, AuditIgnore, nil, new(int64(604800)))
	if blockOff.ShouldBlock(BlockScopeUpdate) {
		t.Error("blocking with block off")
	}

	noAge := NewCooldownPolicyConfig(true, AuditIgnore, nil, nil)
	if noAge.ShouldBlock(BlockScopeUpdate) {
		t.Error("blocking without a period")
	}
}

func TestCooldownPolicyConfig_FormatTimeUntilAvailable(t *testing.T) {
	cooldown := NewCooldownPolicyConfig(true, AuditIgnore, nil, new(int64(604800)))
	now := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	at := func(month time.Month, day, hour, minute, second int) time.Time {
		return time.Date(2026, month, day, hour, minute, second, 0, time.UTC)
	}

	for _, c := range []struct {
		date  time.Time
		short bool
		want  string
	}{
		{at(1, 6, 4, 0, 0), false, "3 days 4 hours"},
		{at(1, 6, 4, 0, 0), true, "3d 4h"},
		{at(1, 4, 0, 0, 0), false, "1 day"},
		{at(1, 4, 0, 0, 0), true, "1d"},
		{at(1, 3, 0, 5, 0), true, "5m"},
		{at(1, 3, 0, 0, 30), true, "<1m"},
		{at(1, 3, 0, 0, 30), false, "less than a minute"},
	} {
		if got := cooldown.FormatTimeUntilAvailable(c.date, now, c.short); got != c.want {
			t.Errorf("%v short=%v: got %q, want %q", c.date, c.short, got, c.want)
		}
	}
}

func TestCooldownPolicyConfig_EnvOverridesAge(t *testing.T) {
	clearCooldownEnv(t)
	util.PutEnv("COMPOSER_POLICY_COOLDOWN_PERIOD", "2 days")

	cooldown := must(CooldownPolicyConfigFromRawConfig(php.ArrayOf("cooldown", php.ArrayOf(
		"period", "7 days",
		"ignore", php.ArrayOf("acme/pkg", "reason"),
	))))
	if cooldown.Period == nil || *cooldown.Period != 172800 {
		t.Errorf("got %+v", cooldown.Period)
	}
	// env overrides the duration but preserves the configured ignore rules
	if !cooldown.Ignore.Has("acme/pkg") {
		t.Error("ignore rule lost")
	}
}

func TestCooldownPolicyConfig_EnvDisablesCooldown(t *testing.T) {
	clearCooldownEnv(t)
	util.PutEnv("COMPOSER_POLICY_COOLDOWN_PERIOD", "0")

	cooldown := must(CooldownPolicyConfigFromRawConfig(php.ArrayOf("cooldown", php.ArrayOf("period", "7 days"))))
	if cooldown.Period != nil || cooldown.HasCooldown() {
		t.Errorf("got %+v", cooldown)
	}
}

func TestCooldownPolicyConfig_InvalidEnvValueThrowsWithEnvName(t *testing.T) {
	clearCooldownEnv(t)
	util.PutEnv("COMPOSER_POLICY_COOLDOWN_PERIOD", "not a duration")

	// the error must name the env var the user set, not the config key
	_, err := CooldownPolicyConfigFromRawConfig(php.NewArray())
	if err == nil || !strings.Contains(err.Error(), "Invalid value for COMPOSER_POLICY_COOLDOWN_PERIOD") {
		t.Errorf("got %v", err)
	}
}

func TestCooldownPolicyConfig_EnvAgeEnablesExplicitlyDisabledCooldown(t *testing.T) {
	clearCooldownEnv(t)
	util.PutEnv("COMPOSER_POLICY_COOLDOWN_PERIOD", "2 days")

	cooldown := must(CooldownPolicyConfigFromRawConfig(php.ArrayOf("cooldown", false)))
	if cooldown.Period == nil || *cooldown.Period != 172800 || !cooldown.Block {
		t.Errorf("got %+v", cooldown)
	}
}

func TestCooldownPolicyConfig_ZeroEnvAgeIsAllowedWhenCooldownDisabled(t *testing.T) {
	clearCooldownEnv(t)
	util.PutEnv("COMPOSER_POLICY_COOLDOWN_PERIOD", "0")

	if cooldown := must(CooldownPolicyConfigFromRawConfig(php.ArrayOf("cooldown", false))); cooldown.HasCooldown() {
		t.Error("has cooldown")
	}
}

func TestCooldownPolicyConfig_WithBlockingDisabled(t *testing.T) {
	cooldown := NewCooldownPolicyConfig(true, AuditIgnore, nil, new(int64(604800))).WithBlockingDisabled()
	if cooldown.Block || cooldown.Period == nil || *cooldown.Period != 604800 {
		t.Errorf("got %+v", cooldown)
	}
}
