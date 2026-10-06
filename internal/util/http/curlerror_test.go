package http

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// listenFunc serves every connection with handle; it returns the address.
func listenFunc(t *testing.T, handle func(net.Conn)) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = l.Close() })

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}

			go handle(c)
		}
	}()

	return l.Addr().String()
}

// tlsListenCert serves TLS with a certificate for "localhost" issued by
// parent (self-signed when nil), valid until notAfter; mod adjusts the
// server's config.
func tlsListenCert(t *testing.T, parent *tls.Certificate, notAfter time.Time, mod func(*tls.Config)) string {
	t.Helper()

	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-48 * time.Hour),
		NotAfter:     notAfter,
		DNSNames:     []string{"localhost"},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	issuer, issuerKey := tmpl, any(key)
	if parent != nil {
		issuer, issuerKey = parent.Leaf, parent.PrivateKey
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, issuer, &key.PublicKey, issuerKey)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	if mod != nil {
		mod(cfg)
	}

	l, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = l.Close() })

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}

			go func() {
				defer c.Close()

				buf := make([]byte, 4096)
				if _, err := c.Read(buf); err == nil {
					_, _ = io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
				}
			}()
		}
	}()

	return l.Addr().String()
}

// testCA is a CA certificate and the file holding it.
func testCA(t *testing.T) (*tls.Certificate, string) {
	t.Helper()

	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "maestro test CA"},
		NotBefore:             time.Now().Add(-48 * time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	leaf, _ := x509.ParseCertificate(der)
	path := filepath.Join(t.TempDir(), "ca.pem")

	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}

	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, path
}

func portOf(addr string) string {
	_, p, _ := net.SplitHostPort(addr)

	return p
}

// TestCurlError_Wording checks the curl errors of common failures against
// the messages php-curl (libcurl 8.22, OpenSSL 3.6) gives for the same
// failures; messages ending in "after " continue with a time.
func TestCurlError_Wording(t *testing.T) {
	ca, caFile := testCA(t)
	_, otherFile := testCA(t)

	selfSigned := tlsListenCert(t, nil, time.Now().Add(time.Hour), nil)
	valid := tlsListenCert(t, ca, time.Now().Add(time.Hour), nil)
	expired := tlsListenCert(t, ca, time.Now().Add(-time.Hour), nil)
	oldTLS := tlsListenCert(t, ca, time.Now().Add(time.Hour), func(c *tls.Config) { c.MinVersion, c.MaxVersion = tls.VersionTLS10, tls.VersionTLS11 })
	noCipher := tlsListenCert(t, ca, time.Now().Add(time.Hour), func(c *tls.Config) {
		c.MaxVersion = tls.VersionTLS12
		c.CipherSuites = []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256}
	})

	plain := listenFunc(t, func(c net.Conn) {
		defer c.Close()

		buf := make([]byte, 4096)
		n, _ := c.Read(buf)
		req := string(buf[:n])

		switch {
		case strings.HasPrefix(req, "GET /empty "):
		case strings.HasPrefix(req, "GET /partial "):
			_, _ = io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: 10\r\n\r\nhello")
		case strings.HasPrefix(req, "GET /chunked "):
			_, _ = io.WriteString(c, "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n")
		case strings.HasPrefix(req, "GET /badstatus "):
			_, _ = io.WriteString(c, "garbage\r\n\r\n")
		case strings.HasPrefix(req, "GET /slowbody "):
			_, _ = io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: 10\r\n\r\nhel")
			time.Sleep(2 * time.Second)
		case strings.HasPrefix(req, "GET /hang "):
			time.Sleep(2 * time.Second)
		default:
			// a TLS ClientHello, or anything else
			_, _ = io.WriteString(c, "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		}
	})
	eof := listenFunc(t, func(c net.Conn) {
		buf := make([]byte, 4096)
		_, _ = c.Read(buf)
		_ = c.Close()
	})
	silent := listenFunc(t, func(c net.Conn) {
		time.Sleep(2 * time.Second)
		_ = c.Close()
	})

	l, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := l.Addr().String()
	_ = l.Close()

	for _, tc := range []struct {
		url, cafile string
		timeout     time.Duration
		errno       int
		msg         string
	}{
		{"http://" + closed + "/", "", 0, 7, "Failed to connect to 127.0.0.1:" + portOf(closed) + " after "},
		{"https://localhost:" + portOf(selfSigned) + "/", caFile, 0, 60, "SSL certificate OpenSSL verify result: self-signed certificate (18)"},
		{"https://localhost:" + portOf(valid) + "/", otherFile, 0, 60, "SSL certificate OpenSSL verify result: unable to get local issuer certificate (20)"},
		{"https://localhost:" + portOf(expired) + "/", caFile, 0, 60, "SSL certificate OpenSSL verify result: certificate has expired (10)"},
		{"https://127.0.0.1:" + portOf(valid) + "/", caFile, 0, 60, "SSL: no alternative certificate subject name matches target IPv4 address '127.0.0.1'"},
		{"https://localhost:" + portOf(valid) + "/", caFile, 0, 0, ""},
		{"https://localhost:" + portOf(oldTLS) + "/", caFile, 0, 35, "TLS connect error: error:0A00042E:SSL routines::tlsv1 alert protocol version"},
		{"https://localhost:" + portOf(noCipher) + "/", caFile, 0, 35, "TLS connect error: error:0A000410:SSL routines::ssl/tls alert handshake failure"},
		{"https://" + plain + "/", caFile, 0, 35, "TLS connect error: error:0A00010B:SSL routines::wrong version number"},
		{"https://" + eof + "/", caFile, 0, 35, "TLS connect error: error:0A000126:SSL routines::unexpected eof while reading"},
		{"https://" + silent + "/", caFile, time.Second, 28, "Connection timed out after "},
		{"http://" + plain + "/empty", "", 0, 52, "Empty reply from server"},
		{"http://" + plain + "/partial", "", 0, 18, "end of response with 5 bytes missing"},
		{"http://" + plain + "/chunked", "", 0, 18, "transfer closed with outstanding read data remaining"},
		{"http://" + plain + "/badstatus", "", 0, 1, "Received HTTP/0.9 when not allowed"},
		{"http://" + plain + "/hang", "", time.Second, 28, "Operation timed out after "},
		{"http://" + plain + "/slowbody", "", time.Second, 28, "Operation timed out after "},
	} {
		var pool transportPool

		r := &transferRequest{url: tc.url, decode: true, curlStatusLines: true, timeout: 300 * time.Second, connectTimeout: 10 * time.Second,
			key: transportKey{fresh: true, tls: tlsSettings{cafile: tc.cafile, verifyPeer: true, verifyPeerName: true}}}
		if tc.timeout != 0 {
			r.timeout = tc.timeout
		}

		res := pool.do(context.Background(), r)
		if res.errno != tc.errno || !strings.HasPrefix(res.errMsg, tc.msg) || tc.msg == "" && res.errMsg != "" {
			t.Errorf("%s: got %d %q, want %d %q", tc.url, res.errno, res.errMsg, tc.errno, tc.msg)
		}

		if strings.HasSuffix(tc.url, "/hang") && !strings.HasSuffix(res.errMsg, " milliseconds with 0 bytes received") ||
			strings.HasSuffix(tc.url, "/slowbody") && !strings.HasSuffix(res.errMsg, " milliseconds with 3 out of 10 bytes received") {
			t.Errorf("%s: got %q", tc.url, res.errMsg)
		}
	}
}

// TestCurlError_AlertAfterHandshake checks an alert read with the response
// (a TLS 1.3 server that requires a client certificate), which curl
// reports from SSL_read with the TLS library's version.
func TestCurlError_AlertAfterHandshake(t *testing.T) {
	ca, caFile := testCA(t)
	srv := tlsListenCert(t, ca, time.Now().Add(time.Hour), func(c *tls.Config) { c.ClientAuth = tls.RequireAnyClientCert })

	SetCurlInfo(func() (CurlInfo, bool) { return CurlInfo{SSLVersion: "OpenSSL/3.6.4"}, true })
	t.Cleanup(func() { SetCurlInfo(nil) })

	var pool transportPool

	res := pool.do(context.Background(), &transferRequest{url: "https://localhost:" + portOf(srv) + "/", curlStatusLines: true,
		key: transportKey{fresh: true, tls: tlsSettings{cafile: caFile, verifyPeer: true, verifyPeerName: true}}})

	want := "OpenSSL SSL_read: OpenSSL/3.6.4: error:0A00045C:SSL routines::tlsv13 alert certificate required, errno 0"
	if res.errno != 56 || res.errMsg != want {
		t.Fatalf("got %d %q", res.errno, res.errMsg)
	}
}
