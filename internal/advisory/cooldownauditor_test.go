// Ports tests/Composer/Test/Advisory/CooldownAuditorTest.php (MIT,
// testdata/LICENSE-composer).

package advisory_test

import (
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/semver"
)

const cooldownNow = "2026-01-15 12:00:00"

func cooldownPackage(t *testing.T, name, version, releaseDate string, publishedDate string) *pkg.Package {
	t.Helper()

	p := pkg.NewPackage(name, version+".0", version)
	if releaseDate != "" {
		d, err := time.Parse(time.DateTime, releaseDate)
		if err != nil {
			t.Fatal(err)
		}
		p.SetReleaseDate(d, true)
	}
	if publishedDate != "" {
		d, err := time.Parse(time.DateTime, publishedDate)
		if err != nil {
			t.Fatal(err)
		}
		p.SetPublishedDate(d, true)
	}

	return p
}

// cooldownPolicy is CooldownAuditorTest::cooldown().
func cooldownPolicy(audit string, period int64, ignore *policy.IgnoreMap) *policy.CooldownPolicyConfig {
	var p *int64
	if period != 0 {
		p = &period
	}

	return policy.NewCooldownPolicyConfig(true, audit, ignore, p)
}

func cooldownAuditor(t *testing.T) *advisory.CooldownAuditor {
	t.Helper()

	now, err := time.Parse(time.DateTime, cooldownNow)
	if err != nil {
		t.Fatal(err)
	}

	return &advisory.CooldownAuditor{Now: now}
}

func TestCooldownAuditor_CollectsOnlyVersionsWithinCooldown(t *testing.T) {
	old := cooldownPackage(t, "vendor/old", "1.0.0", "2026-01-01 12:00:00", "")       // 14 days old -> cleared
	recent := cooldownPackage(t, "vendor/recent", "2.0.0", "2026-01-14 12:00:00", "") // 1 day old -> within

	result := cooldownAuditor(t).Collect([]pkg.PackageInterface{old, recent}, cooldownPolicy(policy.AuditReport, 604800, nil))

	if _, ok := result.Get("vendor/old"); ok {
		t.Error("vendor/old is reported")
	}
	info, ok := result.GetArray("vendor/recent")
	if !ok {
		t.Fatal("vendor/recent is not reported")
	}
	if prettyVersion, _ := info.GetString("prettyVersion"); prettyVersion != "2.0.0" {
		t.Errorf("prettyVersion = %q", prettyVersion)
	}
	if source, _ := info.GetString("source"); source != "time" {
		t.Errorf("source = %q", source)
	}
	if availableIn, _ := info.GetString("availableIn"); availableIn == "" {
		t.Error("availableIn is empty")
	}
}

func TestCooldownAuditor_PrefersPublishedDateOverReleaseDate(t *testing.T) {
	// author-controlled release date is old, but the server publication date is recent
	p := cooldownPackage(t, "vendor/pkg", "1.0.0", "2025-01-01 12:00:00", "2026-01-14 12:00:00")

	result := cooldownAuditor(t).Collect([]pkg.PackageInterface{p}, cooldownPolicy(policy.AuditReport, 604800, nil))

	info, ok := result.GetArray("vendor/pkg")
	if !ok {
		t.Fatal("vendor/pkg is not reported")
	}
	if source, _ := info.GetString("source"); source != "published-time" {
		t.Errorf("source = %q", source)
	}
	published, _ := time.Parse(time.DateTime, "2026-01-14 12:00:00")
	if releaseDate, _ := info.GetString("releaseDate"); releaseDate != published.Format(dumper.RFC3339) {
		t.Errorf("releaseDate = %q", releaseDate)
	}
}

func TestCooldownAuditor_SkipsDevPlatformRootAndDatelessPackages(t *testing.T) {
	dev := cooldownPackage(t, "vendor/dev", "dev-main", cooldownNow, "")
	platform := cooldownPackage(t, "php", "8.3.0", cooldownNow, "")
	dateless := cooldownPackage(t, "vendor/nodate", "1.0.0", "", "")
	root := pkg.NewRootPackage("vendor/root", "1.0.0.0", "1.0.0")
	now, err := time.Parse(time.DateTime, cooldownNow)
	if err != nil {
		t.Fatal(err)
	}
	root.SetReleaseDate(now, true)

	result := cooldownAuditor(t).Collect([]pkg.PackageInterface{dev, platform, dateless, root}, cooldownPolicy(policy.AuditReport, 604800, nil))

	if result.Len() != 0 {
		t.Errorf("result = %v", result.All())
	}
}

func TestCooldownAuditor_RespectsIgnoreRules(t *testing.T) {
	recent := cooldownPackage(t, "vendor/recent", "2.0.0", "2026-01-14 12:00:00", "")
	ignored := cooldownPackage(t, "vendor/ignored", "2.0.0", "2026-01-14 12:00:00", "")

	ignore, err := policy.ParseIgnoreMap(php.ArrayOf("vendor/ignored", "pinned by us"))
	if err != nil {
		t.Fatal(err)
	}
	result := cooldownAuditor(t).Collect([]pkg.PackageInterface{recent, ignored}, cooldownPolicy(policy.AuditReport, 604800, ignore))

	if _, ok := result.Get("vendor/recent"); !ok {
		t.Error("vendor/recent is not reported")
	}
	if _, ok := result.Get("vendor/ignored"); ok {
		t.Error("vendor/ignored is reported")
	}
}

func TestCooldownAuditor_IgnoreRuleScopedToBlockOnlyDoesNotApplyToAudit(t *testing.T) {
	p := cooldownPackage(t, "vendor/pkg", "2.0.0", "2026-01-14 12:00:00", "")

	// on-audit=false means the rule only suppresses blocking, not auditing
	ignore := &policy.IgnoreMap{}
	ignore.Set("vendor/pkg", []*policy.IgnorePackageRule{policy.NewIgnorePackageRule("vendor/pkg", semver.NewMatchAllConstraint(), nil, true, false)})
	result := cooldownAuditor(t).Collect([]pkg.PackageInterface{p}, cooldownPolicy(policy.AuditReport, 604800, ignore))

	if _, ok := result.Get("vendor/pkg"); !ok {
		t.Error("vendor/pkg is not reported")
	}
}

func TestCooldownAuditor_ResolvesAliasesToAliasedPackage(t *testing.T) {
	dev := pkg.NewPackage("vendor/dev", "dev-main", "dev-main")
	d, err := time.Parse(time.DateTime, "2026-01-14 12:00:00")
	if err != nil {
		t.Fatal(err)
	}
	dev.SetReleaseDate(d, true)
	stable := cooldownPackage(t, "vendor/stable", "1.2.0", "2026-01-14 12:00:00", "")

	result := cooldownAuditor(t).Collect([]pkg.PackageInterface{
		pkg.NewAliasPackage(dev, "1.0.0.0", "1.0.0"),
		pkg.NewAliasPackage(stable, "1.0.0.0", "1.0.0"),
	}, cooldownPolicy(policy.AuditReport, 604800, nil))

	if _, ok := result.Get("vendor/dev"); ok {
		t.Error("vendor/dev is reported")
	}
	info, ok := result.GetArray("vendor/stable")
	if !ok {
		t.Fatal("vendor/stable is not reported")
	}
	if prettyVersion, _ := info.GetString("prettyVersion"); prettyVersion != "1.2.0" {
		t.Errorf("prettyVersion = %q", prettyVersion)
	}
}

func TestCooldownAuditor_ReturnsEmptyWhenNoCooldownConfigured(t *testing.T) {
	recent := cooldownPackage(t, "vendor/recent", "2.0.0", "2026-01-14 12:00:00", "")

	if result := cooldownAuditor(t).Collect([]pkg.PackageInterface{recent}, cooldownPolicy(policy.AuditReport, 0, nil)); result.Len() != 0 {
		t.Errorf("result = %v", result.All())
	}
}
