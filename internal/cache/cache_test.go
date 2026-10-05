package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheCacheDirectory(t *testing.T) {
	t.Setenv("MAESTRO_CACHE_DIR", "/tmp/explicit")

	if Dir() != "/tmp/explicit" {
		t.Fatalf("expected MAESTRO_CACHE_DIR to win, got %s", Dir())
	}

	t.Setenv("MAESTRO_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg")

	if Dir() != filepath.Join("/tmp/xdg", "maestro") {
		t.Fatalf("expected the XDG cache home to be used, got %s", Dir())
	}
}

func TestAPackageIsKeyedByReferenceThenURL(t *testing.T) {
	t.Setenv("MAESTRO_CACHE_DIR", t.TempDir())

	if Package("a/b", "r1", "u1", "x") != Package("a/b", "r1", "u2", "x") {
		t.Error("expected one reference from two URLs to share an entry")
	}

	if Package("a/b", "r1", "u1", "x") == Package("a/b", "r2", "u1", "x") {
		t.Error("expected two references to use two entries")
	}

	if Package("a/b", "", "u1", "x") == Package("a/b", "", "u2", "x") {
		t.Error("expected two URLs without a reference to use two entries")
	}

	if Package("a/b", "r1", "", "x") == Package("c/d", "r1", "", "x") {
		t.Error("expected two packages to use two entries")
	}

	if Package("a/b", "r1", "", "x") == Package("a/b", "r1", "", "y") {
		t.Error("expected two extractors to use two entries")
	}
}

func TestNothingClimbsOutOfTheStore(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MAESTRO_CACHE_DIR", root)

	for _, path := range []string{
		Package("../../../etc/passwd", "r", "", "x"),
		Package("a/b", "../../../../etc", "", "x"),
		Package("a/b", "a/../../b", "", "x"),
	} {
		rel, err := filepath.Rel(filepath.Join(root, "pkgs", "v1"), path)
		if err != nil || !filepath.IsLocal(rel) || strings.Count(rel, string(os.PathSeparator)) != 2 {
			t.Errorf("expected %s to be exactly vendor/package/key under the store", path)
		}
	}
}

func TestFingerprintsDiffer(t *testing.T) {
	a := Fingerprint("/usr/bin/unzip", 1, 2, 0o022)
	if a == Fingerprint("/usr/bin/unzip", 1, 2, 0o002) || a == Fingerprint("/nix/unzip", 1, 2, 0o022) {
		t.Fatal("expected the umask and the binary to change the fingerprint")
	}
}
