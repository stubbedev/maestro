package http

import (
	"context"
	"net"
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
	ssl, _ := h.options.At("ssl").(*php.Array)
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
	ssl, _ := h.options.At("ssl").(*php.Array)
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

	h, _ := newPrefetchDownloader(t)
	const limit = 2 // below the connections per host
	h.curl.ahead.limit = limit
	h.curl.ahead.oneLane = true
	ssl, _ := h.options.At("ssl").(*php.Array)
	cafile, _ := optionString(ssl, "cafile")
	ValidateCaFile(cafile, nil)

	// every slot taken by a transfer the server holds
	for i := range limit {
		h.Prefetch(srv.URL+"/slow/"+strconv.Itoa(i), nil)
	}
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()

		return len(order) == limit
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
	tail := slices.Clone(order[limit:])
	mu.Unlock()
	if len(tail) < 2 || tail[0] != "/queued" || tail[1] != "/urgent" {
		t.Errorf("order after the slow transfers: %v", tail)
	}
}

// Transfers started ahead beyond one connection's streams go out at once
// over a second HTTP/2 connection (a lane), not a round trip later.
func TestTransportPool_PrefetchesBeyondALaneOpenASecondConnection(t *testing.T) {
	const limit = 3
	var (
		conns    atomic.Int32
		mu       sync.Mutex
		inFlight int
		most     int
		all      = make(chan struct{})
	)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		inFlight++
		most = max(most, inFlight)
		if inFlight == prefetchLanes*limit {
			close(all)
		}
		mu.Unlock()
		// held until every transfer is in flight at once
		select {
		case <-all:
		case <-time.After(2 * time.Second):
		}
		mu.Lock()
		inFlight--
		mu.Unlock()
		w.WriteHeader(http.StatusNotModified)
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns.Add(1)
		}
	}
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)

	p := &transportPool{}
	a := &prefetches{limit: limit}
	var transfers []*prefetchedTransfer
	for i := range prefetchLanes * limit {
		transfers = append(transfers, a.prefetch(p, &transferRequest{url: srv.URL + "/" + strconv.Itoa(i), connectTimeout: 5 * time.Second, key: transportKey{tls: tlsSettings{verifyPeer: false}}}, false))
	}
	for _, tr := range transfers {
		if status, _, ok := tr.response(); !ok || status != http.StatusNotModified {
			t.Fatalf("status %d ok %v", status, ok)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if most != prefetchLanes*limit {
		t.Errorf("%d transfers in flight at once, want %d", most, prefetchLanes*limit)
	}
	if n := conns.Load(); n != prefetchLanes {
		t.Errorf("%d connections, want %d", n, prefetchLanes)
	}
}

// A downloader created after another (a second Composer instance) makes
// its requests over the connection the first one opened.
func TestHttpDownloader_DownloadersShareConnections(t *testing.T) {
	var conns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)

	for range 2 {
		h, _ := newPrefetchDownloader(t)
		if _, err := h.Get(srv.URL+"/packages.json", nil); err != nil {
			t.Fatal(err)
		}
	}
	if n := conns.Load(); n != 1 {
		t.Errorf("%d connections, want 1", n)
	}
}

// A lane opens no connection of its own to a server that answered the
// first connection with HTTP/1, nor for a URL that waits for no first
// connection.
func TestTransportPool_NoLaneForHTTP1(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	t.Cleanup(srv.Close)

	p := &transportPool{}
	r := &transferRequest{url: srv.URL + "/", connectTimeout: 5 * time.Second, key: transportKey{tls: tlsSettings{verifyPeer: false}}}
	if !p.mayOpenLane(r) {
		t.Error("no lane before the first connection settled")
	}
	if res := p.do(context.Background(), r); res.status != http.StatusNotModified {
		t.Fatalf("got %d %q", res.status, res.fail.Message)
	}
	if p.mayOpenLane(r) {
		t.Error("a lane after an HTTP/1 first connection")
	}
	if p.mayOpenLane(&transferRequest{url: "http://example.org/"}) {
		t.Error("a lane for plain http")
	}
}
