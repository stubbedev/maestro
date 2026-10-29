// Ports tests/Composer/Test/Package/Version/VersionSelectorTest.php.

package version_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/internal/pkgtest"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// repositorySetMock returns the configured packages for every call.
type repositorySetMock struct {
	t        *testing.T
	results  [][]pkg.PackageInterface
	byName   map[string][]pkg.PackageInterface
	calls    int
	wantName string
}

func (m *repositorySetMock) FindPackages(name string, constraint semver.ConstraintInterface, _ int) ([]pkg.PackageInterface, error) {
	if m.wantName != "" && name != m.wantName {
		m.t.Errorf("findPackages(%q)", name)
	}

	if constraint != nil {
		m.t.Errorf("findPackages with constraint %v", constraint)
	}

	if m.byName != nil {
		return m.byName[name], nil
	}

	result := m.results[min(m.calls, len(m.results)-1)]
	m.calls++

	return result, nil
}

func testPackage(t *testing.T, name, prettyVersion string) *pkg.CompletePackage {
	t.Helper()

	normalized, err := pkg.NewVersionParser().Normalize(prettyVersion)
	if err != nil {
		t.Fatal(err)
	}

	return pkg.NewCompletePackage(name, normalized, prettyVersion)
}

func requireLink(t *testing.T, source, target, constraint string) pkg.Links {
	t.Helper()

	c, err := pkg.NewVersionParser().ParseConstraints(constraint)
	if err != nil {
		t.Fatal(err)
	}

	return pkg.LinksOf(pkg.NewLink(source, target, c, pkg.TypeRequire, pkg.Str(constraint)))
}

func platformPackage(t *testing.T, name, prettyVersion string) pkg.PackageInterface {
	t.Helper()

	return testPackage(t, name, prettyVersion)
}

type ignoreAll struct{}

func (ignoreAll) IsIgnored(string) bool           { return true }
func (ignoreAll) IgnoresAllPlatformRequirements() {}

func best(t *testing.T, s *version.VersionSelector, name string, opts version.FindBestCandidateOptions) pkg.PackageInterface {
	t.Helper()

	p, err := s.FindBestCandidate(name, opts)
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func TestVersionSelector_LatestVersionIsReturned(t *testing.T) {
	p1, p2, p3 := testPackage(t, "foo/bar", "1.2.1"), testPackage(t, "foo/bar", "1.2.2"), testPackage(t, "foo/bar", "1.2.0")
	repo := &repositorySetMock{t: t, wantName: "foo/bar", results: [][]pkg.PackageInterface{{p1, p2, p3}}}

	// 1.2.2 should be returned because it's the latest of the returned versions
	if got := best(t, version.NewVersionSelector(repo, nil), "foo/bar", version.FindBestCandidateOptions{}); got != pkg.PackageInterface(p2) {
		t.Errorf("got %v", got)
	}
}

func TestVersionSelector_LatestVersionIsReturnedThatMatchesPhpRequirements(t *testing.T) {
	packages := []pkg.PackageInterface{}
	for _, c := range [][2]string{{"0.9.0", ">=5.6"}, {"1.0.0", ">=5.4"}, {"2.0.0", ">=5.6"}, {"2.1.0", ">=5.6"}} {
		p := testPackage(t, "foo/bar", c[0])
		p.SetRequires(requireLink(t, "foo/bar", "php", c[1]))
		packages = append(packages, p)
	}

	repo := &repositorySetMock{t: t, wantName: "foo/bar", results: [][]pkg.PackageInterface{packages}}
	s := version.NewVersionSelector(repo, []pkg.PackageInterface{platformPackage(t, "php", "5.5.0")})

	bufferIO := func(verbosity int) *io.BufferIO {
		b, err := io.NewBufferIO("", verbosity, nil)
		if err != nil {
			t.Fatal(err)
		}

		return b
	}

	out := bufferIO(32)
	if got := best(t, s, "foo/bar", version.FindBestCandidateOptions{IO: out}); got.String() != packages[1].String() {
		t.Errorf("got %v, want 1.0.0", got)
	}

	if rejected := s.PlatformRejectedCandidates(); len(rejected) != 2 || rejected[0] != packages[3] || rejected[1] != packages[2] {
		t.Errorf("platform rejected %v, want 2.1.0 and 2.0.0", rejected)
	}

	if want := "<warning>Cannot use foo/bar's latest version 2.1.0 as it requires php >=5.6 which is not satisfied by your platform.\n"; php.NormalizeEOL(out.Output()) != want {
		t.Errorf("output %q, want %q", php.NormalizeEOL(out.Output()), want)
	}

	out = bufferIO(64)
	if got := best(t, s, "foo/bar", version.FindBestCandidateOptions{IO: out}); got.String() != packages[1].String() {
		t.Errorf("got %v, want 1.0.0", got)
	}

	if want := "<warning>Cannot use foo/bar's latest version 2.1.0 as it requires php >=5.6 which is not satisfied by your platform.\n" +
		"<warning>Cannot use foo/bar 2.0.0 as it requires php >=5.6 which is not satisfied by your platform.\n"; php.NormalizeEOL(out.Output()) != want {
		t.Errorf("output %q, want %q", php.NormalizeEOL(out.Output()), want)
	}

	if got := best(t, s, "foo/bar", version.FindBestCandidateOptions{PlatformRequirementFilter: ignoreAll{}}); got.String() != packages[3].String() {
		t.Errorf("got %v, want 2.1.0", got)
	}

	if rejected := s.PlatformRejectedCandidates(); len(rejected) != 0 {
		t.Errorf("platform rejected %v, want none", rejected)
	}
}

func checkPlatformSelection(t *testing.T, platform []pkg.PackageInterface, packages []pkg.PackageInterface, wantDefault, wantIgnoreAll pkg.PackageInterface) {
	t.Helper()

	repo := &repositorySetMock{t: t, wantName: "foo/bar", results: [][]pkg.PackageInterface{packages}}
	s := version.NewVersionSelector(repo, platform)

	if got := best(t, s, "foo/bar", version.FindBestCandidateOptions{}); got != wantDefault {
		t.Errorf("got %v, want %v", got, wantDefault)
	}

	if got := best(t, s, "foo/bar", version.FindBestCandidateOptions{PlatformRequirementFilter: ignoreAll{}}); got != wantIgnoreAll {
		t.Errorf("ignoring platform reqs: got %v, want %v", got, wantIgnoreAll)
	}
}

func TestVersionSelector_LatestVersionIsReturnedThatMatchesExtRequirements(t *testing.T) {
	p1, p2 := testPackage(t, "foo/bar", "1.0.0"), testPackage(t, "foo/bar", "2.0.0")
	p1.SetRequires(requireLink(t, "foo/bar", "ext-zip", "^5.2"))
	p2.SetRequires(requireLink(t, "foo/bar", "ext-zip", "^5.4"))

	checkPlatformSelection(t, []pkg.PackageInterface{platformPackage(t, "ext-zip", "5.3.0")}, []pkg.PackageInterface{p1, p2}, p1, p2)
}

func TestVersionSelector_LatestVersionIsReturnedThatMatchesPlatformExt(t *testing.T) {
	p1, p2 := testPackage(t, "foo/bar", "1.0.0"), testPackage(t, "foo/bar", "2.0.0")
	p2.SetRequires(requireLink(t, "foo/bar", "ext-barfoo", "*"))

	// a PlatformRepository always has packages (php, composer, ...)
	checkPlatformSelection(t, []pkg.PackageInterface{platformPackage(t, "php", "8.4.0")}, []pkg.PackageInterface{p1, p2}, p1, p2)
}

func TestVersionSelector_LatestVersionIsReturnedThatMatchesComposerRequirements(t *testing.T) {
	p1, p2 := testPackage(t, "foo/bar", "1.0.0"), testPackage(t, "foo/bar", "1.1.0")
	p1.SetRequires(requireLink(t, "foo/bar", "composer-runtime-api", "^1.0"))
	p2.SetRequires(requireLink(t, "foo/bar", "composer-runtime-api", "^2.0"))

	checkPlatformSelection(t, []pkg.PackageInterface{platformPackage(t, "composer-runtime-api", "1.0.0")}, []pkg.PackageInterface{p1, p2}, p1, p2)
}

func TestVersionSelector_StabilitySelection(t *testing.T) {
	for _, c := range []struct {
		name      string
		versions  []string
		stability string
		want      int
	}{
		{"testMostStableVersionIsReturned", []string{"1.0.0", "1.1.0-beta"}, "", 0},
		{"testHighestVersionIsReturned", []string{"1.0.0", "1.1.0-beta"}, "dev", 1},
		{"testHighestVersionMatchingStabilityIsReturned", []string{"1.0.0", "1.1.0-beta", "1.2.0-alpha"}, "beta", 1},
		{"testMostStableUnstableVersionIsReturned", []string{"1.1.0-beta", "1.2.0-alpha"}, "stable", 0},
	} {
		var packages []pkg.PackageInterface
		for _, v := range c.versions {
			packages = append(packages, testPackage(t, "foo/bar", v))
		}

		repo := &repositorySetMock{t: t, wantName: "foo/bar", results: [][]pkg.PackageInterface{packages}}

		got := best(t, version.NewVersionSelector(repo, nil), "foo/bar", version.FindBestCandidateOptions{PreferredStability: c.stability})
		if got != packages[c.want] {
			t.Errorf("%s: got %v, want %v", c.name, got, packages[c.want])
		}
	}
}

func TestVersionSelector_MostStableVersionIsReturnedRegardlessOfOrder(t *testing.T) {
	p1, p2 := testPackage(t, "foo/bar", "2.x-dev"), testPackage(t, "foo/bar", "2.0.0-beta3")
	repo := &repositorySetMock{t: t, wantName: "foo/bar", results: [][]pkg.PackageInterface{{p1, p2}, {p2, p1}}}
	s := version.NewVersionSelector(repo, nil)

	for range 2 {
		// Expecting 2.0.0-beta3, cause beta is more stable than dev
		if got := best(t, s, "foo/bar", version.FindBestCandidateOptions{}); got != pkg.PackageInterface(p2) {
			t.Errorf("got %v", got)
		}
	}
}

func TestVersionSelector_DefaultBranchAliasIsNeverReturned(t *testing.T) {
	p := testPackage(t, "foo/bar", "1.1.0-beta")
	p2 := testPackage(t, "foo/bar", "dev-main")
	alias := pkg.NewAliasPackage(p2, pkg.DefaultBranchAlias, pkg.DefaultBranchAlias)
	repo := &repositorySetMock{t: t, wantName: "foo/bar", results: [][]pkg.PackageInterface{{p, alias}}}

	if got := best(t, version.NewVersionSelector(repo, nil), "foo/bar", version.FindBestCandidateOptions{PreferredStability: "dev"}); got != pkg.PackageInterface(p2) {
		t.Errorf("got %v", got)
	}
}

func TestVersionSelector_FalseReturnedOnNoPackages(t *testing.T) {
	repo := &repositorySetMock{t: t, results: [][]pkg.PackageInterface{{}}}

	if got := best(t, version.NewVersionSelector(repo, nil), "foobaz", version.FindBestCandidateOptions{}); got != nil {
		t.Errorf("got %v", got)
	}
}

// cooldownMock is Composer's CooldownPolicyConfig as the selector reads it:
// a period in seconds, the block flag and the ignore rules of the block
// scope.
type cooldownMock struct {
	period int64
	block  bool
	ignore map[string][]cooldownRule
}

type cooldownRule struct {
	name       string
	constraint semver.ConstraintInterface
}

func cooldown(period int64) *cooldownMock {
	return &cooldownMock{period: period, block: true}
}

func (c *cooldownMock) HasCooldown() bool { return c.period > 0 }

func (c *cooldownMock) Blocks() bool { return c.block }

func (c *cooldownMock) IsIgnored(p pkg.PackageInterface, operation string) bool {
	if operation != "block" {
		return false
	}

	version := semver.NewConstraintOp(semver.OpEQ, p.Version())
	for _, rule := range c.ignore[p.Name()] {
		if rule.name == p.Name() && rule.constraint.Matches(version) {
			return true
		}
	}

	return false
}

func (c *cooldownMock) AssertPublishedTimeProvided(p pkg.PackageInterface) error {
	if _, ok := p.PublishedDate(); ok {
		return nil
	}

	repo, ok := p.Repository().(interface{ RequiresPublishedTime() bool })
	if !ok || !repo.RequiresPublishedTime() {
		return nil
	}

	return &util.RuntimeError{Message: p.PrettyName() + " " + p.PrettyVersion() + " from " + p.Repository().RepoName() +
		" has no published-time, so the cooldown policy configured in \"policy.cooldown\" cannot tell whether it is new or old."}
}

func (c *cooldownMock) EffectiveDate(p pkg.PackageInterface) (time.Time, bool) {
	if published, ok := p.PublishedDate(); ok {
		return published, true
	}

	return p.ReleaseDate()
}

func (c *cooldownMock) IsWithinCooldown(effectiveDate, now time.Time) bool {
	if !c.HasCooldown() {
		return false
	}

	return effectiveDate.After(now.Add(-time.Duration(c.period) * time.Second))
}

func (c *cooldownMock) FormatTimeUntilAvailable(effectiveDate, now time.Time, short bool) string {
	availableAt := effectiveDate.Add(time.Duration(c.period) * time.Second)
	diff := availableAt.Sub(now)
	days := int(diff.Hours()) / 24
	hours := int(diff.Hours()) % 24
	minutes := int(diff.Minutes()) % 60

	var parts []string
	if days > 0 {
		if short {
			parts = append(parts, strconv.Itoa(days)+"d")
		} else {
			parts = append(parts, strconv.Itoa(days)+" day"+plural(days))
		}
	}
	if hours > 0 {
		if short {
			parts = append(parts, strconv.Itoa(hours)+"h")
		} else {
			parts = append(parts, strconv.Itoa(hours)+" hour"+plural(hours))
		}
	}
	if len(parts) == 0 && minutes > 0 {
		if short {
			parts = append(parts, strconv.Itoa(minutes)+"m")
		} else {
			parts = append(parts, strconv.Itoa(minutes)+" minute"+plural(minutes))
		}
	}

	if result := strings.Join(parts, " "); result != "" {
		return result
	}

	if short {
		return "<1m"
	}

	return "less than a minute"
}

func plural(n int) string {
	if n > 1 {
		return "s"
	}

	return ""
}

// composerRepositoryMock is a Composer repository the cooldown has to vouch
// for a publication date (requiresPublishedTime).
type composerRepositoryMock struct {
	url              string
	requirePublished bool
}

func (r *composerRepositoryMock) RepoName() string { return "composer repo (" + r.url + ")" }
func (r *composerRepositoryMock) PHPClass() string { return `Composer\Repository\ComposerRepository` }

func (r *composerRepositoryMock) RequiresPublishedTime() bool {
	return r.requirePublished
}

func datedPackage(t *testing.T, name, prettyVersion string, since time.Duration) *pkg.CompletePackage {
	t.Helper()

	p := testPackage(t, name, prettyVersion)
	p.SetReleaseDate(time.Now().Add(since), true)

	return p
}

func publishedPackage(t *testing.T, name, prettyVersion string, since time.Duration) *pkg.CompletePackage {
	t.Helper()

	p := testPackage(t, name, prettyVersion)
	p.SetPublishedDate(time.Now().Add(since), true)

	return p
}

func TestVersionSelector_LatestVersionWithinCooldownIsSkipped(t *testing.T) {
	old := datedPackage(t, "foo/bar", "1.2.1", -30*24*time.Hour)
	newer := publishedPackage(t, "foo/bar", "1.2.2", -23*time.Hour)

	repo := &repositorySetMock{t: t, wantName: "foo/bar", results: [][]pkg.PackageInterface{{old, newer}}}
	out, err := io.NewBufferIO("", 32, nil)
	if err != nil {
		t.Fatal(err)
	}

	s := version.NewVersionSelector(repo, nil, cooldown(7*24*3600))
	if got := best(t, s, "foo/bar", version.FindBestCandidateOptions{IO: out}); got != pkg.PackageInterface(old) {
		t.Errorf("got %v, want 1.2.1", got)
	}

	if want := `<warning>Cannot use foo/bar's latest version 1.2.2 as it is still in the cooldown period configured in "policy.cooldown" (available in 6 days).` + "\n"; php.NormalizeEOL(out.Output()) != want {
		t.Errorf("output %q, want %q", php.NormalizeEOL(out.Output()), want)
	}
}

func TestVersionSelector_WithheldVersionBelowTheSelectedOneIsNotReported(t *testing.T) {
	major := datedPackage(t, "foo/bar", "2.0.0", -30*24*time.Hour)
	backport := publishedPackage(t, "foo/bar", "1.9.5", -23*time.Hour)

	repo := &repositorySetMock{t: t, wantName: "foo/bar", results: [][]pkg.PackageInterface{{backport, major}}}
	out, err := io.NewBufferIO("", 64, nil)
	if err != nil {
		t.Fatal(err)
	}

	s := version.NewVersionSelector(repo, nil, cooldown(7*24*3600))
	if got := best(t, s, "foo/bar", version.FindBestCandidateOptions{IO: out}); got != pkg.PackageInterface(major) {
		t.Errorf("got %v, want 2.0.0", got)
	}

	if php.NormalizeEOL(out.Output()) != "" {
		t.Errorf("output %q, want none", php.NormalizeEOL(out.Output()))
	}
}

func TestVersionSelector_DevAndIgnoredVersionsAreNotSubjectToCooldown(t *testing.T) {
	dev := datedPackage(t, "foo/bar", "dev-main", -1*time.Hour)
	ignored := publishedPackage(t, "foo/baz", "2.0.0", -1*time.Hour)

	repo := &repositorySetMock{t: t, byName: map[string][]pkg.PackageInterface{
		"foo/bar": {dev},
		"foo/baz": {ignored},
	}}

	config := cooldown(7 * 24 * 3600)
	config.ignore = map[string][]cooldownRule{"foo/baz": {{name: "foo/baz", constraint: semver.NewMatchAllConstraint()}}}
	s := version.NewVersionSelector(repo, nil, config)

	if got := best(t, s, "foo/bar", version.FindBestCandidateOptions{PreferredStability: "dev"}); got != pkg.PackageInterface(dev) {
		t.Errorf("got %v, want dev-main", got)
	}

	if got := best(t, s, "foo/baz", version.FindBestCandidateOptions{}); got != pkg.PackageInterface(ignored) {
		t.Errorf("got %v, want 2.0.0", got)
	}
}

func TestVersionSelector_AllVersionsWithinCooldownYieldNoCandidate(t *testing.T) {
	newer := publishedPackage(t, "foo/bar", "1.2.2", -1*time.Hour)

	repo := &repositorySetMock{t: t, results: [][]pkg.PackageInterface{{newer}}}
	if got := best(t, version.NewVersionSelector(repo, nil, cooldown(7*24*3600)), "foo/bar", version.FindBestCandidateOptions{}); got != nil {
		t.Errorf("got %v, want none", got)
	}
}

func TestVersionSelector_FirstClearingWithheldCandidateIsTheOldest(t *testing.T) {
	older := publishedPackage(t, "foo/bar", "1.2.1", -5*24*time.Hour)
	newer := publishedPackage(t, "foo/bar", "1.2.2", -1*time.Hour)

	repo := &repositorySetMock{t: t, results: [][]pkg.PackageInterface{{older, newer}}}
	s := version.NewVersionSelector(repo, nil, cooldown(7*24*3600))

	if got := best(t, s, "foo/bar", version.FindBestCandidateOptions{}); got != nil {
		t.Errorf("got %v, want none", got)
	}

	withheld := s.FirstClearingWithheldCandidate()
	if withheld == nil || withheld.Package != pkg.PackageInterface(older) {
		t.Errorf("first clearing withheld %v, want 1.2.1", withheld)
	}
}

func TestVersionSelector_CooldownIsNotAppliedWhenBlockingIsDisabled(t *testing.T) {
	newer := publishedPackage(t, "foo/bar", "1.2.2", -1*time.Hour)

	repo := &repositorySetMock{t: t, results: [][]pkg.PackageInterface{{newer}}}
	config := cooldown(7 * 24 * 3600)
	config.block = false

	if got := best(t, version.NewVersionSelector(repo, nil, config), "foo/bar", version.FindBestCandidateOptions{}); got != pkg.PackageInterface(newer) {
		t.Errorf("got %v, want 1.2.2", got)
	}
}

func TestVersionSelector_EveryWithheldCandidateIsReported(t *testing.T) {
	old := publishedPackage(t, "foo/bar", "1.2.1", -30*24*time.Hour)
	recent := publishedPackage(t, "foo/bar", "1.2.2", -2*24*time.Hour)
	newest := publishedPackage(t, "foo/bar", "1.3.0", -1*24*time.Hour)

	repo := &repositorySetMock{t: t, results: [][]pkg.PackageInterface{{old, recent, newest}}}
	s := version.NewVersionSelector(repo, nil, cooldown(7*24*3600))

	if got := best(t, s, "foo/bar", version.FindBestCandidateOptions{}); got != pkg.PackageInterface(old) {
		t.Errorf("got %v, want 1.2.1", got)
	}

	withheld := s.WithheldCandidates()
	if len(withheld) != 2 || withheld[0].Package != pkg.PackageInterface(newest) || withheld[1].Package != pkg.PackageInterface(recent) {
		t.Errorf("withheld %v, want 1.3.0 and 1.2.2", withheld)
	}

	if first := s.FirstClearingWithheldCandidate(); first == nil || first.Package != pkg.PackageInterface(recent) {
		t.Errorf("first clearing withheld %v, want 1.2.2", first)
	}
}

func TestVersionSelector_ComposerRepositoryVersionWithoutPublishedTimeIsRejected(t *testing.T) {
	p := datedPackage(t, "foo/bar", "1.2.2", -30*24*time.Hour)
	if err := p.SetRepository(&composerRepositoryMock{url: "https://repo.example.org", requirePublished: true}); err != nil {
		t.Fatal(err)
	}

	repo := &repositorySetMock{t: t, results: [][]pkg.PackageInterface{{p}}}
	s := version.NewVersionSelector(repo, nil, cooldown(7*24*3600))

	_, err := s.FindBestCandidate("foo/bar", version.FindBestCandidateOptions{})
	if err == nil || !strings.Contains(err.Error(), "foo/bar 1.2.2 from composer repo (https://repo.example.org) has no published-time") {
		t.Errorf("error %v, want the missing published-time", err)
	}
}

func TestVersionSelector_FindRecommendedRequireVersion(t *testing.T) {
	const phpVersion = "8.5.11"

	for _, c := range pkgtest.Providers(t)["VersionSelectorTest::provideRecommendedRequireVersionPackages"] {
		prettyVersion, expected := c.Args.String(0), c.Args.String(1)
		packageName := "foo/bar"

		if c.Args.Len() > 3 {
			packageName = c.Args.String(3)
		}

		// the provider builds the "ext in sync with php" case from the
		// running PHP's version
		if packageName == "ext-filter" {
			prettyVersion = phpVersion
		}

		normalized, err := pkg.NewVersionParser().Normalize(prettyVersion)
		if err != nil {
			t.Fatal(err)
		}

		p := pkg.NewPackage(packageName, normalized, prettyVersion)
		if c.Args.Len() > 2 && php.ToBool(c.Args.At(2)) {
			p.SetExtra(php.ArrayOf("branch-alias", php.ArrayOf(prettyVersion, c.Args.String(2))))
		}

		s := version.NewVersionSelector(&repositorySetMock{t: t}, nil)
		s.PHPVersion = phpVersion

		got, err := s.FindRecommendedRequireVersion(p)
		if err != nil || got != expected {
			t.Errorf("%s %v: got %q %v, want %q", packageName, c.Args.Vals, got, err, expected)
		}
	}
}

func TestStabilityFilter_IsPackageAcceptable(t *testing.T) {
	acceptable := php.ArrayOf("stable", pkg.StabilityStable, "RC", pkg.StabilityRC)
	flags := php.ArrayOf("a/dev", pkg.StabilityDev, "a/beta", pkg.StabilityBeta)

	for _, c := range []struct {
		names     []string
		stability string
		want      bool
	}{
		{[]string{"x/y"}, "stable", true},
		{[]string{"x/y"}, "RC", true},
		{[]string{"x/y"}, "beta", false},
		{[]string{"a/dev"}, "dev", true},
		{[]string{"a/beta"}, "alpha", false},
		{[]string{"a/beta"}, "beta", true},
		{[]string{"a/beta"}, "stable", true},
		{[]string{"a/beta", "x/y"}, "alpha", false},
		{[]string{"x/y", "a/beta"}, "RC", true},
		{[]string{"x/y", "a/beta"}, "dev", false},
		{nil, "stable", false},
	} {
		if got := version.IsPackageAcceptable(acceptable, flags, c.names, c.stability); got != c.want {
			t.Errorf("%v %s: got %v", c.names, c.stability, got)
		}
	}
}
