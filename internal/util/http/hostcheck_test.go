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
	"strings"
	"testing"
	"time"
)

// peerCert issues a server certificate with the given subject and
// subjectAltName entries from a new root; it returns the leaf, its key
// and the root.
func peerCert(t *testing.T, subject pkix.Name, dns []string, ips []net.IP, emails []string) (*x509.Certificate, *ecdsa.PrivateKey, *x509.Certificate) {
	t.Helper()

	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	rootTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Root"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}

	rootDER, err := x509.CreateCertificate(rand.Reader, rootTmpl, rootTmpl, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}

	root, _ := x509.ParseCertificate(rootDER)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: subject,
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: dns, IPAddresses: ips, EmailAddresses: emails,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, root, &key.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}

	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	return leaf, key, root
}

// TestCheckPeerName checks the peer name check against php-curl (libcurl
// 8.22, OpenSSL 3.6) and PHP 8.4's stream wrapper on servers presenting
// the same certificates: curl matches the subjectAltName entries of the
// target's type and falls back on the common name only when there are
// no DNS or IP entries, with its own wildcard rules (at least two dots);
// PHP matches the entries, then the common name.
func TestCheckPeerName(t *testing.T) {
	cn := func(name string) pkix.Name { return pkix.Name{CommonName: name} }
	ip := net.ParseIP

	for _, tc := range []struct {
		name          string
		subject       pkix.Name
		dns           []string
		ips           []net.IP
		emails        []string
		host          string
		curl, warning string
	}{
		{"nosan", cn("localhost"), nil, nil, nil, "localhost", "", ""},
		{"nosan", cn("LOCALHOST"), nil, nil, nil, "localhost", "", ""},
		{"nosan", cn("localhost"), nil, nil, nil, "127.0.0.1", "SSL: certificate subject name 'localhost' does not match target hostname '127.0.0.1'", "Peer certificate CN=`localhost' did not match expected CN=`127.0.0.1'"},
		{"ipcn", cn("127.0.0.1"), nil, nil, nil, "127.0.0.1", "", ""},
		{"ipcn", cn("127.0.0.1"), nil, nil, nil, "localhost", "SSL: certificate subject name '127.0.0.1' does not match target hostname 'localhost'", "Peer certificate CN=`127.0.0.1' did not match expected CN=`localhost'"},
		{"emailsan", cn("localhost"), nil, nil, []string{"a@b.c"}, "localhost", "", ""},
		{"dnsother", cn("localhost"), []string{"other.example"}, nil, nil, "localhost", "SSL: no alternative certificate subject name matches target hostname 'localhost'", ""},
		{"dnsother", cn("localhost"), []string{"other.example"}, nil, nil, "127.0.0.1", "SSL: no alternative certificate subject name matches target IPv4 address '127.0.0.1'", "Peer certificate CN=`localhost' did not match expected CN=`127.0.0.1'"},
		{"ipsanother", cn("localhost"), nil, []net.IP{ip("10.0.0.1")}, nil, "localhost", "SSL: no alternative certificate subject name matches target hostname 'localhost'", ""},
		{"ipsan", cn("x"), nil, []net.IP{ip("::1")}, nil, "::1", "", ""},
		{"ipsan", cn("x"), []string{"localhost"}, nil, nil, "::1", "SSL: no alternative certificate subject name matches target IPv6 address '::1'", "Peer certificate CN=`x' did not match expected CN=`::1'"},
		{"wild", cn("*.localhost"), nil, nil, nil, "a.localhost", "SSL: certificate subject name '*.localhost' does not match target hostname 'a.localhost'", ""},
		{"wild", cn("*.localhost"), nil, nil, nil, "localhost", "SSL: certificate subject name '*.localhost' does not match target hostname 'localhost'", "Peer certificate CN=`*.localhost' did not match expected CN=`localhost'"},
		{"wild", cn("*.example.com"), nil, nil, nil, "a.example.com", "", ""},
		{"wild", cn("x"), []string{"*.example.com"}, nil, nil, "a.b.example.com", "SSL: no alternative certificate subject name matches target hostname 'a.b.example.com'", "Peer certificate CN=`x' did not match expected CN=`a.b.example.com'"},
		{"wild", cn("x"), []string{"*.example.com."}, nil, nil, "A.Example.com", "", ""},
		{"nocn", pkix.Name{Organization: []string{"Org"}}, nil, nil, nil, "localhost", "SSL: unable to obtain common name from peer certificate", "Unable to locate peer certificate CN"},
	} {
		leaf, _, _ := peerCert(t, tc.subject, tc.dns, tc.ips, tc.emails)

		got := ""
		if err := checkPeerName(leaf, tc.host, false); err != nil {
			got = err.Error()
		}

		if got != tc.curl {
			t.Errorf("%s %s curl: got %q, want %q", tc.name, tc.host, got, tc.curl)
		}

		got = ""
		if err := checkPeerName(leaf, tc.host, true); err != nil {
			got = err.Error()
		}

		if got != tc.warning {
			t.Errorf("%s %s stream: got %q, want %q", tc.name, tc.host, got, tc.warning)
		}
	}
}

// TestCheckPeerName_Transfer connects to a server whose certificate has
// no subjectAltName: curl and the stream wrapper accept it on its common
// name, and report a mismatch in their words.
func TestCheckPeerName_Transfer(t *testing.T) {
	leaf, key, root := peerCert(t, pkix.Name{CommonName: "localhost"}, nil, nil, nil)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{leaf.Raw}, PrivateKey: key}}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	rootFile := filepath.Join(t.TempDir(), "root.pem")
	if err := os.WriteFile(rootFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}

	byName := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)

	for _, stream := range []bool{false, true} {
		var pool transportPool

		s := tlsSettings{cafile: rootFile, verifyPeer: true, verifyPeerName: true, stream: stream}

		res := pool.do(context.Background(), &transferRequest{url: byName, curlStatusLines: !stream, key: transportKey{http1: stream, tls: s}})
		if res.fail.Errno != 0 || string(res.body) != "ok" {
			t.Errorf("stream %v: got %d %q %q", stream, res.fail.Errno, res.fail.Message, res.body)
		}

		res = pool.do(context.Background(), &transferRequest{url: srv.URL, curlStatusLines: !stream, key: transportKey{http1: stream, tls: s}})

		switch {
		case !stream && (res.fail.Errno != 60 || res.fail.Message != "SSL: certificate subject name 'localhost' does not match target hostname '127.0.0.1'"):
			t.Errorf("curl: got %d %q", res.fail.Errno, res.fail.Message)
		case stream && (len(res.streamWarnings) == 0 || res.streamWarnings[0] != "Peer certificate CN=`localhost' did not match expected CN=`127.0.0.1'"):
			t.Errorf("stream: got %d %q", res.fail.Errno, res.streamWarnings)
		}
	}
}
