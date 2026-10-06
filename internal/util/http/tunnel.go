// https through an HTTP proxy: the CONNECT tunnel, opened here rather than
// by net/http so that the proxy's response head is kept (curl reports it
// among the response headers of the transfer that opened the tunnel) and
// its failures are reported as curl reports them.

package http

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/url"
	"strconv"
	"time"
)

// tunnelError is a CONNECT exchange that failed, with curl's errno and
// message.
type tunnelError struct {
	errno int
	msg   string
}

func (e *tunnelError) Error() string { return e.msg }

// proxyConnectTimeout is how long net/http waits for a CONNECT response;
// kept for the tunnels opened here.
const proxyConnectTimeout = time.Minute

// canonicalProxyAddr is the address of a proxy URL with its default port.
func canonicalProxyAddr(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}

	return net.JoinHostPort(u.Hostname(), port)
}

// proxyAuthorization is the Proxy-Authorization value for a proxy URL's
// credentials (curl's CURLOPT_PROXYUSERPWD, basic), else header.
func proxyAuthorization(u *url.URL, header string) string {
	if u.User != nil {
		pass, _ := u.User.Password()

		return "Basic " + base64.StdEncoding.EncodeToString([]byte(u.User.Username()+":"+pass))
	}

	return header
}

// dialTunnel opens a TLS connection to addr through the proxy: TCP (and
// TLS for an https proxy) to the proxy, CONNECT as curl sends it, then the
// TLS handshake with the target. It returns the connection and the
// proxy's response head as received.
func dialTunnel(ctx context.Context, dialer *net.Dialer, network string, proxy *url.URL, authHeader, addr string, base *tls.Config, handshakeTimeout time.Duration) (*tls.Conn, []byte, error) {
	proxyAddr := canonicalProxyAddr(proxy)

	conn, err := dialer.DialContext(ctx, network, proxyAddr)
	if err != nil {
		return nil, nil, err
	}

	if proxy.Scheme == "https" {
		cfg := base.Clone()
		cfg.NextProtos = nil

		tc, err := tlsHandshake(ctx, conn, proxyAddr, cfg, handshakeTimeout)
		if err != nil {
			return nil, nil, err
		}

		conn = tc
	}

	head, rest, err := connectTunnel(ctx, conn, proxy, authHeader, addr)
	if err != nil {
		_ = conn.Close()

		return nil, nil, err
	}

	tc, err := tlsHandshake(ctx, rest, addr, base, handshakeTimeout)
	if err != nil {
		return nil, nil, err
	}

	return tc, head, nil
}

// connectTunnel sends the CONNECT request and reads the response head,
// which it returns as received; rest is the connection to continue on.
// Failures are curl's (lib/cf-h1-proxy.c): a closed connection is
// "Proxy CONNECT aborted" (56), any status but 2xx "CONNECT tunnel
// failed, response N" (7, N 0 for an unreadable status line).
func connectTunnel(ctx context.Context, conn net.Conn, proxy *url.URL, authHeader, addr string) ([]byte, net.Conn, error) {
	req := "CONNECT " + addr + " HTTP/1.1\r\nHost: " + addr + "\r\n"
	if auth := proxyAuthorization(proxy, authHeader); auth != "" {
		req += "Proxy-Authorization: " + auth + "\r\n"
	}

	req += "Proxy-Connection: Keep-Alive\r\n\r\n"

	deadline := time.Now().Add(proxyConnectTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	_ = conn.SetDeadline(deadline)

	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()

	if _, err := io.WriteString(conn, req); err != nil {
		return nil, nil, ctxErr(ctx, err)
	}

	br := bufio.NewReader(conn)

	var head []byte

	for {
		line, err := br.ReadSlice('\n')
		head = append(head, line...)

		if err != nil {
			if errors.Is(err, bufio.ErrBufferFull) && len(head) < maxRecordedHead {
				continue
			}

			if err = ctxErr(ctx, err); errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return nil, nil, err
			}

			return nil, nil, &tunnelError{curleRecvError, "Proxy CONNECT aborted"}
		}

		if t := len(line); (t == 1 || t == 2 && line[0] == '\r') && len(head) > t {
			break
		}
	}

	_ = conn.SetDeadline(time.Time{})

	if code := statusCode(head); code < 200 || code > 299 {
		return nil, nil, &tunnelError{curleCouldntConnect, "CONNECT tunnel failed, response " + strconv.Itoa(code)}
	}

	if br.Buffered() > 0 {
		// bytes the proxy sent after its head belong to the tunnel
		buffered, _ := br.Peek(br.Buffered())

		return head, &prefixConn{Conn: conn, prefix: append([]byte(nil), buffered...)}, nil
	}

	return head, conn, nil
}

// ctxErr is the context's error when it ended, else err.
func ctxErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	return err
}

// prefixConn reads prefix before the connection.
type prefixConn struct {
	net.Conn

	prefix []byte
}

func (c *prefixConn) Read(b []byte) (int, error) {
	if len(c.prefix) > 0 {
		n := copy(b, c.prefix)
		c.prefix = c.prefix[n:]

		return n, nil
	}

	return c.Conn.Read(b)
}
