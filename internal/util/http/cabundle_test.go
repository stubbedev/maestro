package http

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCaBundle_IniLocations checks that getSystemCaRootBundlePath consults
// php.ini's openssl.cafile and openssl.capath after the SSL_CERT_FILE and
// SSL_CERT_DIR environment variables and before the well-known locations.
func TestCaBundle_IniLocations(t *testing.T) {
	cafile := writeTestCA(t)
	capath := t.TempDir()

	if err := os.WriteFile(filepath.Join(capath, "a.pem"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	ini := map[string]string{}

	SetIniSource(func(name string) (string, bool) {
		v, ok := ini[name]

		return v, ok
	})
	t.Cleanup(func() { SetIniSource(nil); ResetCaBundle() })

	t.Setenv("SSL_CERT_FILE", "")
	t.Setenv("SSL_CERT_DIR", "")

	find := func() string {
		t.Helper()
		ResetCaBundle()

		path, err := SystemCaRootBundlePath(nil)
		if err != nil {
			t.Fatal(err)
		}

		return path
	}

	ini["openssl.cafile"] = cafile
	ini["openssl.capath"] = capath

	if got := find(); got != cafile {
		t.Fatalf("openssl.cafile: got %s", got)
	}

	// a cafile that is no CA file falls through to capath
	ini["openssl.cafile"] = filepath.Join(capath, "a.pem")

	if got := find(); got != capath {
		t.Fatalf("openssl.capath: got %s", got)
	}

	// "0" is falsy in `if ($caBundle && ...)`
	ini["openssl.cafile"], ini["openssl.capath"] = "0", "0"

	if got := find(); got == "0" {
		t.Fatal("0 must be skipped")
	}

	// the environment comes first
	ini["openssl.cafile"] = cafile
	t.Setenv("SSL_CERT_DIR", capath)

	if got := find(); got != capath {
		t.Fatalf("SSL_CERT_DIR: got %s", got)
	}
}
