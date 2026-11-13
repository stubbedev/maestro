package http

import (
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

var baseHeaders = []string{"Accept-Encoding: gzip", "Connection: close"}

func headerOptions(headers []string) *php.Array {
	return php.ArrayOf("http", php.ArrayOf("header", php.StringList(headers)))
}

// expectsAuthentication is AuthHelperTest::expectsAuthentication.
func expectsAuthentication(fio *fakeIO, origin string, a ioAuth) {
	fio.hasAuth = func(o string) bool { return o == origin }
	fio.getAuth = func(string) ioAuth { return a }
}

func assertAuthCalledOnce(t *testing.T, fio *fakeIO, origin string) {
	t.Helper()

	if fio.calls["hasAuthentication"] != 1 || fio.args["hasAuthentication"][0] != origin || fio.calls["getAuthentication"] > 1 {
		t.Fatalf("hasAuthentication %v, getAuthentication %v", fio.args["hasAuthentication"], fio.args["getAuthentication"])
	}
}

func assertHeaders(t *testing.T, want []string, options *php.Array) {
	t.Helper()

	if got := headerList(options); !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func assertDebugWrite(t *testing.T, fio *fakeIO, message string) {
	t.Helper()

	if len(fio.writes) != 1 || fio.writes[0] != message+"|true|16" {
		t.Fatalf("writes %q, want %q", fio.writes, message)
	}
}

func TestAuthHelper_AddAuthenticationHeaderWithoutAuthCredentials(t *testing.T) {
	fio := newFakeIO()
	h := NewAuthHelper(fio, newFakeConfig(nil))

	options := h.AddAuthenticationOptions(headerOptions(baseHeaders), "http://example.org", "file://"+thisFile(t))

	assertHeaders(t, baseHeaders, options)
	assertAuthCalledOnce(t, fio, "http://example.org")
}

func TestAuthHelper_AddAuthenticationHeaderWithBearerPassword(t *testing.T) {
	fio := newFakeIO()
	h := NewAuthHelper(fio, newFakeConfig(nil))
	origin := "http://example.org"
	expectsAuthentication(fio, origin, auth("my_username", "bearer"))

	options := h.AddAuthenticationOptions(headerOptions(baseHeaders), origin, "file://"+thisFile(t))

	assertHeaders(t, append(slices.Clone(baseHeaders), "Authorization: Bearer my_username"), options)
	assertAuthCalledOnce(t, fio, origin)
	assertDebugWrite(t, fio, "Using Bearer token authentication")
}

func TestAuthHelper_AddAuthenticationHeaderWithGithubToken(t *testing.T) {
	fio := newFakeIO()
	h := NewAuthHelper(fio, newFakeConfig(nil))
	expectsAuthentication(fio, "github.com", auth("my_username", "x-oauth-basic"))

	options := h.AddAuthenticationOptions(headerOptions(baseHeaders), "github.com", "https://api.github.com/")

	assertHeaders(t, append(slices.Clone(baseHeaders), "Authorization: token my_username"), options)
	assertDebugWrite(t, fio, "Using GitHub token authentication")
}

func TestAuthHelper_AddAuthenticationOptionsForClientCertificate(t *testing.T) {
	fio := newFakeIO()
	h := NewAuthHelper(fio, newFakeConfig(nil))
	certificateConfiguration := `{"local_cert":"certificate value","local_pk":"key value","passphrase":"passphrase value"}`
	expectsAuthentication(fio, "example.org", auth("client-certificate", certificateConfiguration))

	options := h.AddAuthenticationOptions(php.NewArray(), "example.org", "file://"+thisFile(t))

	if got := jsonOf(t, options.At("ssl")); got != certificateConfiguration {
		t.Fatalf("got %s", got)
	}
}

func TestAuthHelper_AddAuthenticationHeaderWithGitlabToken(t *testing.T) {
	const (
		repositoryArchive = "https://gitlab.com/api/v4/projects/foo%2Fbar/repository/archive.zip?sha=abc"
		jobTokenMessage   = "Using GitLab CI job token authentication"
		jobTokenHeader    = "JOB-TOKEN: my_token"
		basicMessage      = `Using HTTP basic authentication with username "gitlab-ci-token"`
		basicHeader       = "Authorization: Basic Z2l0bGFiLWNpLXRva2VuOm15X3Rva2Vu"
	)

	for _, tc := range []struct {
		name           string
		username, pass string
		expectedLog    string
		expectedHeader string
		url            string
	}{
		{"oauth token", "my_token", "oauth2", "Using GitLab OAuth token authentication", "Authorization: Bearer my_token", repositoryArchive},
		{"private token", "my_token", "private-token", "Using GitLab private token authentication", "PRIVATE-TOKEN: my_token", repositoryArchive},
		// GitLab's Composer package registry only accepts job tokens via basic auth
		{"CI job token, registry", "gitlab-ci-token", "my_token", basicMessage, basicHeader, "https://gitlab.com/api/v4/group/123/-/packages/composer/packages.json"},
		// GitLab.AuthorizeOAuth swaps the slots, so both orderings must work
		{"CI job token, slots swapped, registry", "my_token", "gitlab-ci-token", basicMessage, basicHeader, "https://gitlab.com/api/v4/projects/123/packages/composer/archives/foo/bar.zip?sha=abc"},
		{"CI job token, registry on sub-path install", "gitlab-ci-token", "my_token", basicMessage, basicHeader, "https://gitlab.com/gitlab/api/v4/group/123/-/packages/composer/p2/foo/bar.json"},
		{"CI job token, project registry root", "gitlab-ci-token", "my_token", basicMessage, basicHeader, "https://gitlab.com/api/v4/projects/123/packages/composer"},
		// everywhere else GitLab accepts job tokens via the JOB-TOKEN header
		{"CI job token, repository API", "gitlab-ci-token", "my_token", jobTokenMessage, jobTokenHeader, repositoryArchive},
		{"CI job token, slots swapped, repository API", "my_token", "gitlab-ci-token", jobTokenMessage, jobTokenHeader, "https://gitlab.com/api/v4/projects/foo%2Fbar/repository/files/composer%2Ejson/raw?ref=abc"},
		{"CI job token, repository API on sub-path install", "gitlab-ci-token", "my_token", jobTokenMessage, jobTokenHeader, "https://gitlab.com/gitlab/api/v4/projects/123/repository/tags?per_page=100"},
		{"CI job token, job artifacts", "gitlab-ci-token", "my_token", jobTokenMessage, jobTokenHeader, "https://gitlab.com/api/v4/projects/123/jobs/artifacts/main/download?job=build"},
		{"CI job token, registry-like query string", "gitlab-ci-token", "my_token", jobTokenMessage, jobTokenHeader, "https://gitlab.com/api/v4/projects/123/repository/files/foo/raw?ref=/api/v4/projects/x/packages/composer/"},
		{"CI job token, project root", "gitlab-ci-token", "my_token", jobTokenMessage, jobTokenHeader, "https://gitlab.com/api/v4/projects/foo%2Fbar"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fio := newFakeIO()
			config := newFakeConfig(map[string]any{"gitlab-domains": list("gitlab.com")})
			h := NewAuthHelper(fio, config)
			expectsAuthentication(fio, "gitlab.com", auth(tc.username, tc.pass))

			options := h.AddAuthenticationOptions(headerOptions(baseHeaders), "gitlab.com", tc.url)

			assertHeaders(t, append(slices.Clone(baseHeaders), tc.expectedHeader), options)
			assertDebugWrite(t, fio, tc.expectedLog)
		})
	}
}

// A job token stored as http-basic reaches the helper in the same slots as
// one stored as gitlab-token, and a plain token is a personal access token,
// whatever the URL.
func TestAuthHelper_GitLabCiJobTokenAuthentication(t *testing.T) {
	const origin = "gitlab.example.com"
	const basic = "Authorization: Basic Z2l0bGFiLWNpLXRva2VuOmpvYi10b2tlbi12YWx1ZQ=="
	const header = "JOB-TOKEN: job-token-value"

	for _, tc := range []struct{ name, url, want string }{
		{"registry: group packages.json", "https://gitlab.example.com/api/v4/group/123/-/packages/composer/packages.json", basic},
		{"registry: group p2 provider", "https://gitlab.example.com/api/v4/group/123/-/packages/composer/p2/acme/package.json", basic},
		{"registry: project dist archive", "https://gitlab.example.com/api/v4/projects/123/packages/composer/archives/acme/package.zip?sha=abc123", basic},
		{"repository API: archive", "https://gitlab.example.com/api/v4/projects/acme%2Fpackage/repository/archive.zip?sha=abc123", header},
		{"repository API: raw file", "https://gitlab.example.com/api/v4/projects/acme%2Fpackage/repository/files/composer%2Ejson/raw?ref=abc123", header},
		{"repository API: tags", "https://gitlab.example.com/api/v4/projects/acme%2Fpackage/repository/tags?per_page=100", header},
		{"job artifacts", "https://gitlab.example.com/api/v4/projects/123/jobs/artifacts/main/download?job=build", header},
	} {
		for slots, credentials := range map[string]ioAuth{
			"marker as username": auth("gitlab-ci-token", "job-token-value"),
			"marker as password": auth("job-token-value", "gitlab-ci-token"),
		} {
			t.Run(tc.name+", "+slots, func(t *testing.T) {
				fio := newFakeIO()
				h := NewAuthHelper(fio, newFakeConfig(map[string]any{"gitlab-domains": list(origin)}))
				expectsAuthentication(fio, origin, credentials)

				assertHeaders(t, []string{tc.want}, h.AddAuthenticationOptions(php.NewArray(), origin, tc.url))
			})
		}
	}

	for _, url := range []string{
		"https://gitlab.example.com/api/v4/group/123/-/packages/composer/packages.json",
		"https://gitlab.example.com/api/v4/projects/acme%2Fpackage/repository/archive.zip?sha=abc123",
	} {
		fio := newFakeIO()
		h := NewAuthHelper(fio, newFakeConfig(map[string]any{"gitlab-domains": list(origin)}))
		expectsAuthentication(fio, origin, auth("pat-value", "private-token"))

		assertHeaders(t, []string{"PRIVATE-TOKEN: pat-value"}, h.AddAuthenticationOptions(php.NewArray(), origin, url))
	}
}

func TestAuthHelper_AddAuthenticationHeaderWithBitbucketOathToken(t *testing.T) {
	fio := newFakeIO()
	h := NewAuthHelper(fio, newFakeConfig(nil))
	expectsAuthentication(fio, "bitbucket.org", auth("x-token-auth", "my_password"))

	options := h.AddAuthenticationOptions(headerOptions(baseHeaders), "bitbucket.org", "https://bitbucket.org/site/oauth2/authorize")

	assertHeaders(t, append(slices.Clone(baseHeaders), "Authorization: Bearer my_password"), options)
	assertDebugWrite(t, fio, "Using Bitbucket OAuth token authentication")
}

var bitbucketPublicURLs = []string{
	"https://bitbucket.org/user/repo/downloads/whatever",
	"https://bbuseruploads.s3.amazonaws.com/9421ee72-638e-43a9-82ea-39cfaae2bfaa/downloads/b87c59d9-54f3-4922-b711-d89059ec3bcf",
}

func TestAuthHelper_AddAuthenticationHeaderWithBitbucketPublicUrl(t *testing.T) {
	for _, url := range bitbucketPublicURLs {
		fio := newFakeIO()
		h := NewAuthHelper(fio, newFakeConfig(nil))
		expectsAuthentication(fio, "bitbucket.org", auth("x-token-auth", "my_password"))

		options := h.AddAuthenticationOptions(headerOptions(baseHeaders), "bitbucket.org", url)

		assertHeaders(t, baseHeaders, options)
	}
}

var basicHTTPAuthenticationCases = []struct {
	url, origin, username, password string
}{
	{BitbucketOAuth2AccessTokenURL, "bitbucket.org", "x-token-auth", "my_password"},
	{"https://some-api.url.com", "some-api.url.com", "my_username", "my_password"},
	{"https://gitlab.com", "gitlab.com", "my_username", "my_password"},
}

func TestAuthHelper_AddAuthenticationHeaderWithBasicHttpAuthentication(t *testing.T) {
	for _, tc := range basicHTTPAuthenticationCases {
		fio := newFakeIO()
		h := NewAuthHelper(fio, newFakeConfig(nil))
		expectsAuthentication(fio, tc.origin, auth(tc.username, tc.password))

		options := h.AddAuthenticationOptions(headerOptions(baseHeaders), tc.origin, tc.url)

		assertHeaders(t, append(slices.Clone(baseHeaders), "Authorization: Basic "+base64.StdEncoding.EncodeToString([]byte(tc.username+":"+tc.password))), options)
		assertDebugWrite(t, fio, `Using HTTP basic authentication with username "`+tc.username+`"`)
	}
}

func TestAuthHelper_AddAuthenticationHeaderWithBasicHttpAuthenticationMasksTokenUsername(t *testing.T) {
	fio := newFakeIO()
	h := NewAuthHelper(fio, newFakeConfig(nil))
	username := "ghp_1234567890abcdefghijklmnopqrstuvwxyzAB"
	expectsAuthentication(fio, "some-api.url.com", auth(username, "x-oauth-basic"))

	options := h.AddAuthenticationOptions(php.ArrayOf("http", php.ArrayOf("header", php.NewArray())), "some-api.url.com", "https://some-api.url.com")

	// only the first 3 chars are kept, enough to tell which kind of token
	// is in use
	assertDebugWrite(t, fio, `Using HTTP basic authentication with username "ghp***"`)
	// the actual auth header must still contain the real credentials
	assertHeaders(t, []string{"Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(username+":x-oauth-basic"))}, options)
}

func TestAuthHelper_AddAuthenticationHeaderWithCustomHeaders(t *testing.T) {
	fio := newFakeIO()
	h := NewAuthHelper(fio, newFakeConfig(nil))
	customHeaders := []string{"API-TOKEN: abc123", "X-CUSTOM-HEADER: value"}
	expectsAuthentication(fio, "example.org", auth(`["API-TOKEN: abc123","X-CUSTOM-HEADER: value"]`, "custom-headers"))

	got := h.AddAuthenticationHeader(baseHeaders, "example.org", "https://example.org/packages.json")

	if want := append(slices.Clone(baseHeaders), customHeaders...); !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}

	assertDebugWrite(t, fio, "Using custom HTTP headers for authentication")
}

func TestAuthHelper_AddAuthenticationOptionsWarnsAndIgnoresNonStringCustomHeaders(t *testing.T) {
	fio := newFakeIO()
	h := NewAuthHelper(fio, newFakeConfig(nil))
	expectsAuthentication(fio, "example.org", auth(`{"X-Foo": 1, "X-Bar": ["a"]}`, "custom-headers"))

	options := h.AddAuthenticationOptions(php.NewArray(), "example.org", "https://example.org/packages.json")
	h.AddAuthenticationOptions(php.NewArray(), "example.org", "https://example.org/p2/foo/bar.json")

	assertHeaders(t, nil, options)

	want := "<warning>Ignoring custom-headers authentication for example.org as it is not a list of header strings</warning>|true|2"
	if len(fio.writes) != 1 || fio.writes[0] != want {
		t.Fatalf("writes %q, want %q", fio.writes, want)
	}
}

func TestAuthHelper_DisplayMessageOncePerOrigin(t *testing.T) {
	fio := newFakeIO()
	h := NewAuthHelper(fio, newFakeConfig(nil))
	expectsAuthentication(fio, "example.org", auth("u", "bearer"))
	fio.getAuth = func(string) ioAuth { return auth("u", "p") }

	h.AddAuthenticationOptions(nil, "example.org", "https://example.org/a")
	h.AddAuthenticationOptions(nil, "example.org", "https://example.org/b")

	if len(fio.writes) != 1 {
		t.Fatalf("writes %q", fio.writes)
	}
}

func TestAuthHelper_IsPublicBitBucketDownloadWithBitbucketPublicUrl(t *testing.T) {
	for _, url := range bitbucketPublicURLs {
		if !IsPublicBitBucketDownload(url) {
			t.Errorf("%s should be public", url)
		}
	}
}

func TestAuthHelper_IsPublicBitBucketDownloadWithNonBitbucketPublicUrl(t *testing.T) {
	if IsPublicBitBucketDownload("https://bitbucket.org/site/oauth2/authorize") {
		t.Fatal("expected a private download")
	}
}

func TestAuthHelper_StoreAuthAutomatically(t *testing.T) {
	fio := newFakeIO()
	config := newFakeConfig(nil)
	h := NewAuthHelper(fio, config)
	fio.getAuth = func(string) ioAuth { return auth("my_username", "my_password") }

	if err := h.StoreAuth("github.com", StoreAuthYes); err != nil {
		t.Fatal(err)
	}

	if config.authCalls != 1 || fio.args["getAuthentication"][0] != "github.com" ||
		!slices.Equal(config.auth.added, []string{`http-basic.github.com={"username":"my_username","password":"my_password"}`}) {
		t.Fatalf("added %v", config.auth.added)
	}
}

func storeAuthPrompt(t *testing.T, answer string) (*fakeIO, *fakeConfig, error) {
	t.Helper()

	fio := newFakeIO()
	config := newFakeConfig(nil)
	config.auth = &fakeSource{name: "https://api.gitlab.com/source"}
	h := NewAuthHelper(fio, config)
	fio.getAuth = func(string) ioAuth { return auth("my_username", "my_password") }
	fio.validate = func(question string, validator console.Validator, attempts int, def any) (any, error) {
		if question != "Do you want to store credentials for github.com in https://api.gitlab.com/source ? [Yn] " || attempts != 0 || def != "y" {
			t.Fatalf("asked %q %d %v", question, attempts, def)
		}

		if _, err := validator(answer); err != nil {
			return nil, err
		}

		return answer, nil
	}

	err := h.StoreAuth("github.com", StoreAuthPrompt)

	if config.authCalls != 1 || config.auth.nameRead != 1 || fio.calls["askAndValidate"] != 1 {
		t.Fatalf("calls: auth source %d, name %d, ask %d", config.authCalls, config.auth.nameRead, fio.calls["askAndValidate"])
	}

	return fio, config, err
}

func TestAuthHelper_StoreAuthWithPromptYesAnswer(t *testing.T) {
	_, config, err := storeAuthPrompt(t, "y")
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(config.auth.added, []string{`http-basic.github.com={"username":"my_username","password":"my_password"}`}) {
		t.Fatalf("added %v", config.auth.added)
	}
}

func TestAuthHelper_StoreAuthWithPromptNoAnswer(t *testing.T) {
	_, config, err := storeAuthPrompt(t, "n")
	if err != nil || len(config.auth.added) != 0 {
		t.Fatalf("got %v, added %v", err, config.auth.added)
	}
}

func TestAuthHelper_StoreAuthWithPromptInvalidAnswer(t *testing.T) {
	_, _, err := storeAuthPrompt(t, "invalid")
	if re, ok := errors.AsType[*util.RuntimeError](err); !ok || re.Message != "Please answer (y)es or (n)o" {
		t.Fatalf("expected a RuntimeException, got %v", err)
	}
}

// noProcess is a ProcessExecutor mock whose commands all fail.
type noProcess struct{ calls []string }

func (p *noProcess) Execute(command util.Command, _ *string, _ string) (int, error) {
	p.calls = append(p.calls, command.String())

	return 1, nil
}

func TestAuthHelper_PromptAuthIfNeededGitLabNoAuthChange(t *testing.T) {
	fio := newFakeIO()
	fio.hasAuth = func(string) bool { return true }
	fio.getAuth = func(string) ioAuth { return auth("gitlab-user", "gitlab-password") }

	config := newFakeConfig(map[string]any{
		"github-domains": list(),
		"gitlab-domains": list("gitlab.com"),
		"gitlab-token":   php.ArrayOf("gitlab.com", php.ArrayOf("username", "gitlab-user", "token", "gitlab-password")),
	})
	h := NewAuthHelper(fio, config)
	h.newGitLab = func() *GitLab { return NewGitLab(fio, config, &noProcess{}, NewHttpDownloaderMockGetter()) }

	_, err := h.PromptAuthIfNeeded("https://gitlab.com/acme/archive.zip", "gitlab.com", 404, "GitLab requires authentication and it was not provided", nil, 0, "")
	if te, ok := errors.AsType[*util.TransportError](err); !ok || te.Message != "Invalid credentials for 'https://gitlab.com/acme/archive.zip', aborting." {
		t.Fatalf("expected a TransportException, got %v", err)
	}

	if len(fio.auths) != 1 || jsonOf(t, php.ListOf(fio.auths[0][0], fio.auths[0][1], fio.auths[0][2])) != `["gitlab.com","gitlab-user","gitlab-password"]` {
		t.Fatalf("setAuthentication %v", fio.auths)
	}
}

func TestAuthHelper_PromptAuthIfNeededGitLabCiJobTokenAbortsImmediately(t *testing.T) {
	fio := newFakeIO()
	fio.hasAuth = func(string) bool { return true }
	fio.getAuth = func(string) ioAuth { return auth("gitlab-ci-token", "expired-job-token") }

	config := newFakeConfig(map[string]any{
		"github-domains": list(),
		"gitlab-domains": list("gitlab.com"),
		"gitlab-token":   php.ArrayOf("gitlab.com", php.ArrayOf("username", "gitlab-ci-token", "token", "expired-job-token")),
	})
	h := NewAuthHelper(fio, config)
	h.newGitLab = func() *GitLab { return NewGitLab(fio, config, &noProcess{}, NewHttpDownloaderMockGetter()) }

	// a job token cannot be renewed, so we must abort before
	// GitLab.AuthorizeOAuth swaps the slots
	_, err := h.PromptAuthIfNeeded("https://gitlab.com/acme/archive.zip", "gitlab.com", 404, "GitLab requires authentication and it was not provided", nil, 0, "")
	if te, ok := errors.AsType[*util.TransportError](err); !ok || te.Message != "Invalid credentials for 'https://gitlab.com/acme/archive.zip', aborting." {
		t.Fatalf("expected a TransportException, got %v", err)
	}

	if len(fio.auths) != 0 {
		t.Fatalf("setAuthentication %v", fio.auths)
	}
}

func TestAuthHelper_PromptAuthIfNeededMultipleBitbucketDownloads(t *testing.T) {
	fio := newFakeIO()
	fio.hasAuth = func(o string) bool { return o == "bitbucket.org" }

	returns := []ioAuth{auth("bitbucket_client_id", "bitbucket_client_secret"), auth("x-token-auth", "bitbucket_access_token")}
	fio.getAuth = func(string) ioAuth {
		a := returns[0]
		returns = returns[1:]

		return a
	}

	config := newFakeConfig(map[string]any{
		"github-domains": list(),
		"gitlab-domains": list(),
		"bitbucket-oauth": php.ArrayOf("bitbucket.org", php.ArrayOf(
			"access-token", "bitbucket_access_token",
			"access-token-expiration", time.Now().Unix()+1800,
		)),
	})
	h := NewAuthHelper(fio, config)
	h.newBitbucket = func() *Bitbucket { return NewBitbucket(fio, config, &noProcess{}, NewHttpDownloaderMockGetter(), 0) }

	result1, err1 := h.PromptAuthIfNeeded("https://bitbucket.org/workspace/repo1/get/hash1.zip", "bitbucket.org", 401, "HTTP/2 401 ", nil, 0, "")
	result2, err2 := h.PromptAuthIfNeeded("https://bitbucket.org/workspace/repo2/get/hash2.zip", "bitbucket.org", 401, "HTTP/2 401 ", nil, 0, "")

	expected := AuthResult{Retry: true}
	if err1 != nil || err2 != nil || result1 != expected || result2 != expected {
		t.Fatalf("got %v %v, %v %v", result1, err1, result2, err2)
	}

	if fio.calls["hasAuthentication"] != 2 || fio.calls["getAuthentication"] != 2 || len(fio.auths) != 1 ||
		jsonOf(t, php.ListOf(fio.auths[0][0], fio.auths[0][1], fio.auths[0][2])) != `["bitbucket.org","x-token-auth","bitbucket_access_token"]` {
		t.Fatalf("calls %v, auths %v", fio.calls, fio.auths)
	}
}

func TestAuthHelper_AddAuthenticationHeaderIsWorking(t *testing.T) {
	for _, tc := range basicHTTPAuthenticationCases {
		fio := newFakeIO()
		h := NewAuthHelper(fio, newFakeConfig(nil))
		expectsAuthentication(fio, tc.origin, auth(tc.username, tc.password))

		if got := h.AddAuthenticationHeader(baseHeaders, tc.origin, tc.url); len(got) != 3 {
			t.Fatalf("got %q", got)
		}
	}
}

func TestAuthHelper_FindAuthOrigin(t *testing.T) {
	for _, tc := range []struct {
		origin             string
		originResult       bool
		originToPass       string
		originToPassResult bool
		expected           string
	}{
		{"github.com", true, "github.com", true, "github.com"},
		{"github.com", true, "api.github.com", false, "github.com"},
		{"bitbucket.org", true, "bitbucket.org", false, "bitbucket.org"},
		{"bitbucket.org", true, "api.bitbucket.org", false, "bitbucket.org"},
		{"bitbucket.org", false, "bitbucket.org", false, ""},
		{"gitlab.com", true, "api.gitlab.com", false, ""},
	} {
		fio := newFakeIO()
		fio.hasAuth = func(o string) bool {
			switch o {
			case tc.origin:
				return tc.originResult
			case tc.originToPass:
				return tc.originToPassResult
			}

			return false
		}

		got, ok := FindAuthOrigin(fio, tc.originToPass)
		if got != tc.expected || ok != (tc.expected != "") {
			t.Errorf("%+v: got %q %v", tc, got, ok)
		}
	}
}

func TestAuthHelper_PromptAuthIfNeededMultipleGithubDownloads(t *testing.T) {
	fio := newFakeIO()
	fio.hasAuth = func(o string) bool { return o == "github.com" }
	h := NewAuthHelper(fio, newFakeConfig(map[string]any{"github-domains": list("github.com"), "gitlab-domains": list()}))

	result, err := h.PromptAuthIfNeeded("https://api.github.com/repos/symfony/process/zipball/abc", "github.com", 403, "HTTP/2 403 ", nil, 0, "")
	if err != nil || result != (AuthResult{Retry: true}) {
		t.Fatalf("got %v %v", result, err)
	}

	if fio.calls["ask"] != 0 || fio.calls["askAndHideAnswer"] != 0 || fio.calls["setAuthentication"] != 0 {
		t.Fatalf("calls %v", fio.calls)
	}
}

func TestAuthHelper_PromptAuthIfNeededNonInteractive(t *testing.T) {
	h := NewAuthHelper(newFakeIO(), newFakeConfig(map[string]any{"github-domains": list(), "gitlab-domains": list()}))

	for _, tc := range []struct {
		status int
		want   string
	}{
		{401, "The 'https://u:***@example.org/x' URL required authentication (HTTP 401).\nYou must be using the interactive console to authenticate"},
		{403, "The 'https://u:***@example.org/x' URL could not be accessed (HTTP 403): HTTP/1.1 403 Forbidden"},
		{407, "Unknown error code '407', reason: HTTP/1.1 403 Forbidden"},
	} {
		_, err := h.PromptAuthIfNeeded("https://u:p@example.org/x", "example.org", tc.status, "HTTP/1.1 403 Forbidden", nil, 0, "")
		if te, ok := errors.AsType[*util.TransportError](err); !ok || te.Message != tc.want || te.Code != tc.status {
			t.Errorf("%d: got %v", tc.status, err)
		}
	}

	if r, err := h.PromptAuthIfNeeded("https://example.org/x", "example.org", 404, "", nil, 0, ""); err != nil || r.Retry {
		t.Fatalf("404: got %v %v", r, err)
	}
}

func TestAuthHelper_PromptAuthIfNeededGitHubMessages(t *testing.T) {
	config := newFakeConfig(map[string]any{"github-domains": list("github.com"), "gitlab-domains": list()})

	for _, tc := range []struct {
		headers []string
		body    string
		want    string
	}{
		{nil, `{"message":"Bad credentials"}`, "\nCould not fetch https://api.github.com/x: Bad credentials"},
		{nil, "", "\nCould not fetch https://api.github.com/x, please create a GitHub OAuth token to access private repos"},
		{[]string{"X-RateLimit-Remaining: 0", "X-RateLimit-Limit: 60", "X-RateLimit-Reset: 0"}, "", "GitHub API limit (60 calls/hr) is exhausted, could not fetch https://api.github.com/x. Create a GitHub OAuth token to go over the API rate limit. You can also wait until 1970-01-01 00:00:00 for the rate limit to reset.\n"},
		{[]string{"X-GitHub-SSO: required; url=https://github.com/orgs/x/sso?authorization_request=abc"}, "", "GitHub API token requires SSO authorization. Authorize this token at https://github.com/orgs/x/sso?authorization_request=abc\n"},
	} {
		fio := newFakeIO()
		fio.interactive = true
		fio.ask = func(string) any { return "" }
		h := NewAuthHelper(fio, config)
		h.newGitHub = func() *GitHub { return NewGitHub(fio, config, &noProcess{}, NewHttpDownloaderMockGetter()) }

		_, err := h.PromptAuthIfNeeded("https://api.github.com/x", "github.com", 401, "", tc.headers, 0, tc.body)

		if len(fio.args["writeError"]) == 0 || fio.args["writeError"][0] != tc.want {
			t.Errorf("got %q (%v), want %q", fio.args["writeError"], err, tc.want)
		}
	}
}

// NewHttpDownloaderMockGetter is a Getter failing every request, for auth
// flows that must not reach the network.
func NewHttpDownloaderMockGetter() Getter { return failingGetter{} }

type failingGetter struct{}

func (failingGetter) Get(url string, _ *php.Array) (*Response, error) {
	return nil, util.NewTransportError(`The "`+url+`" file could not be downloaded`, 500)
}

func TestStoreAuthOf(t *testing.T) {
	if storeAuthOf(true) != StoreAuthYes || storeAuthOf("prompt") != StoreAuthPrompt || storeAuthOf(false) != StoreAuthNo || storeAuthOf(nil) != StoreAuthNo {
		t.Fatal("unexpected store-auths conversion")
	}

	if !strings.Contains(ComposerVersion, ".") {
		t.Fatal(ComposerVersion)
	}
}

func TestAuthHelper_StripCredentialHeadersOnRedirect(t *testing.T) {
	options := headerOptions([]string{"Authorization: Bearer abc", "Proxy-Authorization: Basic xyz", "Cookie: a=b", "Accept: */*"})

	if got := StripCredentialHeadersOnRedirect(options, "https://example.org/a", "https://EXAMPLE.org:443/b"); !php.StrictEquals(got, options) {
		t.Errorf("same origin: got %s", jsonOf(t, got))
	}

	if got := StripCredentialHeadersOnRedirect(options, "https://example.org/a", "https://example.net/b").At("http").(*php.Array).At("header"); !php.StrictEquals(got, list("Proxy-Authorization: Basic xyz", "Accept: */*")) {
		t.Errorf("other host: got %s", jsonOf(t, got))
	}

	stringHeaders := php.ArrayOf("http", php.ArrayOf("header", "Accept: */*\nAuthorization: Bearer abc"))
	if got := StripCredentialHeadersOnRedirect(stringHeaders, "https://example.org/a", "http://example.org/b").At("http").(*php.Array).At("header"); !php.StrictEquals(got, list("Accept: */*")) {
		t.Errorf("string headers: got %s", jsonOf(t, got))
	}
}
