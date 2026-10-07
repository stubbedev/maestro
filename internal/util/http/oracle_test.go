package http

import (
	"os"
	"sync"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// The goldens are written by tools/oracle/http/http.php from the PHP
// sources.
var loadOracle = sync.OnceValues(func() (*php.Array, error) {
	data, err := os.ReadFile("testdata/oracle/http.json")
	if err != nil {
		return nil, err
	}

	v, err := php.JSONDecode(string(data), true)
	if err != nil {
		return nil, err
	}

	a, _ := v.(*php.Array)

	return a, nil
})

func oracleCases(t *testing.T, section string) []*php.Array {
	t.Helper()

	o, err := loadOracle()
	if err != nil {
		t.Fatal(err)
	}

	cases, _ := o.At(section).(*php.Array)
	if cases == nil || cases.Len() == 0 {
		t.Fatalf("no %s cases", section)
	}

	out := make([]*php.Array, 0, cases.Len())
	for _, c := range cases.All() {
		a, _ := c.(*php.Array)
		out = append(out, a)
	}

	return out
}

func str(c *php.Array, key string) string { return php.ToString(c.At(key)) }

// encode renders a php value as the goldens hold it.
func encode(t *testing.T, v any) string {
	t.Helper()

	s, err := php.JSONEncode(v, php.JSONUnescapedSlashes|php.JSONUnescapedUnicode)
	if err != nil {
		t.Fatal(err)
	}

	return s
}

func nullable(s string, ok bool) any {
	if !ok {
		return nil
	}

	return s
}

func TestOracle_Proxies(t *testing.T) {
	for _, c := range oracleCases(t, "proxies") {
		in := str(c, "in")

		item, err := NewProxyItem(in, "https_proxy")
		if want, ok := c.At("error").(string); ok {
			if err == nil || err.Error() != want {
				t.Errorf("%q: got %v, want error %q", in, err, want)
			}

			continue
		}

		if err != nil {
			t.Errorf("%q: unexpected error %v", in, err)

			continue
		}

		http, https := item.ToRequestProxy("http"), item.ToRequestProxy("https")

		auth := any(http.auth)
		if http.auth == "" {
			auth = nil
		}

		if http.Status() != str(c, "status") || http.url != str(c, "url") ||
			encode(t, http.ContextOptions()) != encode(t, c.At("httpContext")) ||
			encode(t, https.ContextOptions()) != encode(t, c.At("httpsContext")) ||
			encode(t, auth) != encode(t, c.At("auth")) {
			t.Errorf("%q: got status %q url %q auth %v contexts %s %s, want %s", in, http.Status(), http.url, auth,
				encode(t, http.ContextOptions()), encode(t, https.ContextOptions()), encode(t, c))
		}
	}
}

func TestOracle_Redirects(t *testing.T) {
	c := newCurlDownloader(io.NewNullIO(), newFakeConfig(nil), nil, &sync.Mutex{})

	for _, tc := range oracleCases(t, "redirects") {
		url, location := str(tc, "url"), str(tc, "location")

		headers := []string{"HTTP/1.1 302 Found"}
		if location != "" {
			headers = append(headers, "Location: "+location)
		}

		target, err := c.handleRedirect(&curlJob{url: url}, NewResponse(url, 302, headers, ""))

		if want, ok := tc.At("error").(string); ok {
			if err == nil || err.Error() != want {
				t.Errorf("%s + %q: got %q %v, want error %q", url, location, target, err, want)
			}

			continue
		}

		if err != nil || target != str(tc, "target") {
			t.Errorf("%s + %q: got %q %v, want %q", url, location, target, err, str(tc, "target"))
		}
	}
}

func stringsOf(a any) []string {
	arr, _ := a.(*php.Array)
	if arr == nil {
		return nil
	}

	out := make([]string, 0, arr.Len())
	for _, v := range arr.All() {
		out = append(out, php.ToString(v))
	}

	return out
}

func TestOracle_Headers(t *testing.T) {
	for _, c := range oracleCases(t, "headers") {
		headers := stringsOf(c.At("headers"))
		r := NewResponse("https://example.org", 200, headers, "")

		values, _ := c.At("values").(*php.Array)
		for k, want := range values.All() {
			got := nullable(FindHeaderValue(headers, k.String()))
			if encode(t, got) != encode(t, want) {
				t.Errorf("%q / %s: got %v, want %v", headers, k.String(), got, want)
			}
		}

		if got := nullable(r.StatusMessage()); encode(t, got) != encode(t, c.At("statusMessage")) {
			t.Errorf("%q: status message %v", headers, got)
		}

		code, ok := FindStatusCode(headers)

		var got any
		if ok {
			got = code
		}

		if encode(t, got) != encode(t, c.At("statusCode")) {
			t.Errorf("%q: status code %v", headers, got)
		}
	}
}

func TestOracle_AuthenticationOptions(t *testing.T) {
	for _, c := range oracleCases(t, "auth") {
		b, err := io.NewBufferIO("", console.VerbosityDebug, nil)
		if err != nil {
			t.Fatal(err)
		}

		config := newFakeConfig(map[string]any{"gitlab-domains": list("gitlab.com", "gitlab.example.org")})

		if authOrigin := str(c, "authOrigin"); authOrigin != "none" {
			password := str(c, "password")
			b.SetAuthentication(authOrigin, str(c, "username"), &password)
		}

		h := NewAuthHelper(b, config)
		origin, url := str(c, "origin"), str(c, "url")

		options := h.AddAuthenticationOptions(php.ArrayOf("http", php.ArrayOf("header", list("Accept: x"))), origin, url)
		h.AddAuthenticationOptions(php.NewArray(), origin, url)

		if encode(t, options) != encode(t, c.At("options")) || php.NormalizeEOL(b.Output()) != str(c, "output") {
			t.Errorf("%s %s %s/%s: got %s %q, want %s %q", origin, url, str(c, "username"), str(c, "password"),
				encode(t, options), php.NormalizeEOL(b.Output()), encode(t, c.At("options")), str(c, "output"))
		}
	}
}

func TestOracle_GitHub(t *testing.T) {
	g := NewGitHub(io.NewNullIO(), newFakeConfig(nil), &noProcess{}, failingGetter{})

	for _, c := range oracleCases(t, "github") {
		headers := stringsOf(c.At("headers"))

		rl := g.RateLimit(headers)

		var limit any = "?"
		if rl.HasLimit {
			limit = rl.Limit
		}

		got := php.ArrayOf("limit", limit, "reset", rl.Reset)

		if encode(t, got) != encode(t, c.At("rateLimit")) {
			t.Errorf("%q: rate limit %s, want %s", headers, encode(t, got), encode(t, c.At("rateLimit")))
		}

		if sso := nullable(g.SSOURL(headers)); encode(t, sso) != encode(t, c.At("sso")) {
			t.Errorf("%q: sso %v", headers, sso)
		}

		if g.IsRateLimited(headers) != c.At("rateLimited") || g.RequiresSSO(headers) != c.At("requiresSso") {
			t.Errorf("%q: rate limited %v, sso %v", headers, g.IsRateLimited(headers), g.RequiresSSO(headers))
		}
	}
}

func TestOracle_ForgejoURL(t *testing.T) {
	for _, c := range oracleCases(t, "forgejo") {
		url := str(c, "url")
		f, err := util.TryForgejoURL(url)
		if err != nil {
			t.Fatal(err)
		}

		var got any
		if f != nil {
			got = php.ArrayOf("owner", f.Owner, "repository", f.Repository, "originUrl", f.OriginURL, "apiUrl", f.APIURL, "ssh", f.GenerateSSHURL())
		}

		if encode(t, got) != encode(t, c.At("parsed")) {
			t.Errorf("%s: got %s, want %s", url, encode(t, got), encode(t, c.At("parsed")))
		}
	}
}

func TestOracle_PublicBitbucketDownload(t *testing.T) {
	for _, c := range oracleCases(t, "bitbucket") {
		if got := IsPublicBitBucketDownload(str(c, "url")); got != c.At("public") {
			t.Errorf("%s: got %v", str(c, "url"), got)
		}
	}
}

func TestOracle_OutputWarnings(t *testing.T) {
	for _, c := range oracleCases(t, "warnings") {
		decorated := c.At("decorated") == true

		b, err := io.NewBufferIO("", console.VerbosityNormal, console.NewOutputFormatter(decorated))
		if err != nil {
			t.Fatal(err)
		}

		wrote, err := OutputWarnings(b, "https://user:secret@repo.example.org", c.At("data"))
		if err != nil || wrote != c.At("wrote") || php.NormalizeEOL(b.Output()) != str(c, "output") {
			t.Errorf("%s decorated=%v: got %v %q %v, want %q", encode(t, c.At("data")), decorated, wrote, php.NormalizeEOL(b.Output()), err, str(c, "output"))
		}
	}
}

func TestOracle_PromptAuthIfNeeded(t *testing.T) {
	config := newFakeConfig(map[string]any{"github-domains": list(), "gitlab-domains": list()})

	for _, c := range oracleCases(t, "prompts") {
		h := NewAuthHelper(io.NewNullIO(), config)

		result, err := h.PromptAuthIfNeeded(str(c, "url"), str(c, "origin"), int(php.ToInt(c.At("status"))), str(c, "reason"), nil, 0, "")

		if want, ok := c.At("error").(string); ok {
			te, isTransport := err.(*util.TransportError) //nolint:errorlint // thrown directly
			if !isTransport || te.Message != want || int64(te.Code) != php.ToInt(c.At("code")) {
				t.Errorf("%s %v: got %v, want %q", str(c, "url"), c.At("status"), err, want)
			}

			continue
		}

		want, _ := c.At("result").(*php.Array)
		if err != nil || result.Retry != (want.At("retry") == true) || result.StoreAuth != storeAuthOf(want.At("storeAuth")) {
			t.Errorf("%s: got %+v %v", str(c, "url"), result, err)
		}
	}
}
