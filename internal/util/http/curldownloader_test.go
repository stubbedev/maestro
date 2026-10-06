package http

import (
	"bytes"
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// writeTestCA writes a freshly generated self-signed CA certificate (one
// no test server uses) to a PEM file and returns its path.
func writeTestCA(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "maestro test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "other-ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func writeServerCA(t *testing.T, srv *httptest.Server) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "ca.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// newTestDownloader returns a downloader for tests: no proxy, the given
// CA file (or the system bundle), debug output into a BufferIO.
func newTestDownloader(t *testing.T, config *fakeConfig, cafile string) (*HttpDownloader, *io.BufferIO) {
	t.Helper()
	clearProxyEnv(t)
	t.Setenv("COMPOSER_DISABLE_NETWORK", "")
	t.Setenv("COMPOSER_MAX_PARALLEL_HTTP", "")
	t.Setenv("COMPOSER_IPRESOLVE", "")

	if config == nil {
		config = newFakeConfig(map[string]any{"github-domains": list(), "gitlab-domains": list()})
	}

	b, err := io.NewBufferIO("", console.VerbosityDebug, nil)
	if err != nil {
		t.Fatal(err)
	}

	options := php.NewArray()
	if cafile != "" {
		options.Set("ssl", php.ArrayOf("cafile", cafile))
	}

	h, err := NewHttpDownloader(b, config, options, false, NewStaticRuntime("8.3.0", "test"))
	if err != nil {
		t.Fatal(err)
	}

	h.curl.sleep = func(time.Duration) {}

	return h, b
}

func TestHttpDownloader_CaptureAuthenticationParamsFromUrl(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	h, b := newTestDownloader(t, nil, "")
	url := strings.Replace(srv.URL, "http://", "http://user:pass@", 1) + "/composer/composer/404"

	_, err := h.Get(url, nil)

	te, ok := errors.AsType[*util.TransportError](err)
	if !ok || te.Code == 200 {
		t.Fatalf("expected a TransportException, got %v", err)
	}

	origin := strings.TrimPrefix(srv.URL, "http://")
	a := b.Authentication(origin)

	if authString(a.Username) != "user" || authString(a.Password) != "pass" {
		t.Fatalf("got %v", a)
	}
}

func TestHttpDownloader_PreventUrlAccessCallableBlocksDownload(t *testing.T) {
	h, _ := newTestDownloader(t, nil, "")

	_, err := h.Get("https://example.org/blocked", php.ArrayOf("prevent_url_access_callable", RegisterCallable(func(string) bool { return true })))

	te, ok := errors.AsType[*util.TransportError](err)
	if !ok || te.Message != `Access to "https://example.org/blocked" is blocked.` {
		t.Fatalf("got %v", err)
	}
}

func TestHttpDownloader_OutputWarnings(t *testing.T) {
	b, err := io.NewBufferIO("", 0, nil)
	if err != nil {
		t.Fatal(err)
	}

	if wrote, _ := OutputWarnings(b, "$URL", php.NewArray()); wrote || b.Output() != "" {
		t.Fatal("expected nothing written")
	}

	// warning/info keys present but filtered out by version constraints
	data := mustJSON(t, `{"warning":"old warning msg","warning-versions":"<2.0","warnings":[{"message":"should not appear","versions":"<2.2"}]}`)
	if wrote, _ := OutputWarnings(b, "$URL", data); wrote || b.Output() != "" {
		t.Fatal("expected nothing written")
	}

	data = mustJSON(t, `{
		"warning": "old warning msg", "warning-versions": ">=2.0",
		"info": "old info msg", "info-versions": ">=2.0",
		"warnings": [{"message": "should not appear", "versions": "<2.2"}, {"message": "visible warning", "versions": ">=2.2-dev"}],
		"infos": [{"message": "should not appear", "versions": "<2.2"}, {"message": "visible info", "versions": ">=2.2-dev"}]
	}`)

	wrote, err := OutputWarnings(b, "$URL", data)
	if err != nil || !wrote {
		t.Fatalf("expected warnings written: %v", err)
	}

	// the <info> tag is consumed by the OutputFormatter, but not <warning>
	// as that is not a default output format
	want := "<warning>Warning from $URL: old warning msg</warning>\n" +
		"Info from $URL: old info msg\n" +
		"<warning>Warning from $URL: visible warning</warning>\n" +
		"Info from $URL: visible info\n"

	if b.Output() != want {
		t.Fatalf("got %q", b.Output())
	}
}

func TestHttpDownloader_OutputWarningsStripsColors(t *testing.T) {
	b, _ := io.NewBufferIO("", 0, nil)

	data := php.ArrayOf("warning", "\x1b[31mred\x1b[0m text")
	if _, err := OutputWarnings(b, "https://u:p@example.org", data); err != nil {
		t.Fatal(err)
	}

	if b.Output() != "<warning>Warning from https://u:***@example.org: red text</warning>\n" {
		t.Fatalf("got %q", b.Output())
	}
}

func TestHttpDownloader_GetExceptionHints(t *testing.T) {
	old := connectivityCheck
	t.Cleanup(func() { connectivityCheck = old })

	if GetExceptionHints(errors.New("x")) != nil || GetExceptionHints(util.NewTransportError("other", 400)) != nil {
		t.Fatal("expected no hints")
	}

	e := util.NewTransportError("curl error 6 while downloading https://x: Could not resolve host: x", 400)

	connectivityCheck = func() bool { return true }
	if h := GetExceptionHints(e); len(h) != 1 || h[0] != "<error>The following exception probably indicates you have misconfigured DNS resolver(s)</error>" {
		t.Fatal(h)
	}

	connectivityCheck = func() bool { return false }
	if h := GetExceptionHints(e); len(h) != 1 || h[0] != "<error>The following exception probably indicates you are offline or have misconfigured DNS resolver(s)</error>" {
		t.Fatal(h)
	}
}

func TestHttpDownloader_GetAndHeaders(t *testing.T) {
	var gotHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Foo", "bar")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	h, b := newTestDownloader(t, nil, "")

	r, err := h.Get(srv.URL+"/packages.json", php.ArrayOf("http", php.ArrayOf("header", list("X-Custom: 1"))))
	if err != nil {
		t.Fatal(err)
	}

	if r.StatusCode() != 200 || r.Body() != `{"ok":true}` {
		t.Fatalf("got %d %q", r.StatusCode(), r.Body())
	}

	if msg, _ := r.StatusMessage(); msg != "HTTP/1.1 200 OK" {
		t.Fatalf("status line %q", msg)
	}

	if v, _ := r.Header("x-foo"); v != "bar" {
		t.Fatalf("header %q", v)
	}

	if !strings.HasPrefix(gotHeaders.Get("User-Agent"), "Composer/2.10.3 (") || gotHeaders.Get("X-Custom") != "1" ||
		gotHeaders.Get("Accept") != "*/*" || gotHeaders.Get("Accept-Encoding") != "deflate, gzip, br, zstd" {
		t.Fatalf("request headers %v", gotHeaders)
	}

	out := b.Output()
	if !strings.Contains(out, "Downloading "+srv.URL+"/packages.json\n") || !strings.Contains(out, "[200] "+srv.URL+"/packages.json\n") {
		t.Fatalf("output %q", out)
	}
}

func TestHttpDownloader_GzipIsDecoded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, _ = zw.Write([]byte("hello world"))
		_ = zw.Close()

		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()

	h, _ := newTestDownloader(t, nil, "")

	r, err := h.Get(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	if r.Body() != "hello world" {
		t.Fatalf("got %q", r.Body())
	}

	if v, _ := r.Header("content-encoding"); v != "gzip" {
		t.Fatal("expected the headers as received")
	}
}

func TestHttpDownloader_Redirects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/a/abs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://"+r.Host+"/b/rel")
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("/b/rel", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "target?x=1")
		w.WriteHeader(http.StatusMovedPermanently)
	})
	mux.HandleFunc("/b/target", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/final")
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("done")) })
	mux.HandleFunc("/nolocation", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusFound) })
	mux.HandleFunc("/file", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "file:///etc/passwd")
		w.WriteHeader(http.StatusFound)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	h, b := newTestDownloader(t, nil, "")

	r, err := h.Get(srv.URL+"/a/abs", nil)
	if err != nil {
		t.Fatal(err)
	}

	if r.Body() != "done" || r.URL() != srv.URL+"/final" {
		t.Fatalf("got %q from %s", r.Body(), r.URL())
	}

	for _, line := range []string{
		"Following redirect (1) " + srv.URL + "/b/rel",
		"Following redirect (2) " + srv.URL + "/b/target?x=1",
		"Following redirect (3) " + srv.URL + "/final",
	} {
		if !strings.Contains(b.Output(), line+"\n") {
			t.Fatalf("missing %q in %q", line, b.Output())
		}
	}

	_, err = h.Get(srv.URL+"/nolocation", nil)
	if err == nil || err.Error() != `The "`+srv.URL+`/nolocation" file could not be downloaded, got redirect without Location (HTTP/1.1 302 Found)` {
		t.Fatalf("got %v", err)
	}

	_, err = h.Get(srv.URL+"/file", nil)
	if err == nil || err.Error() != `Could not follow the redirect to "file:///etc/passwd" because only http and https redirects are supported.` {
		t.Fatalf("got %v", err)
	}
}

func TestRedirectTarget(t *testing.T) {
	for _, tc := range []struct{ url, location, want string }{
		{"https://example.org/a/b?c", "https://other.org/x", "https://other.org/x"},
		{"https://example.org/a/b?c", "//other.org/x", "https://other.org/x"},
		{"https://example.org:8080/a/b?c", "/x/y", "https://example.org:8080/x/y"},
		{"https://user@example.org/a/b", "/x", "https://user@example.org/x"},
		{"https://example.org/a/b?c", "x/y", "https://example.org/a/x/y"},
		{"https://example.org/a/", "x", "https://example.org/a/x"},
	} {
		got, err := redirectTarget(tc.url, NewResponse(tc.url, 302, []string{"HTTP/1.1 302 Found", "Location: " + tc.location}, ""))
		if err != nil || got != tc.want {
			t.Errorf("%s + %s: got %q %v, want %q", tc.url, tc.location, got, err, tc.want)
		}
	}
}

func TestHttpDownloader_RetriesServerErrors(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)

			return
		}

		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	h, b := newTestDownloader(t, nil, "")

	r, err := h.Get(srv.URL, nil)
	if err != nil || r.Body() != "ok" {
		t.Fatalf("got %v %v", r, err)
	}

	if !strings.Contains(b.Output(), "Retrying (1) "+srv.URL+" due to status code 502\n") ||
		!strings.Contains(b.Output(), "Retrying (2) "+srv.URL+" due to status code 502\n") {
		t.Fatal(b.Output())
	}

	if strings.Count(b.Output(), "Downloading ") != 1 {
		t.Fatal("expected only the first attempt announced")
	}
}

func TestHttpDownloader_FailedResponse(t *testing.T) {
	body := strings.Repeat("x", 250)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/post" {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"` + body + `"}`))
	}))
	defer srv.Close()

	h, _ := newTestDownloader(t, nil, "")

	_, err := h.Get(srv.URL+"/missing", nil)

	te, ok := errors.AsType[*util.TransportError](err)
	if !ok {
		t.Fatalf("got %v", err)
	}

	full := `{"error":"` + body + `"}`
	want := `The "` + srv.URL + `/missing" file could not be downloaded (HTTP/1.1 404 Not Found):` + "\n" + full[:200] + "..."

	if te.Message != want || te.Code != http.StatusNotFound || te.StatusCode != http.StatusNotFound || authString(te.Response) != full || len(te.Headers) == 0 || te.ResponseInfo == nil {
		t.Fatalf("got %q code %d status %d", te.Message, te.Code, te.StatusCode)
	}

	// POSTs are not retried
	_, err = h.Get(srv.URL+"/post", php.ArrayOf("http", php.ArrayOf("method", "POST", "content", "a=b")))
	if err == nil || err.Error() != `The "`+srv.URL+`/post" file could not be downloaded (HTTP/1.1 503 Service Unavailable)` {
		t.Fatalf("got %v", err)
	}
}

func TestHttpDownloader_CurlErrors(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	addr := l.Addr().String()
	_ = l.Close()

	h, b := newTestDownloader(t, nil, "")

	_, err = h.Get("http://"+addr+"/x", nil)
	if err == nil || !strings.HasPrefix(err.Error(), "curl error 7 while downloading http://"+addr+"/x: Failed to connect to "+addr+" after ") {
		t.Fatalf("got %v", err)
	}

	if !strings.Contains(b.Output(), "Retrying (3) http://"+addr+"/x due to curl error 7\n") {
		t.Fatal(b.Output())
	}

	te, _ := errors.AsType[*util.TransportError](err)
	if te.ResponseInfo == nil || te.ResponseInfo.ErrorCode != 7 || te.Headers != nil {
		t.Fatalf("got %+v", te)
	}
}

func TestHttpDownloader_CopyToFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("nope"))

			return
		}

		_, _ = w.Write([]byte("archive"))
	}))
	defer srv.Close()

	h, _ := newTestDownloader(t, nil, "")
	dir := t.TempDir()
	target := filepath.Join(dir, "file.zip")

	r, err := h.Copy(srv.URL+"/file.zip", target, nil)
	if err != nil {
		t.Fatal(err)
	}

	if data, _ := os.ReadFile(target); string(data) != "archive" || r.Body() != target+"~" {
		t.Fatalf("got %q, body %q", data, r.Body())
	}

	if _, err := os.Stat(target + "~"); !os.IsNotExist(err) {
		t.Fatal("expected the temporary file renamed")
	}

	_, err = h.Copy(srv.URL+"/missing", filepath.Join(dir, "missing.zip"), nil)
	if te, ok := errors.AsType[*util.TransportError](err); !ok || authString(te.Response) != "nope" {
		t.Fatalf("got %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "missing.zip~")); !os.IsNotExist(err) {
		t.Fatal("expected the temporary file removed")
	}

	_, err = h.Copy(srv.URL+"/file.zip", filepath.Join(dir, "no/such/dir/file.zip"), nil)
	if err == nil || err.Error() != `The "`+srv.URL+`/file.zip" file could not be written to `+filepath.Join(dir, "no/such/dir/file.zip")+": Failed to open stream: No such file or directory" {
		t.Fatalf("got %v", err)
	}
}

func TestHttpDownloader_MaxFileSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chunked" {
			w.(http.Flusher).Flush()
		}

		_, _ = w.Write([]byte(strings.Repeat("a", 100)))
	}))
	defer srv.Close()

	h, _ := newTestDownloader(t, nil, "")

	_, err := h.Get(srv.URL+"/len", php.ArrayOf("max_file_size", 50))
	if _, ok := errors.AsType[*util.MaxFileSizeExceededError](err); !ok || err.Error() != "Maximum allowed download size reached. Content-length header indicates 100 bytes. Allowed 50 bytes for "+srv.URL+"/len" {
		t.Fatalf("got %v", err)
	}

	_, err = h.Get(srv.URL+"/len", php.ArrayOf("max_file_size", 100))
	if _, ok := errors.AsType[*util.TransportError](err); !ok || err.Error() != "Maximum allowed download size reached. Downloaded 100 of allowed 100 bytes for "+srv.URL+"/len" {
		t.Fatalf("got %v", err)
	}

	if r, err := h.Get(srv.URL+"/len", php.ArrayOf("max_file_size", 101)); err != nil || len(r.Body()) != 100 {
		t.Fatalf("got %v", err)
	}
}

func TestHttpDownloader_NetworkDisabled(t *testing.T) {
	h, _ := newTestDownloader(t, nil, "")
	h.disabled = true

	_, err := h.Get("https://example.org/x", nil)
	if te, ok := errors.AsType[*util.TransportError](err); !ok || te.Message != "Network disabled, request canceled: https://example.org/x" || te.Code != 499 || te.StatusCode != 499 {
		t.Fatalf("got %v", err)
	}

	r, err := h.Get("https://example.org/x", php.ArrayOf("http", php.ArrayOf("header", list("If-Modified-Since: x"))))
	if err != nil || r.StatusCode() != 304 {
		t.Fatalf("got %v %v", r, err)
	}
}

func TestHttpDownloader_EmptyURL(t *testing.T) {
	h, _ := newTestDownloader(t, nil, "")

	if _, err := h.Get("", nil); err == nil || err.Error() != "$url must not be an empty string" {
		t.Fatal(err)
	}

	if _, err := h.Add("x", nil); err == nil || err.Error() != `You must use the HttpDownloader instance which is part of a Composer\Loop instance to be able to run async http requests` {
		t.Fatal(err)
	}
}

func TestHttpDownloader_TLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, r.Proto)
	}))
	defer srv.Close()

	h, _ := newTestDownloader(t, nil, writeServerCA(t, srv))

	r, err := h.Get(srv.URL, nil)
	if err != nil || r.Body() != "HTTP/1.1" {
		t.Fatalf("got %v %v", r, err)
	}

	untrusted, _ := newTestDownloader(t, nil, writeTestCA(t))

	_, err = untrusted.Get(srv.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "curl error 60 while downloading "+srv.URL+": SSL certificate OpenSSL verify result: self-signed certificate (18)") {
		t.Fatalf("got %v", err)
	}

	r, err = untrusted.Get(srv.URL, php.ArrayOf("ssl", php.ArrayOf("verify_peer", false)))
	if err != nil || r.Body() != "HTTP/1.1" {
		t.Fatalf("got %v", err)
	}
}

func TestHttpDownloader_HTTP2(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Proto", r.Proto)
		_, _ = fmt.Fprint(w, r.Proto)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	h, _ := newTestDownloader(t, nil, writeServerCA(t, srv))

	r, err := h.Get(srv.URL, nil)
	if err != nil || r.Body() != "HTTP/2.0" {
		t.Fatalf("got %v %v", r, err)
	}

	if msg, _ := r.StatusMessage(); msg != "HTTP/2 200 " {
		t.Fatalf("status line %q", msg)
	}

	if v, _ := r.Header("X-Proto"); v != "HTTP/2.0" || !strings.Contains(strings.Join(r.Headers(), "\n"), "x-proto: HTTP/2.0") {
		t.Fatalf("headers %q", r.Headers())
	}
}

func TestHttpDownloader_AuthenticationPrompt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); ok && u == "me" && p == "secret" {
			_, _ = w.Write([]byte("private"))

			return
		}

		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	// non-interactive: fails with Composer's message
	h, _ := newTestDownloader(t, nil, "")

	_, err := h.Get(srv.URL+"/p", nil)
	if te, ok := errors.AsType[*util.TransportError](err); !ok || te.Code != 401 ||
		te.Message != "The '"+srv.URL+"/p' URL required authentication (HTTP 401).\nYou must be using the interactive console to authenticate" {
		t.Fatalf("got %v", err)
	}

	// interactive: asks, retries and stores
	m := newIOMock(t)
	m.expects([]map[string]any{
		{"text": "    Authentication required (" + strings.TrimPrefix(srv.URL, "http://") + "):"},
		{"ask": "      Username: ", "reply": "me"},
		{"ask": "      Password: ", "reply": "secret"},
		{"ask": "Do you want to store credentials for " + strings.TrimPrefix(srv.URL, "http://") + " in auth.json ? [Yn] ", "reply": "y"},
	}, false)

	config := newFakeConfig(map[string]any{"github-domains": list(), "gitlab-domains": list(), "store-auths": "prompt"})
	h.io, h.curl.io, h.curl.authHelper.io = m, m, m
	h.config, h.curl.config, h.curl.authHelper.config = config, config, config

	r, err := h.Get(srv.URL+"/p", nil)
	if err != nil || r.Body() != "private" {
		t.Fatalf("got %v %v", r, err)
	}

	m.assertComplete(t)

	if config.auth == nil || len(config.auth.added) != 1 || config.auth.added[0] != "http-basic."+strings.TrimPrefix(srv.URL, "http://")+`={"username":"me","password":"secret"}` {
		t.Fatalf("stored %v", config.auth)
	}
}

func TestHttpDownloader_InsecureURLRefused(t *testing.T) {
	config := newFakeConfig(map[string]any{"gitlab-domains": list()})
	config.prohibit = func(url string) error {
		return util.NewTransportError("Your configuration does not allow connections to "+url+". See https://getcomposer.org/doc/06-config.md#secure-http for details.", 400)
	}

	h, _ := newTestDownloader(t, config, "")

	_, err := h.Get("http://example.org/x", nil)
	if err == nil || !strings.HasPrefix(err.Error(), "Your configuration does not allow connections to http://example.org/x.") {
		t.Fatalf("got %v", err)
	}
}

func TestLoop_WaitsForAsyncRequests(t *testing.T) {
	var concurrent, peak atomic.Int32

	release := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := concurrent.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}

		if r.URL.Path != "/fast" {
			<-release
		}

		concurrent.Add(-1)
		_, _ = w.Write([]byte(r.URL.Path))
	}))
	defer srv.Close()

	t.Setenv("COMPOSER_MAX_PARALLEL_HTTP", "3")

	h, _ := newTestDownloader(t, nil, "")
	h.maxJobs = 3
	loop := NewLoop(h, nil)

	var promises []Waitable

	results := make([]string, 6)

	for i := range 6 {
		p, err := h.Add(fmt.Sprintf("%s/%d", srv.URL, i), nil)
		if err != nil {
			t.Fatal(err)
		}

		promises = append(promises, util.Then(p, func(r *Response) (struct{}, error) {
			results[i] = r.Body()

			return struct{}{}, nil
		}))
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		close(release)
	}()

	if err := loop.Wait(promises, nil); err != nil {
		t.Fatal(err)
	}

	for i, r := range results {
		if r != fmt.Sprintf("/%d", i) {
			t.Fatalf("result %d = %q", i, r)
		}
	}

	if peak.Load() > 3 {
		t.Fatalf("expected at most 3 parallel requests, saw %d", peak.Load())
	}

	// a failing request makes Wait return its error
	srv404 := httptest.NewServer(http.NotFoundHandler())
	defer srv404.Close()

	p, _ := h.Add(srv404.URL+"/x", nil)

	err := loop.Wait([]Waitable{p}, nil)
	if te, ok := errors.AsType[*util.TransportError](err); !ok || te.Code != 404 {
		t.Fatalf("got %v", err)
	}
}

// TestLoop_CallbacksRunInStartOrder: requests finishing in reverse order
// have their callbacks (and Composer's per-response processing, here the
// debug lines) run on the waiting goroutine in the order they started,
// so the output is deterministic.
func TestLoop_CallbacksRunInStartOrder(t *testing.T) {
	const n = 5

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
		time.Sleep(time.Duration(n-i) * 5 * time.Millisecond)
		_, _ = w.Write([]byte(r.URL.Path))
	}))
	defer srv.Close()

	h, out := newTestDownloader(t, nil, "")
	loop := NewLoop(h, nil)

	var (
		order    []string
		promises []Waitable
	)

	for i := range n {
		p, err := h.Add(fmt.Sprintf("%s/%d", srv.URL, i), nil)
		if err != nil {
			t.Fatal(err)
		}

		promises = append(promises, util.Then(p, func(r *Response) (struct{}, error) {
			order = append(order, r.Body())
			out.WriteError("callback "+r.Body(), true, io.Normal)

			return struct{}{}, nil
		}))
	}

	if err := loop.Wait(promises, nil); err != nil {
		t.Fatal(err)
	}

	if want := []string{"/0", "/1", "/2", "/3", "/4"}; !slices.Equal(order, want) {
		t.Fatalf("order %q, want %q", order, want)
	}

	var lines []string
	for line := range strings.SplitSeq(out.Output(), "\n") {
		if strings.HasPrefix(line, "[200]") || strings.HasPrefix(line, "callback") {
			lines = append(lines, strings.ReplaceAll(line, srv.URL, ""))
		}
	}

	var want []string
	for i := range n {
		want = append(want, fmt.Sprintf("[200] /%d", i), fmt.Sprintf("callback /%d", i))
	}

	if !slices.Equal(lines, want) {
		t.Fatalf("output %q, want %q", lines, want)
	}
}

func TestLoop_ThenAddsMoreWork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.URL.Path))
	}))
	defer srv.Close()

	h, _ := newTestDownloader(t, nil, "")
	loop := NewLoop(h, nil)

	var second atomic.Value

	first, _ := h.Add(srv.URL+"/one", nil)
	chained := util.Then(first, func(*Response) (struct{}, error) {
		p, err := h.Add(srv.URL+"/two", nil)
		if err != nil {
			return struct{}{}, err
		}

		// callbacks run on the waiting goroutine: waiting for more work
		// from one is a nested wait, as SyncHelper::await in PHP
		if err := loop.Wait([]Waitable{p}, nil); err != nil {
			return struct{}{}, err
		}

		r, err := p.Wait()
		if err == nil {
			second.Store(r.Body())
		}

		return struct{}{}, err
	})

	if err := loop.Wait([]Waitable{chained}, nil); err != nil {
		t.Fatal(err)
	}

	if second.Load() != "/two" {
		t.Fatalf("got %v", second.Load())
	}
}

func TestHttpDownloader_Cancel(t *testing.T) {
	block := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(block)

	h, _ := newTestDownloader(t, nil, "")
	h.maxJobs = 1
	loop := NewLoop(h, nil)

	started, _ := h.Add(srv.URL+"/1", nil)
	queued, _ := h.Add(srv.URL+"/2", nil)

	go func() {
		time.Sleep(20 * time.Millisecond)
		loop.AbortJobs()
	}()

	err := loop.Wait([]Waitable{started, queued}, nil)
	if _, ok := errors.AsType[*util.IrrecoverableDownloadError](err); !ok {
		t.Fatalf("got %v", err)
	}

	if err.Error() != "Download of "+srv.URL+"/1 canceled" && err.Error() != "Download of "+srv.URL+"/2 canceled" {
		t.Fatal(err)
	}

	if n := h.CountActiveJobs(); n != 0 {
		t.Fatalf("%d jobs still active", n)
	}
}

func TestHttpDownloader_GetUsesRemoteFilesystemForFiles(t *testing.T) {
	h, _ := newTestDownloader(t, nil, "")

	file := filepath.Join(t.TempDir(), "packages.json")
	if err := os.WriteFile(file, []byte(`{"packages":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := h.Get("file://"+file, nil)
	if err != nil || r.Body() != `{"packages":[]}` || r.StatusCode() != 0 {
		t.Fatalf("got %v %v", r, err)
	}

	_, err = h.Get("file://"+file+".missing", nil)
	if err == nil || err.Error() != `The "file://`+file+`.missing" file could not be downloaded: Failed to open stream: No such file or directory` {
		t.Fatalf("got %v", err)
	}
}

// Loop::wait starts the progress bar at countActiveJobs(), which counts
// the queued requests too: with 3 slots and 6 requests the bar goes to 6.
func TestLoop_WaitProgressCountsQueuedJobs(t *testing.T) {
	release := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		_, _ = w.Write([]byte(r.URL.Path))
	}))
	defer srv.Close()

	h, _ := newTestDownloader(t, nil, "")
	h.maxJobs = 3
	loop := NewLoop(h, nil)

	var promises []Waitable

	for i := range 6 {
		p, err := h.Add(fmt.Sprintf("%s/%d", srv.URL, i), nil)
		if err != nil {
			t.Fatal(err)
		}

		promises = append(promises, p)
	}

	close(release)

	out := console.NewBufferedOutput(console.VerbosityNormal, false, nil)
	progress := console.NewProgressBar(out, 0, console.DefaultMinSecondsBetweenRedraws)

	if err := loop.Wait(promises, progress); err != nil {
		t.Fatal(err)
	}

	if got := progress.MaxSteps(); got != 6 {
		t.Fatalf("progress max %d, want 6\n%s", got, out.Fetch())
	}
}
