package vcs

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/http/httpmock"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

// Ports tests/Composer/Test/Repository/Vcs/{GitBitbucketDriverTest,ForgejoDriverTest}.php.

func startBitbucketDriver(t *testing.T, url string, expectations ...httpmock.Expectation) (*GitBitbucketDriver, *httpmock.Downloader) {
	t.Helper()

	http := httpmock.New()
	http.Expects(expectations, true, nil)

	driver := NewGitBitbucketDriver(php.ArrayOf("url", url), deps(newTestIO(), newConfig(t, t.TempDir()), http, processmock.New()))

	return driver, http
}

const (
	bitbucketRepoAPI     = "https://api.bitbucket.org/2.0/repositories/user/repo?fields=-project%2C-owner"
	bitbucketTagsAPI     = "https://api.bitbucket.org/2.0/repositories/user/repo/refs/tags?pagelen=100&fields=values.name%2Cvalues.target.hash%2Cnext&sort=-target.date"
	bitbucketBranchesAPI = "https://api.bitbucket.org/2.0/repositories/user/repo/refs/branches?pagelen=100&fields=values.name%2Cvalues.target.hash%2Cvalues.heads%2Cnext&sort=-target.date"
	bitbucketRepoData    = `{"mainbranch": {"name": "main"}, "scm":"git","website":"","has_wiki":false,"name":"repo","links":{"branches":{"href":"https:\/\/api.bitbucket.org\/2.0\/repositories\/user\/repo\/refs\/branches"},"tags":{"href":"https:\/\/api.bitbucket.org\/2.0\/repositories\/user\/repo\/refs\/tags"},"clone":[{"href":"https:\/\/user@bitbucket.org\/user\/repo.git","name":"https"},{"href":"ssh:\/\/git@bitbucket.org\/user\/repo.git","name":"ssh"}],"html":{"href":"https:\/\/bitbucket.org\/user\/repo"}},"language":"php","created_on":"2015-02-18T16:22:24.688+00:00","updated_on":"2016-05-17T13:20:21.993+00:00","is_private":true,"has_issues":false}`
	bitbucketTags        = `{"values":[{"name":"1.0.1","target":{"hash":"9b78a3932143497c519e49b8241083838c8ff8a1"}},{"name":"1.0.0","target":{"hash":"d3393d514318a9267d2f8ebbf463a9aaa389f8eb"}}]}`
	bitbucketBranches    = `{"values":[{"name":"main","target":{"hash":"937992d19d72b5116c3e8c4a04f960e5fa270b22"}}]}`
)

func TestGitBitbucketDriver_GetRootIdentifierWrongScmType(t *testing.T) {
	t.Parallel()

	driver, _ := startBitbucketDriver(t, "https://bitbucket.org/user/repo.git", httpmock.Expectation{
		URL:  bitbucketRepoAPI,
		Body: `{"scm":"hg","website":"","has_wiki":false,"name":"repo","links":{"branches":{"href":"https:\/\/api.bitbucket.org\/2.0\/repositories\/user\/repo\/refs\/branches"},"tags":{"href":"https:\/\/api.bitbucket.org\/2.0\/repositories\/user\/repo\/refs\/tags"},"clone":[{"href":"https:\/\/user@bitbucket.org\/user\/repo","name":"https"},{"href":"ssh:\/\/hg@bitbucket.org\/user\/repo","name":"ssh"}],"html":{"href":"https:\/\/bitbucket.org\/user\/repo"}},"language":"php","created_on":"2015-02-18T16:22:24.688+00:00","updated_on":"2016-05-17T13:20:21.993+00:00","is_private":true,"has_issues":false}`,
	})
	noErr(t, driver.Initialize())

	_, err := driver.RootIdentifier()
	expectRuntimeError(t, err, "https://bitbucket.org/user/repo.git does not appear to be a git repository, use https://bitbucket.org/user/repo but remember that Bitbucket no longer supports the mercurial repositories. https://bitbucket.org/blog/sunsetting-mercurial-support-in-bitbucket")
}

func TestGitBitbucketDriver_Driver(t *testing.T) {
	t.Parallel()

	driver, http := startBitbucketDriver(t, "https://bitbucket.org/user/repo.git",
		httpmock.Expectation{URL: bitbucketRepoAPI, Body: bitbucketRepoData},
		httpmock.Expectation{URL: bitbucketTagsAPI, Body: bitbucketTags},
		httpmock.Expectation{URL: bitbucketBranchesAPI, Body: bitbucketBranches},
		httpmock.Expectation{URL: "https://api.bitbucket.org/2.0/repositories/user/repo/src/main/composer.json", Body: `{"name": "user/repo","description": "test repo","license": "GPL","authors": [{"name": "Name","email": "local@domain.tld"}],"require": {"creator/package": "^1.0"},"require-dev": {"phpunit/phpunit": "~4.8"}}`},
		httpmock.Expectation{URL: "https://api.bitbucket.org/2.0/repositories/user/repo/commit/main?fields=date", Body: `{"date": "2016-05-17T13:19:52+00:00"}`},
	)
	noErr(t, driver.Initialize())

	assertRoot(t, driver, "main")

	tags, err := driver.Tags()
	noErr(t, err)
	assertArray(t, tags, `{"1.0.1":"9b78a3932143497c519e49b8241083838c8ff8a1","1.0.0":"d3393d514318a9267d2f8ebbf463a9aaa389f8eb"}`)

	branches, err := driver.Branches()
	noErr(t, err)
	assertArray(t, branches, `{"main":"937992d19d72b5116c3e8c4a04f960e5fa270b22"}`)

	data, err := driver.ComposerInformation("main")
	noErr(t, err)
	assertArray(t, data, `{"name":"user/repo","description":"test repo","license":"GPL","authors":[{"name":"Name","email":"local@domain.tld"}],"require":{"creator/package":"^1.0"},"require-dev":{"phpunit/phpunit":"~4.8"},"time":"2016-05-17T13:19:52+00:00","support":{"source":"https://bitbucket.org/user/repo/src/937992d19d72b5116c3e8c4a04f960e5fa270b22/?at=main"},"homepage":"https://bitbucket.org/user/repo"}`)
	assertHTTPComplete(t, http)

	// testGetParams
	url := "https://bitbucket.org/user/repo.git"
	if driver.URL() != url {
		t.Fatalf("URL = %q", driver.URL())
	}

	assertArray(t, driver.Dist("reference"), `{"type":"zip","url":"https://bitbucket.org/user/repo/get/reference.zip","reference":"reference","shasum":""}`)
	assertArray(t, driver.Source("reference"), `{"type":"git","url":"`+url+`","reference":"reference"}`)
}

func TestGitBitbucketDriver_InitializeInvalidRepositoryUrl(t *testing.T) {
	t.Parallel()

	driver, _ := startBitbucketDriver(t, "https://bitbucket.org/acme")
	expectError[*invalidArgument](t, driver.Initialize(), "The Bitbucket repository URL https://bitbucket.org/acme is invalid. It must be the HTTPS URL of a Bitbucket repository.")
}

func TestGitBitbucketDriver_InvalidSupportData(t *testing.T) {
	t.Parallel()

	repoURL := "https://bitbucket.org/user/repo.git"
	driver, http := startBitbucketDriver(t, repoURL,
		httpmock.Expectation{URL: bitbucketRepoAPI, Body: bitbucketRepoData},
		httpmock.Expectation{URL: "https://api.bitbucket.org/2.0/repositories/user/repo/src/main/composer.json", Body: `{"support": "` + repoURL + `"}`},
		httpmock.Expectation{URL: "https://api.bitbucket.org/2.0/repositories/user/repo/commit/main?fields=date", Body: `{"date": "2016-05-17T13:19:52+00:00"}`},
		httpmock.Expectation{URL: bitbucketTagsAPI, Body: bitbucketTags},
		httpmock.Expectation{URL: bitbucketBranchesAPI, Body: bitbucketBranches},
	)
	noErr(t, driver.Initialize())

	_, err := driver.RootIdentifier()
	noErr(t, err)

	data, err := driver.ComposerInformation("main")
	noErr(t, err)

	if got := pathString(data, "support", "source"); got != "https://bitbucket.org/user/repo/src/937992d19d72b5116c3e8c4a04f960e5fa270b22/?at=main" {
		t.Fatalf("support.source = %q", got)
	}

	assertHTTPComplete(t, http)
}

func TestGitBitbucketDriver_Supports(t *testing.T) {
	t.Parallel()

	d := deps(newTestIO(), newConfig(t, t.TempDir()), nil, processmock.New())

	for url, want := range map[string]bool{
		"https://bitbucket.org/user/repo.git": true,
		// should not be changed, see https://github.com/composer/composer/issues/9400
		"git@bitbucket.org:user/repo.git":  false,
		"https://github.com/user/repo.git": false,
	} {
		if got, err := gitBitbucketDriverType.Supports(d, url, false); err != nil || got != want {
			t.Errorf("supports(%q) = %v, %v", url, got, err)
		}
	}
}

const forgejoRepoData = `{"default_branch":"main","has_issues":true,"archived":false,"private":false,"html_url":"https:\/\/codeberg.org\/acme\/repo","ssh_url":"git@codeberg.org:acme\/repo.git","clone_url":"https:\/\/codeberg.org\/acme\/repo.git"}`

func startForgejoDriver(t *testing.T, expectations ...httpmock.Expectation) (*ForgejoDriver, *httpmock.Downloader) {
	t.Helper()

	ioi := newTestIO()
	ioi.interactive = true

	http := httpmock.New()
	http.Expects(append([]httpmock.Expectation{{URL: "https://codeberg.org/api/v1/repos/acme/repo", Body: forgejoRepoData}}, expectations...), true, nil)

	driver := NewForgejoDriver(php.ArrayOf("url", "https://codeberg.org/acme/repo.git"), deps(ioi, newConfig(t, t.TempDir(), "forgejo-domains", php.ListOf("codeberg.org")), http, processmock.New()))
	noErr(t, driver.Initialize())

	return driver, http
}

func TestForgejoDriver_PublicRepository(t *testing.T) {
	t.Parallel()

	driver, http := startForgejoDriver(t)
	assertRoot(t, driver, "main")

	sha := "SOMESHA"
	assertArray(t, driver.Dist(sha), `{"type":"zip","url":"https://codeberg.org/api/v1/repos/acme/repo/archive/SOMESHA.zip","reference":"SOMESHA","shasum":""}`)
	assertArray(t, driver.Source(sha), `{"type":"git","url":"https://codeberg.org/acme/repo.git","reference":"SOMESHA"}`)
	assertHTTPComplete(t, http)
}

func TestForgejoDriver_GetBranches(t *testing.T) {
	t.Parallel()

	driver, http := startForgejoDriver(t, httpmock.Expectation{URL: "https://codeberg.org/api/v1/repos/acme/repo/branches?per_page=100", Body: `[{"name":"main","commit":{"id":"SOMESHA"}}]`})

	branches, err := driver.Branches()
	noErr(t, err)
	assertArray(t, branches, `{"main":"SOMESHA"}`)
	assertHTTPComplete(t, http)
}

func TestForgejoDriver_GetTags(t *testing.T) {
	t.Parallel()

	driver, http := startForgejoDriver(t, httpmock.Expectation{URL: "https://codeberg.org/api/v1/repos/acme/repo/tags?per_page=100", Body: `[{"name":"1.0","commit":{"sha":"SOMESHA"}}]`})

	tags, err := driver.Tags()
	noErr(t, err)
	assertArray(t, tags, `{"1.0":"SOMESHA"}`)
	assertHTTPComplete(t, http)
}

func TestForgejoDriver_GetEmptyFileContent(t *testing.T) {
	t.Parallel()

	driver, http := startForgejoDriver(t, httpmock.Expectation{URL: "https://codeberg.org/api/v1/repos/acme/repo/contents/composer.json?ref=main", Body: `{"encoding":"base64","content":""}`})

	content, ok, err := driver.FileContent("composer.json", "main")
	if err != nil || !ok || content != "" {
		t.Fatalf("got %q %v %v", content, ok, err)
	}

	assertHTTPComplete(t, http)
}

func TestForgejoDriver_Supports(t *testing.T) {
	t.Parallel()

	d := deps(newTestIO(), newConfig(t, t.TempDir(), "forgejo-domains", php.ListOf("codeberg.org")), nil, processmock.New())

	for url, want := range map[string]bool{
		"https://example.org/acme/repo":        false,
		"https://codeberg.org/acme/repository": true,
	} {
		if got, err := forgejoDriverType.Supports(d, url, false); err != nil || got != want {
			t.Errorf("supports(%q) = %v, %v", url, got, err)
		}
	}
}
