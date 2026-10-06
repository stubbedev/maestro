package http

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadClientCertificate checks client certificate loading against
// php-curl (libcurl 8.22, OpenSSL 3) on testdata/clientcert: cli.key is
// the plain key of cli.pem, the other cli-*.key files its encryptions with
// the passphrase "secret" (openssl pkcs8 -topk8 -v2 aes-256-cbc / des3 /
// aes-128-cbc -v2prf hmacWithSHA1, -v1 PBE-SHA1-3DES, -scrypt; openssl
// rsa -aes256 -traditional), comb.pem the certificate and key in one file,
// ec.key a key of another certificate.
func TestLoadClientCertificate(t *testing.T) {
	dir := filepath.Join("testdata", "clientcert")
	f := func(name string) string { return filepath.Join(dir, name) }

	for _, tc := range []struct {
		cert, key, pass string
		errno           int
		msg             string
	}{
		{f("cli.pem"), f("cli.key"), "", 0, ""},
		{f("cli.pem"), f("cli-legacy.key"), "secret", 0, ""},
		{f("cli.pem"), f("cli-pkcs8-aes.key"), "secret", 0, ""},
		{f("cli.pem"), f("cli-pkcs8-3des.key"), "secret", 0, ""},
		{f("cli.pem"), f("cli-pkcs8-sha1.key"), "secret", 0, ""},
		{f("cli.pem"), f("cli-pkcs8-v1.key"), "secret", 0, ""},
		{f("cli.pem"), f("cli-pkcs8-scrypt.key"), "secret", 0, ""},
		{f("cli.pem"), f("cli-pkcs8-scrypt.key"), "wrong", 43, "unable to set private key file: '" + f("cli-pkcs8-scrypt.key") + "' type PEM"},
		{f("comb.pem"), "", "", 0, ""},
		{f("cli.pem"), f("cli-pkcs8-aes.key"), "wrong", 43, "unable to set private key file: '" + f("cli-pkcs8-aes.key") + "' type PEM"},
		{f("cli.pem"), f("cli-legacy.key"), "wrong", 43, "unable to set private key file: '" + f("cli-legacy.key") + "' type PEM"},
		{f("cli.pem"), f("nokey.key"), "", 43, "unable to set private key file: '" + f("nokey.key") + "' type PEM"},
		{f("cli.pem"), "", "", 43, "unable to set private key file: '" + f("cli.pem") + "' type PEM"},
		{f("cli.pem"), f("ec.key"), "", 58, "Private key does not match the certificate public key"},
		{f("nope.pem"), f("cli.key"), "", 58, "could not load PEM client certificate from " + f("nope.pem") + ", OpenSSL error error:80000002:system library::No such file or directory, (no key found, wrong passphrase, or wrong file format?)"},
		{f("cli.key"), f("cli.key"), "", 58, "could not load PEM client certificate from " + f("cli.key") + ", OpenSSL error error:0480006C:PEM routines::no start line, (no key found, wrong passphrase, or wrong file format?)"},
	} {
		_, failure := loadClientCertificate(tc.cert, tc.key, tc.pass)

		switch {
		case tc.errno == 0 && failure != nil:
			t.Errorf("%s %s: %+v", tc.cert, tc.key, failure)
		case tc.errno != 0 && (failure == nil || failure.errno != tc.errno || failure.msg != tc.msg):
			t.Errorf("%s %s: got %+v, want %d %q", tc.cert, tc.key, failure, tc.errno, tc.msg)
		}
	}
}

// TestScryptParamsOK checks OpenSSL's limits on scrypt parameters: openssl
// pkcs8 -scrypt refuses -scrypt_N 65536 -scrypt_r 8 ("memory limit
// exceeded", 64 MiB above SCRYPT_MAX_MEM) and takes its defaults.
func TestScryptParamsOK(t *testing.T) {
	for _, tc := range []struct {
		n, r, p int
		ok      bool
	}{
		{16384, 8, 1, true},
		{16384, 8, 16, true},
		{65536, 8, 1, false},
		{32768, 8, 1, false},
		{16384, 8, 0, false},
		{1000, 8, 1, false},
		{1 << 16, 1, 1, false},
	} {
		if got := scryptParamsOK(tc.n, tc.r, tc.p); got != tc.ok {
			t.Errorf("N=%d r=%d p=%d: got %v", tc.n, tc.r, tc.p, got)
		}
	}
}

// TestClientCertificate_Handshake presents a PKCS#8-encrypted client key
// to a server that requires a client certificate.
func TestClientCertificate_Handshake(t *testing.T) {
	caPEM, err := os.ReadFile(filepath.Join("testdata", "clientcert", "ca.pem"))
	if err != nil {
		t.Fatal(err)
	}

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.TLS.PeerCertificates[0].Subject.CommonName))
	}))
	srv.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	var p transportPool

	res := p.do(context.Background(), &transferRequest{url: srv.URL, curlStatusLines: true, key: transportKey{fresh: true, tls: tlsSettings{
		verifyPeer: false,
		localCert:  filepath.Join("testdata", "clientcert", "cli.pem"),
		localPK:    filepath.Join("testdata", "clientcert", "cli-pkcs8-aes.key"),
		passphrase: "secret",
	}}})

	if res.errno != 0 || string(res.body) != "client" {
		t.Fatalf("got %d %q %q", res.errno, res.errMsg, res.body)
	}
}
