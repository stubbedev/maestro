package testutil

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// homeDir is a user's home directory in a path: /home/<name> or
// /Users/<name>.
var homeDir = regexp.MustCompile(`/(?:home|Users)/([A-Za-z0-9_][A-Za-z0-9._-]*)`)

// placeholderHomes are the names test data may use under /home and /Users:
// the placeholders oracles write (tools/oracle/php/preg_anonymize.php,
// /home/oracle) and paths below a HOME of /home itself (Composer's own
// fixtures and the config factory's oracle).
var placeholderHomes = []string{"user", "oracle", "me", "cache", "composer"}

// installedSoftware is where a package manager installs a machine's
// software, a path only that machine has (the php, CA bundle or unzip an
// oracle ran with): a Nix or Guix store entry, Homebrew's prefixes. The
// all-zero store hash is the placeholder tools/oracle/php/preg_anonymize.php
// writes.
var installedSoftware = regexp.MustCompile(`/(?:nix|gnu)/store/([0-9a-z]{32})-|/opt/homebrew/|/usr/local/Cellar/`)

// pinnedSoftware are the test data recorded from software built from a
// pin, whose store paths are the same on every machine: the platform
// oracle's php builds (tools/oracle/platform/generate.sh).
var pinnedSoftware = []string{"internal/platform/testdata/oracle/"}

// TestTestdataNamesNoMachine: no test data, gzipped goldens included,
// holds a developer's home directory, the home directory of the machine
// running the tests or the path of software installed on a machine, as
// goldens recorded on a machine pick up its paths.
func TestTestdataNamesNoMachine(t *testing.T) {
	root := moduleRoot(t)
	home, _ := os.UserHomeDir()
	if slices.Contains([]string{"", "/", "/root"}, home) || strings.HasPrefix(home, "/home/user") {
		home = ""
	}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}

			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if !slices.Contains(strings.Split(filepath.ToSlash(rel), "/"), "testdata") {
			return nil
		}
		data, err := readTestdata(path)
		if err != nil {
			return err
		}
		for _, m := range homeDir.FindAllSubmatch(data, -1) {
			name := string(m[1])
			if !strings.HasPrefix(name, ".") && !strings.Contains(name, ".") && !slices.Contains(placeholderHomes, name) {
				t.Errorf("%s holds the home directory %s: use a placeholder such as /home/user", rel, m[0])

				break
			}
		}
		pinned := slices.ContainsFunc(pinnedSoftware, func(p string) bool { return strings.HasPrefix(filepath.ToSlash(rel), p) })
		for _, m := range installedSoftware.FindAllSubmatch(data, -1) {
			if !pinned && (m[1] == nil || strings.Trim(string(m[1]), "0") != "") {
				t.Errorf("%s holds the machine path %s: normalise it to a placeholder in the oracle that records it", rel, m[0])

				break
			}
		}
		if home != "" && bytes.Contains(data, []byte(home)) {
			t.Errorf("%s holds this machine's home directory %s", rel, home)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// readTestdata reads a test data file, decompressed when it is gzipped (a
// fixture that only looks gzipped, such as a broken archive, as it is).
func readTestdata(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil || !strings.HasSuffix(path, ".gz") {
		return data, err
	}
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return data, nil //nolint:nilerr // not gzip: scanned as it is
	}
	if inflated, err := io.ReadAll(r); err == nil {
		return inflated, nil
	}

	return data, nil
}

// moduleRoot is the directory holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}
