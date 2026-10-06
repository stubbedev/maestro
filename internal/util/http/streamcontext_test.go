package http

import (
	"encoding/base64"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

func newStreamContextTest(t *testing.T) *StaticRuntime {
	t.Helper()
	clearProxyEnv(t)

	return NewStaticRuntime("8.3.0", "")
}

// assertEquals is PHPUnit's assertEquals on arrays: PHP's ==.
func assertEquals(t *testing.T, expected, actual any) {
	t.Helper()

	if !php.LooseEquals(expected, actual) {
		t.Fatalf("expected %s, got %s", jsonOf(t, expected), jsonOf(t, actual))
	}
}

func TestStreamContextFactory_GetContext(t *testing.T) {
	rt := newStreamContextTest(t)

	for _, tc := range []struct {
		expected, defaults string
	}{
		{`{"http":{"follow_location":1,"max_redirects":20,"header":["User-Agent: foo"]}}`, `{"http":{"header":"User-Agent: foo"}}`},
		{`{"http":{"method":"GET","max_redirects":20,"follow_location":1,"header":["User-Agent: foo"]}}`, `{"http":{"method":"GET","header":"User-Agent: foo"}}`},
	} {
		defaults, _ := mustJSON(t, tc.defaults).(*php.Array)

		context, err := GetContext("http://example.org", defaults, rt)
		if err != nil {
			t.Fatal(err)
		}

		assertEquals(t, mustJSON(t, tc.expected), context)
	}
}

func TestStreamContextFactory_HttpProxy(t *testing.T) {
	rt := newStreamContextTest(t)
	t.Setenv("http_proxy", "http://username:p%40ssword@proxyserver.net:3128/")
	// Environment names are case insensitive on Windows, where this would
	// overwrite http_proxy instead of being shadowed by it.
	if runtime.GOOS != "windows" {
		t.Setenv("HTTP_PROXY", "http://proxyserver/")
	}

	context, err := GetContext("http://example.org", php.ArrayOf("http", php.ArrayOf("method", "GET", "header", "User-Agent: foo")), rt)
	if err != nil {
		t.Fatal(err)
	}

	assertEquals(t, php.ArrayOf("http", php.ArrayOf(
		"proxy", "tcp://proxyserver.net:3128",
		"request_fulluri", true,
		"method", "GET",
		"header", list("User-Agent: foo", "Proxy-Authorization: Basic "+base64.StdEncoding.EncodeToString([]byte("username:p@ssword"))),
		"max_redirects", 20,
		"follow_location", 1,
	)), context)
}

func TestStreamContextFactory_HttpProxyWithNoProxy(t *testing.T) {
	for _, noProxy := range []string{"foo,example.org", "*"} {
		rt := newStreamContextTest(t)
		t.Setenv("http_proxy", "http://username:password@proxyserver.net:3128/")
		t.Setenv("no_proxy", noProxy)

		context, err := GetContext("http://example.org", php.ArrayOf("http", php.ArrayOf("method", "GET", "header", "User-Agent: foo")), rt)
		if err != nil {
			t.Fatal(err)
		}

		assertEquals(t, php.ArrayOf("http", php.ArrayOf(
			"method", "GET",
			"max_redirects", 20,
			"follow_location", 1,
			"header", list("User-Agent: foo"),
		)), context)
	}
}

func TestStreamContextFactory_OptionsArePreserved(t *testing.T) {
	rt := newStreamContextTest(t)
	t.Setenv("http_proxy", "http://username:password@proxyserver.net:3128/")

	context, err := GetContext("http://example.org", php.ArrayOf("http", php.ArrayOf("method", "GET", "header", list("User-Agent: foo", "X-Foo: bar"), "request_fulluri", false)), rt)
	if err != nil {
		t.Fatal(err)
	}

	assertEquals(t, php.ArrayOf("http", php.ArrayOf(
		"proxy", "tcp://proxyserver.net:3128",
		"request_fulluri", false,
		"method", "GET",
		"header", list("User-Agent: foo", "X-Foo: bar", "Proxy-Authorization: Basic "+base64.StdEncoding.EncodeToString([]byte("username:password"))),
		"max_redirects", 20,
		"follow_location", 1,
	)), context)
}

func TestStreamContextFactory_HttpProxyWithoutPort(t *testing.T) {
	rt := newStreamContextTest(t)
	t.Setenv("https_proxy", "http://username:password@proxyserver.net")

	context, err := GetContext("https://example.org", php.ArrayOf("http", php.ArrayOf("method", "GET", "header", "User-Agent: foo")), rt)
	if err != nil {
		t.Fatal(err)
	}

	assertEquals(t, php.ArrayOf("http", php.ArrayOf(
		"proxy", "tcp://proxyserver.net:80",
		"method", "GET",
		"header", list("User-Agent: foo", "Proxy-Authorization: Basic "+base64.StdEncoding.EncodeToString([]byte("username:password"))),
		"max_redirects", 20,
		"follow_location", 1,
	)), context)
}

func TestStreamContextFactory_HttpsProxyOverride(t *testing.T) {
	rt := newStreamContextTest(t)
	t.Setenv("http_proxy", "http://username:password@proxyserver.net")
	t.Setenv("https_proxy", "https://woopproxy.net")

	_, err := GetContext("https://example.org", php.ArrayOf("http", php.ArrayOf("method", "GET", "header", "User-Agent: foo")), rt)
	if _, ok := errors.AsType[*util.TransportError](err); !ok {
		t.Fatalf("expected a TransportException, got %v", err)
	}
}

func TestStreamContextFactory_SSLProxy(t *testing.T) {
	for _, tc := range [][2]string{
		{"ssl://proxyserver:443", "https://proxyserver/"},
		{"ssl://proxyserver:8443", "https://proxyserver:8443"},
	} {
		rt := newStreamContextTest(t)
		t.Setenv("http_proxy", tc[1])

		context, err := GetContext("http://example.org", php.ArrayOf("http", php.ArrayOf("header", "User-Agent: foo")), rt)
		if err != nil {
			t.Fatal(err)
		}

		assertEquals(t, php.ArrayOf("http", php.ArrayOf(
			"proxy", tc[0],
			"request_fulluri", true,
			"max_redirects", 20,
			"follow_location", 1,
			"header", list("User-Agent: foo"),
		)), context)
	}
}

func TestStreamContextFactory_EnsureThatfixHttpHeaderFieldMovesContentTypeToEndOfOptions(t *testing.T) {
	rt := newStreamContextTest(t)

	options := php.ArrayOf("http", php.ArrayOf("header", "User-agent: foo\r\nX-Foo: bar\r\nContent-Type: application/json\r\nAuthorization: Basic aW52YWxpZA=="))

	context, err := GetContext("http://example.org", options, rt)
	if err != nil {
		t.Fatal(err)
	}

	headers := headerList(context)
	if want := []string{"User-agent: foo", "X-Foo: bar", "Authorization: Basic aW52YWxpZA==", "Content-Type: application/json"}; strings.Join(headers, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", headers)
	}
}

func TestStreamContextFactory_InitOptionsDoesIncludeProxyAuthHeaders(t *testing.T) {
	rt := newStreamContextTest(t)
	t.Setenv("https_proxy", "http://username:password@proxyserver.net:3128/")

	options, err := InitOptions("https://example.org", php.NewArray(), false, rt)
	if err != nil {
		t.Fatal(err)
	}

	if php.Stripos(strings.Join(headerList(options), " "), "Proxy-Authorization") < 0 {
		t.Fatal("expected a Proxy-Authorization header")
	}
}

func TestStreamContextFactory_InitOptionsForCurlDoesNotIncludeProxyAuthHeaders(t *testing.T) {
	rt := newStreamContextTest(t)
	t.Setenv("http_proxy", "http://username:password@proxyserver.net:3128/")

	options, err := InitOptions("https://example.org", php.NewArray(), true, rt)
	if err != nil {
		t.Fatal(err)
	}

	if php.Stripos(strings.Join(headerList(options), " "), "Proxy-Authorization") >= 0 {
		t.Fatal("expected no Proxy-Authorization header")
	}
}

func userAgentOf(t *testing.T, rt Runtime) string {
	t.Helper()

	options, err := InitOptions("https://example.org", php.NewArray(), false, rt)
	if err != nil {
		t.Fatal(err)
	}

	for _, header := range headerList(options) {
		if php.Strncasecmp(header, "User-Agent:", 11) == 0 {
			return header
		}
	}

	t.Fatal("No User-Agent header was built")

	return ""
}

func TestStreamContextFactory_UserAgentIncludesRunningCommand(t *testing.T) {
	rt := newStreamContextTest(t)
	rt.SetRunningCommand("install", true)

	if ok, _ := php.PregIsMatch(`{User-Agent: Composer/\S.*; cmd:install\)}`, userAgentOf(t, rt)); !ok {
		t.Fatal(userAgentOf(t, rt))
	}
}

func TestStreamContextFactory_UserAgentOmitsCommandWhenNotSet(t *testing.T) {
	rt := newStreamContextTest(t)

	if strings.Contains(userAgentOf(t, rt), "cmd:") {
		t.Fatal(userAgentOf(t, rt))
	}
}

func TestStreamContextFactory_UserAgentSanitizesRunningCommand(t *testing.T) {
	rt := newStreamContextTest(t)
	rt.SetRunningCommand("foo bar\r\nInjected: header", true)

	ua := userAgentOf(t, rt)
	if strings.ContainsAny(ua, "\r\n") || !strings.Contains(ua, "; cmd:foobarInjected:header)") {
		t.Fatal(ua)
	}
}

func TestStreamContextFactory_UserAgentAppendsRunningOperation(t *testing.T) {
	rt := newStreamContextTest(t)
	rt.SetRunningCommand("require", true)
	rt.SetRunningOperation("update", true)

	if ua := userAgentOf(t, rt); !strings.Contains(ua, "; cmd:require,update)") {
		t.Fatal(ua)
	}
}

func TestStreamContextFactory_UserAgentOmitsRunningOperationWhenSameAsCommand(t *testing.T) {
	rt := newStreamContextTest(t)
	rt.SetRunningCommand("install", true)
	rt.SetRunningOperation("install", true)

	if ua := userAgentOf(t, rt); !strings.Contains(ua, "; cmd:install)") || strings.Contains(ua, "install,install") {
		t.Fatal(ua)
	}
}

func TestStreamContextFactory_UserAgentUsesRunningOperationWhenNoCommand(t *testing.T) {
	rt := newStreamContextTest(t)
	rt.SetRunningOperation("update", true)

	if ua := userAgentOf(t, rt); !strings.Contains(ua, "; cmd:update)") {
		t.Fatal(ua)
	}
}

func TestStreamContextFactory_UserAgentFormat(t *testing.T) {
	rt := newStreamContextTest(t)
	rt.SetPlatformPHPVersion("8.1.0")
	t.Setenv("CI", "1")

	sysname, release := uname()
	want := "User-Agent: Composer/2.10.3 (" + sysname + "; " + release + "; PHP 8.3.0; maestro; Platform-PHP 8.1.0; CI)"

	if ua := userAgentOf(t, rt); ua != want {
		t.Fatalf("got %q, want %q", ua, want)
	}

	t.Setenv("CI", "0")

	if ua := userAgentOf(t, NewStaticRuntime("", "1.2.3")); !strings.Contains(ua, "; PHP unknown; maestro 1.2.3)") {
		t.Fatal(ua)
	}
}

func TestStreamContextFactory_GetTlsDefaults(t *testing.T) {
	ResetCaBundle()
	t.Cleanup(ResetCaBundle)

	_, err := GetTLSDefaults(php.ArrayOf("ssl", php.ArrayOf("cafile", "/some/path/file.crt")), nil)
	if te, ok := errors.AsType[*util.TransportError](err); !ok || te.Message != "The configured cafile was not valid or could not be read." {
		t.Fatalf("got %v", err)
	}

	_, err = GetTLSDefaults(php.ArrayOf("ssl", php.ArrayOf("capath", "/some/missing/dir")), nil)
	if te, ok := errors.AsType[*util.TransportError](err); !ok || te.Message != "The configured capath was not valid or could not be read." {
		t.Fatalf("got %v", err)
	}

	cafile := writeTestCA(t)

	defaults, err := GetTLSDefaults(php.ArrayOf("ssl", php.ArrayOf("cafile", cafile)), nil)
	if err != nil {
		t.Fatal(err)
	}

	ciphers, _ := optionString(defaults, "ssl", "ciphers")
	if !strings.Contains(ciphers, "!aNULL:!eNULL:!EXPORT:!DES:!3DES:!RC4:!MD5:!PSK:!aECDH:!EDH-DSS-DES-CBC3-SHA:!EDH-RSA-DES-CBC3-SHA:!KRB5-DES-CBC3-SHA") {
		t.Fatal(ciphers)
	}

	for key, want := range map[string]any{"verify_peer": true, "SNI_enabled": true, "verify_depth": 7, "cafile": cafile, "disable_compression": true} {
		if v, _ := path(defaults, "ssl", key); !php.LooseEquals(v, want) {
			t.Errorf("%s = %v", key, v)
		}
	}
}
