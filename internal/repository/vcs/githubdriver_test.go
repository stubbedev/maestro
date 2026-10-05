package vcs

import (
	"encoding/base64"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/http/httpmock"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

// Ports tests/Composer/Test/Repository/Vcs/GitHubDriverTest.php.

const (
	ghTestURL    = "http://github.com/composer/packagist"
	ghTestAPIURL = "https://api.github.com/repos/composer/packagist"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func assertDistSource(t *testing.T, d Driver, sha, sourceRef, sourceURL string) {
	t.Helper()
	assertArray(t, d.Dist(sha), `{"type":"zip","url":"https://api.github.com/repos/composer/packagist/zipball/`+sha+`","reference":"`+sha+`","shasum":""}`)
	assertArray(t, d.Source(sourceRef), `{"type":"git","url":"`+sourceURL+`","reference":"`+sourceRef+`"}`)
}

func assertRoot(t *testing.T, d Driver, want string) {
	t.Helper()

	root, err := d.RootIdentifier()
	noErr(t, err)

	if root != want {
		t.Fatalf("root = %q, want %q", root, want)
	}
}

func TestGitHubDriver_PrivateRepository(t *testing.T) {
	t.Parallel()

	ioi := newTestIO()
	ioi.interactive = true
	ioi.replies = []string{"sometoken"}

	http := httpmock.New()
	http.Expects([]httpmock.Expectation{
		{URL: ghTestAPIURL, Status: 404},
		{URL: "https://api.github.com/", Body: "{}"},
		{URL: ghTestAPIURL, Body: `{"master_branch": "test_master", "private": true, "owner": {"login": "composer"}, "name": "packagist"}`},
	}, true, nil)

	process := processmock.New()
	process.Expects(nil, false, &processmock.Expectation{Return: 1})

	cfg := newConfig(t, t.TempDir())
	cfg.SetConfigSource(nopSource{"config.json"})
	cfg.SetAuthConfigSource(nopSource{"auth.json"})

	driver := NewGitHubDriver(php.ArrayOf("url", ghTestURL), deps(ioi, cfg, http, process))
	noErr(t, driver.Initialize())
	driver.tags = strMap("v0.0.0", "SOMESHA")

	assertRoot(t, driver, "test_master")
	assertDistSource(t, driver, "SOMESHA", "SOMESHA", "git@github.com:composer/packagist.git")
	assertHTTPComplete(t, http)

	if len(ioi.asked) != 1 || ioi.asked[0] != "Token (hidden): " {
		t.Fatalf("asked %q", ioi.asked)
	}

	auth := ioi.Authentication("github.com")
	if auth.Username == nil || *auth.Username != "sometoken" || auth.Password == nil || *auth.Password != "x-oauth-basic" {
		t.Fatalf("authentication %+v", auth)
	}
}

// newPublicGitHubDriver initializes a driver on ghTestURL answering
// the requests of expectations after the repository data request.
func newPublicGitHubDriver(t *testing.T, repoData string, expectations ...httpmock.Expectation) (*GitHubDriver, *httpmock.Downloader) {
	t.Helper()

	ioi := newTestIO()
	ioi.interactive = true

	http := httpmock.New()
	http.Expects(append([]httpmock.Expectation{{URL: ghTestAPIURL, Body: repoData}}, expectations...), true, nil)

	driver := NewGitHubDriver(php.ArrayOf("url", ghTestURL), deps(ioi, newConfig(t, t.TempDir()), http, processmock.New()))
	noErr(t, driver.Initialize())

	return driver, http
}

const gitHubPublicRepoData = `{"master_branch": "test_master", "owner": {"login": "composer"}, "name": "packagist"}`

func TestGitHubDriver_PublicRepository(t *testing.T) {
	t.Parallel()

	driver, http := newPublicGitHubDriver(t, gitHubPublicRepoData)
	driver.tags = strMap("v0.0.0", "SOMESHA")

	assertRoot(t, driver, "test_master")
	assertDistSource(t, driver, "SOMESHA", "SOMESHA", "https://github.com/composer/packagist.git")
	assertHTTPComplete(t, http)
}

func composerJSONResponses(composerJSON, funding string) []httpmock.Expectation {
	return []httpmock.Expectation{
		{URL: "https://api.github.com/repos/composer/packagist/contents/composer.json?ref=feature%2F3.2-foo", Body: `{"encoding":"base64","content":"` + b64(composerJSON) + `"}`},
		{URL: "https://api.github.com/repos/composer/packagist/commits/feature%2F3.2-foo", Body: `{"commit": {"committer":{ "date": "2012-09-10"}}}`},
		{URL: "https://api.github.com/repos/composer/packagist/contents/.github/FUNDING.yml", Body: `{"encoding": "base64", "content": "` + b64(funding) + `"}`},
	}
}

func TestGitHubDriver_PublicRepository2(t *testing.T) {
	t.Parallel()

	driver, http := newPublicGitHubDriver(t, gitHubPublicRepoData, composerJSONResponses(`{"support": {"source": "`+ghTestURL+`" }}`, "custom: https://example.com")...)
	driver.tags = strMap("feature/3.2-foo", "SOMESHA")

	assertRoot(t, driver, "test_master")
	assertDistSource(t, driver, "SOMESHA", "SOMESHA", "https://github.com/composer/packagist.git")

	data, err := driver.ComposerInformation("feature/3.2-foo")
	noErr(t, err)

	if data == nil || data.Has("abandoned") {
		t.Fatalf("data = %v", data)
	}

	assertArray(t, data, `{"support":{"source":"http://github.com/composer/packagist"},"time":"2012-09-10T00:00:00+00:00","funding":[{"type":"custom","url":"https://example.com"}]}`)
	assertHTTPComplete(t, http)
}

func TestGitHubDriver_InvalidSupportData(t *testing.T) {
	t.Parallel()

	driver, http := newPublicGitHubDriver(t, gitHubPublicRepoData, composerJSONResponses(`{"support": "`+ghTestURL+`" }`, "custom: https://example.com")...)
	driver.tags = strMap("feature/3.2-foo", "SOMESHA")
	driver.branches = strMap("test_master", "SOMESHA")

	data, err := driver.ComposerInformation("feature/3.2-foo")
	noErr(t, err)

	if got := pathString(data, "support", "source"); got != "https://github.com/composer/packagist/tree/feature/3.2-foo" {
		t.Fatalf("support.source = %q", got)
	}

	assertHTTPComplete(t, http)
}

func TestGitHubDriver_FundingFormat(t *testing.T) {
	t.Parallel()

	allNamedPlatforms := "community_bridge: project-name\ngithub: [userA, userB]\nissuehunt: userName\nko_fi: userName\nliberapay: userName\nopen_collective: userName\npatreon: userName\ntidelift: Platform/Package\npolar: userName\nbuy_me_a_coffee: userName\nthanks_dev: u/gh/userName\notechie: userName"

	for name, c := range map[string]struct{ funding, want string }{
		"All named platforms":                                                           {allNamedPlatforms, `[{"type":"community_bridge","url":"https://funding.communitybridge.org/projects/project-name"},{"type":"github","url":"https://github.com/userA"},{"type":"github","url":"https://github.com/userB"},{"type":"issuehunt","url":"https://issuehunt.io/r/userName"},{"type":"ko_fi","url":"https://ko-fi.com/userName"},{"type":"liberapay","url":"https://liberapay.com/userName"},{"type":"open_collective","url":"https://opencollective.com/userName"},{"type":"patreon","url":"https://www.patreon.com/userName"},{"type":"tidelift","url":"https://tidelift.com/funding/github/Platform/Package"},{"type":"polar","url":"https://polar.sh/userName"},{"type":"buy_me_a_coffee","url":"https://www.buymeacoffee.com/userName"},{"type":"thanks_dev","url":"https://thanks.dev/u/gh/userName"},{"type":"otechie","url":"https://otechie.com/userName"}]`},
		"Custom: single schemaless URL":                                                 {"custom: example.com", `[{"type":"custom","url":"https://example.com"}]`},
		"Custom: single schemaless URL in array format":                                 {"custom: [example.com]", `[{"type":"custom","url":"https://example.com"}]`},
		"Custom: double-quoted single URL":                                              {`custom: "https://example.com"`, `[{"type":"custom","url":"https://example.com"}]`},
		"Custom: double-quoted single URL in array format":                              {`custom: ["https://example.com"]`, `[{"type":"custom","url":"https://example.com"}]`},
		"Custom: array with quoted URL and schemaless unquoted URL":                     {`custom: ["https://example.com", example.org]`, `[{"type":"custom","url":"https://example.com"},{"type":"custom","url":"https://example.org"}]`},
		"Custom: array containing a non-simple scheme-less URL which will be discarded": {`custom: [example.net/funding, "https://example.com", example.org]`, `[{"type":"custom","url":"https://example.com"},{"type":"custom","url":"https://example.org"}]`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			driver, http := newPublicGitHubDriver(t, gitHubPublicRepoData, composerJSONResponses(`{"support": {"source": "`+ghTestURL+`" }}`, c.funding)...)
			driver.tags = strMap("feature/3.2-foo", "SOMESHA")
			driver.branches = strMap("test_master", "SOMESHA")

			data, err := driver.ComposerInformation("feature/3.2-foo")
			noErr(t, err)

			funding, _ := data.GetArray("funding")
			assertArray(t, php.ArrayValues(funding), c.want)
			assertHTTPComplete(t, http)
		})
	}
}

func TestGitHubDriver_PublicRepositoryArchived(t *testing.T) {
	t.Parallel()

	sha := "SOMESHA"
	driver, http := newPublicGitHubDriver(t, `{"master_branch": "test_master", "owner": {"login": "composer"}, "name": "packagist", "archived": true}`,
		httpmock.Expectation{URL: "https://api.github.com/repos/composer/packagist/contents/composer.json?ref=" + sha, Body: `{"encoding": "base64", "content": "` + b64(`{"name": "composer/packagist"}`) + `"}`},
		httpmock.Expectation{URL: "https://api.github.com/repos/composer/packagist/commits/" + sha, Body: `{"commit": {"committer":{ "date": "2012-09-10"}}}`},
		httpmock.Expectation{URL: "https://api.github.com/repos/composer/packagist/contents/.github/FUNDING.yml", Body: `{"encoding": "base64", "content": "` + b64("custom: https://example.com") + `"}`},
	)
	driver.tags = strMap("v0.0.0", sha)

	data, err := driver.ComposerInformation(sha)
	noErr(t, err)

	if v, _ := data.Get("abandoned"); v != true {
		t.Fatalf("abandoned = %v", v)
	}

	assertHTTPComplete(t, http)
}

func TestGitHubDriver_PrivateRepositoryNoInteraction(t *testing.T) {
	pinGitVersion(t)
	t.Setenv("COMPOSER_DISABLE_NETWORK", "")

	repoSSHURL := "git@github.com:composer/packagist.git"
	identifier := "v0.0.0"
	sha := "SOMESHA"

	http := httpmock.New()
	http.Expects([]httpmock.Expectation{{URL: ghTestAPIURL, Status: 404}}, true, nil)

	cacheVcsDir := filepath.Join(t.TempDir(), "composer-test", "cache")
	cfg := newConfig(t, t.TempDir(), "cache-vcs-dir", cacheVcsDir)

	process := processmock.New()
	process.Expects([]processmock.Expectation{
		{Cmd: processmock.Cmd("git", "config", "github.accesstoken").Cmd, Return: 1},
		processmock.Cmd("git", "clone", "--mirror", "--", repoSSHURL, cacheVcsDir+"/git-github.com-composer-packagist.git/"),
		processmock.Cmd("git", "remote", "-v"),
		processmock.Cmd("git", "remote", "set-url", "origin", "--", repoSSHURL),
		{Cmd: processmock.Cmd("git", "show-ref", "--tags", "--dereference").Cmd, Stdout: sha + " refs/tags/" + identifier},
		{Cmd: processmock.Cmd("git", "branch", "--no-color", "--no-abbrev", "-v").Cmd, Stdout: "  test_master     edf93f1fccaebd8764383dc12016d0a1a9672d89 Fix test & behavior"},
		{Cmd: processmock.Cmd("git", "branch", "--no-color").Cmd, Stdout: "* test_master"},
	}, true, nil)

	driver := NewGitHubDriver(php.ArrayOf("url", ghTestURL), deps(newTestIO(), cfg, http, process))
	noErr(t, driver.Initialize())

	assertRoot(t, driver, "test_master")
	assertDistSource(t, driver, sha, identifier, repoSSHURL)
	assertArray(t, driver.Source(sha), `{"type":"git","url":"`+repoSSHURL+`","reference":"`+sha+`"}`)
	assertProcessComplete(t, process)
	assertHTTPComplete(t, http)
}

func TestGitHubDriver_InitializeInvalidRepoUrl(t *testing.T) {
	t.Parallel()

	for _, url := range []string{
		"https://github.com/acme",
		"https://github.com/acme/repository/releases",
		"https://github.com/acme/repository/pulls",
	} {
		driver := NewGitHubDriver(php.ArrayOf("url", url), deps(newTestIO(), newConfig(t, t.TempDir()), httpmock.New(), processmock.New()))
		expectError[*invalidArgument](t, driver.Initialize(), "The GitHub repository URL "+url+" is invalid.")
	}
}

func TestGitHubDriver_Supports(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		want bool
		url  string
	}{
		{false, "https://github.com/acme"},
		{true, "https://github.com/acme/repository"},
		{true, "git@github.com:acme/repository.git"},
		{false, "https://github.com/acme/repository/releases"},
		{false, "https://github.com/acme/repository/pulls"},
	} {
		got, err := gitHubDriverType.Supports(deps(newTestIO(), newConfig(t, t.TempDir()), nil, processmock.New()), c.url, false)
		if err != nil || got != c.want {
			t.Errorf("supports(%q) = %v, %v", c.url, got, err)
		}
	}
}

func TestGitHubDriver_GetEmptyFileContent(t *testing.T) {
	t.Parallel()

	driver, http := newPublicGitHubDriver(t, `{"master_branch": "test_master", "owner": {"login": "composer"}, "name": "packagist", "archived": true}`,
		httpmock.Expectation{URL: "https://api.github.com/repos/composer/packagist/contents/composer.json?ref=main", Body: `{"encoding":"base64","content":""}`},
	)

	content, ok, err := driver.FileContent("composer.json", "main")
	if err != nil || !ok || content != "" {
		t.Fatalf("got %q %v %v", content, ok, err)
	}

	assertHTTPComplete(t, http)
}
