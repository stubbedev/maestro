package http

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

func rfsConfig() *fakeConfig {
	return newFakeConfig(map[string]any{"github-domains": list(), "gitlab-domains": list()})
}

func newTestRFS(t *testing.T, fio *fakeIO, options *php.Array, authHelper Authenticator) *RemoteFilesystem {
	t.Helper()
	clearProxyEnv(t)

	if fio == nil {
		fio = newFakeIO()
	}

	r, err := NewRemoteFilesystem(fio, rfsConfig(), options, false, authHelper)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

// callGetOptionsForUrl is RemoteFilesystemTest::callGetOptionsForUrl.
func callGetOptionsForUrl(t *testing.T, fio *fakeIO, originURL string, additional, options *php.Array, fileURL string) *php.Array {
	t.Helper()

	r := newTestRFS(t, fio, options, nil)
	r.fileURL = fileURL

	return r.optionsForURL(originURL, additional)
}

func TestRemoteFilesystem_GetOptionsForUrl(t *testing.T) {
	fio := newFakeIO()

	res := callGetOptionsForUrl(t, fio, "http://example.org", php.NewArray(), nil, "")

	if h, ok := path(res, "http", "header"); !ok {
		t.Fatal("getOptions must return an array with headers")
	} else if _, isArray := h.(*php.Array); !isArray {
		t.Fatal("getOptions must return an array with headers")
	}

	if fio.calls["hasAuthentication"] != 1 {
		t.Fatalf("hasAuthentication called %d times", fio.calls["hasAuthentication"])
	}
}

func TestRemoteFilesystem_GetOptionsForUrlWithAuthorization(t *testing.T) {
	fio := newFakeIO()
	fio.hasAuth = func(string) bool { return true }
	fio.getAuth = func(string) ioAuth { return auth("login", "password") }

	options := callGetOptionsForUrl(t, fio, "http://example.org", php.NewArray(), nil, "")

	found := false
	for _, header := range headerList(options) {
		if strings.HasPrefix(header, "Authorization: Basic") {
			found = true
		}
	}

	if !found || fio.calls["hasAuthentication"] != 1 || fio.calls["getAuthentication"] != 1 {
		t.Fatal("getOptions must have an Authorization header")
	}
}

func TestRemoteFilesystem_GetOptionsForUrlWithStreamOptions(t *testing.T) {
	fio := newFakeIO()
	fio.hasAuth = func(string) bool { return true }
	fio.getAuth = func(string) ioAuth { return auth("\x00", "\x00") }

	res := callGetOptionsForUrl(t, fio, "https://example.org", php.NewArray(), php.ArrayOf("ssl", php.ArrayOf("allow_self_signed", true)), "")

	if v, _ := path(res, "ssl", "allow_self_signed"); v != true {
		t.Fatal("getOptions must return an array with a allow_self_signed set to true")
	}
}

func TestRemoteFilesystem_GetOptionsForUrlWithCallOptionsKeepsHeader(t *testing.T) {
	fio := newFakeIO()
	fio.hasAuth = func(string) bool { return true }
	fio.getAuth = func(string) ioAuth { return auth("\x00", "\x00") }

	res := callGetOptionsForUrl(t, fio, "https://example.org", php.ArrayOf("http", php.ArrayOf("header", "Foo: bar")), nil, "")

	headers := headerList(res)

	found := false
	for _, header := range headers {
		if header == "Foo: bar" {
			found = true
		}
	}

	if !found || len(headers) <= 1 {
		t.Fatalf("getOptions must have a Foo: bar header, got %q", headers)
	}
}

func TestRemoteFilesystem_CallbackGetFileSize(t *testing.T) {
	r := newTestRFS(t, nil, nil, nil)

	if err := r.CallbackGet(StreamNotifyFileSizeIs, 0, "", 0, 0, 20); err != nil || r.bytesMax != 20 {
		t.Fatalf("bytesMax %d, %v", r.bytesMax, err)
	}
}

func TestRemoteFilesystem_CallbackGetNotifyProgress(t *testing.T) {
	fio := newFakeIO()
	r := newTestRFS(t, fio, nil, nil)
	r.bytesMax = 20
	r.progress = true

	if err := r.CallbackGet(StreamNotifyProgress, 0, "", 0, 10, 20); err != nil {
		t.Fatal(err)
	}

	if r.lastProgress != 50 || fio.calls["overwriteError"] != 1 {
		t.Fatalf("lastProgress %d, overwriteError %d", r.lastProgress, fio.calls["overwriteError"])
	}
}

func TestRemoteFilesystem_CallbackGetPassesThrough404(t *testing.T) {
	r := newTestRFS(t, nil, nil, nil)

	if err := r.CallbackGet(StreamNotifyFailure, 0, "HTTP/1.1 404 Not Found", 404, 0, 0); err != nil {
		t.Fatal(err)
	}

	err := r.CallbackGet(StreamNotifyFailure, 0, "HTTP/1.1 400 Bad Request", 400, 0, 0)
	if te, ok := errors.AsType[*util.TransportError](err); !ok || te.Code != 400 {
		t.Fatalf("got %v", err)
	}
}

func thisFile(t *testing.T) string {
	t.Helper()

	_, file, _, _ := runtime.Caller(0)

	return file
}

func TestRemoteFilesystem_GetContents(t *testing.T) {
	r := newTestRFS(t, nil, nil, nil)

	contents, err := r.GetContents("http://example.org", "file://"+thisFile(t), false, nil)
	if err != nil || !strings.Contains(contents, "TestRemoteFilesystem_GetContents") {
		t.Fatalf("got %v", err)
	}
}

func TestRemoteFilesystem_Copy(t *testing.T) {
	r := newTestRFS(t, nil, nil, nil)
	file := filepath.Join(t.TempDir(), "copy")

	ok, err := r.Copy("http://example.org", "file://"+thisFile(t), file, false, nil)
	if err != nil || !ok {
		t.Fatalf("got %v %v", ok, err)
	}

	if data, _ := os.ReadFile(file); !strings.Contains(string(data), "TestRemoteFilesystem_Copy") {
		t.Fatal("expected the copied file")
	}
}

func TestRemoteFilesystem_CopyWithNoRetryOnFailure(t *testing.T) {
	r := newTestRFS(t, nil, nil, nil)
	calls := 0
	r.getRemoteContents = func(string, string, *php.Array, int64) (remoteContents, error) {
		calls++

		return remoteContents{ok: true, headers: []string{"http/1.1 401 unauthorized"}}, nil
	}

	_, err := r.Copy("http://example.org", "file://"+thisFile(t), filepath.Join(t.TempDir(), "x"), true, php.ArrayOf("retry-auth-failure", false))
	if _, ok := errors.AsType[*util.TransportError](err); !ok || calls != 1 {
		t.Fatalf("got %v after %d calls", err, calls)
	}
}

// promptAuthHelper is an AuthHelper whose promptAuthIfNeeded is mocked.
type promptAuthHelper struct {
	*AuthHelper
	prompts int
	result  AuthResult
}

func (p *promptAuthHelper) PromptAuthIfNeeded(string, string, int, string, []string, int, string) (AuthResult, error) {
	p.prompts++

	return p.result, nil
}

func (p *promptAuthHelper) StoreAuth(string, StoreAuth) error { return nil }

func TestRemoteFilesystem_CopyWithSuccessOnRetry(t *testing.T) {
	helper := &promptAuthHelper{AuthHelper: NewAuthHelper(newFakeIO(), rfsConfig()), result: AuthResult{Retry: true, StoreAuth: StoreAuthYes}}
	r := newTestRFS(t, nil, nil, helper)

	counter := 0
	r.getRemoteContents = func(string, string, *php.Array, int64) (remoteContents, error) {
		counter++
		if counter == 1 {
			return remoteContents{ok: true, headers: []string{"http/1.1 401 unauthorized"}}, nil
		}

		return remoteContents{ok: true, result: `<?php $copied = "Copied"; `, headers: []string{"http/1.1 200 OK"}}, nil
	}

	file := filepath.Join(t.TempDir(), "copy")

	ok, err := r.Copy("http://example.org", "file://"+thisFile(t), file, true, php.ArrayOf("retry-auth-failure", true))
	if err != nil || !ok || counter != 2 || helper.prompts != 1 {
		t.Fatalf("got %v %v after %d calls", ok, err, counter)
	}

	if data, _ := os.ReadFile(file); !strings.Contains(string(data), "Copied") {
		t.Fatal("expected the copied file")
	}
}

func TestRemoteFilesystem_RedirectToDisallowedSchemeIsRejected(t *testing.T) {
	for _, location := range []string{"file://localhost/etc/passwd", "phar://archive.phar/file", "data://text/plain;base64,Zm9v"} {
		r := newTestRFS(t, nil, nil, nil)
		r.getRemoteContents = func(string, string, *php.Array, int64) (remoteContents, error) {
			return remoteContents{ok: true, headers: []string{"HTTP/1.1 302 Found", "Location: " + location}}, nil
		}

		_, err := r.GetContents("http://example.org", "http://example.org/packages.json", false, nil)
		if err == nil || !strings.Contains(err.Error(), "only http and https redirects are supported") {
			t.Fatalf("%s: got %v", location, err)
		}
	}
}

func TestRemoteFilesystem_GetOptionsForUrlCreatesSecureTlsDefaults(t *testing.T) {
	cafile := writeTestCA(t)
	res := callGetOptionsForUrl(t, newFakeIO(), "example.org", php.ArrayOf("ssl", php.ArrayOf("cafile", cafile)), nil, "http://www.example.org")

	ciphers, _ := optionString(res, "ssl", "ciphers")
	if !strings.Contains(ciphers, "!aNULL:!eNULL:!EXPORT:!DES:!3DES:!RC4:!MD5:!PSK:!aECDH:!EDH-DSS-DES-CBC3-SHA:!EDH-RSA-DES-CBC3-SHA:!KRB5-DES-CBC3-SHA") {
		t.Fatal(ciphers)
	}

	for key, want := range map[string]any{"verify_peer": true, "SNI_enabled": true, "verify_depth": 7, "cafile": cafile, "disable_compression": true} {
		if v, _ := path(res, "ssl", key); !php.LooseEquals(v, want) {
			t.Errorf("%s = %v", key, v)
		}
	}
}

// TestRemoteFilesystem_HTTP replaces testBitBucketPublicDownload, which
// downloads from bitbucket.org, with a local server.
func TestRemoteFilesystem_HTTP(t *testing.T) {
	var gotHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()

		switch r.URL.Path {
		case "/redirect":
			w.Header().Set("Location", "/file.txt")
			w.WriteHeader(http.StatusFound)
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("not here"))
		default:
			_, _ = w.Write([]byte("1234"))
		}
	}))
	defer srv.Close()

	r := newTestRFS(t, nil, nil, nil)

	contents, err := r.GetContents("127.0.0.1", srv.URL+"/file.txt", false, nil)
	if err != nil || contents != "1234" {
		t.Fatalf("got %q %v", contents, err)
	}

	if gotHeaders.Get("Accept-Encoding") != "gzip" || !strings.HasPrefix(gotHeaders.Get("User-Agent"), "Composer/") {
		t.Fatalf("headers %v", gotHeaders)
	}

	if code, _ := FindStatusCode(r.LastHeaders()); code != 200 {
		t.Fatalf("last headers %q", r.LastHeaders())
	}

	contents, err = r.GetContents("127.0.0.1", srv.URL+"/redirect", false, nil)
	if err != nil || contents != "1234" {
		t.Fatalf("got %q %v", contents, err)
	}

	_, err = r.GetContents("127.0.0.1", srv.URL+"/missing", false, nil)
	if te, ok := errors.AsType[*util.TransportError](err); !ok || te.Message != `The "`+srv.URL+`/missing" file could not be downloaded (HTTP/1.1 404 Not Found)` || authString(te.Response) != "not here" {
		t.Fatalf("got %v", err)
	}
}

func TestHttpDownloader_AllowSelfSignedUsesRemoteFilesystem(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("self-signed"))
	}))
	defer srv.Close()

	h, _ := newTestDownloader(t, nil, "")

	r, err := h.Get(srv.URL, php.ArrayOf("ssl", php.ArrayOf("allow_self_signed", true, "verify_peer_name", false)))
	if err != nil || r.Body() != "self-signed" || r.StatusCode() != 200 {
		t.Fatalf("got %v %v", r, err)
	}
}
