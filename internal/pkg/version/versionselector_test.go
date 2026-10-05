// Ports tests/Composer/Test/Package/Version/VersionSelectorTest.php.

package version_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/internal/pkgtest"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/semver"
)

// repositorySetMock returns the configured packages for every call.
type repositorySetMock struct {
	t        *testing.T
	results  [][]pkg.PackageInterface
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

	if want := "<warning>Cannot use foo/bar's latest version 2.1.0 as it requires php >=5.6 which is not satisfied by your platform.\n"; out.Output() != want {
		t.Errorf("output %q, want %q", out.Output(), want)
	}

	out = bufferIO(64)
	if got := best(t, s, "foo/bar", version.FindBestCandidateOptions{IO: out}); got.String() != packages[1].String() {
		t.Errorf("got %v, want 1.0.0", got)
	}

	if want := "<warning>Cannot use foo/bar's latest version 2.1.0 as it requires php >=5.6 which is not satisfied by your platform.\n" +
		"<warning>Cannot use foo/bar 2.0.0 as it requires php >=5.6 which is not satisfied by your platform.\n"; out.Output() != want {
		t.Errorf("output %q, want %q", out.Output(), want)
	}

	if got := best(t, s, "foo/bar", version.FindBestCandidateOptions{PlatformRequirementFilter: ignoreAll{}}); got.String() != packages[3].String() {
		t.Errorf("got %v, want 2.1.0", got)
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
