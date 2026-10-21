package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/switches"
)

// TestInstallNoCacheWithoutWritableHome holds --no-cache to what Composer
// gives it: an install that needs no writable cache directory and leaves no
// cache behind. HOME and MAESTRO_CACHE_DIR sit under a directory nobody can
// write to, as in a nix build sandbox, so only a cache kept for the run
// alone lets the install through.
func TestInstallNoCacheWithoutWritableHome(t *testing.T) {
	if !switches.On(switches.E2E) {
		t.Skip("set MAESTRO_E2E=1 to run maestro installs (php for the platform probe)")
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes through the read-only directory")
	}

	root := t.TempDir()
	bin := filepath.Join(root, "maestro")

	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("building maestro: %v\n%s", err, out)
	}

	zipFile(t, filepath.Join(root, "artifacts", "acme-art-1.0.0.zip"), map[string]string{
		"art/":              "",
		"art/composer.json": libComposerJSON("acme/art", `, "version": "1.0.0"`),
		"art/src/":          "",
		"art/src/Art.php":   "<?php\n\nnamespace Acme\\Lib;\n\nfinal class Art\n{\n}\n",
	})

	project := filepath.Join(root, "project")
	tmp := filepath.Join(root, "tmp")
	readOnly := filepath.Join(root, "ro")
	for _, dir := range []string{project, tmp, readOnly} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o755) })

	if err := os.WriteFile(filepath.Join(project, "composer.json"), []byte(`{
    "name": "maestro/no-cache",
    "repositories": [
        {"type": "artifact", "url": "../artifacts"},
        {"packagist.org": false}
    ],
    "require": {"acme/art": "^1.0"}
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	mcache := filepath.Join(readOnly, "mcache")
	cmd := exec.Command(bin, "install", "--no-cache", "--no-interaction")
	cmd.Dir = project
	cmd.Env = append(os.Environ(),
		"HOME="+filepath.Join(readOnly, "home"),
		"XDG_CACHE_HOME=",
		"COMPOSER_HOME="+filepath.Join(root, "home"),
		"COMPOSER_HTACCESS_PROTECT=0",
		"MAESTRO_CACHE_DIR="+mcache,
		"TMPDIR="+tmp,
		"COMPOSER_DISABLE_NETWORK=1",
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install --no-cache: %v\n%s", err, out)
	}

	if _, err := os.Stat(filepath.Join(project, "vendor", "acme", "art", "src", "Art.php")); err != nil {
		t.Errorf("acme/art not installed: %v", err)
	}

	if _, err := os.Lstat(mcache); !os.IsNotExist(err) {
		t.Errorf("--no-cache used MAESTRO_CACHE_DIR %s: %v", mcache, err)
	}

	left, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range left {
		t.Errorf("--no-cache left %s in TMPDIR", e.Name())
	}
}
