package http

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// queueWaiters starts n transfers waiting for the first connection, one
// after the other, and returns the channel each sends its index on when
// it goes ahead, with its sent function.
func queueWaiters(t *testing.T, p *transportPool, u *url.URL, n int) chan struct {
	i    int
	sent func()
} {
	t.Helper()

	released := make(chan struct {
		i    int
		sent func()
	}, n)

	for i := range n {
		go func() {
			_, sent, _ := p.awaitFirstConn(context.Background(), transportKey{}, u)
			released <- struct {
				i    int
				sent func()
			}{i, sent}
		}()

		// wait for it to be queued, so that the order is known
		for deadline := time.Now().Add(5 * time.Second); ; {
			p.mu.Lock()
			queued := len(p.first[firstConnKey{addr: "example.test:443"}].waiters)
			p.mu.Unlock()

			if queued == i+1 {
				break
			}

			if time.Now().After(deadline) {
				t.Fatalf("waiter %d not queued", i)
			}

			time.Sleep(time.Millisecond)
		}
	}

	return released
}

func TestAwaitFirstConn_HTTP2ReleasesInOrder(t *testing.T) {
	var p transportPool

	u, _ := url.Parse("https://example.test/")
	connected, _, _ := p.awaitFirstConn(context.Background(), transportKey{}, u)
	released := queueWaiters(t, &p, u, 3)

	select {
	case w := <-released:
		t.Fatalf("waiter %d went ahead before the first connection", w.i)
	case <-time.After(20 * time.Millisecond):
	}

	connected(true)

	for want := range 3 {
		w := <-released
		if w.i != want {
			t.Fatalf("got waiter %d, want %d", w.i, want)
		}

		select {
		case next := <-released:
			t.Fatalf("waiter %d went ahead before waiter %d sent its request", next.i, w.i)
		case <-time.After(10 * time.Millisecond):
		}

		w.sent()
		w.sent() // only the first call counts
	}

	// later transfers go ahead at once
	if _, sent, done := p.awaitFirstConn(context.Background(), transportKey{}, u); sent == nil || done == nil {
		t.Fatal("no functions for a later transfer")
	}
}

func TestAwaitFirstConn_FailureOrHTTP1ReleasesAll(t *testing.T) {
	for _, h2 := range []bool{false, true} {
		var p transportPool

		u, _ := url.Parse("https://example.test/")
		connected, _, done := p.awaitFirstConn(context.Background(), transportKey{}, u)
		released := queueWaiters(t, &p, u, 3)

		if h2 {
			done() // the first transfer failed
		} else {
			connected(false)
		}

		for range 3 {
			select {
			case <-released:
			case <-time.After(5 * time.Second):
				t.Fatalf("h2=%v: waiters not released together", h2)
			}
		}
	}
}

func TestAwaitFirstConn_NotForHTTPProxyOrHTTP1(t *testing.T) {
	var p transportPool

	for _, tc := range []struct {
		url string
		key transportKey
	}{
		{"http://example.test/", transportKey{}},
		{"https://example.test/", transportKey{proxy: "http://proxy.test:3128"}},
		{"https://example.test/", transportKey{http1: true}},
		{"https://example.test/", transportKey{fresh: true}},
	} {
		u, _ := url.Parse(tc.url)
		p.awaitFirstConn(context.Background(), tc.key, u)

		// a second transfer would wait if the first counted
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		start := time.Now()
		p.awaitFirstConn(ctx, tc.key, u)
		cancel()

		if time.Since(start) > 500*time.Millisecond {
			t.Errorf("%s %+v: the second transfer waited", tc.url, tc.key)
		}
	}
}

// TestTransportPool_ConcurrentTransfersShareOneHTTP2Connection: transfers
// started together to an HTTP/2 server open one connection, not one each.
func TestTransportPool_ConcurrentTransfersShareOneHTTP2Connection(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))

	var conns atomic.Int32

	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns.Add(1)
		}
	}
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)

	var (
		p  transportPool
		wg sync.WaitGroup
	)

	for range 20 {
		wg.Go(func() {
			res := p.do(context.Background(), &transferRequest{url: srv.URL + "/", connectTimeout: 5 * time.Second, key: transportKey{tls: tlsSettings{verifyPeer: false}}})
			if res.status != http.StatusNotModified {
				t.Errorf("got %d %d %q", res.status, res.errno, res.errMsg)
			}
		})
	}

	wg.Wait()

	if n := conns.Load(); n != 1 {
		t.Errorf("%d connections, want 1", n)
	}
}
