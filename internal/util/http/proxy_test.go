package http

import (
	"errors"
	"testing"

	"github.com/stubbedev/maestro/internal/json/jsonlint"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// clearProxyEnv unsets the proxy variables ProxyManagerTest unsets and
// resets the manager, before and after the test.
func clearProxyEnv(t *testing.T) {
	t.Helper()

	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy", "CGI_HTTP_PROXY", "cgi_http_proxy"} {
		t.Setenv(name, "")
	}

	ResetProxyManager()
	t.Cleanup(ResetProxyManager)
}

func setEnv(t *testing.T, env map[string]string) {
	t.Helper()

	for k, v := range env {
		t.Setenv(k, v)
	}
}

func TestProxyItem_ThrowsOnMalformedUrl(t *testing.T) {
	for name, url := range map[string]string{
		"ws-r":     "http://user\rname@localhost:80",
		"ws-n":     "http://user\nname@localhost:80",
		"ws-t":     "http://user\tname@localhost:80",
		"no-host":  "localhost",
		"no-port":  "scheme://localhost",
		"port-0":   "http://localhost:0",
		"port-big": "http://localhost:65536",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewProxyItem(url, "http_proxy")
			if _, ok := errors.AsType[*util.RuntimeError](err); !ok {
				t.Fatalf("expected a RuntimeException, got %v", err)
			}
		})
	}
}

func TestProxyItem_UrlFormatting(t *testing.T) {
	for name, tc := range map[string][2]string{
		"none":              {"http://proxy.com:8888", "http://proxy.com:8888"},
		"lowercases-scheme": {"HTTP://proxy.com:8888", "http://proxy.com:8888"},
		"adds-http-scheme":  {"proxy.com:80", "http://proxy.com:80"},
		"adds-http-port":    {"http://proxy.com", "http://proxy.com:80"},
		"adds-https-port":   {"https://proxy.com", "https://proxy.com:443"},
		"removes-user":      {"http://user@proxy.com:6180", "http://***@proxy.com:6180"},
		"removes-user-pass": {"http://user:p%40ss@proxy.com:6180", "http://***:***@proxy.com:6180"},
	} {
		t.Run(name, func(t *testing.T) {
			item, err := NewProxyItem(tc[0], "http_proxy")
			if err != nil {
				t.Fatal(err)
			}

			if got := item.ToRequestProxy("http").Status(); got != tc[1] {
				t.Fatalf("got %q, want %q", got, tc[1])
			}
		})
	}
}

func TestProxyItem_Messages(t *testing.T) {
	for url, want := range map[string]string{
		"http://user\rname@localhost:80": "unsupported `http_proxy` syntax",
		"localhost":                      "unable to find proxy host in http_proxy",
		"scheme://localhost":             "unable to find proxy port in http_proxy",
		"http://localhost:0":             "port 0 is reserved in http_proxy",
		"http://localhost:65536":         "unsupported `http_proxy` syntax",
	} {
		if _, err := NewProxyItem(url, "http_proxy"); err == nil || err.Error() != want {
			t.Errorf("%q: got %v, want %q", url, err, want)
		}
	}
}

func TestProxyManager_Instantiation(t *testing.T) {
	clearProxyEnv(t)

	original := GetProxyManager()
	same := GetProxyManager()

	if original != same {
		t.Fatal("expected the same instance")
	}

	ResetProxyManager()

	if GetProxyManager() == same {
		t.Fatal("expected a new instance after reset")
	}
}

func TestProxyManager_GetProxyForRequestThrowsOnBadProxyUrl(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("http_proxy", "localhost")

	_, err := GetProxyManager().ProxyForRequest("http://example.com")
	if te, ok := errors.AsType[*util.TransportError](err); !ok || te.Message != "Unable to use a proxy: unable to find proxy host in http_proxy" {
		t.Fatalf("expected a TransportException, got %v", err)
	}
}

func TestProxyManager_LowercaseOverridesUppercase(t *testing.T) {
	for _, tc := range []struct {
		server   map[string]string
		url      string
		expected string
	}{
		{map[string]string{"HTTP_PROXY": "http://upper.com", "http_proxy": "http://lower.com"}, "http://repo.org", "http://lower.com:80"},
		{map[string]string{"CGI_HTTP_PROXY": "http://upper.com", "cgi_http_proxy": "http://lower.com"}, "http://repo.org", "http://lower.com:80"},
		{map[string]string{"HTTPS_PROXY": "http://upper.com", "https_proxy": "http://lower.com"}, "https://repo.org", "http://lower.com:80"},
	} {
		clearProxyEnv(t)
		setEnv(t, tc.server)

		proxy, err := GetProxyManager().ProxyForRequest(tc.url)
		if err != nil {
			t.Fatal(err)
		}

		if proxy.Status() != tc.expected {
			t.Errorf("%v: got %q, want %q", tc.server, proxy.Status(), tc.expected)
		}
	}
}

func TestProxyManager_CGIProxyIsOnlyUsedWhenNoHttpProxy(t *testing.T) {
	for _, tc := range []struct {
		server   map[string]string
		expected string
	}{
		{map[string]string{"CGI_HTTP_PROXY": "http://cgi.com:80"}, "http://cgi.com:80"},
		{map[string]string{"http_proxy": "http://http.com:80", "CGI_HTTP_PROXY": "http://cgi.com:80"}, "http://http.com:80"},
	} {
		clearProxyEnv(t)
		setEnv(t, tc.server)

		proxy, err := GetProxyManager().ProxyForRequest("http://repo.org")
		if err != nil {
			t.Fatal(err)
		}

		if proxy.Status() != tc.expected {
			t.Errorf("%v: got %q, want %q", tc.server, proxy.Status(), tc.expected)
		}
	}
}

func TestProxyManager_NoHttpProxyDoesNotUseHttpsProxy(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("https_proxy", "https://proxy.com:443")

	proxy, _ := GetProxyManager().ProxyForRequest("http://repo.org")
	if proxy.Status() != "" {
		t.Fatalf("got %q", proxy.Status())
	}
}

func TestProxyManager_NoHttpsProxyDoesNotUseHttpProxy(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("http_proxy", "http://proxy.com:80")

	proxy, _ := GetProxyManager().ProxyForRequest("https://repo.org")
	if proxy.Status() != "" {
		t.Fatalf("got %q", proxy.Status())
	}
}

func TestProxyManager_GetProxyForRequest(t *testing.T) {
	server := map[string]string{
		"http_proxy":  "http://user:p%40ss@proxy.com",
		"https_proxy": "https://proxy.com:443",
		"no_proxy":    "other.repo.org",
	}

	for _, tc := range []struct {
		server   map[string]string
		url      string
		options  string
		status   string
		excluded bool
	}{
		{nil, "http://repo.org", "null", "", false},
		{server, "http://repo.org", `{"http":{"proxy":"tcp://proxy.com:80","header":"Proxy-Authorization: Basic dXNlcjpwQHNz","request_fulluri":true}}`, "http://***:***@proxy.com:80", false},
		{server, "https://repo.org", `{"http":{"proxy":"ssl://proxy.com:443"}}`, "https://proxy.com:443", false},
		{server, "https://other.repo.org", "null", "excluded by no_proxy", true},
	} {
		clearProxyEnv(t)
		setEnv(t, tc.server)

		proxy, err := GetProxyManager().ProxyForRequest(tc.url)
		if err != nil {
			t.Fatal(err)
		}

		var options any
		if o := proxy.ContextOptions(); o != nil {
			options = o
		}

		if got := jsonOf(t, options); got != tc.options {
			t.Errorf("%s: options %s, want %s", tc.url, got, tc.options)
		}

		if proxy.Status() != tc.status || proxy.IsExcludedByNoProxy() != tc.excluded {
			t.Errorf("%s: status %q excluded %v", tc.url, proxy.Status(), proxy.IsExcludedByNoProxy())
		}
	}
}

func TestRequestProxy_FactoryNone(t *testing.T) {
	proxy := NoneProxy()

	if proxy.CurlOptions(php.NewArray()) != (CurlOptions{}) {
		t.Fatal("expected only an empty CURLOPT_PROXY")
	}

	if proxy.ContextOptions() != nil || proxy.Status() != "" {
		t.Fatal("expected no context options or status")
	}
}

func TestRequestProxy_FactoryNoProxy(t *testing.T) {
	proxy := NoProxy()

	if proxy.CurlOptions(php.NewArray()) != (CurlOptions{}) {
		t.Fatal("expected only an empty CURLOPT_PROXY")
	}

	if proxy.ContextOptions() != nil || proxy.Status() != "excluded by no_proxy" {
		t.Fatal("unexpected context options or status")
	}
}

func TestRequestProxy_IsSecure(t *testing.T) {
	for name, tc := range map[string]struct {
		url    string
		secure bool
	}{
		"basic":  {"http://proxy.com:80", false},
		"secure": {"https://proxy.com:443", true},
		"none":   {"", false},
	} {
		if got := NewRequestProxy(tc.url, "", nil, "").IsSecure(); got != tc.secure {
			t.Errorf("%s: got %v", name, got)
		}
	}
}

func TestRequestProxy_GetStatusThrowsOnBadFormatSpecifier(t *testing.T) {
	proxy := NewRequestProxy("http://proxy.com:80", "", nil, "http://proxy.com:80")

	_, err := proxy.StatusFormat("using proxy")
	if _, ok := errors.AsType[*util.InvalidArgumentError](err); !ok {
		t.Fatalf("expected an InvalidArgumentException, got %v", err)
	}
}

func TestRequestProxy_GetStatus(t *testing.T) {
	format := "proxy (%s)"

	for name, tc := range map[string]struct {
		url, format, expected string
		nullFormat            bool
	}{
		"no-proxy":    {"", format, "", false},
		"null-format": {"http://proxy.com:80", "", "http://proxy.com:80", true},
		"with-format": {"http://proxy.com:80", format, "proxy (http://proxy.com:80)", false},
	} {
		proxy := NewRequestProxy(tc.url, "", nil, tc.url)

		if tc.nullFormat {
			if proxy.Status() != tc.expected {
				t.Errorf("%s: got %q", name, proxy.Status())
			}

			got, err := proxy.StatusFormat("%s")
			if err != nil || got != tc.expected {
				t.Errorf("%s: got %q %v", name, got, err)
			}

			continue
		}

		got, err := proxy.StatusFormat(tc.format)
		if err != nil || got != tc.expected {
			t.Errorf("%s: got %q %v", name, got, err)
		}
	}
}

func TestRequestProxy_GetCurlOptions(t *testing.T) {
	for _, tc := range []struct {
		url, auth string
		expected  CurlOptions
	}{
		{"", "", CurlOptions{}},
		{"http://proxy.com:80", "", CurlOptions{Proxy: "http://proxy.com:80", NoProxy: true}},
		{"http://proxy.com:80", "user:p%40ss", CurlOptions{Proxy: "http://proxy.com:80", NoProxy: true, UserPwd: "user:p%40ss"}},
	} {
		if got := NewRequestProxy(tc.url, tc.auth, nil, "").CurlOptions(php.NewArray()); got != tc.expected {
			t.Errorf("%q: got %+v, want %+v", tc.url, got, tc.expected)
		}
	}
}

func TestRequestProxy_GetCurlOptionsWithSSL(t *testing.T) {
	for _, tc := range []struct {
		url, auth string
		ssl       *php.Array
		expected  CurlOptions
	}{
		{"https://proxy.com:443", "", php.ArrayOf("cafile", "/certs/bundle.pem"), CurlOptions{Proxy: "https://proxy.com:443", NoProxy: true, CAInfo: "/certs/bundle.pem"}},
		{"https://proxy.com:443", "user:p%40ss", php.ArrayOf("capath", "/certs"), CurlOptions{Proxy: "https://proxy.com:443", NoProxy: true, UserPwd: "user:p%40ss", CAPath: "/certs"}},
	} {
		if got := NewRequestProxy(tc.url, tc.auth, nil, "").CurlOptions(tc.ssl); got != tc.expected {
			t.Errorf("%q: got %+v, want %+v", tc.url, got, tc.expected)
		}
	}
}

func TestResponse_DecodeJsonParsesValidBody(t *testing.T) {
	response := NewResponse("https://example.org/packages.json", 200, nil, `{"foo":"bar"}`)

	v, err := response.DecodeJSON()
	if err != nil {
		t.Fatal(err)
	}

	if got := jsonOf(t, v); got != `{"foo":"bar"}` {
		t.Fatalf("got %s", got)
	}
}

func TestResponse_DecodeJsonDoesNotLeakResponseBodyOnParseError(t *testing.T) {
	url := "http://169.254.169.254/latest/meta-data/iam/security-credentials"
	response := NewResponse(url, 200, nil, `{"k":"secret-value-LEAKMARKER" X}`)

	_, err := response.DecodeJSON()

	pe, ok := errors.AsType[*jsonlint.ParsingError](err)
	if !ok {
		t.Fatalf("Expected a ParsingException to be thrown for invalid JSON, got %v", err)
	}

	if pe.Message != `"`+url+`" does not contain valid JSON` {
		t.Fatalf("got %q", pe.Message)
	}

	if pe.Details.Kind != jsonlint.NoDetails {
		t.Fatal("expected no details")
	}
}

func TestResponse_Headers(t *testing.T) {
	r := NewResponse("https://example.org", 200, []string{
		"HTTP/1.1 302 Found",
		"Location: /a",
		"HTTP/2 200 ",
		"content-type:   application/json  ",
		"Content-Type: text/plain",
	}, "")

	if v, _ := r.StatusMessage(); v != "HTTP/2 200 " {
		t.Errorf("status message %q", v)
	}

	if v, _ := r.Header("CONTENT-TYPE"); v != "text/plain" {
		t.Errorf("content-type %q", v)
	}

	if _, ok := r.Header("x-missing"); ok {
		t.Error("expected no header")
	}

	if v, _ := FindStatusCode(r.Headers()); v != 200 {
		t.Errorf("status code %d", v)
	}
}
