package http

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
)

func newPrefetchDownloader(t *testing.T) (*HttpDownloader, *io.BufferIO) {
	t.Helper()
	clearProxyEnv(t)
	t.Setenv("COMPOSER_DISABLE_NETWORK", "")
	t.Setenv("COMPOSER_IPRESOLVE", "")

	b, err := io.NewBufferIO("", console.VerbosityDebug, nil)
	if err != nil {
		t.Fatal(err)
	}
	config := newFakeConfig(map[string]any{"github-domains": list(), "gitlab-domains": list(), "secure-http": false})
	h, err := NewHttpDownloader(b, config, php.NewArray(), false, NewStaticRuntime("8.3.0", "test"))
	if err != nil {
		t.Fatal(err)
	}
	h.EnableAsync() // as a Loop does

	return h, b
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A prefetched request is made once, silently; the request made later
// takes its response and prints what it prints without a prefetch.
func TestHttpDownloader_PrefetchIsTakenByTheSameRequest(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("If-Modified-Since") != "" {
			w.WriteHeader(http.StatusNotModified)

			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	h, b := newPrefetchDownloader(t)
	opts := php.ArrayOf("http", php.ArrayOf("header", php.ListOf("If-Modified-Since: Thu, 21 May 2026 08:42:02 GMT")))

	// the CA bundle is checked (and logged) once, by a request Composer
	// makes: a prefetch before that would log it, so it is not made
	ResetCaBundle()
	start := len(b.Output())
	h.Prefetch(srv.URL+"/p2/a/b.json", opts)
	time.Sleep(50 * time.Millisecond)
	if n := calls.Load(); n != 0 || b.Output()[start:] != "" {
		t.Fatalf("prefetched before the CA check: %d requests, output %q", n, b.Output()[start:])
	}
	// a TLS request would have validated the default options' cafile
	ssl, _ := arrayValue(h.options, "ssl").(*php.Array)
	cafile, _ := optionString(ssl, "cafile")
	ValidateCaFile(cafile, nil)
	mark := len(b.Output())

	h.Prefetch(srv.URL+"/p2/a/b.json", opts)
	waitFor(t, func() bool { return calls.Load() == 1 })
	if out := b.Output()[mark:]; out != "" {
		t.Fatalf("the prefetch printed %q", out)
	}

	r, err := h.Get(srv.URL+"/p2/a/b.json", opts)
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode() != 304 {
		t.Errorf("status %d, want 304", r.StatusCode())
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("%d requests reached the server, want 1", n)
	}
	if out := b.Output()[mark:]; !strings.Contains(out, "Downloading "+srv.URL+"/p2/a/b.json if modified") || !strings.Contains(out, "[304] "+srv.URL+"/p2/a/b.json") {
		t.Errorf("output %q lacks the request's lines", out)
	}

	// taken once: the next identical request goes to the network
	if _, err := h.Get(srv.URL+"/p2/a/b.json", opts); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("%d requests reached the server, want 2", n)
	}
}

// A request that differs in any way from the prefetched one does not take
// its response.
func TestHttpDownloader_PrefetchOnlyMatchesTheSameRequest(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	h, _ := newPrefetchDownloader(t)
	ssl, _ := arrayValue(h.options, "ssl").(*php.Array)
	cafile, _ := optionString(ssl, "cafile")
	ValidateCaFile(cafile, nil)
	h.Prefetch(srv.URL+"/p2/a/b.json", php.ArrayOf("http", php.ArrayOf("header", php.ListOf("If-Modified-Since: Thu, 21 May 2026 08:42:02 GMT"))))
	waitFor(t, func() bool { return calls.Load() == 1 })

	if _, err := h.Get(srv.URL+"/p2/a/b.json", php.ArrayOf("http", php.ArrayOf("header", php.ListOf("If-Modified-Since: Fri, 22 May 2026 08:42:02 GMT")))); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("%d requests reached the server, want 2", n)
	}
}

// PrefetchResponse gives the speculative reader the response the later
// request takes; urgent transfers start before the others, and a request
// taking a transfer still queued makes it at once.
func TestHttpDownloader_PrefetchQueue(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	var order []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		order = append(order, r.URL.Path)
		mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/slow/") {
			<-release
		}
		_, _ = w.Write([]byte(`{"path":"` + r.URL.Path + `"}`))
	}))
	defer srv.Close()
	defer close(release)

	defer func(n int) { maxPrefetches = n }(maxPrefetches)
	maxPrefetches = 2 // below the connections per host

	h, _ := newPrefetchDownloader(t)
	ssl, _ := arrayValue(h.options, "ssl").(*php.Array)
	cafile, _ := optionString(ssl, "cafile")
	ValidateCaFile(cafile, nil)

	// every slot taken by a transfer the server holds
	for i := range maxPrefetches {
		h.Prefetch(srv.URL+"/slow/"+strconv.Itoa(i), nil)
	}
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()

		return len(order) == maxPrefetches
	})

	h.Prefetch(srv.URL+"/later", nil)
	response := h.PrefetchResponse(srv.URL+"/urgent", nil)
	queued := h.PrefetchResponse(srv.URL+"/queued", nil)

	// taken while queued: made at once, its reader sees the response
	r, err := h.Get(srv.URL+"/queued", nil)
	if err != nil {
		t.Fatal(err)
	}
	if status, body, ok := queued(); !ok || status != 200 || body != r.Body() || body != `{"path":"/queued"}` {
		t.Errorf("queued response: %d %q %v", status, body, ok)
	}

	// a slot frees: the urgent transfer goes before the earlier other one
	release <- struct{}{}
	if status, body, ok := response(); !ok || status != 200 || body != `{"path":"/urgent"}` {
		t.Errorf("urgent response: %d %q %v", status, body, ok)
	}
	mu.Lock()
	tail := slices.Clone(order[maxPrefetches:])
	mu.Unlock()
	if len(tail) < 2 || tail[0] != "/queued" || tail[1] != "/urgent" {
		t.Errorf("order after the slow transfers: %v", tail)
	}
}
