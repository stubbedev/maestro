// Ports tests/Composer/Test/Command/HomeCommandTest.php.

package command_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/pkg"
)

// homeProject writes a project whose installed repository holds
// vendor/package (homepage packageURL) and the dev package
// vendor/devpackage (homepage devURL); an empty URL leaves the homepage
// unset.
func homeProject(t *testing.T, composerJSON, packageURL, devURL string) string {
	t.Helper()
	dir := commandtest.InitTempComposer(t, composerJSON, nil, nil, true)
	p := commandtest.GetPackage(t, "vendor/package", "1.2.3")
	dev := commandtest.GetPackage(t, "vendor/devpackage", "2.3.4")
	for cp, url := range map[*pkg.CompletePackage]string{p: packageURL, dev: devURL} {
		if url != "" {
			cp.SetHomepage(pkg.Str(url))
		}
	}
	commandtest.CreateInstalledJSON(t, gbPkgs(p), gbPkgs(dev), true)

	return dir
}

// TestHomeCommand_HomeCommandWithShowFlag runs useCaseProvider's cases,
// and asserts the stream of each line and the status code, which the
// PHPUnit test leaves out: the URL on stdout, the warnings on stderr, and
// 1 when a package had no usable URL. The last case is maestro's: a
// homepage that is not a URL (FILTER_VALIDATE_URL) is no usable URL.
func TestHomeCommand_HomeCommandWithShowFlag(t *testing.T) {
	const packageRepo = `{
		"repositories": {
			"packages": {
				"type": "package",
				"package": [
					{"name": "vendor/package", "description": "generic description", "version": "1.0.0"}
				]
			}
		},
		"require": {"vendor/package": "^1.0"}
	}`
	tests := []struct {
		name               string
		composerJSON       string
		packages           []string
		packageURL, devURL string
		code               int
		stdout, stderr     string
	}{
		{
			name:         "Invalid or missing repository URL",
			composerJSON: packageRepo,
			packages:     []string{"vendor/package"},
			code:         1,
			stderr:       "<warning>Invalid or missing repository URL for vendor/package</warning>\n",
		},
		{
			name:         "No Packages Provided",
			composerJSON: `{"repositories": []}`,
			code:         1,
			stderr:       "No package specified, opening homepage for the root package\n<warning>Invalid or missing repository URL for __root__</warning>\n",
		},
		{
			name:         "Package not found",
			composerJSON: `{"repositories": []}`,
			packages:     []string{"vendor/anotherpackage"},
			code:         1,
			stderr:       "<warning>Package vendor/anotherpackage not found</warning>\n<warning>Invalid or missing repository URL for vendor/anotherpackage</warning>\n",
		},
		{
			name:         "A valid package URL",
			composerJSON: `{"repositories": []}`,
			packages:     []string{"vendor/package"},
			packageURL:   "https://example.org",
			stdout:       "https://example.org\n",
		},
		{
			name:         "A valid dev package URL",
			composerJSON: `{"repositories": []}`,
			packages:     []string{"vendor/devpackage"},
			devURL:       "https://example.org/dev",
			stdout:       "https://example.org/dev\n",
		},
		{
			name:         "A homepage that is not a URL",
			composerJSON: `{"repositories": []}`,
			packages:     []string{"vendor/package"},
			packageURL:   "not a url",
			code:         1,
			stderr:       "<warning>Invalid or missing repository URL for vendor/package</warning>\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeProject(t, tt.composerJSON, tt.packageURL, tt.devURL)
			kv := []any{"command", "home", "--show", true}
			if tt.packages != nil {
				kv = append(kv, "packages", tt.packages)
			}
			got := commandtest.GetApplicationTester(t).RunStreams(kv...)
			if got.Err != nil {
				t.Fatal(got.Err)
			}
			if got.Code != tt.code || got.Stdout != tt.stdout || got.Stderr != tt.stderr {
				t.Errorf("status %d, stdout %q, stderr %q; want %d, %q, %q", got.Code, got.Stdout, got.Stderr, tt.code, tt.stdout, tt.stderr)
			}
		})
	}
}

// TestHomeCommand_OpensBrowser runs browse without --show against a PATH
// holding only a `which` and the browser openers each case provides:
// Composer asks `which` for xdg-open, then for open, runs the first found
// with the URL, and names the URL on stderr when there is neither. Each
// case exits 0 and writes nothing to stdout.
func TestHomeCommand_OpensBrowser(t *testing.T) {
	skipShellScripts(t)
	const url = "https://example.org"
	tests := []struct {
		name    string
		openers []string
		opened  string
		stderr  string
	}{
		{name: "xdg-open first", openers: []string{"xdg-open", "open"}, opened: "xdg-open " + url + "\n"},
		{name: "open without xdg-open", openers: []string{"open"}, opened: "open " + url + "\n"},
		{name: "neither", stderr: "No suitable browser opening command found, open yourself: " + url + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeProject(t, `{"repositories": []}`, url, "")
			bin := t.TempDir()
			log := filepath.Join(bin, "opened")
			scripts := map[string]string{"which": "#!/bin/sh\n[ -x \"" + bin + "/$1\" ]\n"}
			for _, name := range tt.openers {
				scripts[name] = "#!/bin/sh\necho \"" + name + " $*\" >> \"" + log + "\"\n"
			}
			for name, script := range scripts {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o777); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin)

			got := commandtest.GetApplicationTester(t).RunStreams("command", "browse", "packages", []string{"vendor/package"})
			if got.Err != nil {
				t.Fatal(got.Err)
			}
			if got.Code != 0 || got.Stdout != "" || got.Stderr != tt.stderr {
				t.Errorf("status %d, stdout %q, stderr %q; want 0, \"\", %q", got.Code, got.Stdout, got.Stderr, tt.stderr)
			}
			opened, err := os.ReadFile(log)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if string(opened) != tt.opened {
				t.Errorf("opened %q, want %q", opened, tt.opened)
			}
		})
	}
}
