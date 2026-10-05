// Ports tests/Composer/Test/Package/Loader/RootPackageLoaderTest.php.

package loader_test

import (
	"os"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/internal/pkgtest"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/util/vcs"
)

type repositoryManagerStub struct{}

func (repositoryManagerStub) AddDefaultRepositories() error { return nil }

type rootConfigStub struct{}

func (rootConfigStub) Repositories() *php.Array { return php.NewArray() }

// keepGitEnv restores the variables the version guesser's Git::cleanEnv
// changes, and clears the process-wide git version cache (Git::$version)
// so that each test sees its own git --version call.
func keepGitEnv(t *testing.T) {
	t.Helper()

	vcs.SetVersion("", false)
	t.Cleanup(func() { vcs.SetVersion("", false) })

	for _, name := range []string{"GIT_TERMINAL_PROMPT", "GIT_ASKPASS", "GIT_DIR", "GIT_WORK_TREE", "LANGUAGE", "DYLD_LIBRARY_PATH", "COMPOSER_ROOT_VERSION"} {
		t.Setenv(name, os.Getenv(name))
	}

	os.Unsetenv("COMPOSER_ROOT_VERSION")
}

func loadRoot(t *testing.T, guesser loader.VersionGuesser, config *php.Array) pkg.PackageInterface {
	t.Helper()

	p, err := loader.NewRootPackageLoader(repositoryManagerStub{}, rootConfigStub{}, nil, guesser, nil).Load(config, pkg.ClassRootPackage)
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func TestRootPackageLoader_StabilityFlagsParsing(t *testing.T) {
	keepGitEnv(t)

	// Composer runs real git commands in its checkout here; their result
	// does not matter, so every command fails.
	process := pkgtest.NewProcessExecutorMock(t)
	process.Expects(nil, false, pkgtest.Expectation{Return: 1})

	p := loadRoot(t, version.NewVersionGuesser(process, nil), php.ArrayOf(
		"require", php.ArrayOf(
			"foo/bar", "~2.1.0-beta2",
			"bar/baz", "1.0.x-dev as 1.2.0",
			"qux/quux", "1.0.*@rc",
			"zux/complex", "~1.0,>=1.0.2@dev",
			"or/op", "^2.0@dev || ^2.0@dev",
			"multi/lowest-wins", "^2.0@rc || >=3.0@dev , ~3.5@alpha",
			"or/op-without-flags", "dev-master || 2.0 , ~3.5-alpha",
			"or/op-without-flags2", "3.0-beta || 2.0 , ~3.5-alpha",
		),
		"minimum-stability", "alpha",
	)).(pkg.RootPackageInterface)

	if p.MinimumStability() != "alpha" {
		t.Errorf("minimum stability %q", p.MinimumStability())
	}

	want := php.ArrayOf(
		"bar/baz", pkg.StabilityDev,
		"qux/quux", pkg.StabilityRC,
		"zux/complex", pkg.StabilityDev,
		"or/op", pkg.StabilityDev,
		"multi/lowest-wins", pkg.StabilityDev,
		"or/op-without-flags", pkg.StabilityDev,
		"or/op-without-flags2", pkg.StabilityAlpha,
	)
	if !php.LooseEquals(want, p.StabilityFlags()) {
		t.Errorf("stability flags %s, want %s", enc(t, p.StabilityFlags()), enc(t, want))
	}
}

func TestRootPackageLoader_NoVersionIsVisibleInPrettyVersion(t *testing.T) {
	keepGitEnv(t)

	process := pkgtest.NewProcessExecutorMock(t)
	process.Expects(nil, false, pkgtest.Expectation{Return: 1})

	p := loadRoot(t, version.NewVersionGuesser(process, nil), php.NewArray())

	if p.Version() != "1.0.0.0" || p.PrettyVersion() != pkg.DefaultPrettyVersion {
		t.Errorf("got %q %q", p.Version(), p.PrettyVersion())
	}
}

type guesserStub struct{ data *loader.VersionData }

func (g guesserStub) GuessVersion(*php.Array, string) (*loader.VersionData, error) {
	return g.data, nil
}

func (g guesserStub) RootVersionFromEnv() (string, error) { return "", nil }

func TestRootPackageLoader_PrettyVersionForRootPackageInVersionBranch(t *testing.T) {
	keepGitEnv(t)

	// see #6845
	p := loadRoot(t, guesserStub{&loader.VersionData{
		Version:       "3.0.9999999.9999999-dev",
		PrettyVersion: "3.0-dev",
		Commit:        pkg.Str("aabbccddee"),
	}}, php.NewArray())

	if p.PrettyVersion() != "3.0-dev" {
		t.Errorf("got %q", p.PrettyVersion())
	}
}

// Composer's tests rely on Git::getVersion's static cache being filled
// earlier; here the guesser asks git for its version first.
var gitVersionCall = pkgtest.Expectation{Cmd: []string{"git", "--version"}, Stdout: "git version 2.52.0"}

func TestRootPackageLoader_FeatureBranchPrettyVersion(t *testing.T) {
	keepGitEnv(t)

	process := pkgtest.NewProcessExecutorMock(t)
	process.Expects([]pkgtest.Expectation{
		gitVersionCall,
		{
			Cmd:    []string{"git", "branch", "-a", "--no-color", "--no-abbrev", "-v"},
			Stdout: "* latest-production 38137d2f6c70e775e137b2d8a7a7d3eaebf7c7e5 Commit message\n  master 4f6ed96b0bc363d2aa4404c3412de1c011f67c66 Commit message\n",
		},
		{Cmd: []string{"git", "rev-list", "master..latest-production"}},
	}, true, pkgtest.Expectation{})

	p := loadRoot(t, version.NewVersionGuesser(process, nil), php.ArrayOf("require", php.ArrayOf("foo/bar", "self.version")))
	process.AssertComplete()

	if p.PrettyVersion() != "dev-master" {
		t.Errorf("got %q", p.PrettyVersion())
	}
}

func TestRootPackageLoader_NonFeatureBranchPrettyVersion(t *testing.T) {
	keepGitEnv(t)

	process := pkgtest.NewProcessExecutorMock(t)
	process.Expects([]pkgtest.Expectation{
		gitVersionCall,
		{
			Cmd:    []string{"git", "branch", "-a", "--no-color", "--no-abbrev", "-v"},
			Stdout: "* latest-production 38137d2f6c70e775e137b2d8a7a7d3eaebf7c7e5 Commit message\n  master 4f6ed96b0bc363d2aa4404c3412de1c011f67c66 Commit message\n",
		},
	}, true, pkgtest.Expectation{})

	p := loadRoot(t, version.NewVersionGuesser(process, nil), php.ArrayOf(
		"require", php.ArrayOf("foo/bar", "self.version"),
		"non-feature-branches", php.ListOf("latest-.*"),
	))
	process.AssertComplete()

	if p.PrettyVersion() != "dev-latest-production" {
		t.Errorf("got %q", p.PrettyVersion())
	}
}
