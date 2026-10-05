package http_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
	"github.com/stubbedev/maestro/internal/util/http/httpmock"
)

// processMock is the ProcessExecutorMock of BitbucketTest: every command
// returns code.
type processMock struct{ code int }

func (p processMock) Execute(util.Command, *string, string) (int, error) { return p.code, nil }

func assertMockComplete(t *testing.T, d *httpmock.Downloader) {
	t.Helper()

	if err := d.AssertComplete(); err != nil {
		t.Fatal(err)
	}
}

func TestGitHub_UsernamePasswordAuthenticationFlow(t *testing.T) {
	io := http.NewIOMock(t)
	io.Expects([]map[string]any{
		{"text": "mymessage"},
		{"ask": "Token (hidden): ", "reply": "password"},
	}, false)

	d := httpmock.New()
	d.Expects([]httpmock.Expectation{{URL: "https://api.github.com/", Body: "{}"}}, true, nil)

	config := http.NewFakeConfig(nil)
	github := http.NewGitHub(io, config, processMock{1}, d)

	ok, err := github.AuthorizeOAuthInteractively("github.com", "mymessage")
	if err != nil || !ok {
		t.Fatalf("got %v %v", ok, err)
	}

	io.AssertComplete(t)
	assertMockComplete(t, d)

	if config.AuthCalls() != 2 || config.SourceCalls() != 1 || config.Auth().NameReads() < 1 ||
		!slices.Equal(config.Source().Removed(), []string{"github-oauth.github.com"}) ||
		!slices.Equal(config.Auth().Added(), []string{`github-oauth.github.com="password"`}) {
		t.Fatalf("auth %d %v, source %d %v", config.AuthCalls(), config.Auth().Added(), config.SourceCalls(), config.Source().Removed())
	}
}

func TestGitHub_UsernamePasswordFailure(t *testing.T) {
	io := http.NewIOMock(t)
	io.Expects([]map[string]any{
		{"ask": "Token (hidden): ", "reply": "password"},
	}, false)

	d := httpmock.New()
	d.Expects([]httpmock.Expectation{{URL: "https://api.github.com/", Status: 401}}, true, nil)

	config := http.NewFakeConfig(nil)
	github := http.NewGitHub(io, config, processMock{1}, d)

	ok, err := github.AuthorizeOAuthInteractively("github.com", "")
	if err != nil || ok {
		t.Fatalf("got %v %v", ok, err)
	}

	io.AssertComplete(t)
	assertMockComplete(t, d)

	if config.AuthCalls() != 1 {
		t.Fatalf("auth source read %d times", config.AuthCalls())
	}
}

func TestGitHub_RateLimit(t *testing.T) {
	github := http.NewGitHub(http.NewIOMock(t), http.NewFakeConfig(nil), processMock{1}, httpmock.New())

	headers := []string{"HTTP/2 403 ", "X-RateLimit-Limit: 60", "x-ratelimit-remaining: 0", "X-RateLimit-Reset: 1700000000"}

	if !github.IsRateLimited(headers) || github.IsRateLimited([]string{"X-RateLimit-Remaining: 10"}) {
		t.Fatal("isRateLimited")
	}

	if rl := github.RateLimit(headers); !rl.HasLimit || rl.Limit != 60 || rl.Reset != "2023-11-14 22:13:20" {
		t.Fatalf("got %+v", rl)
	}

	if rl := github.RateLimit(nil); rl.HasLimit || rl.Reset != "?" {
		t.Fatalf("got %+v", rl)
	}

	sso := []string{"X-GitHub-SSO: required; url=https://github.com/orgs/acme/sso?authorization_request=x"}
	if url, ok := github.SSOURL(sso); !github.RequiresSSO(sso) || !ok || url != "https://github.com/orgs/acme/sso?authorization_request=x" {
		t.Fatalf("got %q", url)
	}
}

func TestGitHub_AuthorizeOAuth(t *testing.T) {
	io := http.NewIOMock(t)
	config := http.NewFakeConfig(map[string]any{"github-domains": http.List("github.com")})

	if http.NewGitHub(io, config, processMock{0}, httpmock.New()).AuthorizeOAuth("example.org") {
		t.Fatal("expected false for a non GitHub domain")
	}

	if !http.NewGitHub(io, config, processMock{0}, httpmock.New()).AuthorizeOAuth("github.com") {
		t.Fatal("expected the git config token to be used")
	}

	if a := io.Authentication("github.com"); a.Password == nil || *a.Password != "x-oauth-basic" {
		t.Fatalf("got %v", a)
	}
}

const (
	gitlabToken        = "gitlabtoken"
	gitlabRefreshToken = "gitlabrefreshtoken"
)

func TestGitLab_UsernamePasswordAuthenticationFlow(t *testing.T) {
	io := http.NewIOMock(t)
	io.Expects([]map[string]any{
		{"text": "mymessage"},
		{"ask": "Username: ", "reply": "username"},
		{"ask": "Password: ", "reply": "password"},
	}, false)

	d := httpmock.New()
	d.Expects([]httpmock.Expectation{{
		URL:  "http://gitlab.com/oauth/token",
		Body: `{"access_token": "` + gitlabToken + `", "refresh_token": "` + gitlabRefreshToken + `", "token_type": "bearer", "expires_in": 7200, "created_at": 0}`,
	}}, true, nil)

	config := http.NewFakeConfig(nil)
	gitLab := http.NewGitLab(io, config, processMock{1}, d)

	ok, err := gitLab.AuthorizeOAuthInteractively("http", "gitlab.com", "mymessage")
	if err != nil || !ok {
		t.Fatalf("got %v %v", ok, err)
	}

	io.AssertComplete(t)
	assertMockComplete(t, d)

	if config.AuthCalls() != 2 || !slices.Equal(config.Auth().Added(), []string{`gitlab-oauth.gitlab.com={"expires-at":7200,"refresh-token":"gitlabrefreshtoken","token":"gitlabtoken"}`}) {
		t.Fatalf("auth %d %v", config.AuthCalls(), config.Auth().Added())
	}
}

func TestGitLab_UsernamePasswordFailure(t *testing.T) {
	io := http.NewIOMock(t)

	var asks []map[string]any
	for range 5 {
		asks = append(asks, map[string]any{"ask": "Username: ", "reply": "username"}, map[string]any{"ask": "Password: ", "reply": "password"})
	}

	io.Expects(asks, false)

	d := httpmock.New()

	var expectations []httpmock.Expectation
	for range 5 {
		expectations = append(expectations, httpmock.Expectation{URL: "https://gitlab.com/oauth/token", Status: 401, Body: "{}"})
	}

	d.Expects(expectations, true, nil)

	config := http.NewFakeConfig(nil)
	gitLab := http.NewGitLab(io, config, processMock{1}, d)

	_, err := gitLab.AuthorizeOAuthInteractively("https", "gitlab.com", "")
	if re, ok := errors.AsType[*util.RuntimeError](err); !ok || re.Message != "Invalid GitLab credentials 5 times in a row, aborting." {
		t.Fatalf("got %v", err)
	}

	if config.AuthCalls() != 1 {
		t.Fatalf("auth source read %d times", config.AuthCalls())
	}
}

func TestGitLab_AuthorizeOAuth(t *testing.T) {
	for _, tc := range []struct {
		token        any
		user, secret string
	}{
		{"tok", "tok", "private-token"},
		{php.ArrayOf("username", "deploy", "token", "secret"), "deploy", "secret"},
		{php.ArrayOf("username", "gitlab-ci-token", "token", "secret"), "secret", "gitlab-ci-token"},
	} {
		io := http.NewIOMock(t)
		config := http.NewFakeConfig(map[string]any{
			"gitlab-domains": http.List("gitlab.example.org"),
			"gitlab-token":   php.ArrayOf("gitlab.example.org", tc.token),
		})

		if !http.NewGitLab(io, config, processMock{1}, httpmock.New()).AuthorizeOAuth("gitlab.example.org:8443") {
			t.Fatal("expected the configured token to be used")
		}

		a := io.Authentication("gitlab.example.org:8443")
		if *a.Username != tc.user || *a.Password != tc.secret {
			t.Fatalf("got %s %s", *a.Username, *a.Password)
		}
	}
}

func TestGitLab_IsOAuthExpired(t *testing.T) {
	config := http.NewFakeConfig(map[string]any{"gitlab-oauth": php.ArrayOf(
		"old.example.org", php.ArrayOf("expires-at", 1),
		"new.example.org", php.ArrayOf("expires-at", time.Now().Unix()+3600),
	)})
	gitLab := http.NewGitLab(http.NewIOMock(t), config, processMock{1}, httpmock.New())

	if !gitLab.IsOAuthExpired("old.example.org") || gitLab.IsOAuthExpired("new.example.org") || gitLab.IsOAuthExpired("other.example.org") {
		t.Fatal("unexpected expiry")
	}
}

const (
	bbConsumerKey    = "consumer_key"
	bbConsumerSecret = "consumer_secret"
	bbToken          = "bitbuckettoken"
)

var bbTokenOptions = php.ArrayOf(
	"retry-auth-failure", false,
	"http", php.ArrayOf("method", "POST", "content", "grant_type=client_credentials"),
)

func bbTokenBody(refresh string) string {
	return `{"access_token": "` + bbToken + `", "scopes": "repository", "expires_in": 3600, "refresh_token": "` + refresh + `", "token_type": "bearer"}`
}

type bitbucketTest struct {
	io     *http.IOMock
	d      *httpmock.Downloader
	config *http.FakeConfig
	time   int64
	bb     *http.Bitbucket
}

func newBitbucketTest(t *testing.T, values map[string]any) *bitbucketTest {
	t.Helper()

	b := &bitbucketTest{io: http.NewIOMock(t), d: httpmock.New(), config: http.NewFakeConfig(values), time: time.Now().Unix()}
	b.bb = http.NewBitbucket(b.io, b.config, processMock{1}, b.d, b.time)

	return b
}

// assertStoredAccessToken is setExpectationsForStoringAccessToken.
func (b *bitbucketTest) assertStoredAccessToken(t *testing.T, removeBasicAuth bool) {
	t.Helper()

	if !slices.Equal(b.config.Source().Removed(), []string{"bitbucket-oauth.bitbucket.org"}) {
		t.Fatalf("removed %v", b.config.Source().Removed())
	}

	want := `bitbucket-oauth.bitbucket.org={"consumer-key":"consumer_key","consumer-secret":"consumer_secret","access-token":"bitbuckettoken","access-token-expiration":` + itoa(b.time+3600) + `}`
	if !slices.Equal(b.config.Auth().Added(), []string{want}) {
		t.Fatalf("added %v", b.config.Auth().Added())
	}

	if removeBasicAuth && !slices.Equal(b.config.Auth().Removed(), []string{"http-basic.bitbucket.org"}) {
		t.Fatalf("auth removed %v", b.config.Auth().Removed())
	}
}

func itoa(i int64) string { return php.ToString(i) }

func TestBitbucket_RequestAccessTokenWithValidOAuthConsumer(t *testing.T) {
	b := newBitbucketTest(t, nil)
	b.io.Expects([]map[string]any{{"auth": [3]any{"bitbucket.org", bbConsumerKey, bbConsumerSecret}}}, false)
	b.d.Expects([]httpmock.Expectation{{URL: http.BitbucketOAuth2AccessTokenURL, Options: bbTokenOptions, Body: bbTokenBody("refreshtoken")}}, true, nil)

	token, err := b.bb.RequestToken("bitbucket.org", bbConsumerKey, bbConsumerSecret)
	if err != nil || token != bbToken {
		t.Fatalf("got %q %v", token, err)
	}

	b.io.AssertComplete(t)
	assertMockComplete(t, b.d)
	b.assertStoredAccessToken(t, false)

	if b.config.Gets("bitbucket-oauth") != 1 {
		t.Fatal("expected bitbucket-oauth read once")
	}
}

func TestBitbucket_RequestAccessTokenWithValidOAuthConsumerAndValidStoredAccessToken(t *testing.T) {
	b := newBitbucketTest(t, nil)
	b.config = http.NewFakeConfig(map[string]any{"bitbucket-oauth": php.ArrayOf("bitbucket.org", php.ArrayOf(
		"access-token", bbToken,
		"access-token-expiration", b.time+1800,
		"consumer-key", bbConsumerKey,
		"consumer-secret", bbConsumerSecret,
	))})
	b.bb = http.NewBitbucket(b.io, b.config, processMock{1}, b.d, b.time)

	token, err := b.bb.RequestToken("bitbucket.org", bbConsumerKey, bbConsumerSecret)
	if err != nil || token != bbToken {
		t.Fatalf("got %q %v", token, err)
	}

	// testGetTokenWithAccessToken depends on this test's Bitbucket
	if b.bb.Token() != bbToken {
		t.Fatalf("token %q", b.bb.Token())
	}
}

func TestBitbucket_RequestAccessTokenWithValidOAuthConsumerAndExpiredAccessToken(t *testing.T) {
	b := newBitbucketTest(t, nil)
	b.config = http.NewFakeConfig(map[string]any{"bitbucket-oauth": php.ArrayOf("bitbucket.org", php.ArrayOf(
		"access-token", "randomExpiredToken",
		"access-token-expiration", b.time-400,
		"consumer-key", bbConsumerKey,
		"consumer-secret", bbConsumerSecret,
	))})
	b.bb = http.NewBitbucket(b.io, b.config, processMock{1}, b.d, b.time)
	b.io.Expects([]map[string]any{{"auth": [3]any{"bitbucket.org", bbConsumerKey, bbConsumerSecret}}}, false)
	b.d.Expects([]httpmock.Expectation{{URL: http.BitbucketOAuth2AccessTokenURL, Options: bbTokenOptions, Body: bbTokenBody("refreshtoken")}}, true, nil)

	token, err := b.bb.RequestToken("bitbucket.org", bbConsumerKey, bbConsumerSecret)
	if err != nil || token != bbToken {
		t.Fatalf("got %q %v", token, err)
	}

	b.io.AssertComplete(t)
	b.assertStoredAccessToken(t, false)
}

func TestBitbucket_RequestAccessTokenWithUsernameAndPassword(t *testing.T) {
	b := newBitbucketTest(t, nil)
	b.io.Expects([]map[string]any{
		{"auth": [3]any{"bitbucket.org", "username", "password"}},
		{"text": "Invalid OAuth consumer provided."},
		{"text": "This can have three reasons:"},
		{"text": "1. You are authenticating with a bitbucket username/password combination"},
		{"text": "2. You are using an OAuth consumer, but didn't configure a (dummy) callback url"},
		{"text": "3. You are using an OAuth consumer, but didn't configure it as private consumer"},
	}, true)
	b.d.Expects([]httpmock.Expectation{{URL: http.BitbucketOAuth2AccessTokenURL, Options: bbTokenOptions, Status: 400}}, true, nil)

	token, err := b.bb.RequestToken("bitbucket.org", "username", "password")
	if err != nil || token != "" {
		t.Fatalf("got %q %v", token, err)
	}

	b.io.AssertComplete(t)
}

func TestBitbucket_RequestAccessTokenWithUsernameAndPasswordWithUnauthorizedResponse(t *testing.T) {
	b := newBitbucketTest(t, nil)
	b.io.Expects([]map[string]any{
		{"auth": [3]any{"bitbucket.org", "username", "password"}},
		{"text": "Invalid OAuth consumer provided."},
		{"text": `You can also add it manually later by using "composer config --global --auth bitbucket-oauth.bitbucket.org <consumer-key> <consumer-secret>"`},
	}, true)
	b.d.Expects([]httpmock.Expectation{{URL: http.BitbucketOAuth2AccessTokenURL, Options: bbTokenOptions, Status: 401}}, true, nil)

	token, err := b.bb.RequestToken("bitbucket.org", "username", "password")
	if err != nil || token != "" {
		t.Fatalf("got %q %v", token, err)
	}

	b.io.AssertComplete(t)
}

func TestBitbucket_RequestAccessTokenWithUsernameAndPasswordWithNotFoundResponse(t *testing.T) {
	b := newBitbucketTest(t, nil)
	b.io.Expects([]map[string]any{{"auth": [3]any{"bitbucket.org", "username", "password"}}}, false)
	b.d.Expects([]httpmock.Expectation{{URL: http.BitbucketOAuth2AccessTokenURL, Options: bbTokenOptions, Status: 404}}, true, nil)

	_, err := b.bb.RequestToken("bitbucket.org", "username", "password")
	if te, ok := errors.AsType[*util.TransportError](err); !ok || te.Code != 404 {
		t.Fatalf("expected a TransportException, got %v", err)
	}
}

func TestBitbucket_UsernamePasswordAuthenticationFlow(t *testing.T) {
	b := newBitbucketTest(t, nil)
	b.io.Expects([]map[string]any{
		{"text": "mymessage"},
		{"ask": "Consumer Key (hidden): ", "reply": bbConsumerKey},
		{"ask": "Consumer Secret (hidden): ", "reply": bbConsumerSecret},
	}, false)
	b.d.Expects([]httpmock.Expectation{{URL: "https://bitbucket.org/site/oauth2/access_token", Body: bbTokenBody("refresh_token")}}, true, nil)

	ok, err := b.bb.AuthorizeOAuthInteractively("bitbucket.org", "mymessage")
	if err != nil || !ok {
		t.Fatalf("got %v %v", ok, err)
	}

	b.io.AssertComplete(t)
	b.assertStoredAccessToken(t, true)
}

func TestBitbucket_AuthorizeOAuthInteractivelyWithEmptyUsername(t *testing.T) {
	b := newBitbucketTest(t, nil)
	b.io.Expects([]map[string]any{{"ask": "Consumer Key (hidden): ", "reply": ""}}, false)

	if ok, err := b.bb.AuthorizeOAuthInteractively("bitbucket.org", "mymessage"); ok || err != nil {
		t.Fatalf("got %v %v", ok, err)
	}

	b.io.AssertComplete(t)

	if b.config.AuthCalls() < 1 {
		t.Fatal("expected the auth source to be named")
	}
}

func TestBitbucket_AuthorizeOAuthInteractivelyWithEmptyPassword(t *testing.T) {
	b := newBitbucketTest(t, nil)
	b.io.Expects([]map[string]any{
		{"text": "mymessage"},
		{"ask": "Consumer Key (hidden): ", "reply": bbConsumerKey},
		{"ask": "Consumer Secret (hidden): ", "reply": ""},
	}, false)

	if ok, err := b.bb.AuthorizeOAuthInteractively("bitbucket.org", "mymessage"); ok || err != nil {
		t.Fatalf("got %v %v", ok, err)
	}

	b.io.AssertComplete(t)
}

func TestBitbucket_AuthorizeOAuthInteractivelyWithRequestAccessTokenFailure(t *testing.T) {
	b := newBitbucketTest(t, nil)
	b.io.Expects([]map[string]any{
		{"text": "mymessage"},
		{"ask": "Consumer Key (hidden): ", "reply": bbConsumerKey},
		{"ask": "Consumer Secret (hidden): ", "reply": bbConsumerSecret},
	}, false)
	b.d.Expects([]httpmock.Expectation{{URL: "https://bitbucket.org/site/oauth2/access_token", Status: 400}}, true, nil)

	if ok, err := b.bb.AuthorizeOAuthInteractively("bitbucket.org", "mymessage"); ok || err != nil {
		t.Fatalf("got %v %v", ok, err)
	}

	b.io.AssertComplete(t)
}

func TestBitbucket_GetTokenWithoutAccessToken(t *testing.T) {
	if token := newBitbucketTest(t, nil).bb.Token(); token != "" {
		t.Fatalf("got %q", token)
	}
}

func TestBitbucket_AuthorizeOAuthWithWrongOriginUrl(t *testing.T) {
	if newBitbucketTest(t, nil).bb.AuthorizeOAuth("non-bitbucket.org") {
		t.Fatal("expected false")
	}
}

func TestBitbucket_AuthorizeOAuthWithoutAvailableGitConfigToken(t *testing.T) {
	b := newBitbucketTest(t, nil)

	if http.NewBitbucket(b.io, b.config, processMock{-1}, b.d, b.time).AuthorizeOAuth("bitbucket.org") {
		t.Fatal("expected false")
	}
}

func TestBitbucket_AuthorizeOAuthWithAvailableGitConfigToken(t *testing.T) {
	b := newBitbucketTest(t, nil)

	if !http.NewBitbucket(b.io, b.config, processMock{0}, b.d, b.time).AuthorizeOAuth("bitbucket.org") {
		t.Fatal("expected true")
	}
}

func TestForgejo_UsernamePasswordAuthenticationFlow(t *testing.T) {
	io := http.NewIOMock(t)
	io.Expects([]map[string]any{
		{"text": "mymessage"},
		{"ask": "Username: ", "reply": "username"},
		{"ask": "Token (hidden): ", "reply": "access-token"},
	}, false)

	d := httpmock.New()
	d.Expects([]httpmock.Expectation{{URL: "https://codeberg.org/api/v1/version", Body: "{}"}}, true, nil)

	config := http.NewFakeConfig(nil)

	ok, err := http.NewForgejo(io, config, d).AuthorizeOAuthInteractively("codeberg.org", "mymessage")
	if err != nil || !ok {
		t.Fatalf("got %v %v", ok, err)
	}

	io.AssertComplete(t)
	assertMockComplete(t, d)

	if config.AuthCalls() != 2 || config.SourceCalls() != 1 ||
		!slices.Equal(config.Source().Removed(), []string{"forgejo-token.codeberg.org"}) ||
		!slices.Equal(config.Auth().Added(), []string{`forgejo-token.codeberg.org={"username":"username","token":"access-token"}`}) {
		t.Fatalf("auth %d %v", config.AuthCalls(), config.Auth().Added())
	}
}

func TestForgejo_UsernamePasswordFailure(t *testing.T) {
	io := http.NewIOMock(t)
	io.Expects([]map[string]any{
		{"ask": "Username: ", "reply": "username"},
		{"ask": "Token (hidden): ", "reply": "access-token"},
	}, false)

	d := httpmock.New()
	d.Expects([]httpmock.Expectation{{URL: "https://codeberg.org/api/v1/version", Status: 404}}, true, nil)

	config := http.NewFakeConfig(nil)

	ok, err := http.NewForgejo(io, config, d).AuthorizeOAuthInteractively("codeberg.org", "")
	if err != nil || ok {
		t.Fatalf("got %v %v", ok, err)
	}

	io.AssertComplete(t)

	if config.AuthCalls() != 1 {
		t.Fatalf("auth source read %d times", config.AuthCalls())
	}
}

func TestHttpDownloaderMock_Strict(t *testing.T) {
	d := httpmock.New()
	d.Expects([]httpmock.Expectation{{URL: "https://a"}}, true, nil)

	if _, err := d.Get("https://b", nil); err == nil {
		t.Fatal("expected an unexpected-request failure")
	}

	if err := d.AssertComplete(); err == nil {
		t.Fatal("expected an incomplete-expectations failure")
	}

	p, _ := d.Add("https://a", nil)
	if r, err := p.Wait(); err != nil || r.StatusCode() != 200 {
		t.Fatalf("got %v %v", r, err)
	}

	assertMockComplete(t, d)
}
