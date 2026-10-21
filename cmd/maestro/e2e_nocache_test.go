package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/switches"
)

// TestRunWithoutWritableCacheDir holds maestro to what Composer does when its
// cache directory cannot be created: the command still succeeds and leaves
// no cache behind, with a warning unless --no-cache asked for exactly that.
// HOME and MAESTRO_CACHE_DIR sit under a directory nobody can write to, as in
// a nix build sandbox, so only a cache kept for the run alone lets a command
// through.
func TestRunWithoutWritableCacheDir(t *testing.T) {
	if !switches.On(switches.E2E) {
		t.Skip("set MAESTRO_E2E=1 to run maestro installs (php for the platform probe)")
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes through the read-only directory")
	}

	bin := filepath.Join(t.TempDir(), "maestro")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("building maestro: %v\n%s", err, out)
	}

	for _, tc := range []struct {
		args    []string
		warns   bool
		install bool
	}{
		{args: []string{"install", "--no-cache"}, install: true},
		{args: []string{"install"}, warns: true, install: true},
		{args: []string{"validate", "--strict", "--check-lock"}, warns: true},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			root := t.TempDir()

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
    "description": "maestro without a writable cache directory",
    "license": "MIT",
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
			env := append(os.Environ(),
				"HOME="+filepath.Join(readOnly, "home"),
				"XDG_CACHE_HOME=",
				"COMPOSER_HOME="+filepath.Join(root, "home"),
				"COMPOSER_CACHE_DIR="+filepath.Join(root, "cache"),
				"COMPOSER_HTACCESS_PROTECT=0",
				"MAESTRO_CACHE_DIR="+mcache,
				"TMPDIR="+tmp,
				"COMPOSER_DISABLE_NETWORK=1",
			)
			run := func(args ...string) string {
				t.Helper()
				cmd := exec.Command(bin, append(args, "--no-interaction")...)
				cmd.Dir, cmd.Env = project, env
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%v: %v\n%s", args, err, out)
				}

				return string(out)
			}

			if !tc.install {
				run("update", "--no-cache")
			}
			out := run(tc.args...)

			if warned := strings.Contains(out, "Cannot create cache directory "+mcache); warned != tc.warns {
				t.Errorf("warned about the cache directory: %v, want %v\n%s", warned, tc.warns, out)
			}

			if tc.install {
				if _, err := os.Stat(filepath.Join(project, "vendor", "acme", "art", "src", "Art.php")); err != nil {
					t.Errorf("acme/art not installed: %v", err)
				}
			}

			if _, err := os.Lstat(mcache); !os.IsNotExist(err) {
				t.Errorf("used MAESTRO_CACHE_DIR %s: %v", mcache, err)
			}

			left, err := os.ReadDir(tmp)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range left {
				t.Errorf("left %s in TMPDIR", e.Name())
			}
		})
	}
}
