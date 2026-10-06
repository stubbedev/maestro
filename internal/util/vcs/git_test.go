// Ports tests/Composer/Test/Util/GitTest.php.

package vcs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http/httpmock"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

// fakeFS is the Filesystem mock: removeDirectory returns true.
type fakeFS struct{ removed []string }

func (f *fakeFS) RemoveDirectory(dir string) (bool, error) {
	f.removed = append(f.removed, dir)

	return true, nil
}

type gitFixture struct {
	git     *Git
	io      *fakeIO
	config  *fakeConfig
	process *processmock.Mock
	fs      *fakeFS
}

func newGitFixture(values map[string]any) *gitFixture {
	f := &gitFixture{io: newFakeIO(), config: newFakeConfig(values), process: processmock.New(), fs: &fakeFS{}}
	f.git = NewGit(f.io, f.config, f.process, f.fs)

	return f
}

func mockConfig(protocol string) map[string]any {
	return map[string]any{"github-domains": list("github.com"), "github-protocols": list(protocol)}
}

func mockSyncMirrorConfig() map[string]any {
	return map[string]any{"github-domains": list("github.com"), "gitlab-domains": list("gitlab.com"), "github-protocols": list("https")}
}

// expectedURLCallable returns 'git command ok' for expectedURL and 'git
// command failing' for any other URL.
func expectedURLCallable(expectedURL string) []CommandFunc {
	return []CommandFunc{func(url string) util.Command {
		if url != expectedURL {
			return util.ShellCmd("git command failing")
		}

		return util.ShellCmd("git command ok")
	}}
}

func failuresThenOK(n int, extra ...processmock.Expectation) []processmock.Expectation {
	var calls []processmock.Expectation
	for range n {
		calls = append(calls, processmock.Expectation{Cmd: util.ShellCmd("git command failing"), Return: 1})
	}

	calls = append(calls, extra...)

	return append(calls, processmock.Shell("git command ok"))
}

func TestGit_RunCommandPublicGitHubRepositoryNotInitialClone(t *testing.T) {
	for _, tc := range []struct{ protocol, expectedURL string }{
		{"ssh", "git@github.com:acme/repo"},
		{"https", "https://github.com/acme/repo"},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			f := newGitFixture(mockConfig(tc.protocol))
			f.process.Expects([]processmock.Expectation{processmock.Shell("git command")}, true, nil)

			callable := func(url string) util.Command {
				if url != tc.expectedURL {
					t.Errorf("url %q, want %q", url, tc.expectedURL)
				}

				return util.ShellCmd("git command")
			}

			if err := f.git.RunCommand([]CommandFunc{callable}, "https://github.com/acme/repo", "", true, nil); err != nil {
				t.Fatal(err)
			}

			assertComplete(t, f.process)
		})
	}
}

func TestGit_RunCommandPrivateGitHubRepositoryNotInitialCloneNotInteractiveWithoutAuthentication(t *testing.T) {
	f := newGitFixture(mockConfig("https"))
	f.process.Expects([]processmock.Expectation{
		{Cmd: util.ShellCmd("git command"), Return: 1},
		{Cmd: util.Cmd("git", "--version"), Return: 0},
	}, true, nil)

	callable := func(url string) util.Command {
		if url != "https://github.com/acme/repo" {
			t.Errorf("url %q", url)
		}

		return util.ShellCmd("git command")
	}

	err := f.git.RunCommand([]CommandFunc{callable}, "https://github.com/acme/repo", "", true, nil)

	var re *util.RuntimeError
	if !errors.As(err, &re) {
		t.Fatalf("got %v, want a RuntimeError", err)
	}

	want := "Failed to clone https://github.com/acme/repo via https protocols, aborting.\n\n- https://github.com/acme/repo\n  "
	if re.Message != want {
		t.Fatalf("message %q, want %q", re.Message, want)
	}
}

func TestGit_RunCommandPrivateGitHubRepositoryNotInitialCloneNotInteractiveWithAuthentication(t *testing.T) {
	for _, tc := range []struct {
		gitURL, protocol, gitHubToken, expectedURL string
		expectedFailuresBeforeSuccess              int
	}{
		{"git@github.com:acme/repo.git", "ssh", "MY_GITHUB_TOKEN", "https://token:MY_GITHUB_TOKEN@github.com/acme/repo.git", 1},
		{"https://github.com/acme/repo", "https", "MY_GITHUB_TOKEN", "https://token:MY_GITHUB_TOKEN@github.com/acme/repo.git", 2},
	} {
		t.Run(tc.gitURL, func(t *testing.T) {
			f := newGitFixture(mockConfig(tc.protocol))
			f.process.Expects(failuresThenOK(tc.expectedFailuresBeforeSuccess), true, nil)
			f.io.auths["github.com"] = authOf("token", tc.gitHubToken)

			if err := f.git.RunCommand(expectedURLCallable(tc.expectedURL), tc.gitURL, "", true, nil); err != nil {
				t.Fatal(err)
			}

			assertComplete(t, f.process)
			assertOnlyOrigin(t, f.io, "github.com")
		})
	}
}

func assertOnlyOrigin(t *testing.T, fio *fakeIO, origin string) {
	t.Helper()

	if len(fio.hasAuthArgs) == 0 {
		t.Fatal("hasAuthentication was not called")
	}

	for _, arg := range fio.hasAuthArgs {
		if arg != origin {
			t.Fatalf("hasAuthentication(%q), want %q", arg, origin)
		}
	}
}

func TestGit_RunCommandPrivateBitbucketRepositoryNotInitialCloneNotInteractiveWithAuthentication(t *testing.T) {
	for _, tc := range []struct {
		gitURL                        string
		bitbucketToken                *string
		expectedURL                   string
		expectedFailuresBeforeSuccess int
		bitbucketGitAuthCalls         int
	}{
		{"git@bitbucket.org:acme/repo.git", new("MY_BITBUCKET_TOKEN"), "https://token:MY_BITBUCKET_TOKEN@bitbucket.org/acme/repo.git", 1, 0},
		{"https://bitbucket.org/acme/repo", new("MY_BITBUCKET_TOKEN"), "https://token:MY_BITBUCKET_TOKEN@bitbucket.org/acme/repo.git", 1, 0},
		{"https://bitbucket.org/acme/repo.git", new("MY_BITBUCKET_TOKEN"), "https://token:MY_BITBUCKET_TOKEN@bitbucket.org/acme/repo.git", 1, 0},
		{"git@bitbucket.org:acme/repo.git", nil, "git@bitbucket.org:acme/repo.git", 0, 0},
		{"https://bitbucket.org/acme/repo", nil, "git@bitbucket.org:acme/repo.git", 1, 1},
		{"https://bitbucket.org/acme/repo.git", nil, "git@bitbucket.org:acme/repo.git", 1, 1},
		{"https://bitbucket.org/acme/repo.git", new("ATAT_BITBUCKET_API_TOKEN"), "https://x-bitbucket-api-token-auth:ATAT_BITBUCKET_API_TOKEN@bitbucket.org/acme/repo.git", 1, 0},
	} {
		t.Run(tc.gitURL, func(t *testing.T) {
			f := newGitFixture(map[string]any{"gitlab-domains": list("gitlab.com"), "github-domains": list("github.com")})

			// When we are testing what happens without auth saved, and URLs
			// with https, there will also be an attempt to find the token in
			// the git config for the folder and repo, locally.
			var extra []processmock.Expectation
			for range tc.bitbucketGitAuthCalls {
				extra = append(extra, processmock.Expectation{Cmd: util.Cmd("git", "config", "bitbucket.accesstoken"), Return: 1})
			}

			f.process.Expects(failuresThenOK(tc.expectedFailuresBeforeSuccess, extra...), true, nil)

			if tc.bitbucketToken != nil {
				f.io.auths["bitbucket.org"] = authOf("token", *tc.bitbucketToken)
			}

			if err := f.git.RunCommand(expectedURLCallable(tc.expectedURL), tc.gitURL, "", true, nil); err != nil {
				t.Fatal(err)
			}

			assertComplete(t, f.process)

			if tc.bitbucketToken != nil {
				assertOnlyOrigin(t, f.io, "bitbucket.org")
			}
		})
	}
}

func TestGit_RunCommandPrivateBitbucketRepositoryNotInitialCloneInteractiveWithOauth(t *testing.T) {
	for _, tc := range []struct {
		gitURL, expectedURL string
		initialConfig       map[string]io.Authentication
	}{
		{"git@bitbucket.org:acme/repo.git", "https://x-token-auth:my-access-token@bitbucket.org/acme/repo.git", nil},
		{"https://bitbucket.org/acme/repo.git", "https://x-token-auth:my-access-token@bitbucket.org/acme/repo.git", nil},
		{"https://bitbucket.org/acme/repo", "https://x-token-auth:my-access-token@bitbucket.org/acme/repo.git", nil},
		{"git@bitbucket.org:acme/repo.git", "https://x-token-auth:my-access-token@bitbucket.org/acme/repo.git", map[string]io.Authentication{"bitbucket.org": authOf("someuseralsoswappedfortoken", "little green men")}},
	} {
		t.Run(tc.gitURL, func(t *testing.T) {
			expected := []processmock.Expectation{{Cmd: util.ShellCmd("git command failing"), Return: 1}}
			if len(tc.initialConfig) > 0 {
				expected = append(expected, processmock.Expectation{Cmd: util.ShellCmd("git command failing"), Return: 1})
			} else {
				expected = append(expected, processmock.Expectation{Cmd: util.Cmd("git", "config", "bitbucket.accesstoken"), Return: 1})
			}

			expected = append(expected, processmock.Shell("git command ok"))

			f := newGitFixture(map[string]any{"gitlab-domains": list("gitlab.com"), "github-domains": list("github.com")})
			f.process.Expects(expected, true, nil)
			f.io.interactive = true
			f.io.confirm = true
			f.io.ask = func(question string) any {
				switch question {
				case "Consumer Key (hidden): ":
					return "my-consumer-key"
				case "Consumer Secret (hidden): ":
					return "my-consumer-secret"
				}

				return ""
			}

			maps.Copy(f.io.auths, tc.initialConfig)

			downloader := httpmock.New()
			downloader.Expects([]httpmock.Expectation{
				{URL: "https://bitbucket.org/site/oauth2/access_token", Status: 200, Body: `{"expires_in": 600, "access_token": "my-access-token"}`},
			}, false, nil)
			f.git.SetHttpDownloader(downloader)

			if err := f.git.RunCommand(expectedURLCallable(tc.expectedURL), tc.gitURL, "", true, nil); err != nil {
				t.Fatal(err)
			}

			assertComplete(t, f.process)
			assertOnlyOrigin(t, f.io, "bitbucket.org")

			if err := downloader.AssertComplete(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGit_SyncMirrorSanitizesUrlAfterInitialClone(t *testing.T) {
	for _, url := range []string{
		"https://user:secret@example.com/repo.git",
		// a bare token in the user slot has no colon to key off
		"https://ghp_1234567890abcdefghijklmnopqrstuvwxyzAB@example.com/repo.git",
		// a password containing @ must not leave its tail behind
		"https://user:sec@ret@example.com/repo.git",
	} {
		t.Run(url, func(t *testing.T) {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			nonExistentDir := os.TempDir() + "/composer-test-nonexistent-" + hex.EncodeToString(b)

			f := newGitFixture(mockSyncMirrorConfig())
			f.process.Expects([]processmock.Expectation{
				processmock.Cmd("git", "clone", "--mirror", "--", url, nonExistentDir),
				processmock.Cmd("git", "remote", "-v"),
				processmock.Cmd("git", "remote", "set-url", "origin", "--", "https://example.com/repo.git"),
			}, true, nil)

			result, err := f.git.SyncMirror(url, nonExistentDir)
			if err != nil {
				t.Fatal(err)
			}

			if !result {
				t.Fatal("SyncMirror returned false")
			}

			assertComplete(t, f.process)
		})
	}
}

func TestGit_SyncMirrorSanitizesUrlEvenAfterFailedUpdate(t *testing.T) {
	dir := os.TempDir()

	f := newGitFixture(mockSyncMirrorConfig())
	f.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("git", "rev-parse", "--git-dir"), Stdout: ".\n"},
		processmock.Cmd("git", "remote", "-v"),
		processmock.Cmd("git", "remote", "set-url", "origin", "--", "https://user:secret@example.com/repo.git"),
		{Cmd: util.Cmd("git", "remote", "update", "--prune", "origin"), Return: 1},
		processmock.Cmd("git", "--version"),
		processmock.Cmd("git", "remote", "-v"),
		processmock.Cmd("git", "remote", "set-url", "origin", "--", "https://example.com/repo.git"),
	}, true, nil)

	result, err := f.git.SyncMirror("https://user:secret@example.com/repo.git", dir)
	if err != nil {
		t.Fatal(err)
	}

	if result {
		t.Fatal("SyncMirror returned true")
	}

	assertComplete(t, f.process)
}

// The tests below cover Git.php paths GitTest leaves out.

func TestGit_RunCommandsRejectsSSHURLWithoutPort(t *testing.T) {
	f := newGitFixture(mockConfig("https"))
	f.process.Expects(nil, true, nil)

	err := f.git.RunCommands([][]string{{"git", "clone", "%url%"}}, "ssh://git@example.com:path/repo", "", true, nil)

	var ia *util.InvalidArgumentError
	if !errors.As(err, &ia) || !strings.HasPrefix(ia.Message, `The source URL ssh://git@example.com:path/repo is invalid, ssh URLs should have a port number after ":".`) {
		t.Fatalf("got %v", err)
	}
}

func TestGit_RunCommandsFailureMasksCredentials(t *testing.T) {
	f := newGitFixture(map[string]any{"github-domains": list("github.com"), "gitlab-domains": list("gitlab.com"), "store-auths": false})
	f.io.auths["example.com"] = authOf("someuser", "longsecretvalue")
	f.process.Expects([]processmock.Expectation{
		processmock.Cmd("git", "remote", "-v"),
		{Cmd: util.Cmd("git", "fetch", "https://example.com/repo.git"), Return: 1, Stderr: "fatal: Authentication failed for 'https://example.com/repo.git'"},
		{Cmd: util.Cmd("git", "fetch", "https://someuser:longsecretvalue@example.com/repo.git"), Return: 1, Stderr: "fatal: longsecretvalue rejected"},
		processmock.Cmd("git", "--version"),
	}, true, nil)

	err := f.git.RunCommands([][]string{{"git", "fetch", "%url%"}}, "https://example.com/repo.git", "/tmp", false, nil)

	want := "Failed to execute git fetch https://som...ser:***@example.com/repo.git\n\nfatal: lon...lue rejected"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v\nwant %s", err, want)
	}

	assertComplete(t, f.process)
}

func TestGit_RunCommandsPromptsAndStoresCredentials(t *testing.T) {
	f := newGitFixture(map[string]any{"github-domains": list("github.com"), "gitlab-domains": list("gitlab.com"), "store-auths": true})
	f.io.interactive = true
	f.io.ask = func(q string) any {
		if q == "      Username: " {
			return "bob"
		}

		return "pw"
	}
	f.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("git", "clone", "https://alice@example.com/repo.git", "dir"), Return: 1, Stderr: "fatal: could not read Username"},
		processmock.Cmd("git", "clone", "https://bob:pw@example.com/repo.git", "dir"),
	}, true, nil)

	var output string
	if err := f.git.RunCommands([][]string{{"git", "clone", "%url%", "dir"}}, "https://alice@example.com/repo.git", "/tmp/x", true, &output); err != nil {
		t.Fatal(err)
	}

	assertComplete(t, f.process)

	if a := f.io.auths["example.com"]; strOf(a.Username) != "bob" || strOf(a.Password) != "pw" {
		t.Fatalf("auth %v", a)
	}

	if !slices.Contains(f.config.auth.added, "http-basic.example.com") {
		t.Fatalf("auth not stored: %v", f.config.auth.added)
	}

	if !slices.Contains(f.io.writes, "    Authentication required (<info>example.com</info>):") {
		t.Fatalf("writes %v", f.io.writes)
	}
}

func TestGit_RunCommandsCapturesRemoteCredentials(t *testing.T) {
	f := newGitFixture(map[string]any{"github-domains": list("github.com")})
	f.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("git", "remote", "-v"), Stdout: "origin\thttps://us%40er:p%3Ass@example.org/repo.git (fetch)\n"},
		{Cmd: util.Cmd("git", "fetch"), Stdout: "a"},
		{Cmd: util.Cmd("git", "status"), Stdout: "b"},
	}, true, nil)

	var output string
	if err := f.git.RunCommands([][]string{{"git", "fetch"}, {"git", "status"}}, "https://example.org/repo.git", "/tmp", false, &output); err != nil {
		t.Fatal(err)
	}

	if output != "ab" {
		t.Fatalf("output %q", output)
	}

	if a := f.io.auths["example.org"]; strOf(a.Username) != "us@er" || strOf(a.Password) != "p:ss" {
		t.Fatalf("auth %v", a)
	}
}

func TestGit_GetMirrorDefaultBranch(t *testing.T) {
	f := newGitFixture(nil)
	f.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("git", "remote", "show", "origin"), Stdout: "* remote origin\n  Fetch URL: x\n  HEAD branch: main\n"},
	}, true, nil)

	branch, ok := f.git.GetMirrorDefaultBranch("https://example.org/r.git", "/tmp", true)
	if !ok || branch != "main" {
		t.Fatalf("got %q %v", branch, ok)
	}
}

func TestGit_MaskCredentials(t *testing.T) {
	got := maskCredentials("a x-token-auth abc abcd abcdefg", []string{"x-token-auth", "abcdefg", "abcd", "abc"})
	if want := "a x-token-auth XXX XXX... XXX...efg"; got != want { // str_replace is sequential
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Git::checkForRepoOwnershipError joins the message with PHP_EOL, "\r\n" on
// Windows.
func TestGit_CheckForRepoOwnershipErrorWindowsEOL(t *testing.T) {
	php.SetEOLForTest(t, "\r\n")

	err := CheckForRepoOwnershipError("fatal: detected dubious ownership in repository", "/p", nil)
	if err == nil || err.Error() != "The repository at \"/p\" does not have the correct ownership and git refuses to use it:\r\n\r\nfatal: detected dubious ownership in repository" {
		t.Fatalf("got %q", err)
	}
}

func TestGit_CheckForRepoOwnershipError(t *testing.T) {
	if err := CheckForRepoOwnershipError("ok", "/p", nil); err != nil {
		t.Fatal(err)
	}

	err := CheckForRepoOwnershipError("fatal: detected dubious ownership in repository", "/p", nil)
	if err == nil || err.Error() != "The repository at \"/p\" does not have the correct ownership and git refuses to use it:"+php.EOL+php.EOL+"fatal: detected dubious ownership in repository" {
		t.Fatalf("got %v", err)
	}

	fio := newFakeIO()
	if err := CheckForRepoOwnershipError("fatal: detected dubious ownership", "/p", fio); err != nil || len(fio.writes) != 1 || !strings.HasPrefix(fio.writes[0], "<warning>") {
		t.Fatalf("got %v %v", err, fio.writes)
	}
}

func TestGit_DomainsRegex(t *testing.T) {
	c := newFakeConfig(map[string]any{"github-domains": list("github.com", "gh.example.org"), "gitlab-domains": list("gitlab.com")})
	if got := GetGitHubDomainsRegex(c); got != `(github\.com|gh\.example\.org)` {
		t.Fatalf("got %s", got)
	}

	if got := GetGitLabDomainsRegex(c); got != `(gitlab\.com)` {
		t.Fatalf("got %s", got)
	}
}
