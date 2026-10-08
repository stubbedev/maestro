package http

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// corked reports whether conn's socket holds its writes back (TCP_CORK).
func corked(t *testing.T, conn *net.TCPConn) bool {
	t.Helper()

	raw, err := conn.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}

	var v int
	var serr error
	if err := raw.Control(func(fd uintptr) { v, serr = unix.GetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_CORK) }); err != nil || serr != nil {
		t.Fatal(err, serr)
	}

	return v != 0
}

func tcpPair(t *testing.T) *net.TCPConn {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		accepted <- c
	}()

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server := <-accepted
	t.Cleanup(func() { _ = c.Close(); _ = server.Close() })

	return c.(*net.TCPConn)
}

// TestAwaitFirstConn_HoldsWritesOfTheReleasedRequests: the requests that
// waited for the first HTTP/2 connection are written while the socket
// holds small writes back, which ends once the last of them was sent.
func TestAwaitFirstConn_HoldsWritesOfTheReleasedRequests(t *testing.T) {
	tcp := tcpPair(t)
	h2 := newH2HeadConn(tls.Client(tcp, &tls.Config{}), nil)

	var p transportPool

	u, _ := url.Parse("https://example.test/")
	connected, _, _ := p.awaitFirstConn(context.Background(), transportKey{}, u)
	released := queueWaiters(t, &p, u, 3)

	if corked(t, tcp) {
		t.Fatal("writes held back before the connection")
	}

	connected(h2)

	for i := range 3 {
		w := <-released
		if !corked(t, tcp) {
			t.Fatalf("writes not held back while waiter %d sends", i)
		}
		w.sent()
	}

	for deadline := time.Now().Add(maxWriteHold / 2); corked(t, tcp); {
		if time.Now().After(deadline) {
			t.Fatal("writes still held back after the last waiter sent")
		}
		time.Sleep(100 * time.Microsecond)
	}
}

// TestHoldWrites_Bounded: writes are held back for maxWriteHold at most.
func TestHoldWrites_Bounded(t *testing.T) {
	tcp := tcpPair(t)

	flush := holdWrites(tcp)
	defer flush()

	if !corked(t, tcp) {
		t.Fatal("writes not held back")
	}

	for deadline := time.Now().Add(maxWriteHold * 10); corked(t, tcp); {
		if time.Now().After(deadline) {
			t.Fatal("writes held back past maxWriteHold")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestHoldWrites_SharedByAnHTTP2Connection: the holds on one HTTP/2
// connection's socket keep it held back until the last is released.
func TestHoldWrites_SharedByAnHTTP2Connection(t *testing.T) {
	tcp := tcpPair(t)
	h2 := newH2HeadConn(tls.Client(tcp, &tls.Config{}), nil)

	first := holdWrites(h2)
	second := holdWrites(h2)
	first()
	first()
	if !corked(t, tcp) {
		t.Fatal("writes no longer held back while a hold remains")
	}
	second()
	if corked(t, tcp) {
		t.Fatal("writes held back after the last hold was released")
	}
}

// TestTransportPool_BurstHoldsWritesOnAnOpenConnection: a burst transfer
// over an HTTP/2 connection already open holds its writes back until
// burstLinger after its request was written, so that the requests
// started with it share its segments; a transfer that is not part of a
// burst does not.
func TestTransportPool_BurstHoldsWritesOnAnOpenConnection(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)

	var p transportPool
	key := transportKey{tls: tlsSettings{verifyPeer: false}}
	if res := p.do(context.Background(), &transferRequest{url: srv.URL + "/open", connectTimeout: 5 * time.Second, key: key}); res.status != http.StatusNotModified {
		t.Fatalf("got %d %q", res.status, res.fail.Message)
	}

	for _, burst := range []bool{false, true} {
		var conn *net.TCPConn
		wrote := make(chan bool, 1)
		ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{
			GotConn: func(info httptrace.GotConnInfo) { conn = tcpConnOf(info.Conn) },
			WroteHeaders: func() {
				// after the transport's own hook: the hold lingers
				v := -1
				if raw, err := conn.SyscallConn(); err == nil {
					_ = raw.Control(func(fd uintptr) { v, _ = unix.GetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_CORK) })
				}
				wrote <- v == 1
			},
		})
		if res := p.do(ctx, &transferRequest{url: srv.URL + "/", connectTimeout: 5 * time.Second, key: key, burst: burst}); res.status != http.StatusNotModified {
			t.Fatalf("got %d %q", res.status, res.fail.Message)
		}
		if held := <-wrote; held != burst {
			t.Errorf("burst %v: writes held back after the request was written: %v", burst, held)
		}
	}
}

// TestHoldWrites_NotTCP: a connection without a TCP socket is left alone.
func TestHoldWrites_NotTCP(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	holdWrites(a)()
	holdWrites(newH2HeadConn(nil, nil))()
}
