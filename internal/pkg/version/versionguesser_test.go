// Ports tests/Composer/Test/Package/Version/VersionGuesserTest.php.

package version_test

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

const (
	commitHash        = "03a15d220da53c52eddd5f32ffca64a7b3801bea"
	anotherCommitHash = "13a15d220da53c52eddd5f32ffca64a7b3801bea"
)

var (
	gitVersion252   = pkgtest.Expectation{Cmd: []string{"git", "--version"}, Stdout: "git version 2.52.0"}
	gitBranchCmd    = []string{"git", "branch", "-a", "--no-color", "--no-abbrev", "-v"}
	gitDescribeTags = pkgtest.Expectation{Cmd: []string{"git", "describe", "--exact-match", "--tags"}}
)

// keepEnv restores the variables Git::cleanEnv changes.
func keepEnv(t *testing.T) {
	t.Helper()

	for _, name := range []string{"GIT_TERMINAL_PROMPT", "GIT_ASKPASS", "GIT_DIR", "GIT_WORK_TREE", "LANGUAGE", "DYLD_LIBRARY_PATH", "COMPOSER_ROOT_VERSION"} {
		t.Setenv(name, os.Getenv(name))
	}
}

// resetGitVersion clears Git::$version before and after the test, as
// VersionGuesserTest's setUp and tearDown do.
func resetGitVersion(t *testing.T) {
	t.Helper()
	vcs.SetVersion("", false)
	t.Cleanup(func() { vcs.SetVersion("", false) })
}

func guess(t *testing.T, expectations []pkgtest.Expectation, config *php.Array) *loader.VersionData {
	t.Helper()
	keepEnv(t)
	resetGitVersion(t)

	process := pkgtest.NewProcessExecutorMock(t)
	process.Expects(expectations, true, pkgtest.Expectation{})

	data, err := version.NewVersionGuesser(process, nil).GuessVersion(config, "dummy/path")
	if err != nil {
		t.Fatal(err)
	}

	process.AssertComplete()

	if data == nil {
		t.Fatal("no version data")
	}

	return data
}

func TestVersionGuesser_HgGuessVersionReturnsData(t *testing.T) {
	data := guess(t, []pkgtest.Expectation{
		{Cmd: []string{"git", "--version"}, Stdout: "git version 2.33.0"},
		{Cmd: gitBranchCmd, Return: 128},
		{Cmd: []string{"git", "describe", "--exact-match", "--tags"}, Return: 128},
		{Cmd: []string{"git", "rev-list", "--no-commit-header", "--format=%H", "-n1", "HEAD", "--no-show-signature"}, Return: 128},
		{Cmd: []string{"hg", "branch"}, Stdout: "default"},
		{Cmd: []string{"hg", "branches"}},
		{Cmd: []string{"hg", "bookmarks"}},
	}, php.NewArray())

	if data.Version != "dev-default" || data.PrettyVersion != "dev-default" || php.ToBool(data.Commit.Value()) {
		t.Errorf("got %+v", data)
	}
}

func TestVersionGuesser_GuessVersionReturnsData(t *testing.T) {
	data := guess(t, []pkgtest.Expectation{
		gitVersion252,
		{Cmd: gitBranchCmd, Stdout: "* master " + commitHash + " Commit message\n(no branch) " + commitHash + " Commit message\n"},
	}, php.NewArray())

	if data.Version != "dev-master" || data.PrettyVersion != "dev-master" || data.FeatureVersion.Valid || data.FeaturePrettyVersion.Valid ||
		data.Commit != pkg.Str(commitHash) {
		t.Errorf("got %+v", data)
	}
}

func TestVersionGuesser_GuessVersionDoesNotSeeCustomDefaultBranchAsNonFeatureBranch(t *testing.T) {
	data := guess(t, []pkgtest.Expectation{
		gitVersion252,
		// Assumption here is that arbitrary would be the default branch
		{Cmd: gitBranchCmd, Stdout: "  arbitrary " + commitHash + " Commit message\n* current " + anotherCommitHash + " Another message\n"},
	}, php.ArrayOf("version", "self.version"))

	if data.Version != "dev-current" || data.Commit != pkg.Str(anotherCommitHash) {
		t.Errorf("got %+v", data)
	}
}

func TestVersionGuesser_GuessVersionReadsAndRespectsNonFeatureBranchesConfigurationForArbitraryNaming(t *testing.T) {
	data := guess(t, []pkgtest.Expectation{
		gitVersion252,
		{Cmd: gitBranchCmd, Stdout: "  arbitrary " + commitHash + " Commit message\n* feature " + anotherCommitHash + " Another message\n"},
		{Cmd: []string{"git", "rev-list", "arbitrary..feature"}, Stdout: anotherCommitHash + "\n"},
	}, php.ArrayOf("version", "self.version", "non-feature-branches", php.ListOf("arbitrary")))

	if data.Version != "dev-arbitrary" || data.Commit != pkg.Str(anotherCommitHash) ||
		data.FeatureVersion != pkg.Str("dev-feature") || data.FeaturePrettyVersion != pkg.Str("dev-feature") {
		t.Errorf("got %+v", data)
	}
}

func TestVersionGuesser_GuessVersionReadsAndRespectsNonFeatureBranchesConfigurationForArbitraryNamingRegex(t *testing.T) {
	data := guess(t, []pkgtest.Expectation{
		gitVersion252,
		{Cmd: gitBranchCmd, Stdout: "  latest-testing " + commitHash + " Commit message\n* feature " + anotherCommitHash + " Another message\n"},
		{Cmd: []string{"git", "rev-list", "latest-testing..feature"}, Stdout: anotherCommitHash + "\n"},
	}, php.ArrayOf("version", "self.version", "non-feature-branches", php.ListOf("latest-.*")))

	if data.Version != "dev-latest-testing" || data.Commit != pkg.Str(anotherCommitHash) ||
		data.FeatureVersion != pkg.Str("dev-feature") || data.FeaturePrettyVersion != pkg.Str("dev-feature") {
		t.Errorf("got %+v", data)
	}
}

func TestVersionGuesser_GuessVersionReadsAndRespectsNonFeatureBranchesConfigurationForArbitraryNamingWhenOnNonFeatureBranch(t *testing.T) {
	data := guess(t, []pkgtest.Expectation{
		gitVersion252,
		{Cmd: gitBranchCmd, Stdout: "* latest-testing " + commitHash + " Commit message\n  current " + anotherCommitHash + " Another message\n  master " + anotherCommitHash + " Another message\n"},
	}, php.ArrayOf("version", "self.version", "non-feature-branches", php.ListOf("latest-.*")))

	if data.Version != "dev-latest-testing" || data.Commit != pkg.Str(commitHash) || data.FeatureVersion.Valid || data.FeaturePrettyVersion.Valid {
		t.Errorf("got %+v", data)
	}
}

func TestVersionGuesser_DetachedHeadBecomesDevHash(t *testing.T) {
	for _, line := range []string{
		"* (no branch) " + commitHash + " Commit message\n",                   // testDetachedHeadBecomesDevHash
		"* (HEAD detached at FETCH_HEAD) " + commitHash + " Commit message\n", // testDetachedFetchHeadBecomesDevHashGit2
		"* (HEAD detached at 03a15d220) " + commitHash + " Commit message\n",  // testDetachedCommitHeadBecomesDevHashGit2
	} {
		data := guess(t, []pkgtest.Expectation{gitVersion252, {Cmd: gitBranchCmd, Stdout: line}, gitDescribeTags}, php.NewArray())

		if data.Version != "dev-"+commitHash {
			t.Errorf("%q: got %+v", line, data)
		}
	}
}

func TestVersionGuesser_TagBecomesVersion(t *testing.T) {
	data := guess(t, []pkgtest.Expectation{
		gitVersion252,
		{Cmd: gitBranchCmd, Stdout: "* (HEAD detached at v2.0.5-alpha2) 433b98d4218c181bae01865901aac045585e8a1a Commit message\n"},
		{Cmd: gitDescribeTags.Cmd, Stdout: "v2.0.5-alpha2"},
	}, php.NewArray())

	if data.Version != "2.0.5.0-alpha2" {
		t.Errorf("got %+v", data)
	}
}

func TestVersionGuesser_TagBecomesPrettyVersion(t *testing.T) {
	data := guess(t, []pkgtest.Expectation{
		gitVersion252,
		{Cmd: gitBranchCmd, Stdout: "* (HEAD detached at 1.0.0) c006f0c12bbbf197b5c071ffb1c0e9812bb14a4d Commit message\n"},
		{Cmd: gitDescribeTags.Cmd, Stdout: "1.0.0"},
	}, php.NewArray())

	if data.Version != "1.0.0.0" || data.PrettyVersion != "1.0.0" {
		t.Errorf("got %+v", data)
	}
}

func TestVersionGuesser_InvalidTagBecomesVersion(t *testing.T) {
	data := guess(t, []pkgtest.Expectation{
		gitVersion252,
		{Cmd: gitBranchCmd, Stdout: "* foo 03a15d220da53c52eddd5f32ffca64a7b3801bea Commit message\n"},
	}, php.NewArray())

	if data.Version != "dev-foo" {
		t.Errorf("got %+v", data)
	}
}

func TestVersionGuesser_NumericBranchesShowNicely(t *testing.T) {
	data := guess(t, []pkgtest.Expectation{
		gitVersion252,
		{Cmd: gitBranchCmd, Stdout: "* 1.5 03a15d220da53c52eddd5f32ffca64a7b3801bea Commit message\n"},
	}, php.NewArray())

	if data.PrettyVersion != "1.5.x-dev" || data.Version != "1.5.9999999.9999999-dev" {
		t.Errorf("got %+v", data)
	}
}

func TestVersionGuesser_RemoteBranchesAreSelected(t *testing.T) {
	data := guess(t, []pkgtest.Expectation{
		gitVersion252,
		{
			Cmd: gitBranchCmd,
			Stdout: "* feature-branch 03a15d220da53c52eddd5f32ffca64a7b3801bea Commit message\n" +
				"remotes/origin/1.5 03a15d220da53c52eddd5f32ffca64a7b3801bea Commit message\n",
		},
		{Cmd: []string{"git", "rev-list", "remotes/origin/1.5..feature-branch"}, Stdout: "\n"},
	}, php.ArrayOf("version", "self.version"))

	if data.PrettyVersion != "1.5.x-dev" || data.Version != "1.5.9999999.9999999-dev" {
		t.Errorf("got %+v", data)
	}
}

func TestVersionGuesser_GetRootVersionFromEnv(t *testing.T) {
	keepEnv(t)

	for _, c := range [][2]string{
		{"1.0-dev", "1.0.x-dev"},
		{"1.0.x-dev", "1.0.x-dev"},
		{"1-dev", "1.x-dev"},
		{"1.x-dev", "1.x-dev"},
		{"1.0.0", "1.0.0"},
	} {
		t.Setenv("COMPOSER_ROOT_VERSION", c[0])

		got, err := version.NewVersionGuesser(pkgtest.NewProcessExecutorMock(t), nil).RootVersionFromEnv()
		if err != nil || got != c[1] {
			t.Errorf("%q: got %q %v, want %q", c[0], got, err, c[1])
		}
	}
}

func TestVersionGuesser_OldGitFiltersRevListOutput(t *testing.T) {
	data := guess(t, []pkgtest.Expectation{
		{Cmd: []string{"git", "--version"}, Stdout: "git version 2.9.0"},
		{Cmd: gitBranchCmd, Return: 128},
		{Cmd: gitDescribeTags.Cmd, Return: 128},
		{Cmd: []string{"git", "rev-list", "--format=%H", "-n1", "HEAD"}, Stdout: "commit " + commitHash + "\n" + commitHash + "\n"},
		{Cmd: []string{"hg", "branch"}, Return: 255},
		{Cmd: []string{"fossil", "branch", "list"}, Stdout: "trunk"},
		{Cmd: []string{"fossil", "tag", "list"}, Stdout: "nope"},
	}, php.NewArray())

	if data.Version != "dev-trunk" || data.PrettyVersion != "dev-trunk" || data.Commit != pkg.Str("") {
		t.Errorf("got %+v", data)
	}
}

func TestVersionGuesser_SvnBranchesPath(t *testing.T) {
	data := guess(t, []pkgtest.Expectation{
		gitVersion252,
		{Cmd: gitBranchCmd, Return: 128},
		{Cmd: gitDescribeTags.Cmd, Return: 128},
		{Cmd: []string{"git", "rev-list", "--no-commit-header", "--format=%H", "-n1", "HEAD", "--no-show-signature"}, Return: 128},
		{Cmd: []string{"hg", "branch"}, Return: 255},
		{Cmd: []string{"fossil", "branch", "list"}, Return: 1},
		{Cmd: []string{"fossil", "tag", "list"}, Return: 1},
		{Cmd: []string{"svn", "info", "--xml"}, Stdout: "<info><entry><url>https://svn.example.org/repo/branches/1.2</url></entry></info>"},
	}, php.NewArray())

	if data.Version != "1.2.9999999.9999999-dev" || data.PrettyVersion != "1.2.x-dev" {
		t.Errorf("got %+v", data)
	}
}
