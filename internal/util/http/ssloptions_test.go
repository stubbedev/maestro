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
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/php"
)

// chainServer is an https server for "localhost" whose certificate chain
// is leaf <- Inter2 <- Inter1 <- Root (it sends the intermediates); it
// returns its URL and the root's file.
func chainServer(t *testing.T, handler http.Handler) (string, string) {
	t.Helper()

	type node struct {
		cert *x509.Certificate
		key  *ecdsa.PrivateKey
	}

	issue := func(cn string, parent *node, ca bool) *node {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}

		tmpl := &x509.Certificate{
			SerialNumber:          big.NewInt(time.Now().UnixNano()),
			Subject:               pkix.Name{CommonName: cn},
			NotBefore:             time.Now().Add(-time.Hour),
			NotAfter:              time.Now().Add(time.Hour),
			IsCA:                  ca,
			BasicConstraintsValid: true,
		}

		if ca {
			tmpl.KeyUsage = x509.KeyUsageCertSign
		} else {
			tmpl.DNSNames = []string{"localhost"}
			tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		}

		parentCert, parentKey := tmpl, key
		if parent != nil {
			parentCert, parentKey = parent.cert, parent.key
		}

		der, err := x509.CreateCertificate(rand.Reader, tmpl, parentCert, &key.PublicKey, parentKey)
		if err != nil {
			t.Fatal(err)
		}

		cert, _ := x509.ParseCertificate(der)

		return &node{cert, key}
	}

	root := issue("Root", nil, true)
	i1 := issue("Inter1", root, true)
	i2 := issue("Inter2", i1, true)
	leaf := issue("localhost", i2, false)

	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{leaf.cert.Raw, i2.cert.Raw, i1.cert.Raw}, PrivateKey: leaf.key}},
		MaxVersion:   tls.VersionTLS12,
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	rootFile := filepath.Join(t.TempDir(), "root.pem")
	if err := os.WriteFile(rootFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.cert.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}

	return "https://localhost:" + strconv.Itoa(srv.Listener.Addr().(*net.TCPAddr).Port) + "/", rootFile
}

// TestStreamSSL_VerifyDepth checks verify_depth as PHP 8.4 applies it to
// a chain of four certificates (OpenSSL 3, s_server): depth 3 passes, 2
// and less fail with PHP's warnings. curl is not given the option.
func TestStreamSSL_VerifyDepth(t *testing.T) {
	url, root := chainServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	failed := []string{
		"SSL operation failed with code 1. OpenSSL Error messages:\nerror:0A000086:SSL routines::certificate verify failed",
		"Failed to enable crypto",
		"Failed to open stream: operation failed",
	}

	for depth, ok := range []bool{false, false, false, true, true} {
		var pool transportPool

		res := pool.do(context.Background(), &transferRequest{url: url, key: transportKey{http1: true, fresh: true, tls: tlsSettings{
			cafile: root, verifyPeer: true, verifyPeerName: true, verifyDepth: int64(depth), hasVerifyDepth: true,
		}}})

		if ok && res.errno != 0 || !ok && (res.errno != 60 || !slices.Equal(res.streamWarnings, failed)) {
			t.Errorf("depth %d: got %d %q %q", depth, res.errno, res.errMsg, res.streamWarnings)
		}
	}

	// curl's options have no depth: tlsFromOptions drops it for curl
	s := tlsFromOptions(php.ArrayOf("verify_depth", 0, "ciphers", "NOPE"), false)
	if s.hasVerifyDepth || s.hasCiphers {
		t.Fatalf("got %+v", s)
	}
}

// TestStreamSSL_Ciphers checks that the ciphers option restricts the TLS
// 1.2 suites offered, and fails as PHP does when it selects none.
func TestStreamSSL_Ciphers(t *testing.T) {
	url, root := chainServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(tls.CipherSuiteName(r.TLS.CipherSuite)))
	}))

	for _, tc := range []struct{ ciphers, want string }{
		{"ECDHE-ECDSA-AES128-SHA", "TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA"},
		{"ECDHE+AES256:!SHA1", "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384"},
		{"CHACHA20", "TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256"},
	} {
		var pool transportPool

		res := pool.do(context.Background(), &transferRequest{url: url, key: transportKey{http1: true, fresh: true, tls: tlsSettings{
			cafile: root, verifyPeer: true, verifyPeerName: true, ciphers: tc.ciphers, hasCiphers: true,
		}}})

		if res.errno != 0 || string(res.body) != tc.want {
			t.Errorf("%s: got %d %q %q", tc.ciphers, res.errno, res.errMsg, res.body)
		}
	}

	var pool transportPool

	res := pool.do(context.Background(), &transferRequest{url: url, key: transportKey{http1: true, tls: tlsSettings{
		cafile: root, verifyPeer: true, ciphers: "NOPE", hasCiphers: true,
	}}})

	if res.errno == 0 || !slices.Equal(res.streamWarnings, []string{"Failed to enable crypto", "Failed to open stream: operation failed"}) {
		t.Fatalf("got %d %q", res.errno, res.streamWarnings)
	}
}

// TestStreamSSL_PeerName checks PHP's peer name failure.
func TestStreamSSL_PeerName(t *testing.T) {
	url, root := chainServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	var pool transportPool

	res := pool.do(context.Background(), &transferRequest{url: strings.Replace(url, "localhost", "127.0.0.1", 1), key: transportKey{http1: true, tls: tlsSettings{
		cafile: root, verifyPeer: true, verifyPeerName: true, stream: true,
	}}})

	want := []string{"Peer certificate CN=`localhost' did not match expected CN=`127.0.0.1'", "Failed to enable crypto", "Failed to open stream: operation failed"}
	if !slices.Equal(res.streamWarnings, want) {
		t.Fatalf("got %d %q", res.errno, res.streamWarnings)
	}
}
