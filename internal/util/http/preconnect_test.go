package http

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestTransportPool_PreconnectRacesTransfer: a connection opened ahead
// while transfers to the same host start on the transport (net/http
// setting HTTP/2 up on its first use) shares no TLS configuration with
// them (issue #105, caught under -race), and the transfers still take
// that connection: one connection, over HTTP/2.
func TestTransportPool_PreconnectRacesTransfer(t *testing.T) {
	for range 10 {
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ProtoMajor != 2 {
				w.WriteHeader(http.StatusHTTPVersionNotSupported)

				return
			}

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

		u, _ := url.Parse(srv.URL)
		key := transportKey{tls: tlsSettings{verifyPeer: false}}

		var (
			p  transportPool
			wg sync.WaitGroup
		)

		p.preconnect(key, u.Host, 5*time.Second)

		for range 4 {
			wg.Go(func() {
				res := p.do(context.Background(), &transferRequest{url: srv.URL + "/", connectTimeout: 5 * time.Second, key: key})
				if res.status != http.StatusNotModified {
					t.Errorf("got %d %d %q", res.status, res.fail.Errno, res.fail.Message)
				}
			})
		}

		wg.Wait()
		srv.Close()

		if n := conns.Load(); n != 1 {
			t.Fatalf("%d connections, want 1", n)
		}
	}
}

// TestTransportPool_TransportSetUpBeforeShared: a pooled transport comes
// out with net/http's HTTP/2 set-up done, and the TLS configuration its
// connections are opened with is a snapshot of it, not the transport's
// own (which net/http owns).
func TestTransportPool_TransportSetUpBeforeShared(t *testing.T) {
	for _, tc := range []struct {
		http1 bool
		want  []string
	}{
		{false, []string{"h2", "http/1.1"}},
		{true, nil},
	} {
		var p transportPool

		pt, failure := p.transport(transportKey{http1: tc.http1}, time.Second)
		if failure != nil {
			t.Fatalf("http1 %v: %+v", tc.http1, failure)
		}

		tr, _ := pt.client.Transport.(*http.Transport)
		if tr.TLSClientConfig == pt.tls {
			t.Errorf("http1 %v: the snapshot is the transport's own config", tc.http1)
		}

		if !slices.Equal(tr.TLSClientConfig.NextProtos, tc.want) || !slices.Equal(pt.tls.NextProtos, tc.want) {
			t.Errorf("http1 %v: NextProtos %q (snapshot %q), want %q", tc.http1, tr.TLSClientConfig.NextProtos, pt.tls.NextProtos, tc.want)
		}
	}
}
