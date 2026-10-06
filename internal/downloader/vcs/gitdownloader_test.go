// Ports tests/Composer/Test/Downloader/GitDownloaderTest.php.

package vcs

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

const sha = "1234567890123456789012345678901234567890"

type gitTest struct {
	io      mio.IO
	config  http.Config
	process *processmock.Mock
	fs      *fakeFS
}

// newGitTest is the setUp of GitDownloaderTest: git 1.0.0, a process mock.
func newGitTest(t *testing.T) *gitTest {
	t.Helper()

	initGitVersion(t, "1.0.0")
	keepEnv(t)

	return &gitTest{io: mio.NewNullIO(), process: processmock.New(), fs: &fakeFS{}}
}

// downloader is getDownloaderMock().
func (g *gitTest) downloader(t *testing.T) *GitDownloader {
	t.Helper()

	if g.config == nil {
		g.config = newConfig(t)
	}

	return NewGitDownloader(Deps{IO: g.io, Config: g.config, Process: g.process, Filesystem: g.fs})
}

func TestGitDownloader_DownloadForPackageWithoutSourceReference(t *testing.T) {
	g := newGitTest(t)
	p := sourcePackage("1.0.0.0", "1.0.0", "")

	err := run(installSteps(g.downloader(t), p, "/path")...)
	wantError[*util.InvalidArgumentError](t, err, "Package dummy/pkg is missing reference information")

	if !phperr.Is(err, "VcsDownloader.php", 66) {
		t.Errorf("throw site = %v, want VcsDownloader.php:66", err)
	}
}

func TestGitDownloader_Download(t *testing.T) {
	g := newGitTest(t)
	p := sourcePackage("dev-master", "dev-master", sha, "https://example.com/composer/composer")

	g.process.Expects([]processmock.Expectation{
		processmock.Cmd("git", "clone", "--no-checkout", "--", "https://example.com/composer/composer", composerPath(t)),
		processmock.Cmd("git", "remote", "add", "composer", "--", "https://example.com/composer/composer"),
		processmock.Cmd("git", "fetch", "composer"),
		processmock.Cmd("git", "remote", "set-url", "origin", "--", "https://example.com/composer/composer"),
		processmock.Cmd("git", "remote", "set-url", "composer", "--", "https://example.com/composer/composer"),
		processmock.Cmd("git", "branch", "-r"),
		processmock.Cmd("git", "checkout", "master", "--"),
		processmock.Cmd("git", "reset", "--hard", sha, "--"),
	}, true, nil)

	noError(t, run(installSteps(g.downloader(t), p, "composerPath")...))
	assertComplete(t, g.process)
}

func TestGitDownloader_DownloadWithCache(t *testing.T) {
	g := newGitTest(t)
	initGitVersion(t, "2.17.0")

	p := sourcePackage("dev-master", "dev-master", sha, "https://example.com/composer/composer")
	g.config = newConfig(t)
	cachePath := php.ToString(g.config.Get("cache-vcs-dir")) + "/https---example.com-composer-composer/"

	g.process.Expects([]processmock.Expectation{
		{
			Cmd:      util.Cmd("git", "clone", "--mirror", "--", "https://example.com/composer/composer", cachePath),
			Callback: func() { _ = os.MkdirAll(cachePath, 0o777) },
		},
		processmock.Cmd("git", "remote", "-v"),
		processmock.Cmd("git", "remote", "set-url", "origin", "--", "https://example.com/composer/composer"),
		{Cmd: util.Cmd("git", "rev-parse", "--git-dir"), Stdout: "."},
		processmock.Cmd("git", "rev-parse", "--quiet", "--verify", sha+"^{commit}"),
		processmock.Cmd("git", "clone", "--no-checkout", cachePath, composerPath(t), "--dissociate", "--reference", cachePath),
		processmock.Cmd("git", "remote", "set-url", "origin", "--", "https://example.com/composer/composer"),
		processmock.Cmd("git", "remote", "add", "composer", "--", "https://example.com/composer/composer"),
		processmock.Cmd("git", "branch", "-r"),
		{Cmd: util.Cmd("git", "checkout", "master", "--"), Return: 1},
		processmock.Cmd("git", "checkout", "-B", "master", "composer/master", "--"),
		processmock.Cmd("git", "reset", "--hard", sha, "--"),
	}, true, nil)

	noError(t, run(installSteps(g.downloader(t), p, "composerPath")...))
	assertComplete(t, g.process)
}

func TestGitDownloader_DownloadUsesVariousProtocolsAndSetsPushUrlForGithub(t *testing.T) {
	g := newGitTest(t)
	p := sourcePackage("1.0.0.0", "1.0.0", "ref", "https://github.com/mirrors/composer", "https://github.com/composer/composer")

	g.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("git", "clone", "--no-checkout", "--", "https://github.com/mirrors/composer", composerPath(t)), Return: 1, Stderr: "Error1"},

		processmock.Cmd("git", "clone", "--no-checkout", "--", "git@github.com:mirrors/composer", composerPath(t)),
		processmock.Cmd("git", "remote", "add", "composer", "--", "git@github.com:mirrors/composer"),
		processmock.Cmd("git", "fetch", "composer"),
		processmock.Cmd("git", "remote", "set-url", "origin", "--", "git@github.com:mirrors/composer"),
		processmock.Cmd("git", "remote", "set-url", "composer", "--", "git@github.com:mirrors/composer"),

		processmock.Cmd("git", "remote", "set-url", "origin", "--", "https://github.com/composer/composer"),
		processmock.Cmd("git", "remote", "set-url", "--push", "origin", "--", "git@github.com:composer/composer.git"),
		processmock.Cmd("git", "branch", "-r"),
		processmock.Cmd("git", "checkout", "ref", "--"),
		processmock.Cmd("git", "reset", "--hard", "ref", "--"),
	}, true, nil)

	noError(t, run(installSteps(g.downloader(t), p, "composerPath")...))
	assertComplete(t, g.process)
}

func TestGitDownloader_DownloadAndSetPushUrlUseCustomVariousProtocolsForGithub(t *testing.T) {
	cases := []struct {
		protocols    []string
		url, pushURL string
	}{
		// ssh proto should use git@ all along
		{[]string{"ssh"}, "git@github.com:composer/composer", "git@github.com:composer/composer.git"},
		// auto-proto uses git@ by default for push url, but not fetch
		{[]string{"https", "ssh", "git"}, "https://github.com/composer/composer", "git@github.com:composer/composer.git"},
		// if restricted to https then push url is not overwritten to git@
		{[]string{"https"}, "https://github.com/composer/composer", "https://github.com/composer/composer.git"},
	}

	for _, c := range cases {
		t.Run(strings.Join(c.protocols, ","), func(t *testing.T) {
			g := newGitTest(t)
			p := sourcePackage("1.0.0.0", "1.0.0", "ref", "https://github.com/composer/composer")

			g.process.Expects([]processmock.Expectation{
				processmock.Cmd("git", "clone", "--no-checkout", "--", c.url, composerPath(t)),
				processmock.Cmd("git", "remote", "add", "composer", "--", c.url),
				processmock.Cmd("git", "fetch", "composer"),
				processmock.Cmd("git", "remote", "set-url", "origin", "--", c.url),
				processmock.Cmd("git", "remote", "set-url", "composer", "--", c.url),

				processmock.Cmd("git", "remote", "set-url", "--push", "origin", "--", c.pushURL),
				processmock.Cmd("git", "branch", "-r"),
				processmock.Cmd("git", "checkout", "ref", "--"),
				processmock.Cmd("git", "reset", "--hard", "ref", "--"),
			}, true, nil)

			g.config = newConfig(t, "github-protocols", php.StringList(c.protocols))

			noError(t, run(installSteps(g.downloader(t), p, "composerPath")...))
			assertComplete(t, g.process)
		})
	}
}

func TestGitDownloader_DownloadThrowsRuntimeExceptionIfGitCommandFails(t *testing.T) {
	g := newGitTest(t)
	p := sourcePackage("1.0.0.0", "1.0.0", "ref", "https://example.com/composer/composer")

	g.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("git", "clone", "--no-checkout", "--", "https://example.com/composer/composer", composerPath(t)), Return: 1},
	}, false, nil)

	err := run(installSteps(g.downloader(t), p, "composerPath")...)
	wantError[*util.RuntimeError](t, err, "Failed to execute git clone --no-checkout -- https://example.com/composer/composer "+composerPath(t))
}

func TestGitDownloader_UpdateforPackageWithoutSourceReference(t *testing.T) {
	g := newGitTest(t)
	initial := sourcePackage("1.0.0.0", "1.0.0", "ref")
	target := sourcePackage("1.0.0.0", "1.0.0", "")

	err := run(updateSteps(g.downloader(t), initial, target, "/path")...)
	wantError[*util.InvalidArgumentError](t, err, "missing reference information")

	if !phperr.Is(err, "VcsDownloader.php", 66) { // download() comes first
		t.Errorf("throw site = %v, want VcsDownloader.php:66", err)
	}
}

func TestGitDownloader_Update(t *testing.T) {
	g := newGitTest(t)
	p := sourcePackage("1.0.0.0", "1.0.0", "ref", "https://github.com/composer/composer")

	g.process.Expects([]processmock.Expectation{
		processmock.Cmd("git", "show-ref", "--head", "-d"),
		processmock.Cmd("git", "status", "--porcelain", "--untracked-files=no"),
		{Cmd: util.Cmd("git", "rev-parse", "--quiet", "--verify", "ref^{commit}"), Return: 1},

		// fallback commands for the above failing
		processmock.Cmd("git", "remote", "-v"),
		processmock.Cmd("git", "remote", "set-url", "composer", "--", "https://github.com/composer/composer"),
		processmock.Cmd("git", "fetch", "composer"),
		processmock.Cmd("git", "fetch", "--tags", "composer"),

		processmock.Cmd("git", "remote", "-v"),
		processmock.Cmd("git", "remote", "set-url", "composer", "--", "https://github.com/composer/composer"),

		processmock.Cmd("git", "branch", "-r"),
		processmock.Cmd("git", "checkout", "ref", "--"),
		processmock.Cmd("git", "reset", "--hard", "ref", "--"),
		processmock.Cmd("git", "remote", "-v"),
	}, true, nil)

	dir := gitDir(t, ".git")
	noError(t, run(updateSteps(g.downloader(t), p, p, dir)...))
	assertComplete(t, g.process)
}

func TestGitDownloader_UpdateWithNewRepoUrl(t *testing.T) {
	g := newGitTest(t)
	p := sourcePackage("1.0.0.0", "1.0.0", "ref", "https://github.com/composer/composer")

	g.process.Expects([]processmock.Expectation{
		processmock.Cmd("git", "show-ref", "--head", "-d"),
		processmock.Cmd("git", "status", "--porcelain", "--untracked-files=no"),
		{Cmd: util.Cmd("git", "rev-parse", "--quiet", "--verify", "ref^{commit}"), Return: 0},

		processmock.Cmd("git", "remote", "-v"),
		processmock.Cmd("git", "remote", "set-url", "composer", "--", "https://github.com/composer/composer"),

		processmock.Cmd("git", "branch", "-r"),
		processmock.Cmd("git", "checkout", "ref", "--"),
		processmock.Cmd("git", "reset", "--hard", "ref", "--"),
		{
			Cmd: util.Cmd("git", "remote", "-v"),
			Stdout: "origin https://github.com/old/url (fetch)\n" +
				"origin https://github.com/old/url (push)\n" +
				"composer https://github.com/old/url (fetch)\n" +
				"composer https://github.com/old/url (push)\n",
		},
		processmock.Cmd("git", "remote", "set-url", "origin", "--", "https://github.com/composer/composer"),
		processmock.Cmd("git", "remote", "set-url", "--push", "origin", "--", "git@github.com:composer/composer.git"),
	}, true, nil)

	dir := gitDir(t, ".git")
	noError(t, run(updateSteps(g.downloader(t), p, p, dir)...))
	assertComplete(t, g.process)
}

func TestGitDownloader_UpdateThrowsRuntimeExceptionIfGitCommandFails(t *testing.T) {
	g := newGitTest(t)
	p := sourcePackage("1.0.0.0", "", "ref", "https://github.com/composer/composer")

	g.process.Expects([]processmock.Expectation{
		processmock.Cmd("git", "show-ref", "--head", "-d"),
		processmock.Cmd("git", "status", "--porcelain", "--untracked-files=no"),

		// commit not yet in so we try to fetch
		{Cmd: util.Cmd("git", "rev-parse", "--quiet", "--verify", "ref^{commit}"), Return: 1},

		// fail first fetch
		processmock.Cmd("git", "remote", "-v"),
		processmock.Cmd("git", "remote", "set-url", "composer", "--", "https://github.com/composer/composer"),
		{Cmd: util.Cmd("git", "fetch", "composer"), Return: 1},

		// fail second fetch
		processmock.Cmd("git", "remote", "set-url", "composer", "--", "git@github.com:composer/composer"),
		{Cmd: util.Cmd("git", "fetch", "composer"), Return: 1},

		processmock.Cmd("git", "--version"),
	}, true, nil)

	dir := gitDir(t, ".git")
	err := run(updateSteps(g.downloader(t), p, p, dir)...)
	wantError[*util.RuntimeError](t, err, "Failed to clone https://github.com/composer/composer via https, ssh protocols, aborting.")

	if !strings.Contains(err.Error(), "git@github.com:composer/composer") {
		t.Fatalf("error %q does not name the ssh url", err)
	}
}

func TestGitDownloader_UpdateDoesntThrowsRuntimeExceptionIfGitCommandFailsAtFirstButIsAbleToRecover(t *testing.T) {
	g := newGitTest(t)
	// A local first URL: the filesystem root, C:\ on Windows.
	root := "/"
	if util.IsWindows() {
		root = `C:\`
	}
	p := sourcePackage("1.0.0.0", "1.0.0", "ref", root, "https://github.com/composer/composer")

	g.process.Expects([]processmock.Expectation{
		processmock.Cmd("git", "show-ref", "--head", "-d"),
		processmock.Cmd("git", "status", "--porcelain", "--untracked-files=no"),

		// commit not yet in so we try to fetch
		{Cmd: util.Cmd("git", "rev-parse", "--quiet", "--verify", "ref^{commit}"), Return: 1},

		// fail first source URL
		processmock.Cmd("git", "remote", "-v"),
		processmock.Cmd("git", "remote", "set-url", "composer", "--", root),
		{Cmd: util.Cmd("git", "fetch", "composer"), Return: 1},
		processmock.Cmd("git", "--version"),

		// commit not yet in so we try to fetch
		{Cmd: util.Cmd("git", "rev-parse", "--quiet", "--verify", "ref^{commit}"), Return: 1},

		// pass second source URL
		processmock.Cmd("git", "remote", "-v"),
		processmock.Cmd("git", "remote", "set-url", "composer", "--", "https://github.com/composer/composer"),
		{Cmd: util.Cmd("git", "fetch", "composer"), Return: 0},
		processmock.Cmd("git", "fetch", "--tags", "composer"),
		processmock.Cmd("git", "remote", "-v"),
		processmock.Cmd("git", "remote", "set-url", "composer", "--", "https://github.com/composer/composer"),

		processmock.Cmd("git", "branch", "-r"),
		processmock.Cmd("git", "checkout", "ref", "--"),
		processmock.Cmd("git", "reset", "--hard", "ref", "--"),
		processmock.Cmd("git", "remote", "-v"),
	}, true, nil)

	dir := gitDir(t, ".git")
	noError(t, run(updateSteps(g.downloader(t), p, p, dir)...))
	assertComplete(t, g.process)
}

func TestGitDownloader_DowngradeShowsAppropriateMessage(t *testing.T) {
	g := newGitTest(t)
	buffer := bufferIO(t, console.VerbosityNormal)
	g.io = buffer

	oldPackage := sourcePackage("1.2.0.0", "1.2.0", "ref", "/foo/bar", "https://github.com/composer/composer")
	newPackage := sourcePackage("1.0.0.0", "1.0.0", "ref", "https://github.com/composer/composer")

	dir := gitDir(t, ".git")
	noError(t, run(updateSteps(g.downloader(t), oldPackage, newPackage, dir)...))

	if !strings.Contains(buffer.Output(), "Downgrading ") {
		t.Fatalf("output %q has no Downgrading line", buffer.Output())
	}
}

func TestGitDownloader_NotUsingDowngradingWithReferences(t *testing.T) {
	g := newGitTest(t)
	buffer := bufferIO(t, console.VerbosityNormal)
	g.io = buffer

	oldPackage := sourcePackage("dev-ref", "dev-ref", "ref", "/foo/bar", "https://github.com/composer/composer")
	newPackage := sourcePackage("dev-ref2", "dev-ref2", "ref", "https://github.com/composer/composer")

	dir := gitDir(t, ".git")
	noError(t, run(updateSteps(g.downloader(t), oldPackage, newPackage, dir)...))

	if !strings.Contains(buffer.Output(), "Upgrading ") {
		t.Fatalf("output %q has no Upgrading line", buffer.Output())
	}
}

func TestGitDownloader_Remove(t *testing.T) {
	g := newGitTest(t)
	p := sourcePackage("1.0.0.0", "1.0.0", "ref")

	g.process.Expects([]processmock.Expectation{
		processmock.Cmd("git", "show-ref", "--head", "-d"),
		processmock.Cmd("git", "status", "--porcelain", "--untracked-files=no"),
	}, true, nil)

	dir := gitDir(t, ".git")
	noError(t, run(removeSteps(g.downloader(t), p, dir)...))

	if !slices.Equal(g.fs.removed, []string{dir}) {
		t.Fatalf("removeDirectoryAsync calls: %v", g.fs.removed)
	}

	assertComplete(t, g.process)
}

func TestGitDownloader_GetInstallationSource(t *testing.T) {
	g := newGitTest(t)

	if got := g.downloader(t).InstallationSource(); got != "source" {
		t.Fatalf("got %q", got)
	}
}
