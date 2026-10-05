// Ports tests/Composer/Test/Command/BumpCommandTest.php.

package command_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

// assertComposerJSON is `assertSame($expected, (new JsonFile('./composer.json'))->read())`:
// both are compared through their JSON encoding, which keeps key order and
// types.
func assertComposerJSON(t *testing.T, expected string) {
	t.Helper()
	f, err := json.NewFile("./composer.json", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := f.Read()
	if err != nil {
		t.Fatal(err)
	}
	want, err := php.JSONEncode(commandtest.ToArray(t, expected), 0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := php.JSONEncode(actual, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want != got {
		t.Errorf("composer.json:\nwant %s\ngot  %s", want, got)
	}
}

func TestBumpCommand_Bump(t *testing.T) {
	tests := []struct {
		name         string
		composerJSON string
		command      []console.Param
		expected     string
		noLock       bool
		exitCode     int
	}{
		{
			name:         "bump all by default",
			composerJSON: `{"require": {"first/pkg": "^v2.0", "second/pkg": "3.*"}, "require-dev": {"dev/pkg": "~2.0"}}`,
			expected:     `{"require": {"first/pkg": "^2.3.4", "second/pkg": "^3.4"}, "require-dev": {"dev/pkg": "^2.3.4.5"}}`,
		},
		{
			name:         "bump only dev with --dev-only",
			composerJSON: `{"require": {"first/pkg": "^2.0", "second/pkg": "3.*"}, "require-dev": {"dev/pkg": "~2.0"}}`,
			command:      []console.Param{console.P("--dev-only", true)},
			expected:     `{"require": {"first/pkg": "^2.0", "second/pkg": "3.*"}, "require-dev": {"dev/pkg": "^2.3.4.5"}}`,
		},
		{
			name:         "bump only non-dev with --no-dev-only",
			composerJSON: `{"require": {"first/pkg": "^2.0", "second/pkg": "3.*"}, "require-dev": {"dev/pkg": "~2.0"}}`,
			command:      []console.Param{console.P("--no-dev-only", true)},
			expected:     `{"require": {"first/pkg": "^2.3.4", "second/pkg": "^3.4"}, "require-dev": {"dev/pkg": "~2.0"}}`,
		},
		{
			name:         "bump only listed with packages arg",
			composerJSON: `{"require": {"first/pkg": "^2.0", "second/pkg": "3.*"}, "require-dev": {"dev/pkg": "~2.0"}}`,
			command:      []console.Param{console.P("packages", []string{"first/pkg:3.0.1", "dev/*"})},
			expected:     `{"require": {"first/pkg": "^2.3.4", "second/pkg": "3.*"}, "require-dev": {"dev/pkg": "^2.3.4.5"}}`,
		},
		{
			name:         "bump works from installed repo without lock file",
			composerJSON: `{"require": {"first/pkg": "^2.0", "second/pkg": "3.*"}}`,
			expected:     `{"require": {"first/pkg": "^2.3.4", "second/pkg": "^3.4"}}`,
			noLock:       true,
		},
		{
			name:         "bump with --dry-run with packages to bump",
			composerJSON: `{"require": {"first/pkg": "^2.0", "second/pkg": "3.*"}, "require-dev": {"dev/pkg": "~2.0"}}`,
			command:      []console.Param{console.P("--dry-run", true)},
			expected:     `{"require": {"first/pkg": "^2.0", "second/pkg": "3.*"}, "require-dev": {"dev/pkg": "~2.0"}}`,
			exitCode:     1,
		},
		{
			name:         "bump with --dry-run without packages to bump",
			composerJSON: `{"require": {"first/pkg": "^2.3.4", "second/pkg": "^3.4"}, "require-dev": {"dev/pkg": "^2.3.4.5"}}`,
			command:      []console.Param{console.P("--dry-run", true)},
			expected:     `{"require": {"first/pkg": "^2.3.4", "second/pkg": "^3.4"}, "require-dev": {"dev/pkg": "^2.3.4.5"}}`,
		},
		{
			name:         "bump works with non-standard package",
			composerJSON: `{"require": {"php": ">=5.3", "first/pkg": "^2.3.4", "second/pkg": "^3.4"}, "require-dev": {"dev/pkg": "^2.3.4.5"}}`,
			expected:     `{"require": {"php": ">=5.3", "first/pkg": "^2.3.4", "second/pkg": "^3.4"}, "require-dev": {"dev/pkg": "^2.3.4.5"}}`,
		},
		{
			name:         "bump works with unknown package",
			composerJSON: `{"require": {"first/pkg": "^2.3.4", "second/pkg": "^3.4", "third/pkg": "^1.2"}}`,
			expected:     `{"require": {"first/pkg": "^2.3.4", "second/pkg": "^3.4", "third/pkg": "^1.2"}}`,
		},
		{
			name:         "bump works with aliased package",
			composerJSON: `{"require": {"first/pkg": "^2.3.4", "second/pkg": "dev-bugfix as 3.4.x-dev"}}`,
			expected:     `{"require": {"first/pkg": "^2.3.4", "second/pkg": "dev-bugfix as 3.4.x-dev"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, tt.composerJSON, nil, nil, true)

			packages := []pkg.PackageInterface{
				commandtest.GetPackage(t, "first/pkg", "2.3.4"),
				commandtest.GetPackage(t, "second/pkg", "3.4.0"),
			}
			devPackages := []pkg.PackageInterface{
				commandtest.GetPackage(t, "dev/pkg", "2.3.4.5"),
			}

			commandtest.CreateInstalledJSON(t, packages, devPackages, true)
			if !tt.noLock {
				commandtest.CreateComposerLock(t, packages, devPackages)
			}

			appTester := commandtest.GetApplicationTester(t)
			params := append([]console.Param{console.P("command", "bump")}, tt.command...)
			code, err := appTester.Run(params, commandtest.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if code != tt.exitCode {
				t.Errorf("exit code = %d, want %d\n%s", code, tt.exitCode, appTester.Display(true))
			}

			assertComposerJSON(t, tt.expected)
		})
	}
}

func TestBumpCommand_BumpFailsOnNonExistingComposerFile(t *testing.T) {
	dir := commandtest.InitTempComposer(t, nil, nil, nil, true)
	if err := os.Remove(filepath.Join(dir, "composer.json")); err != nil {
		t.Fatal(err)
	}

	appTester := commandtest.GetApplicationTester(t)
	code, err := appTester.Run([]console.Param{console.P("command", "bump")}, commandtest.Options{CaptureStderrSeparately: true})
	if err != nil {
		t.Fatal(err)
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if out := appTester.ErrorOutput(false); !strings.Contains(out, "./composer.json is not readable.") {
		t.Errorf("error output %q", out)
	}
}

func TestBumpCommand_BumpFailsOnWriteErrorToComposerFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Cannot run as root")
	}

	dir := commandtest.InitTempComposer(t, nil, nil, nil, true)
	if err := os.Chmod(filepath.Join(dir, "composer.json"), 0o444); err != nil {
		t.Fatal(err)
	}

	appTester := commandtest.GetApplicationTester(t)
	code, err := appTester.Run([]console.Param{console.P("command", "bump")}, commandtest.Options{CaptureStderrSeparately: true})
	if err != nil {
		t.Fatal(err)
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if out := appTester.ErrorOutput(false); !strings.Contains(out, "./composer.json is not writable.") {
		t.Errorf("error output %q", out)
	}
}
