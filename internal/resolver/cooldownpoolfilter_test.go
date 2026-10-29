package resolver

import (
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
)

// Ports tests/Composer/Test/DependencyResolver/CooldownPoolFilterTest.php.

func cooldownAt(t *testing.T, year int, month time.Month, day, hour, minute int) time.Time {
	t.Helper()

	return time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
}

func cooldownConfig(period int64, ignore *policy.IgnoreMap) *policy.CooldownPolicyConfig {
	return policy.NewCooldownPolicyConfig(true, policy.AuditIgnore, ignore, &period)
}

func cooldownPackage(t *testing.T, name, version, pretty string, released time.Time, published *time.Time) *pkg.Package {
	t.Helper()
	p := pkg.NewPackage(name, normalize(t, version), pretty)
	p.SetReleaseDate(released, true)
	if published != nil {
		p.SetPublishedDate(*published, true)
	}

	return p
}

func cooldownRemovalType(pool *Pool, packageName, version string) string {
	return removalType(pool, packageName, version)
}

func TestCooldownPoolFilter_FilterNewPackages(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)

	oldPackage := cooldownPackage(t, "vendor/pkg", "1.0.0", "1.0.0", cooldownAt(t, 2026, 1, 1, 12, 0), nil)
	newPackage := cooldownPackage(t, "vendor/pkg", "2.0.0", "2.0.0", cooldownAt(t, 2026, 1, 14, 12, 0), nil)

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{oldPackage, newPackage}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{oldPackage}, filteredPool.Packages())
	if got := cooldownRemovalType(filteredPool, "vendor/pkg", "2.0.0.0"); got != PolicyRemovalCooldown {
		t.Errorf("removal %q", got)
	}
}

func TestCooldownPoolFilter_AvailableInReportsTotalDaysForLongWaitsSpanningMultipleMonths(t *testing.T) {
	// 60-day cooldown; released 1 day ago -> available in 59 days
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(60*24*3600, nil), now)

	p := cooldownPackage(t, "vendor/pkg", "2.0.0", "2.0.0", cooldownAt(t, 2026, 1, 14, 12, 0), nil)

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{p}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	info := cooldownInfoOf(t, filteredPool, "vendor/pkg", "2.0.0.0")
	if info == nil || info.AvailableIn != "59 days" {
		t.Fatalf("info %+v", info)
	}
}

func cooldownInfoOf(t *testing.T, pool *Pool, packageName, version string) *CooldownRemovalInfo {
	t.Helper()
	reason := pool.PolicyRemovalReason(packageName, version)
	if reason == nil {
		return nil
	}

	return reason.Cooldown
}

func TestCooldownPoolFilter_PublishedDateTakesPrecedenceOverReleaseDate(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)

	// Author-controlled `time` claims the version is old enough, but the
	// server-set published date shows it is actually brand new -> withheld
	published := cooldownAt(t, 2026, 1, 14, 12, 0)
	p := cooldownPackage(t, "vendor/pkg", "2.0.0", "2.0.0", cooldownAt(t, 2025, 1, 1, 12, 0), &published)

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{p}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	if len(filteredPool.Packages()) != 0 {
		t.Fatalf("packages %v", filteredPool.Packages())
	}
	info := cooldownInfoOf(t, filteredPool, "vendor/pkg", "2.0.0.0")
	if info == nil || info.Source != "published-time" {
		t.Fatalf("info %+v", info)
	}
}

func TestCooldownPoolFilter_FallsBackToReleaseDateWhenNoPublishedDate(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)

	p := cooldownPackage(t, "vendor/pkg", "2.0.0", "2.0.0", cooldownAt(t, 2026, 1, 14, 12, 0), nil)

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{p}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	if len(filteredPool.Packages()) != 0 {
		t.Fatalf("packages %v", filteredPool.Packages())
	}
	info := cooldownInfoOf(t, filteredPool, "vendor/pkg", "2.0.0.0")
	if info == nil || info.Source != "time" {
		t.Fatalf("info %+v", info)
	}
}

func TestCooldownPoolFilter_IgnoredPackagesAreNotFiltered(t *testing.T) {
	ignore := &policy.IgnoreMap{}
	ignore.Set("internal/*", []*policy.IgnorePackageRule{
		policy.NewIgnorePackageRule("internal/*", semver.NewMatchAllConstraint(), nil, true, true),
	})
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, ignore), now)

	newPackage := cooldownPackage(t, "internal/pkg", "1.0.0", "1.0.0", cooldownAt(t, 2026, 1, 14, 12, 0), nil)

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{newPackage}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{newPackage}, filteredPool.Packages())
	if got := cooldownRemovalType(filteredPool, "internal/pkg", "1.0.0.0"); got != "" {
		t.Errorf("removal %q", got)
	}
}

func TestCooldownPoolFilter_IgnoreRuleDoesNotApplyToPackagesReplacingTheIgnoredName(t *testing.T) {
	ignore := &policy.IgnoreMap{}
	ignore.Set("internal/*", []*policy.IgnorePackageRule{
		policy.NewIgnorePackageRule("internal/*", semver.NewMatchAllConstraint(), nil, true, true),
	})
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, ignore), now)

	replacer := cooldownPackage(t, "evil/pkg", "2.0.0", "2.0.0", cooldownAt(t, 2026, 1, 14, 12, 0), nil)
	replacer.SetReplaces(pkg.LinksOf(pkg.NewLink("evil/pkg", "internal/pkg", semver.NewMatchAllConstraint(), pkg.TypeReplace, pkg.Str("*"))))

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{replacer}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	if len(filteredPool.Packages()) != 0 {
		t.Fatalf("packages %v", filteredPool.Packages())
	}
	if got := cooldownRemovalType(filteredPool, "evil/pkg", "2.0.0.0"); got != PolicyRemovalCooldown {
		t.Errorf("removal %q", got)
	}
}

func TestCooldownPoolFilter_DevVersionsAreNotFiltered(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)

	devPackage := cooldownPackage(t, "vendor/pkg", "dev-main", "dev-main", cooldownAt(t, 2026, 1, 14, 12, 0), nil)

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{devPackage}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{devPackage}, filteredPool.Packages())
}

func TestCooldownPoolFilter_PackagesWithoutReleaseDateAreNotFiltered(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)

	p := pkg.NewPackage("vendor/pkg", "1.0.0.0", "1.0.0")

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{p}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{p}, filteredPool.Packages())
}

func TestCooldownPoolFilter_DisabledConfigDoesNotFilter(t *testing.T) {
	config := policy.NewCooldownPolicyConfig(true, policy.AuditIgnore, nil, nil)
	filter := NewCooldownPoolFilter(config)

	newPackage := cooldownPackage(t, "vendor/pkg", "1.0.0", "1.0.0", time.Now(), nil)

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{newPackage}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{newPackage}, filteredPool.Packages())
}

func TestCooldownPoolFilter_ZeroConfigDoesNotFilter(t *testing.T) {
	zero := int64(0)
	config := policy.NewCooldownPolicyConfig(true, policy.AuditIgnore, nil, &zero)
	filter := NewCooldownPoolFilter(config)

	newPackage := cooldownPackage(t, "vendor/pkg", "1.0.0", "1.0.0", time.Now(), nil)

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{newPackage}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{newPackage}, filteredPool.Packages())
}

func TestCooldownPoolFilter_CooldownInfoIsStored(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)

	newPackage := cooldownPackage(t, "vendor/pkg", "2.0.0", "2.0.0", cooldownAt(t, 2026, 1, 14, 12, 0), nil)

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{newPackage}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	if len(filteredPool.Packages()) != 0 {
		t.Fatalf("packages %v", filteredPool.Packages())
	}

	info := cooldownInfoOf(t, filteredPool, "vendor/pkg", "2.0.0.0")
	if info == nil {
		t.Fatal("no cooldown info")
	}
	reason := filteredPool.PolicyRemovalReason("vendor/pkg", "2.0.0.0")
	if reason == nil || reason.PrettyVersion != "2.0.0" {
		t.Fatalf("reason %+v", reason)
	}
	if info.ReleaseDate == "" || info.AvailableIn == "" {
		t.Fatalf("info %+v", info)
	}
}

func TestCooldownPoolFilter_OldEnoughPackagesAreKept(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(24*3600, nil), now)

	oldEnoughPackage := cooldownPackage(t, "vendor/pkg", "1.0.0", "1.0.0", cooldownAt(t, 2026, 1, 13, 12, 0), nil)

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{oldEnoughPackage}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{oldEnoughPackage}, filteredPool.Packages())
}

func TestCooldownPoolFilter_ExactlyAtCutoffIsNotFiltered(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(24*3600, nil), now)

	// Package released exactly 24 hours ago is old enough and must be kept
	exactCutoffPackage := cooldownPackage(t, "vendor/pkg", "1.0.0", "1.0.0", cooldownAt(t, 2026, 1, 14, 12, 0), nil)

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{exactCutoffPackage}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{exactCutoffPackage}, filteredPool.Packages())
}

func TestCooldownPoolFilter_RootPackageNotFiltered(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)

	rootPackage := pkg.NewRootPackage("my/project", "1.0.0.0", "1.0.0")
	rootPackage.SetReleaseDate(cooldownAt(t, 2026, 1, 14, 12, 0), true)

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{rootPackage}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{rootPackage}, filteredPool.Packages())
}

func TestCooldownPoolFilter_PlatformPackagesNotFiltered(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)

	phpPackage := cooldownPackage(t, "php", "8.3.0", "8.3.0", cooldownAt(t, 2026, 1, 14, 12, 0), nil)
	extPackage := cooldownPackage(t, "ext-json", "8.3.0", "8.3.0", cooldownAt(t, 2026, 1, 14, 12, 0), nil)
	libPackage := cooldownPackage(t, "lib-openssl", "3.0.0", "3.0.0", cooldownAt(t, 2026, 1, 14, 12, 0), nil)

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{phpPackage, extPackage, libPackage}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	if len(filteredPool.Packages()) != 3 {
		t.Fatalf("packages %v", filteredPool.Packages())
	}
}

func TestCooldownPoolFilter_LockedPackagesNotFiltered(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)

	lockedPackage := cooldownPackage(t, "vendor/pkg", "1.0.0", "1.0.0", cooldownAt(t, 2026, 1, 14, 12, 0), nil)

	request := NewRequest(nil)
	request.LockPackage(lockedPackage)

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{lockedPackage}, nil, nil), request)
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{lockedPackage}, filteredPool.Packages())
}

func TestCooldownPoolFilter_PackageWithMultipleNamesTrackedCorrectly(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)

	newPackage := cooldownPackage(t, "vendor/pkg", "2.0.0", "2.0.0", cooldownAt(t, 2026, 1, 14, 12, 0), nil)
	newPackage.SetReplaces(pkg.LinksOf(pkg.NewLink("vendor/pkg", "vendor/replaced", semver.NewConstraintOp(semver.OpEQ, "2.0.0.0"), pkg.TypeReplace, pkg.Str("2.0.0"))))

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{newPackage}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	if len(filteredPool.Packages()) != 0 {
		t.Fatalf("packages %v", filteredPool.Packages())
	}

	// Replaced names are part of Names(false), so a requirement on either
	// name explains the cooldown
	if got := cooldownRemovalType(filteredPool, "vendor/pkg", "2.0.0.0"); got != PolicyRemovalCooldown {
		t.Errorf("removal %q", got)
	}
	if got := cooldownRemovalType(filteredPool, "vendor/replaced", "2.0.0.0"); got != PolicyRemovalCooldown {
		t.Errorf("removal %q", got)
	}
	// while the primary name is kept so output can list the package once
	reason := filteredPool.PolicyRemovalReason("vendor/replaced", "2.0.0.0")
	if reason == nil || reason.Cooldown == nil || reason.PackageName != "vendor/pkg" {
		t.Fatalf("reason %+v", reason)
	}
}

func TestCooldownPoolFilter_ReplacerDoesNotHideTheReasonOfThePackageItReplaces(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)

	replacer := cooldownPackage(t, "vendor/pkg", "2.0.0", "2.0.0", cooldownAt(t, 2026, 1, 14, 12, 0), nil)
	replacer.SetReplaces(pkg.LinksOf(pkg.NewLink("vendor/pkg", "vendor/replaced", semver.NewConstraintOp(semver.OpEQ, "2.0.0.0"), pkg.TypeReplace, pkg.Str("2.0.0"))))

	// an earlier filter removed vendor/replaced 2.0.0 itself
	removed := &repository.NameMap[*repository.NameMap[*PolicyRemovalReason]]{}
	versions := &repository.NameMap[*PolicyRemovalReason]{}
	versions.Set("2.0.0.0", NewFilterListRemovalReason("vendor/replaced", "2.0.0", []*repository.FilterListEntry{
		{PackageName: "vendor/replaced", Constraint: semver.NewMatchAllConstraint(), ListName: "malware"},
	}))
	removed.Set("vendor/replaced", versions)

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{replacer}, nil, &Removed{Policy: removed}), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	if got := cooldownRemovalType(filteredPool, "vendor/pkg", "2.0.0.0"); got != PolicyRemovalCooldown {
		t.Errorf("removal %q", got)
	}
	if got := cooldownRemovalType(filteredPool, "vendor/replaced", "2.0.0.0"); got != PolicyRemovalFilterList {
		t.Errorf("removal %q", got)
	}
}

func TestCooldownPoolFilter_DevAliasIsKeptTogetherWithItsDevTarget(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)

	// "dev-main as 1.0.0": the alias reports a stable version while the
	// branch commit is recent
	branch := cooldownPackage(t, "vendor/pkg", "dev-main", "dev-main", cooldownAt(t, 2026, 1, 14, 12, 0), nil)
	alias := pkg.NewAliasPackage(branch, "1.0.0.0", "1.0.0")

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{branch, alias}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{branch, alias}, filteredPool.Packages())
}

func TestCooldownPoolFilter_AliasOfWithheldVersionIsWithheldToo(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)

	// "2.0.0 as 2.0.x-dev": the alias is a dev version but requires the
	// withheld base
	base := cooldownPackage(t, "vendor/pkg", "2.0.0", "2.0.0", cooldownAt(t, 2026, 1, 14, 12, 0), nil)
	alias := pkg.NewAliasPackage(base, "2.0.9999999.9999999-dev", "2.0.x-dev")

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{base, alias}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	if len(filteredPool.Packages()) != 0 {
		t.Fatalf("packages %v", filteredPool.Packages())
	}
	if got := cooldownRemovalType(filteredPool, "vendor/pkg", "2.0.0.0"); got != PolicyRemovalCooldown {
		t.Errorf("removal %q", got)
	}
	if got := cooldownRemovalType(filteredPool, "vendor/pkg", "2.0.9999999.9999999-dev"); got != PolicyRemovalCooldown {
		t.Errorf("removal %q", got)
	}
}

// cooldownComposerRepository is a Composer repository that has to vouch for
// the publication date.
type cooldownComposerRepository struct {
	name                  string
	requiresPublishedTime bool
}

func (r *cooldownComposerRepository) RepoName() string { return r.name }
func (r *cooldownComposerRepository) PHPClass() string {
	return `Composer\Repository\ComposerRepository`
}

func (r *cooldownComposerRepository) RequiresPublishedTime() bool {
	return r.requiresPublishedTime
}

func newCooldownComposerRepository(requiresPublishedTime bool) *cooldownComposerRepository {
	return &cooldownComposerRepository{name: "composer repo (https://repo.example.org)", requiresPublishedTime: requiresPublishedTime}
}

func TestCooldownPoolFilter_ComposerRepositoryVersionWithoutPublishedTimeIsRejected(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)

	// an old author-supplied time does not help, only the repository can
	// vouch for the age
	p := cooldownPackage(t, "vendor/pkg", "1.0.0", "1.0.0", cooldownAt(t, 2020, 1, 1, 0, 0), nil)
	if err := p.SetRepository(newCooldownComposerRepository(true)); err != nil {
		t.Fatal(err)
	}

	_, err := filter.Filter(NewPool([]pkg.PackageInterface{p}, nil, nil), NewRequest(nil))
	if err == nil || !strings.Contains(err.Error(), "vendor/pkg 1.0.0 from composer repo (https://repo.example.org) has no published-time") {
		t.Fatalf("got %v", err)
	}
}

func TestCooldownPoolFilter_ComposerRepositoryCanWaivePublishedTime(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)

	p := cooldownPackage(t, "vendor/pkg", "1.0.0", "1.0.0", cooldownAt(t, 2026, 1, 14, 12, 0), nil)
	if err := p.SetRepository(newCooldownComposerRepository(false)); err != nil {
		t.Fatal(err)
	}

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{p}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	// falls back to the author-supplied time, and says so
	if len(filteredPool.Packages()) != 0 {
		t.Fatalf("packages %v", filteredPool.Packages())
	}
	info := cooldownInfoOf(t, filteredPool, "vendor/pkg", "1.0.0.0")
	if info == nil || info.Source != "time" {
		t.Fatalf("info %+v", info)
	}
}

func TestCooldownPoolFilter_OtherRepositoriesNeedNoPublishedTime(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)

	p := cooldownPackage(t, "vendor/pkg", "1.0.0", "1.0.0", cooldownAt(t, 2020, 1, 1, 0, 0), nil)
	newArrayRepository(t, p)

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{p}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{p}, filteredPool.Packages())
}

func TestCooldownPoolFilter_DevAndIgnoredVersionsFromComposerRepositoriesNeedNoPublishedTime(t *testing.T) {
	ignore := &policy.IgnoreMap{}
	ignore.Set("internal/*", []*policy.IgnorePackageRule{
		policy.NewIgnorePackageRule("internal/*", semver.NewMatchAllConstraint(), nil, true, true),
	})
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, ignore), now)

	composerRepo := newCooldownComposerRepository(true)
	branch := pkg.NewPackage("vendor/pkg", "dev-main", "dev-main")
	if err := branch.SetRepository(composerRepo); err != nil {
		t.Fatal(err)
	}
	ignored := pkg.NewPackage("internal/pkg", "1.0.0.0", "1.0.0")
	if err := ignored.SetRepository(composerRepo); err != nil {
		t.Fatal(err)
	}

	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{branch, ignored}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{branch, ignored}, filteredPool.Packages())
}

func TestCooldownPoolFilter_UnverifiedVersionsAreGroupedByRepository(t *testing.T) {
	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)

	timeOnly := cooldownPackage(t, "vendor/a", "1.0.0", "1.0.0", cooldownAt(t, 2020, 1, 1, 0, 0), nil)
	repoA := newArrayRepository(t, timeOnly)

	noDate := pkg.NewPackage("vendor/b", "1.0.0.0", "1.0.0")
	published := cooldownAt(t, 2020, 1, 1, 0, 0)
	verified := cooldownPackage(t, "vendor/c", "1.0.0", "1.0.0", cooldownAt(t, 2020, 1, 1, 0, 0), &published)
	branch := pkg.NewPackage("vendor/d", "dev-main", "dev-main")
	repoB := newArrayRepository(t, noDate, verified, branch)

	if _, err := filter.Filter(NewPool([]pkg.PackageInterface{timeOnly, noDate, verified, branch}, nil, nil), NewRequest(nil)); err != nil {
		t.Fatal(err)
	}

	// dev versions and versions with a repository-provided date are not
	// listed
	want := &repository.NameMap[[]string]{}
	want.Set(repoA.RepoName(), []string{"vendor/a (1.0.0)"})
	want.Set(repoB.RepoName(), []string{"vendor/b (1.0.0)"})
	got := filter.UnverifiedVersions()
	if strings.Join(got.Keys(), "|") != strings.Join(want.Keys(), "|") {
		t.Fatalf("repositories %v, want %v", got.Keys(), want.Keys())
	}
	for _, name := range want.Keys() {
		wantVersions, _ := want.Get(name)
		gotVersions, _ := got.Get(name)
		if len(gotVersions) != len(wantVersions) || (len(wantVersions) > 0 && gotVersions[0] != wantVersions[0]) {
			t.Fatalf("%s: %v, want %v", name, gotVersions, wantVersions)
		}
	}
}

// Ports tests/Composer/Test/DependencyResolver/PoolBuilderCooldownOutputTest.php.

func newCooldownOutputPool(t *testing.T, verbosity int, publishedTimeProvided bool) *io.BufferIO {
	t.Helper()
	out, err := io.NewBufferIO("", verbosity, nil)
	if err != nil {
		t.Fatal(err)
	}

	now := cooldownAt(t, 2026, 1, 15, 12, 0)
	old := pkg.NewPackage("vendor/pkg", "1.0.0.0", "1.0.0")
	newPackage := pkg.NewPackage("vendor/pkg", "2.0.0.0", "2.0.0")
	newPackage.SetReplaces(pkg.LinksOf(pkg.NewLink("vendor/pkg", "vendor/replaced", semver.NewConstraintOp(semver.OpEQ, "2.0.0.0"), pkg.TypeReplace, pkg.Str("2.0.0"))))
	if publishedTimeProvided {
		old.SetPublishedDate(cooldownAt(t, 2025, 1, 1, 0, 0), true)
		newPackage.SetPublishedDate(cooldownAt(t, 2026, 1, 14, 12, 0), true)
	} else {
		old.SetReleaseDate(cooldownAt(t, 2025, 1, 1, 0, 0), true)
		newPackage.SetReleaseDate(cooldownAt(t, 2026, 1, 14, 12, 0), true)
	}

	repo := newArrayRepository(t, old, newPackage)
	set, err := repository.NewRepositorySet("stable", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.AddRepository(repo); err != nil {
		t.Fatal(err)
	}

	request := NewRequest(nil)
	if err := request.RequireName("vendor/pkg", semver.NewMatchAllConstraint()); err != nil {
		t.Fatal(err)
	}

	filter := NewCooldownPoolFilter(cooldownConfig(7*24*3600, nil), now)
	if _, err := CreatePool(set, request, out, CreatePoolOptions{CooldownPoolFilter: filter}); err != nil {
		t.Fatal(err)
	}

	return out
}

func TestPoolBuilder_WithheldSummaryShownAtDefaultVerbosityWithHint(t *testing.T) {
	output := newCooldownOutputPool(t, console.VerbosityNormal, true).Output()
	if !strings.Contains(output, "1 package version(s) withheld by the cooldown policy (run with -vv to list them).") {
		t.Errorf("output %q", output)
	}
	if strings.Contains(output, "vendor/pkg (2.0.0)") {
		t.Errorf("output %q", output)
	}
}

func TestPoolBuilder_WithheldVersionsListedAtVeryVerbose(t *testing.T) {
	output := newCooldownOutputPool(t, console.VerbosityVeryVerbose, true).Output()
	if !strings.Contains(output, "1 package version(s) withheld by the cooldown policy:") {
		t.Errorf("output %q", output)
	}
	if !strings.Contains(output, "  - vendor/pkg (2.0.0) published 2026-01-14T12:00:00+00:00") {
		t.Errorf("output %q", output)
	}
	// the withheld version is recorded under the replaced name too, but
	// listed once
	if strings.Count(output, "  - ") != 1 {
		t.Errorf("output %q", output)
	}
	if strings.Contains(output, "vendor/replaced") {
		t.Errorf("output %q", output)
	}
}

func TestPoolBuilder_NoWarningWhenTheRepositoryProvidesPublishedTime(t *testing.T) {
	output := newCooldownOutputPool(t, console.VerbosityNormal, true).Output()
	if strings.Contains(output, "could not be verified") {
		t.Errorf("output %q", output)
	}
}

func TestPoolBuilder_UnverifiedPublicationDatesAreReportedPerRepository(t *testing.T) {
	output := newCooldownOutputPool(t, console.VerbosityNormal, false).Output()
	if !strings.Contains(output, "The publication date of 2 package version(s) from array repo (defining 2 packages) could not be verified, the cooldown policy relied on the time field set by the package authors (run with -vv to list them).") {
		t.Errorf("output %q", output)
	}
	if strings.Contains(output, "  - vendor/pkg (1.0.0)") {
		t.Errorf("output %q", output)
	}
}

func TestPoolBuilder_UnverifiedPublicationDatesListedAtVeryVerbose(t *testing.T) {
	output := newCooldownOutputPool(t, console.VerbosityVeryVerbose, false).Output()
	if !strings.Contains(output, "could not be verified, the cooldown policy relied on the time field set by the package authors:") {
		t.Errorf("output %q", output)
	}
	if !strings.Contains(output, "  - vendor/pkg (1.0.0)") || !strings.Contains(output, "  - vendor/pkg (2.0.0)") {
		t.Errorf("output %q", output)
	}
}
