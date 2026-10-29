// Ports tests/Composer/Test/Util/Http/TlsChainDenylistTest.php.

package http

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/util"
)

// distrustedFixture is tests/Composer/Test/Util/Fixtures/distrusted/em-ev-tls-ca-g3b.pem,
// the eMudhra intermediate whose key is distrusted.
func distrustedFixture(t *testing.T) []byte {
	t.Helper()

	data, err := os.ReadFile("testdata/distrusted/em-ev-tls-ca-g3b.pem")
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func TestTlsChainDenylist_FindDistrustedCertificate(t *testing.T) {
	distrusted := distrustedFixture(t)

	if name := FindDistrustedCertificate(nil); name != "" {
		t.Fatalf("got %q", name)
	}

	if name := FindDistrustedCertificate([][]byte{bundledCaCert}); name != "" {
		t.Fatalf("got %q", name)
	}

	if name := FindDistrustedCertificate([][]byte{bundledCaCert, distrusted}); name != "EM EV TLS CA - G3B" {
		t.Fatalf("got %q", name)
	}

	// the DER of the same certificate, as crypto/tls reports a chain
	block, _ := pem.Decode(distrusted)
	if name := FindDistrustedCertificate([][]byte{block.Bytes}); name != "EM EV TLS CA - G3B" {
		t.Fatalf("got %q", name)
	}
}

func TestTlsChainDenylist_GetHostKey(t *testing.T) {
	for _, tc := range []struct {
		url, want string
		found     bool
	}{
		{"https://Example.org/foo", "example.org:443", true},
		{"https://example.org:8443/foo", "example.org:8443", true},
		{"http://example.org/foo", "", false},
		{"file:///tmp/foo", "", false},
	} {
		if got, found := TlsChainHostKey(tc.url); got != tc.want || found != tc.found {
			t.Errorf("%s: got %q %v, want %q %v", tc.url, got, found, tc.want, tc.found)
		}
	}
}

func TestTlsChainDenylist_CreateException(t *testing.T) {
	err := NewTlsChainError("https://user:pass@example.org/", "Foo CA")

	se, ok := errors.AsType[*util.SecurityError](err)
	if !ok {
		t.Fatalf("got %v", err)
	}

	if !strings.Contains(se.Message, `"Foo CA"`) {
		t.Fatalf("got %q", se.Message)
	}

	if !strings.Contains(se.Message, "aborted before it was sent") {
		t.Fatalf("got %q", se.Message)
	}

	if strings.Contains(se.Message, "pass@") || !strings.Contains(se.Message, "user:***@") {
		t.Fatalf("the URL was not sanitized: %q", se.Message)
	}
}

// tlsChainServer returns an https test server presenting certs, and the CA
// file to trust.
func tlsChainServer(t *testing.T, handler http.HandlerFunc, certs ...*x509.Certificate) (*httptest.Server, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "maestro TLS chain test root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}

	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}

	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, root, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	chain := [][]byte{leafDER}
	for _, cert := range certs {
		chain = append(chain, cert.Raw)
	}

	caFile := writeTestCAFile(t, rootDER)

	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: chain, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	srv.StartTLS()

	t.Cleanup(srv.Close)

	return srv, caFile
}

func writeTestCAFile(t *testing.T, der []byte) string {
	t.Helper()

	path := t.TempDir() + "/ca.pem"
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// A chain the system CA store trusts is refused when it holds a distrusted
// intermediate, and the request is never sent.
func TestHttpDownloader_RejectsDistrustedTlsChain(t *testing.T) {
	block, _ := pem.Decode(distrustedFixture(t))

	intermediate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}

	var served atomic.Int32

	srv, caFile := tlsChainServer(t, func(w http.ResponseWriter, _ *http.Request) {
		served.Add(1)
		_, _ = w.Write([]byte("secret"))
	}, intermediate)

	h, _ := newTestDownloader(t, nil, caFile)

	_, err = h.Get(srv.URL, nil)

	se, ok := errors.AsType[*util.SecurityError](err)
	if !ok {
		t.Fatalf("got %v, want a SecurityException", err)
	}

	if !strings.Contains(se.Message, `contains the distrusted certificate "EM EV TLS CA - G3B"`) {
		t.Fatalf("got %q", se.Message)
	}

	if served.Load() != 0 {
		t.Fatal("the request was sent to the host presenting the distrusted chain")
	}

	// the verdict is kept for the host: the next request is not sent at all
	_, err = h.Get(srv.URL, nil)
	if se, ok := errors.AsType[*util.SecurityError](err); !ok || !strings.Contains(se.Message, "aborted before it was sent") {
		t.Fatalf("got %v", err)
	}
}

// A chain without a distrusted certificate is accepted.
func TestHttpDownloader_AcceptsChainWithoutDistrustedCertificate(t *testing.T) {
	srv, caFile := tlsChainServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	h, _ := newTestDownloader(t, nil, caFile)

	r, err := h.Get(srv.URL, nil)
	if err != nil || r.Body() != "ok" {
		t.Fatalf("got %v %v", r, err)
	}
}
