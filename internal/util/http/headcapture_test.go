package http

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

// rawResponses are canned HTTP/1 responses by path, as bytes on the wire.
var rawResponses = map[string]string{
	"/order":    "HTTP/1.1 200 OK\r\nZ-Last: 1\r\ncontent-type: text/plain\r\nX-Multi: a\r\nA-First: 2\r\nx-multi: b\r\nContent-Length: 2\r\n\r\nok",
	"/continue": "HTTP/1.1 100 Continue\r\n\r\nHTTP/1.1 103 Early Hints\r\nLink: </a>; rel=preload\r\n\r\nHTTP/1.1 200 OK\r\nX-B: 1\r\nContent-Length: 2\r\n\r\nok",
	"/lf":       "HTTP/1.1 200 OK\nX-B: 1\nX-A:  spaced  \nContent-Length: 2\n\nok",
	"/fold":     "HTTP/1.1 200 OK\r\nX-Fold: a\r\n  b\r\nContent-Length: 2\r\n\r\nok",
	"/fold2":    "HTTP/1.1 200 OK\r\nX-Fold: a \r\n\t b  \r\n \r\nX-N: 1\r\nContent-Length: 2\r\n\r\nok",
	"/trail":    "HTTP/1.1 200 OK  \r\nX-T: v \t\r\nX-E:\r\nContent-Length: 2\r\n\r\nok",
	"/http10":   "HTTP/1.0 404 Not Found\r\nX-A: 1\r\nContent-Length: 2\r\n\r\nno",
	"/nostatus": "HTTP/1.1 200\r\nX-A: 1\r\nContent-Length: 2\r\n\r\nok",
}

// rawServer serves rawResponses (requests in origin or absolute form) and,
// as a proxy, CONNECT: to tunnel (the address), or failing as the target
// host asks ("deny." 407, "close." no answer, "garbage." no status line).
// tlsConfig makes it an https server.
func rawServer(t *testing.T, tunnel string, tlsConfig *tls.Config) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	if tlsConfig != nil {
		l = tls.NewListener(l, tlsConfig)
	}

	t.Cleanup(func() { _ = l.Close() })

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}

			go serveRaw(c, tunnel)
		}
	}()

	return l.Addr().String()
}

func serveRaw(c net.Conn, tunnel string) {
	defer c.Close()

	br := bufio.NewReader(c)

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}

		for {
			h, err := br.ReadString('\n')
			if err != nil || h == "\r\n" || h == "\n" {
				break
			}
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			return
		}

		if parts[0] == "CONNECT" {
			switch {
			case strings.HasPrefix(parts[1], "deny."):
				_, _ = io.WriteString(c, "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic realm=\"x\"\r\nContent-Length: 0\r\n\r\n")
			case strings.HasPrefix(parts[1], "garbage."):
				_, _ = io.WriteString(c, "garbage\r\n\r\n")
			case strings.HasPrefix(parts[1], "close."):
			default:
				up, err := net.Dial("tcp", tunnel)
				if err != nil {
					return
				}

				_, _ = io.WriteString(c, "HTTP/1.1 200 Connection established\r\nProxy-Agent: raw\r\n\r\n")

				go func() { _, _ = io.Copy(up, br) }()

				_, _ = io.Copy(c, up)
			}

			return
		}

		path := parts[1]
		if _, rest, ok := strings.Cut(path, "://"); ok {
			path = rest[strings.Index(rest, "/"):]
		}

		resp, ok := rawResponses[path]
		if !ok {
			resp = "HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n"
		}

		_, _ = io.WriteString(c, resp)
	}
}

func doTransfer(t *testing.T, r *transferRequest) *transferResult {
	t.Helper()

	var pool transportPool

	res := pool.do(context.Background(), r)
	if res.err != nil {
		t.Fatal(res.err)
	}

	return res
}

// TestHeaderLines_WireOrder checks the header lines of HTTP/1 responses
// against what php-curl (libcurl 8.22) and PHP 8.4's http stream wrapper
// report for the same bytes.
func TestHeaderLines_WireOrder(t *testing.T) {
	addr := rawServer(t, "", nil)

	for _, tc := range []struct {
		path         string
		curl, stream []string
	}{
		{"/order",
			[]string{"HTTP/1.1 200 OK", "Z-Last: 1", "content-type: text/plain", "X-Multi: a", "A-First: 2", "x-multi: b", "Content-Length: 2"},
			[]string{"HTTP/1.1 200 OK", "Z-Last: 1", "content-type: text/plain", "X-Multi: a", "A-First: 2", "x-multi: b", "Content-Length: 2"}},
		// PHP's wrapper takes the 103 head for the response (it skips only
		// 100 Continue); net/http reads past it, so the stream lines are
		// the 200 head's
		{"/continue",
			[]string{"HTTP/1.1 100 Continue", "", "HTTP/1.1 103 Early Hints", "Link: </a>; rel=preload", "", "HTTP/1.1 200 OK", "X-B: 1", "Content-Length: 2"},
			[]string{"HTTP/1.1 200 OK", "X-B: 1", "Content-Length: 2"}},
		{"/lf",
			[]string{"HTTP/1.1 200 OK\nX-B: 1\nX-A:  spaced  \nContent-Length: 2"},
			[]string{"HTTP/1.1 200 OK", "X-B: 1", "X-A:  spaced", "Content-Length: 2"}},
		{"/fold",
			[]string{"HTTP/1.1 200 OK", "X-Fold: a b", "Content-Length: 2"},
			[]string{"HTTP/1.1 200 OK", "X-Fold: a b", "Content-Length: 2"}},
		{"/fold2",
			[]string{"HTTP/1.1 200 OK", "X-Fold: a b ", "X-N: 1", "Content-Length: 2"},
			[]string{"HTTP/1.1 200 OK", "X-Fold: a b", "X-N: 1", "Content-Length: 2"}},
		{"/trail",
			[]string{"HTTP/1.1 200 OK  ", "X-T: v \t", "X-E:", "Content-Length: 2"},
			[]string{"HTTP/1.1 200 OK  ", "X-T: v", "X-E:", "Content-Length: 2"}},
		{"/http10",
			[]string{"HTTP/1.0 404 Not Found", "X-A: 1", "Content-Length: 2"},
			[]string{"HTTP/1.0 404 Not Found", "X-A: 1", "Content-Length: 2"}},
		{"/nostatus",
			[]string{"HTTP/1.1 200", "X-A: 1", "Content-Length: 2"},
			[]string{"HTTP/1.1 200", "X-A: 1", "Content-Length: 2"}},
	} {
		curl := doTransfer(t, &transferRequest{url: "http://" + addr + tc.path, decode: true, curlStatusLines: true})
		if !slices.Equal(curl.headers, tc.curl) {
			t.Errorf("%s curl: got %q, want %q", tc.path, curl.headers, tc.curl)
		}

		stream := doTransfer(t, &transferRequest{url: "http://" + addr + tc.path, key: transportKey{http1: true}})
		if !slices.Equal(stream.headers, tc.stream) {
			t.Errorf("%s stream: got %q, want %q", tc.path, stream.headers, tc.stream)
		}
	}
}

// TestHeaderLines_ConnectTunnel checks https through a proxy: curl reports
// the proxy's CONNECT head before the response's, for the transfer that
// opened the tunnel; the stream wrapper does not.
func TestHeaderLines_ConnectTunnel(t *testing.T) {
	tlsSrv := httptest.NewUnstartedServer(http.NotFoundHandler())
	tlsSrv.StartTLS()
	t.Cleanup(tlsSrv.Close)

	target := rawServer(t, "", tlsSrv.TLS)
	proxy := rawServer(t, target, nil)
	connect := []string{"HTTP/1.1 200 Connection established", "Proxy-Agent: raw", ""}
	order := []string{"HTTP/1.1 200 OK", "Z-Last: 1", "content-type: text/plain", "X-Multi: a", "A-First: 2", "x-multi: b", "Content-Length: 2"}

	var pool transportPool

	key := transportKey{proxy: "http://" + proxy, tls: tlsSettings{verifyPeer: false}}

	for i, want := range [][]string{append(slices.Clone(connect), order...), order} {
		res := pool.do(context.Background(), &transferRequest{url: "https://example.test/order", decode: true, curlStatusLines: true, key: key})
		if res.errno != 0 || !slices.Equal(res.headers, want) || string(res.body) != "ok" {
			t.Fatalf("request %d: got %d %q %q %q, want %q", i, res.errno, res.errMsg, res.headers, res.body, want)
		}
	}

	stream := doTransfer(t, &transferRequest{url: "https://example.test/order", key: transportKey{proxy: "http://" + proxy, http1: true, tls: tlsSettings{verifyPeer: false}}})
	if !slices.Equal(stream.headers, order) {
		t.Fatalf("stream: got %q", stream.headers)
	}

	// HTTP/2 through the tunnel: the CONNECT head, then the HTTP/2 lines
	h2 := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-A", "1")
	}))
	h2.EnableHTTP2 = true
	h2.StartTLS()
	t.Cleanup(h2.Close)

	h2proxy := rawServer(t, h2.Listener.Addr().String(), nil)
	res := doTransfer(t, &transferRequest{url: "https://example.test/", decode: true, curlStatusLines: true, key: transportKey{proxy: "http://" + h2proxy, tls: tlsSettings{verifyPeer: false}}})

	if len(res.headers) < 4 || !slices.Equal(res.headers[:4], append(slices.Clone(connect), "HTTP/2 200 ")) || !slices.Contains(res.headers, "x-a: 1") {
		t.Fatalf("h2: got %q", res.headers)
	}
}

// TestCurlError_Proxy checks proxy failures against libcurl 8.22's errors.
func TestCurlError_Proxy(t *testing.T) {
	proxy := rawServer(t, "", nil)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	closed := l.Addr().String()
	_ = l.Close()
	_, closedPort, _ := net.SplitHostPort(closed)

	for _, tc := range []struct {
		url, proxy string
		errno      int
		msg        string
	}{
		{"https://deny.test:1/", proxy, 7, "CONNECT tunnel failed, response 407"},
		{"https://garbage.test:1/", proxy, 7, "CONNECT tunnel failed, response 0"},
		{"https://close.test:1/", proxy, 56, "Proxy CONNECT aborted"},
		{"https://example.test/", closed, 7, "Failed to connect to example.test:443 over proxy 127.0.0.1 after "},
		{"http://example.test/", closed, 7, "Failed to connect to 127.0.0.1:" + closedPort + " over proxy 127.0.0.1 after "},
	} {
		var pool transportPool

		res := pool.do(context.Background(), &transferRequest{url: tc.url, curlStatusLines: true, connectTimeout: 5 * time.Second, key: transportKey{proxy: "http://" + tc.proxy, fresh: true}})
		if res.errno != tc.errno || !strings.HasPrefix(res.errMsg, tc.msg) {
			t.Errorf("%s: got %d %q, want %d %q", tc.url, res.errno, res.errMsg, tc.errno, tc.msg)
		}
	}
}
