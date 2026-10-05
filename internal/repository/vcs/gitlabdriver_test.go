package vcs

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/http/httpmock"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

// Ports tests/Composer/Test/Repository/Vcs/GitLabDriverTest.php.

func gitLabConfig(t *testing.T, extra ...any) *config.Config {
	t.Helper()

	return newConfig(t, t.TempDir(), append([]any{"gitlab-domains", php.ListOf(
		"mycompany.com/gitlab",
		"gitlab.mycompany.com",
		"othercompany.com/nested/gitlab",
		"gitlab.com",
		"gitlab.mycompany.local",
	)}, extra...)...)
}

var gitLabInitializeURLs = [][2]string{
	{"https://gitlab.com/mygroup/myproject", "https://gitlab.com/api/v4/projects/mygroup%2Fmyproject"},
	{"http://gitlab.com/mygroup/myproject", "http://gitlab.com/api/v4/projects/mygroup%2Fmyproject"},
	{"git@gitlab.com:mygroup/myproject", "https://gitlab.com/api/v4/projects/mygroup%2Fmyproject"},
}

// startGitLabDriver initializes a driver on url, answering the project
// request at apiURL with projectData.
func startGitLabDriver(t *testing.T, cfg *config.Config, repoConfig *php.Array, apiURL, projectData string) (*GitLabDriver, *httpmock.Downloader) {
	t.Helper()

	http := httpmock.New()
	http.Expects([]httpmock.Expectation{{URL: apiURL, Body: projectData}}, true, nil)

	driver := NewGitLabDriver(repoConfig, deps(newTestIO(), cfg, http, processmock.New()))
	noErr(t, driver.Initialize())
	assertHTTPComplete(t, http)

	return driver, http
}

func assertGitLabDriver(t *testing.T, driver *GitLabDriver, apiURL, root, repositoryURL, url string) {
	t.Helper()

	if got := driver.APIURL(); got != apiURL {
		t.Errorf("API URL = %q, want %q (derived from the repository URL)", got, apiURL)
	}

	assertRoot(t, driver, root)

	if got := driver.RepositoryURL(); got != repositoryURL {
		t.Errorf("repository URL = %q, want %q", got, repositoryURL)
	}

	if url != "" && driver.URL() != url {
		t.Errorf("URL = %q, want %q", driver.URL(), url)
	}
}

const gitLabPrivateProject = `{
    "id": 17,
    "default_branch": "mymaster",
    "visibility": "private",
    "issues_enabled": true,
    "archived": false,
    "http_url_to_repo": "https://gitlab.com/mygroup/myproject.git",
    "ssh_url_to_repo": "git@gitlab.com:mygroup/myproject.git",
    "last_activity_at": "2014-12-01T09:17:51.000+01:00",
    "name": "My Project",
    "name_with_namespace": "My Group / My Project",
    "path": "myproject",
    "path_with_namespace": "mygroup/myproject",
    "web_url": "https://gitlab.com/mygroup/myproject"
}`

func initializeGitLab(t *testing.T, url, apiURL string) (*GitLabDriver, *httpmock.Downloader) {
	t.Helper()

	driver, http := startGitLabDriver(t, gitLabConfig(t), php.ArrayOf("url", url), apiURL, gitLabPrivateProject)
	assertGitLabDriver(t, driver, apiURL, "mymaster", "git@gitlab.com:mygroup/myproject.git", "https://gitlab.com/mygroup/myproject")

	return driver, http
}

func TestGitLabDriver_Initialize(t *testing.T) {
	t.Parallel()

	for _, c := range gitLabInitializeURLs {
		initializeGitLab(t, c[0], c[1])
	}
}

const gitLabPublicProject = `{
    "id": 17,
    "default_branch": "mymaster",
    "visibility": "public",
    "http_url_to_repo": "https://gitlab.com/mygroup/myproject.git",
    "ssh_url_to_repo": "git@gitlab.com:mygroup/myproject.git",
    "last_activity_at": "2014-12-01T09:17:51.000+01:00",
    "name": "My Project",
    "name_with_namespace": "My Group / My Project",
    "path": "myproject",
    "path_with_namespace": "mygroup/myproject",
    "web_url": "https://gitlab.com/mygroup/myproject"
}`

func initializeGitLabPublic(t *testing.T, url, apiURL string) *GitLabDriver {
	t.Helper()

	driver, _ := startGitLabDriver(t, gitLabConfig(t), php.ArrayOf("url", url), apiURL, gitLabPublicProject)
	assertGitLabDriver(t, driver, apiURL, "mymaster", "https://gitlab.com/mygroup/myproject.git", "https://gitlab.com/mygroup/myproject")

	return driver
}

func TestGitLabDriver_InitializePublicProject(t *testing.T) {
	t.Parallel()

	for _, c := range gitLabInitializeURLs {
		initializeGitLabPublic(t, c[0], c[1])
	}
}

func TestGitLabDriver_InitializePublicProjectAsAnonymous(t *testing.T) {
	t.Parallel()

	projectData := `{
    "id": 17,
    "default_branch": "mymaster",
    "http_url_to_repo": "https://gitlab.com/mygroup/myproject.git",
    "ssh_url_to_repo": "git@gitlab.com:mygroup/myproject.git",
    "last_activity_at": "2014-12-01T09:17:51.000+01:00",
    "name": "My Project",
    "name_with_namespace": "My Group / My Project",
    "path": "myproject",
    "path_with_namespace": "mygroup/myproject",
    "web_url": "https://gitlab.com/mygroup/myproject"
}`

	for _, c := range gitLabInitializeURLs {
		driver, _ := startGitLabDriver(t, gitLabConfig(t), php.ArrayOf("url", c[0]), c[1], projectData)
		assertGitLabDriver(t, driver, c[1], "mymaster", "https://gitlab.com/mygroup/myproject.git", "https://gitlab.com/mygroup/myproject")
	}
}

// TestGitLabDriver_InitializeWithPortNumber: also support repositories
// over HTTP (TLS) and has a port number.
func TestGitLabDriver_InitializeWithPortNumber(t *testing.T) {
	t.Parallel()

	url := "https://gitlab.mycompany.com:5443/mygroup/myproject"
	apiURL := "https://gitlab.mycompany.com:5443/api/v4/projects/mygroup%2Fmyproject"

	// An incomplete single project API response payload.
	projectData := `{
    "default_branch": "1.0.x",
    "http_url_to_repo": "https://gitlab.mycompany.com:5443/mygroup/myproject.git",
    "path": "myproject",
    "path_with_namespace": "mygroup/myproject",
    "web_url": "https://gitlab.mycompany.com:5443/mygroup/myproject"
}`

	driver, _ := startGitLabDriver(t, gitLabConfig(t), php.ArrayOf("url", url), apiURL, projectData)
	assertGitLabDriver(t, driver, apiURL, "1.0.x", url+".git", url)
}

func TestGitLabDriver_InvalidSupportData(t *testing.T) {
	t.Parallel()

	repoURL := "https://gitlab.com/mygroup/myproject"
	driver, http := initializeGitLab(t, repoURL, "https://gitlab.com/api/v4/projects/mygroup%2Fmyproject")
	driver.branches = strMap("main", "SOMESHA")
	driver.tags = php.NewArray()

	http.Expects([]httpmock.Expectation{
		{URL: "https://gitlab.com/api/v4/projects/mygroup%2Fmyproject/repository/files/composer%2Ejson/raw?ref=SOMESHA", Body: `{"support": "` + repoURL + `" }`},
	}, true, nil)

	data, err := driver.ComposerInformation("main")
	noErr(t, err)

	if got := pathString(data, "support", "source"); got != "https://gitlab.com/mygroup/myproject/-/tree/main" {
		t.Fatalf("support.source = %q", got)
	}

	assertHTTPComplete(t, http)
}

func TestGitLabDriver_GetDist(t *testing.T) {
	t.Parallel()

	driver, _ := initializeGitLab(t, "https://gitlab.com/mygroup/myproject", "https://gitlab.com/api/v4/projects/mygroup%2Fmyproject")
	reference := "c3ebdbf9cceddb82cd2089aaef8c7b992e536363"

	assertArray(t, driver.Dist(reference), `{"type":"zip","url":"https://gitlab.com/api/v4/projects/mygroup%2Fmyproject/repository/archive.zip?sha=`+reference+`","reference":"`+reference+`","shasum":""}`)
}

func TestGitLabDriver_GetSource(t *testing.T) {
	t.Parallel()

	driver, _ := initializeGitLab(t, "https://gitlab.com/mygroup/myproject", "https://gitlab.com/api/v4/projects/mygroup%2Fmyproject")
	reference := "c3ebdbf9cceddb82cd2089aaef8c7b992e536363"

	assertArray(t, driver.Source(reference), `{"type":"git","url":"git@gitlab.com:mygroup/myproject.git","reference":"`+reference+`"}`)
}

func TestGitLabDriver_GetSource_GivenPublicProject(t *testing.T) {
	t.Parallel()

	driver := initializeGitLabPublic(t, "https://gitlab.com/mygroup/myproject", "https://gitlab.com/api/v4/projects/mygroup%2Fmyproject")
	reference := "c3ebdbf9cceddb82cd2089aaef8c7b992e536363"

	assertArray(t, driver.Source(reference), `{"type":"git","url":"https://gitlab.com/mygroup/myproject.git","reference":"`+reference+`"}`)
}

// assertRefsCached checks refs twice (they are cached) against want.
func assertRefsCached(t *testing.T, refs func() (*php.Array, error), want string) {
	t.Helper()

	for range 2 {
		got, err := refs()
		noErr(t, err)
		assertArray(t, got, want)
	}
}

func TestGitLabDriver_GetTags(t *testing.T) {
	t.Parallel()

	driver, _ := initializeGitLab(t, "https://gitlab.com/mygroup/myproject", "https://gitlab.com/api/v4/projects/mygroup%2Fmyproject")

	tagData := `[
    {
       "name": "v1.0.0",
        "commit": {
            "id": "092ed2c762bbae331e3f51d4a17f67310bf99a81",
            "committed_date": "2012-05-28T04:42:42-07:00"
        }
    },
    {
        "name": "v2.0.0",
        "commit": {
            "id": "8e8f60b3ec86d63733db3bd6371117a758027ec6",
            "committed_date": "2014-07-06T12:59:11.000+02:00"
        }
    }
]`

	http := httpmock.New()
	http.Expects([]httpmock.Expectation{{URL: "https://gitlab.com/api/v4/projects/mygroup%2Fmyproject/repository/tags?per_page=100", Body: tagData}}, true, nil)
	driver.SetHttpDownloader(http)

	assertRefsCached(t, driver.Tags, `{"v1.0.0":"092ed2c762bbae331e3f51d4a17f67310bf99a81","v2.0.0":"8e8f60b3ec86d63733db3bd6371117a758027ec6"}`)
	assertHTTPComplete(t, http)

	// the commit dates are kept for getChangeDate
	date, ok, err := driver.ChangeDate("092ed2c762bbae331e3f51d4a17f67310bf99a81")
	if err != nil || !ok || date.Format(dateRFC3339) != "2012-05-28T04:42:42-07:00" {
		t.Fatalf("change date %v %v %v", date, ok, err)
	}
}

func TestGitLabDriver_GetPaginatedRefs(t *testing.T) {
	t.Parallel()

	driver, _ := initializeGitLab(t, "https://gitlab.com/mygroup/myproject", "https://gitlab.com/api/v4/projects/mygroup%2Fmyproject")

	branch := func(name, id, date string) *php.Array {
		return php.ArrayOf("name", name, "commit", php.ArrayOf("id", id, "committed_date", date))
	}

	branchData := php.ListOf(
		branch("mymaster", "97eda36b5c1dd953a3792865c222d4e85e5f302e", "2013-01-03T21:04:07.000+01:00"),
		branch("staging", "502cffe49f136443f2059803f2e7192d1ac066cd", "2013-03-09T16:35:23.000+01:00"),
	)
	for range 98 {
		branchData.Append(branch("stagingdupe", "502cffe49f136443f2059803f2e7192d1ac066cd", "2013-03-09T16:35:23.000+01:00"))
	}

	body, err := php.JSONEncode(branchData, 0)
	noErr(t, err)

	page := func(n string) string {
		return "http://gitlab.com/api/v4/projects/mygroup%2Fmyproject/repository/tags?id=mygroup%2Fmyproject&page=" + n + "&per_page=20"
	}

	http := httpmock.New()
	http.Expects([]httpmock.Expectation{
		{
			URL:     "https://gitlab.com/api/v4/projects/mygroup%2Fmyproject/repository/branches?per_page=100",
			Body:    body,
			Headers: []string{`Link: <` + page("2") + `>; rel="next", <` + page("1") + `>; rel="first", <` + page("3") + `>; rel="last"`},
		},
		{
			URL:     page("2"),
			Body:    body,
			Headers: []string{`Link: <` + page("2") + `>; rel="prev", <` + page("1") + `>; rel="first", <` + page("3") + `>; rel="last"`},
		},
	}, true, nil)
	driver.SetHttpDownloader(http)

	assertRefsCached(t, driver.Branches, `{"mymaster":"97eda36b5c1dd953a3792865c222d4e85e5f302e","staging":"502cffe49f136443f2059803f2e7192d1ac066cd","stagingdupe":"502cffe49f136443f2059803f2e7192d1ac066cd"}`)
	assertHTTPComplete(t, http)
}

func TestGitLabDriver_GetBranches(t *testing.T) {
	t.Parallel()

	driver, _ := initializeGitLab(t, "https://gitlab.com/mygroup/myproject", "https://gitlab.com/api/v4/projects/mygroup%2Fmyproject")

	branchData := `[
    {
       "name": "mymaster",
        "commit": {
            "id": "97eda36b5c1dd953a3792865c222d4e85e5f302e",
            "committed_date": "2013-01-03T21:04:07.000+01:00"
        }
    },
    {
        "name": "staging",
        "commit": {
            "id": "502cffe49f136443f2059803f2e7192d1ac066cd",
            "committed_date": "2013-03-09T16:35:23.000+01:00"
        }
    }
]`

	http := httpmock.New()
	http.Expects([]httpmock.Expectation{{URL: "https://gitlab.com/api/v4/projects/mygroup%2Fmyproject/repository/branches?per_page=100", Body: branchData}}, true, nil)
	driver.SetHttpDownloader(http)

	assertRefsCached(t, driver.Branches, `{"mymaster":"97eda36b5c1dd953a3792865c222d4e85e5f302e","staging":"502cffe49f136443f2059803f2e7192d1ac066cd"}`)
	assertHTTPComplete(t, http)
}

func TestGitLabDriver_Supports(t *testing.T) {
	t.Parallel()

	cfg := gitLabConfig(t)

	for _, c := range []struct {
		url  string
		want bool
	}{
		{"http://gitlab.com/foo/bar", true},
		{"http://gitlab.mycompany.com:5443/foo/bar", true},
		{"http://gitlab.com/foo/bar/", true},
		{"http://gitlab.com/foo/bar/", true},
		{"http://gitlab.com/foo/bar.git", true},
		{"http://gitlab.com/foo/bar.git", true},
		{"http://gitlab.com/foo/bar.baz.git", true},
		{"https://gitlab.com/foo/bar", true},
		{"https://gitlab.mycompany.com:5443/foo/bar", true},
		{"git@gitlab.com:foo/bar.git", true},
		{"git@example.com:foo/bar.git", false},
		{"http://example.com/foo/bar", false},
		{"http://mycompany.com/gitlab/mygroup/myproject", true},
		{"https://mycompany.com/gitlab/mygroup/myproject", true},
		{"http://othercompany.com/nested/gitlab/mygroup/myproject", true},
		{"https://othercompany.com/nested/gitlab/mygroup/myproject", true},
		{"http://gitlab.com/mygroup/mysubgroup/mysubsubgroup/myproject", true},
		{"https://gitlab.com/mygroup/mysubgroup/mysubsubgroup/myproject", true},
	} {
		got, err := gitLabDriverType.Supports(deps(newTestIO(), cfg, nil, processmock.New()), c.url, false)
		if err != nil || got != c.want {
			t.Errorf("supports(%q) = %v, %v", c.url, got, err)
		}
	}
}

func TestGitLabDriver_GitlabSubDirectory(t *testing.T) {
	t.Parallel()

	apiURL := "https://mycompany.com/gitlab/api/v4/projects/mygroup%2Fmy-pro%2Eject"
	projectData := strings.NewReplacer("gitlab.com:mygroup/myproject", "gitlab.com:mygroup/my-pro.ject", "myproject.git", "my-pro.ject").Replace(gitLabPrivateProject)

	driver, _ := startGitLabDriver(t, gitLabConfig(t), php.ArrayOf("url", "https://mycompany.com/gitlab/mygroup/my-pro.ject"), apiURL, projectData)

	if driver.APIURL() != apiURL {
		t.Fatalf("API URL = %q", driver.APIURL())
	}
}

func TestGitLabDriver_GitlabSubGroup(t *testing.T) {
	t.Parallel()

	apiURL := "https://gitlab.com/api/v4/projects/mygroup%2Fmysubgroup%2Fmyproject"
	driver, _ := startGitLabDriver(t, gitLabConfig(t), php.ArrayOf("url", "https://gitlab.com/mygroup/mysubgroup/myproject"), apiURL, gitLabPrivateProject)

	if driver.APIURL() != apiURL {
		t.Fatalf("API URL = %q", driver.APIURL())
	}
}

func TestGitLabDriver_GitlabSubDirectorySubGroup(t *testing.T) {
	t.Parallel()

	apiURL := "https://mycompany.com/gitlab/api/v4/projects/mygroup%2Fmysubgroup%2Fmyproject"
	driver, _ := startGitLabDriver(t, gitLabConfig(t), php.ArrayOf("url", "https://mycompany.com/gitlab/mygroup/mysubgroup/myproject"), apiURL, gitLabPrivateProject)

	if driver.APIURL() != apiURL {
		t.Fatalf("API URL = %q", driver.APIURL())
	}
}

func TestGitLabDriver_ForwardsOptions(t *testing.T) {
	t.Parallel()

	options := php.ArrayOf("ssl", php.ArrayOf("verify_peer", false))

	http := httpmock.New()
	http.Expects([]httpmock.Expectation{
		{URL: "https://gitlab.mycompany.local/api/v4/projects/mygroup%2Fmyproject", Options: options, Body: gitLabPrivateProject},
	}, true, nil)

	driver := NewGitLabDriver(php.ArrayOf("url", "https://gitlab.mycompany.local/mygroup/myproject", "options", options), deps(newTestIO(), gitLabConfig(t), http, processmock.New()))
	noErr(t, driver.Initialize())
	assertHTTPComplete(t, http)
}

func TestGitLabDriver_ProtocolOverrideRepositoryUrlGeneration(t *testing.T) {
	t.Parallel()

	driver, _ := startGitLabDriver(t, gitLabConfig(t, "gitlab-protocol", "http"), php.ArrayOf("url", "git@gitlab.com:mygroup/myproject"), "https://gitlab.com/api/v4/projects/mygroup%2Fmyproject", gitLabPrivateProject)

	if got := driver.RepositoryURL(); got != "https://gitlab.com/mygroup/myproject.git" {
		t.Fatalf("repository URL = %q, want the http one the config requests", got)
	}
}
