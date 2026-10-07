// Ports tests/Composer/Test/Command/BumpCommandTest.php.

package command_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/locker"
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

// assertLockFresh checks that composer.lock's content-hash is the one
// composer.json has now: bump updates it with every rewrite.
func assertLockFresh(t *testing.T) {
	t.Helper()
	contents, err := os.ReadFile("./composer.json")
	if err != nil {
		t.Fatal(err)
	}
	want, err := locker.GetContentHash(string(contents))
	if err != nil {
		t.Fatal(err)
	}
	f, err := json.NewFile("./composer.lock", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := f.Read()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := lock.(*php.Array).Get("content-hash")
	if got != want {
		t.Errorf("composer.lock content-hash = %v, want %s", got, want)
	}
}

// The warnings bump writes to stderr for a root package that is not a
// project: the first for any such type, the other two when composer.json
// sets no "type" at all. The tester's formatter has no warning style (as
// in Composer's tests), so the tags stay.
const (
	bumpLibraryWarning = "<warning>Warning: Bumping dependency constraints is not recommended for libraries as it will narrow down your dependencies and may cause problems for your users.</warning>\n"
	bumpUntypedWarning = bumpLibraryWarning +
		"<warning>If your package is not a library, you can explicitly specify the \"type\" by using \"composer config type project\".</warning>\n" +
		"<warning>Alternatively you can use --dev-only to only bump dependencies within \"require-dev\".</warning>\n"
)

// bumpNoChanges and bumpUpdated are bump's summary on stdout.
const bumpNoChanges = "No requirements to update in ./composer.json.\n"

func bumpUpdated(changes int) string {
	return "./composer.json has been updated (" + strconv.Itoa(changes) + " changes).\n"
}

func TestBumpCommand_Bump(t *testing.T) {
	const (
		unbumped    = `{"require": {"first/pkg": "^2.0", "second/pkg": "3.*"}, "require-dev": {"dev/pkg": "~2.0"}}`
		devBumped   = `{"require": {"first/pkg": "^2.0", "second/pkg": "3.*"}, "require-dev": {"dev/pkg": "^2.3.4.5"}}`
		noDevBumped = `{"require": {"first/pkg": "^2.3.4", "second/pkg": "^3.4"}, "require-dev": {"dev/pkg": "~2.0"}}`
		bumped      = `{"require": {"first/pkg": "^2.3.4", "second/pkg": "^3.4"}, "require-dev": {"dev/pkg": "^2.3.4.5"}}`
	)
	tests := []struct {
		name         string
		composerJSON string
		command      []console.Param
		expected     string
		noLock       bool
		exitCode     int
		// stdout and stderr are the whole streams.
		stdout string
		stderr string
	}{
		{
			name:         "bump all by default",
			composerJSON: `{"require": {"first/pkg": "^v2.0", "second/pkg": "3.*"}, "require-dev": {"dev/pkg": "~2.0"}}`,
			expected:     bumped,
			stdout:       bumpUpdated(3),
			stderr:       bumpUntypedWarning,
		},
		{
			name:         "bump only dev with --dev-only",
			composerJSON: unbumped,
			command:      []console.Param{console.P("--dev-only", true)},
			expected:     devBumped,
			stdout:       bumpUpdated(1),
		},
		{
			name:         "bump only non-dev with --no-dev-only",
			composerJSON: unbumped,
			command:      []console.Param{console.P("--no-dev-only", true)},
			expected:     noDevBumped,
			stdout:       bumpUpdated(2),
			stderr:       bumpUntypedWarning,
		},
		{
			name:         "bump only listed with packages arg",
			composerJSON: unbumped,
			command:      []console.Param{console.P("packages", []string{"first/pkg:3.0.1", "dev/*"})},
			expected:     `{"require": {"first/pkg": "^2.3.4", "second/pkg": "3.*"}, "require-dev": {"dev/pkg": "^2.3.4.5"}}`,
			stdout:       bumpUpdated(2),
			stderr:       bumpUntypedWarning,
		},
		{
			name:         "bump works from installed repo without lock file",
			composerJSON: `{"require": {"first/pkg": "^2.0", "second/pkg": "3.*"}}`,
			expected:     `{"require": {"first/pkg": "^2.3.4", "second/pkg": "^3.4"}}`,
			noLock:       true,
			stdout:       bumpUpdated(2),
			stderr:       bumpUntypedWarning,
		},
		{
			name:         "bump with --dry-run with packages to bump",
			composerJSON: unbumped,
			command:      []console.Param{console.P("--dry-run", true)},
			expected:     unbumped,
			exitCode:     1,
			stdout: "./composer.json would be updated with:\n" +
				" - require.first/pkg: ^2.3.4\n" +
				" - require.second/pkg: ^3.4\n" +
				" - require-dev.dev/pkg: ^2.3.4.5\n",
			stderr: bumpUntypedWarning,
		},
		{
			name:         "bump with --dry-run without packages to bump",
			composerJSON: bumped,
			command:      []console.Param{console.P("--dry-run", true)},
			expected:     bumped,
			stdout:       bumpNoChanges,
			stderr:       bumpUntypedWarning,
		},
		{
			name:         "bump works with non-standard package",
			composerJSON: `{"require": {"php": ">=5.3", "first/pkg": "^2.3.4", "second/pkg": "^3.4"}, "require-dev": {"dev/pkg": "^2.3.4.5"}}`,
			expected:     `{"require": {"php": ">=5.3", "first/pkg": "^2.3.4", "second/pkg": "^3.4"}, "require-dev": {"dev/pkg": "^2.3.4.5"}}`,
			stdout:       bumpNoChanges,
			stderr:       bumpUntypedWarning,
		},
		{
			name:         "bump works with unknown package",
			composerJSON: `{"require": {"first/pkg": "^2.3.4", "second/pkg": "^3.4", "third/pkg": "^1.2"}}`,
			expected:     `{"require": {"first/pkg": "^2.3.4", "second/pkg": "^3.4", "third/pkg": "^1.2"}}`,
			stdout:       bumpNoChanges,
			stderr:       bumpUntypedWarning,
		},
		{
			name:         "bump works with aliased package",
			composerJSON: `{"require": {"first/pkg": "^2.3.4", "second/pkg": "dev-bugfix as 3.4.x-dev"}}`,
			expected:     `{"require": {"first/pkg": "^2.3.4", "second/pkg": "dev-bugfix as 3.4.x-dev"}}`,
			stdout:       bumpNoChanges,
			stderr:       bumpUntypedWarning,
		},
		// Beyond BumpCommandTest's provider:
		{
			name:         "-D is --dev-only",
			composerJSON: unbumped,
			command:      []console.Param{console.P("-D", true)},
			expected:     devBumped,
			stdout:       bumpUpdated(1),
		},
		{
			name:         "-R is --no-dev-only",
			composerJSON: unbumped,
			command:      []console.Param{console.P("-R", true)},
			expected:     noDevBumped,
			stdout:       bumpUpdated(2),
			stderr:       bumpUntypedWarning,
		},
		{
			name:         "packages arg matching nothing changes nothing",
			composerJSON: unbumped,
			command:      []console.Param{console.P("packages", []string{"nothing/*"})},
			expected:     unbumped,
			stdout:       bumpNoChanges,
			stderr:       bumpUntypedWarning,
		},
		{
			name:         "an explicit library type gets only the first warning",
			composerJSON: `{"type": "library", "require": {"first/pkg": "^2.0"}}`,
			expected:     `{"type": "library", "require": {"first/pkg": "^2.3.4"}}`,
			stdout:       bumpUpdated(1),
			stderr:       bumpLibraryWarning,
		},
		{
			name:         "a project gets no warning",
			composerJSON: `{"type": "project", "require": {"first/pkg": "^2.0"}}`,
			expected:     `{"type": "project", "require": {"first/pkg": "^2.3.4"}}`,
			stdout:       bumpUpdated(1),
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
			code, err := appTester.Run(params, commandtest.Options{CaptureStderrSeparately: true})
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr := appTester.Display(true), appTester.ErrorOutput(true)
			if code != tt.exitCode {
				t.Errorf("exit code = %d, want %d\n%s%s", code, tt.exitCode, stdout, stderr)
			}
			if stdout != tt.stdout {
				t.Errorf("stdout:\n%s\nwant:\n%s", stdout, tt.stdout)
			}
			if stderr != tt.stderr {
				t.Errorf("stderr:\n%s\nwant:\n%s", stderr, tt.stderr)
			}

			assertComposerJSON(t, tt.expected)
			if !tt.noLock {
				assertLockFresh(t)
			}
		})
	}
}

// assertBumpFails runs bump and checks that it fails with exit code 1 and
// reports want on stderr.
func assertBumpFails(t *testing.T, want string) {
	t.Helper()
	appTester := commandtest.GetApplicationTester(t)
	code, err := appTester.Run([]console.Param{console.P("command", "bump")}, commandtest.Options{CaptureStderrSeparately: true})
	if err != nil {
		t.Fatal(err)
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if out := appTester.ErrorOutput(false); !strings.Contains(out, want) {
		t.Errorf("error output %q, want %q", out, want)
	}
}

func TestBumpCommand_BumpFailsOnNonExistingComposerFile(t *testing.T) {
	dir := commandtest.InitTempComposer(t, nil, nil, nil, true)
	if err := os.Remove(filepath.Join(dir, "composer.json")); err != nil {
		t.Fatal(err)
	}

	assertBumpFails(t, "./composer.json is not readable.")
}

// The unreadable file is the one COMPOSER names (Factory::getComposerFile),
// as given.
func TestBumpCommand_BumpFailsOnNonExistingComposerEnvFile(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	t.Setenv("COMPOSER", "missing.json")

	assertBumpFails(t, "missing.json is not readable.")
}

func TestBumpCommand_BumpFailsOnWriteErrorToComposerFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Cannot run as root")
	}

	dir := commandtest.InitTempComposer(t, nil, nil, nil, true)
	if err := os.Chmod(filepath.Join(dir, "composer.json"), 0o444); err != nil {
		t.Fatal(err)
	}

	assertBumpFails(t, "./composer.json is not writable.")
}
