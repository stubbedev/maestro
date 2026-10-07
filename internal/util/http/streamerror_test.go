package http

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"slices"
	"testing"
	"time"
)

// TestStreamWarnings_Wording checks the warnings of the stream wrapper
// path (RemoteFilesystem) against what PHP 8.4's http wrapper raised for
// the same failures, and that it returns the body read before a failure.
func TestStreamWarnings_Wording(t *testing.T) {
	ca, caFile := testCA(t)

	readHead := func(c net.Conn) {
		br := bufio.NewReader(c)

		for {
			line, err := br.ReadString('\n')
			if err != nil || line == "\r\n" {
				return
			}
		}
	}

	hang := listenFunc(t, func(c net.Conn) {
		time.Sleep(2 * time.Second)
		_ = c.Close()
	})
	closeAtOnce := listenFunc(t, func(c net.Conn) { _ = c.Close() })
	readClose := listenFunc(t, func(c net.Conn) {
		buf := make([]byte, 4096)
		_, _ = c.Read(buf)
		_ = c.Close()
	})
	reset := listenFunc(t, func(c net.Conn) {
		buf := make([]byte, 4096)
		_, _ = c.Read(buf)
		_ = c.(*net.TCPConn).SetLinger(0)
		_ = c.Close()
	})
	shortBody := listenFunc(t, func(c net.Conn) {
		readHead(c)
		_, _ = io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: 10\r\n\r\nhel")
		time.Sleep(100 * time.Millisecond)
		_ = c.Close()
	})
	slowBody := listenFunc(t, func(c net.Conn) {
		readHead(c)
		_, _ = io.WriteString(c, "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhel")
		time.Sleep(2 * time.Second)
		_ = c.Close()
	})
	resetBody := listenFunc(t, func(c net.Conn) {
		readHead(c)
		_, _ = io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: 10\r\n\r\nhel")
		time.Sleep(100 * time.Millisecond)
		_ = c.(*net.TCPConn).SetLinger(0)
		_ = c.Close()
	})
	oldTLS := tlsListenCert(t, ca, time.Now().Add(time.Hour), func(c *tls.Config) { c.MinVersion, c.MaxVersion = tls.VersionTLS10, tls.VersionTLS11 })
	plainHTTP := listenFunc(t, func(c net.Conn) {
		buf := make([]byte, 4096)
		_, _ = c.Read(buf)
		_, _ = io.WriteString(c, "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		_ = c.Close()
	})

	const (
		crypto = "Failed to enable crypto"
		open   = "Failed to open stream: operation failed"
	)

	for _, tc := range []struct {
		url      string
		warnings []string
		body     string
	}{
		{"http://" + hang + "/", []string{"Failed to open stream: HTTP request failed!"}, ""},
		{"http://" + closeAtOnce + "/", []string{"Failed to open stream: HTTP request failed!"}, ""},
		{"http://" + reset + "/", []string{"Failed to open stream: HTTP request failed!"}, ""},
		{"https://" + hang + "/", []string{"SSL: Handshake timed out", crypto, open}, ""},
		{"https://" + readClose + "/", []string{crypto, open}, ""},
		{"https://" + reset + "/", []string{"SSL: Connection reset by peer", crypto, open}, ""},
		{"https://localhost:" + portOf(oldTLS) + "/", []string{"SSL operation failed with code 1. OpenSSL Error messages:\nerror:0A00042E:SSL routines::tlsv1 alert protocol version", crypto, open}, ""},
		{"https://" + plainHTTP + "/", []string{"SSL operation failed with code 1. OpenSSL Error messages:\nerror:0A00010B:SSL routines::wrong version number", crypto, open}, ""},
		{"http://" + shortBody + "/", nil, "hel"},
		{"http://" + slowBody + "/", nil, "hel"},
		{"http://" + resetBody + "/", nil, "hel"},
	} {
		var pool transportPool

		res := pool.do(context.Background(), &transferRequest{
			url: tc.url, readTimeout: time.Second, connectTimeout: time.Second,
			key: transportKey{http1: true, fresh: true, tls: tlsSettings{cafile: caFile, verifyPeer: true, verifyPeerName: true, stream: true}},
		})

		switch {
		case tc.warnings != nil && (res.fail.Errno == 0 || !slices.Equal(res.streamWarnings, tc.warnings)):
			t.Errorf("%s: got %d %q %q, want %q", tc.url, res.fail.Errno, res.fail.Message, res.streamWarnings, tc.warnings)
		case tc.warnings == nil && (res.fail.Errno != 0 || string(res.body) != tc.body):
			t.Errorf("%s: got %d %q %q, want %q", tc.url, res.fail.Errno, res.fail.Message, res.body, tc.body)
		}
	}
}

// TestStreamWarnings_DNS checks the wrapper's warnings for a failed lookup
// (php_network_getaddresses, with glibc's gai_strerror()).
func TestStreamWarnings_DNS(t *testing.T) {
	err := &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "nope.example", IsNotFound: true}}

	msg := "php_network_getaddresses: getaddrinfo for nope.example failed: " + gaiStrerror(&net.DNSError{IsNotFound: true})
	if got := streamWarnings(err, false); !slices.Equal(got, []string{msg, "Failed to open stream: " + msg}) {
		t.Fatalf("got %q", got)
	}

	if got := gaiStrerror(&net.DNSError{IsTemporary: true}); got != "Temporary failure in name resolution" {
		t.Fatalf("got %q", got)
	}
}
