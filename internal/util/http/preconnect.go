// Ports nothing: connections opened ahead of the requests that use them
// (deliberate deviation 3, speed).

package http

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// preconnectMaxAge is how long a connection opened ahead stays usable: the
// first transfer to its host takes it only while the server cannot have
// dropped it as idle (a request on a dropped connection would fail where
// one on a new connection succeeds).
const preconnectMaxAge = 3 * time.Second

// preKey names the connections a transport may take: its TLS settings and
// IP version, and the address dialed.
type preKey struct {
	tls       tlsSettings
	ipResolve int
	http1     bool
	addr      string
}

// preconn is a connection being opened ahead.
type preconn struct {
	done    chan struct{}
	conn    net.Conn
	err     error
	settled time.Time
}

// Preconnect opens a connection (name resolution, TCP and TLS handshakes)
// to the host of an https URL in the background, as a request for it with
// these options would, without sending anything on it: the first request
// to that host then skips the round trips of opening one. It does nothing
// for other URLs, behind a proxy, with the network disabled, or when the
// options restrict where requests may go.
func (h *HttpDownloader) Preconnect(url string, options *php.Array) {
	if h == nil || h.disabled || !strings.HasPrefix(url, "https://") {
		return
	}

	h.mu.Lock()
	merged := h.options
	h.mu.Unlock()

	if options != nil {
		merged = php.ArrayReplaceRecursive(merged, options)
	}

	if _, ok := merged.Get("prevent_url_access_callable"); ok {
		return
	}

	if _, ok := merged.Get("prevent_ip_access_callable"); ok {
		return
	}

	proxy, err := GetProxyManager().ProxyForRequest(url)
	if err != nil {
		return
	}

	ssl, _ := arrayValue(merged, "ssl").(*php.Array)
	if proxyOptions := proxy.CurlOptions(ssl); proxyOptions.Proxy != "" {
		return
	}

	// the address net/http dials (canonicalAddr) for an ASCII host
	u, err := neturl.Parse(url)
	if err != nil || u.Hostname() == "" {
		return
	}

	port := u.Port()
	if port == "" {
		port = "443"
	}

	ipResolve := 0

	switch v, _ := util.GetEnv("COMPOSER_IPRESOLVE"); v {
	case "4":
		ipResolve = 4
	case "6":
		ipResolve = 6
	}

	key := transportKey{tls: tlsFromOptions(ssl, false), ipResolve: ipResolve}
	h.curl.pool.preconnect(key, net.JoinHostPort(u.Hostname(), port), 10*time.Second)
}

// preconnect opens a connection for the key's transport to addr in the
// background, once per key and address.
func (p *transportPool) preconnect(key transportKey, addr string, connectTimeout time.Duration) {
	client, failure := p.client(key, connectTimeout)
	if failure != nil {
		return
	}

	t, ok := client.Transport.(*http.Transport)
	if !ok || t.DialTLSContext == nil {
		return
	}

	pk := preKey{tls: key.tls, ipResolve: key.ipResolve, http1: key.http1, addr: addr}

	p.mu.Lock()
	if p.pre == nil {
		p.pre = map[preKey]*preconn{}
	}
	if _, ok := p.pre[pk]; ok {
		p.mu.Unlock()

		return
	}
	pc := &preconn{done: make(chan struct{})}
	p.pre[pk] = pc
	p.mu.Unlock()

	cfg := t.TLSClientConfig.Clone()
	if key.http1 {
		cfg.NextProtos = nil
	} else {
		// what net/http's HTTP/2 set-up gives the transport's TLS config
		cfg.NextProtos = []string{"h2", "http/1.1"}
	}

	go func() {
		defer close(pc.done)

		pc.conn, pc.err = dialTLS(context.Background(), newDialer(connectTimeout), dialNetwork(key), addr, cfg, connectTimeout)
		pc.settled = time.Now()
	}()
}

// takePreconnected returns the connection opened ahead for the key and
// address, if it opened and is fresh enough; it is handed out once.
func (p *transportPool) takePreconnected(ctx context.Context, key transportKey, addr string) net.Conn {
	pk := preKey{tls: key.tls, ipResolve: key.ipResolve, http1: key.http1, addr: addr}

	p.mu.Lock()
	pc, ok := p.pre[pk]
	if ok {
		// handed out once; later dials open their own
		p.pre[pk] = &preconn{done: closedChan, err: errPreconnTaken}
	}
	p.mu.Unlock()

	if !ok {
		return nil
	}

	select {
	case <-pc.done:
	case <-ctx.Done():
		go func() {
			<-pc.done
			if pc.conn != nil {
				_ = pc.conn.Close()
			}
		}()

		return nil
	}

	if pc.err != nil {
		return nil
	}

	if time.Since(pc.settled) > preconnectMaxAge {
		_ = pc.conn.Close()

		return nil
	}

	return pc.conn
}

var (
	closedChan      = func() chan struct{} { c := make(chan struct{}); close(c); return c }()
	errPreconnTaken = &net.OpError{Op: "preconnect"}
)

// newDialer is the dialer of a transport.
func newDialer(connectTimeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: connectTimeout, KeepAlive: 60 * time.Second}
}

// dialNetwork is the network a key's transport dials.
func dialNetwork(key transportKey) string {
	switch key.ipResolve {
	case 4:
		return "tcp4"
	case 6:
		return "tcp6"
	}

	return "tcp"
}

// tlsHandshakeTimeoutError is net/http's error for a TLS handshake that
// timed out (same message and behaviour).
type tlsHandshakeTimeoutError struct{}

func (tlsHandshakeTimeoutError) Timeout() bool   { return true }
func (tlsHandshakeTimeoutError) Temporary() bool { return true }
func (tlsHandshakeTimeoutError) Error() string   { return "net/http: TLS handshake timeout" }

// dialTLS opens a TLS connection the way net/http's Transport does for a
// direct https request (persistConn.addTLS): dial, then the handshake with
// the transport's TLS config, the server name being the host of addr, under
// the TLS handshake timeout.
func dialTLS(ctx context.Context, dialer *net.Dialer, network, addr string, base *tls.Config, handshakeTimeout time.Duration) (*tls.Conn, error) {
	plain, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}

	return tlsHandshake(ctx, plain, addr, base, handshakeTimeout)
}

// tlsHandshake runs the TLS handshake on a connection to addr as net/http
// does (persistConn.addTLS); the connection is closed when it fails.
func tlsHandshake(ctx context.Context, plain net.Conn, addr string, base *tls.Config, handshakeTimeout time.Duration) (*tls.Conn, error) {
	cfg := base.Clone()
	if cfg == nil {
		cfg = &tls.Config{}
	}

	if cfg.ServerName == "" {
		// connectMethod.tlsHost(): the address without its port
		name := addr
		if i := strings.LastIndex(name, ":"); i > strings.LastIndex(name, "]") {
			name = name[:i]
		}

		cfg.ServerName = name
	}

	tlsConn := tls.Client(plain, cfg)
	errc := make(chan error, 2)

	var timer *time.Timer
	if handshakeTimeout != 0 {
		timer = time.AfterFunc(handshakeTimeout, func() { errc <- tlsHandshakeTimeoutError{} })
	}

	go func() {
		err := tlsConn.HandshakeContext(ctx)
		if timer != nil {
			timer.Stop()
		}
		errc <- err
	}()

	if err := <-errc; err != nil {
		_ = plain.Close()
		if err == (tlsHandshakeTimeoutError{}) { //nolint:errorlint // net/http compares it so
			<-errc
		}

		return nil, err
	}

	return tlsConn, nil
}
