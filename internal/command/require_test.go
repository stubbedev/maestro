// Ports tests/Composer/Test/Command/RequireCommandTest.php.

package command_test

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
)

// assertRequireInvalidArgument checks a PHP InvalidArgumentException (or a
// subclass) with the given message.
func assertRequireInvalidArgument(t *testing.T, err error, message string) {
	t.Helper()
	if !phperr.InstanceOf(err, command.ClassInvalidArgument) {
		t.Fatalf("expected InvalidArgumentException, got %T %v", err, err)
	}
	if err.Error() != message {
		t.Errorf("message:\n%q\nwant\n%q", err.Error(), message)
	}
}

func TestRequireCommand_RequireThrowsIfNoneMatches(t *testing.T) {
	commandtest.InitTempComposer(t, `{"repositories": {"packages": {"type": "package", "package": [
		{"name": "required/pkg", "version": "1.0.0", "require": {"ext-foobar": "^1"}}]}}}`, nil, nil, true)

	appTester := commandtest.GetApplicationTester(t)
	_, err := appTester.RunArgs(commandtest.Options{}, "command", "require", "--dry-run", true, "--no-audit", true, "packages", []string{"required/pkg"})
	assertRequireInvalidArgument(t, err, "Package required/pkg has requirements incompatible with your PHP version, PHP extensions and Composer version:"+php.EOL+
		"  - required/pkg 1.0.0 requires ext-foobar ^1 but it is not present.")
}

func TestRequireCommand_RequireThrowsOnUnquotedInlineAlias(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)

	appTester := commandtest.GetApplicationTester(t)
	_, err := appTester.RunArgs(commandtest.Options{}, "command", "require", "--dry-run", true, "--no-audit", true, "packages", []string{"required/pkg", "dev-main", "as", "1.2.x-dev"})
	assertRequireInvalidArgument(t, err, `Cannot use "as" as a separate argument. Quote the inline alias as one argument, e.g. "vendor/package:dev-main as 1.2.x-dev".`)
}

func TestRequireCommand_RequireWarnsIfResolvedToFeatureBranch(t *testing.T) {
	commandtest.InitTempComposer(t, `{"repositories": {"packages": {"type": "package", "package": [
		{"name": "required/pkg", "version": "2.0.0", "require": {"common/dep": "^1"}},
		{"name": "required/pkg", "version": "dev-foo-bar", "require": {"common/dep": "^2"}},
		{"name": "common/dep", "version": "2.0.0"}]}},
		"require": {"common/dep": "^2.0"},
		"minimum-stability": "dev",
		"prefer-stable": true}`, nil, nil, true)

	appTester := commandtest.GetApplicationTester(t)
	appTester.SetInputs("n")
	interactive := true
	if _, err := appTester.RunArgs(commandtest.Options{Interactive: &interactive}, "command", "require", "--dry-run", true, "--no-audit", true, "packages", []string{"required/pkg"}); err != nil {
		t.Fatal(err)
	}
	want := `./composer.json has been updated
Running composer update required/pkg
Loading composer repositories with package information
Updating dependencies
Lock file operations: 2 installs, 0 updates, 0 removals
  - Locking common/dep (2.0.0)
  - Locking required/pkg (dev-foo-bar)
Installing dependencies from lock file (including require-dev)
Package operations: 2 installs, 0 updates, 0 removals
  - Installing common/dep (2.0.0)
  - Installing required/pkg (dev-foo-bar)
Using version dev-foo-bar for required/pkg
<warning>Version dev-foo-bar looks like it may be a feature branch which is unlikely to keep working in the long run and may be in an unstable state</warning>
Are you sure you want to use this constraint (y) or would you rather abort (n) the whole operation [y,n]? ` + `
Installation failed, reverting ./composer.json to its original content.
`
	if got := appTester.Display(true); got != want {
		t.Errorf("output:\n%s\nwant:\n%s", got, want)
	}
}

const requireThreeVersionsRepo = `"repositories": {"packages": {"type": "package", "package": [
	{"name": "required/pkg", "version": "1.2.0", "require": {"ext-foobar": "^1"}},
	{"name": "required/pkg", "version": "1.1.0", "require": {"ext-foobar": "^1"}},
	{"name": "required/pkg", "version": "1.0.0"}]}}`

const requirePhpRepo = `"repositories": {"packages": {"type": "package", "package": [
	{"name": "required/pkg", "version": "1.1.0", "require": {"php": "^20"}},
	{"name": "required/pkg", "version": "1.0.0", "require": {"php": ">=7"}}]}}`

func TestRequireCommand_Require(t *testing.T) {
	tests := []struct {
		name         string
		composerJSON string
		command      []any
		expected     string
	}{
		{
			name:         "warn once for missing ext but a lower package matches",
			composerJSON: `{` + requireThreeVersionsRepo + `}`,
			command:      []any{"packages", []string{"required/pkg"}},
			expected: `<warning>Cannot use required/pkg's latest version 1.2.0 as it requires ext-foobar ^1 which is missing from your platform.
./composer.json has been updated
Running composer update required/pkg
Loading composer repositories with package information
Updating dependencies
Lock file operations: 1 install, 0 updates, 0 removals
  - Locking required/pkg (1.0.0)
Installing dependencies from lock file (including require-dev)
Package operations: 1 install, 0 updates, 0 removals
  - Installing required/pkg (1.0.0)
Using version ^1.0 for required/pkg`,
		},
		{
			name:         "warn multiple times when verbose",
			composerJSON: `{` + requireThreeVersionsRepo + `}`,
			command:      []any{"packages", []string{"required/pkg"}, "--no-install", true, "-v", true},
			expected: `<warning>Cannot use required/pkg's latest version 1.2.0 as it requires ext-foobar ^1 which is missing from your platform.
<warning>Cannot use required/pkg 1.1.0 as it requires ext-foobar ^1 which is missing from your platform.
./composer.json has been updated
Running composer update required/pkg
Loading composer repositories with package information
Updating dependencies
Dependency resolution completed in %d seconds
Analyzed %d packages to resolve dependencies
Analyzed %d rules to resolve dependencies
Lock file operations: 1 install, 0 updates, 0 removals
Installs: required/pkg:1.0.0
  - Locking required/pkg (1.0.0)
Using version ^1.0 for required/pkg`,
		},
		{
			name:         "warn for not satisfied req which is satisfied by lower version",
			composerJSON: `{` + requirePhpRepo + `}`,
			command:      []any{"packages", []string{"required/pkg"}, "--no-install", true},
			expected: `<warning>Cannot use required/pkg's latest version 1.1.0 as it requires php ^20 which is not satisfied by your platform.
./composer.json has been updated
Running composer update required/pkg
Loading composer repositories with package information
Updating dependencies
Lock file operations: 1 install, 0 updates, 0 removals
  - Locking required/pkg (1.0.0)
Using version ^1.0 for required/pkg`,
		},
		{
			name:         "version selection happens early even if not completely accurate if no update is requested",
			composerJSON: `{` + requirePhpRepo + `}`,
			command:      []any{"packages", []string{"required/pkg"}, "--no-update", true},
			expected: `<warning>Cannot use required/pkg's latest version 1.1.0 as it requires php ^20 which is not satisfied by your platform.
Using version ^1.0 for required/pkg
./composer.json has been updated`,
		},
		{
			name: "pick best matching version when not provided",
			composerJSON: `{"repositories": {"packages": {"type": "package", "package": [
				{"name": "existing/dep", "version": "1.1.0", "require": {"required/pkg": "^1"}},
				{"name": "required/pkg", "version": "2.0.0"},
				{"name": "required/pkg", "version": "1.1.0"},
				{"name": "required/pkg", "version": "1.0.0"}]}},
				"require": {"existing/dep": "^1"}}`,
			command: []any{"packages", []string{"required/pkg"}, "--no-install", true},
			expected: `./composer.json has been updated
Running composer update required/pkg
Loading composer repositories with package information
Updating dependencies
Lock file operations: 2 installs, 0 updates, 0 removals
  - Locking existing/dep (1.1.0)
  - Locking required/pkg (1.1.0)
Using version ^1.1 for required/pkg`,
		},
		{
			name: "use exact constraint with --fixed",
			composerJSON: `{"type": "project", "repositories": {"packages": {"type": "package", "package": [
				{"name": "required/pkg", "version": "1.1.0"}]}}}`,
			command: []any{"packages", []string{"required/pkg"}, "--no-install", true, "--fixed", true},
			expected: `./composer.json has been updated
Running composer update required/pkg
Loading composer repositories with package information
Updating dependencies
Lock file operations: 1 install, 0 updates, 0 removals
  - Locking required/pkg (1.1.0)
Using version 1.1.0 for required/pkg`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, tt.composerJSON, nil, nil, true)

			appTester := commandtest.GetApplicationTester(t)
			args := append([]any{"command", "require", "--dry-run", true, "--no-audit", true}, tt.command...)
			if _, err := appTester.RunArgs(commandtest.Options{}, args...); err != nil {
				t.Fatalf("%v\n%s", err, appTester.Display(true))
			}

			got := strings.TrimSpace(appTester.Display(true))
			expected := strings.TrimSpace(tt.expected)
			if strings.Contains(expected, "%d") {
				pattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(expected), "%d", "[0-9.]+") + "$"
				if !regexp.MustCompile(pattern).MatchString(got) {
					t.Errorf("output:\n%s\ndoes not match:\n%s", got, expected)
				}
			} else if got != expected {
				t.Errorf("output:\n%s\nwant:\n%s", got, expected)
			}
		})
	}
}

func TestRequireCommand_InconsistentRequireKeys(t *testing.T) {
	tests := []struct {
		isDev, isInteractive bool
		expectedWarning      string
	}{
		{true, false, "<warning>required/pkg is currently present in the require key and you ran the command with the --dev flag, which will move it to the require-dev key.</warning>"},
		{false, false, "<warning>required/pkg is currently present in the require-dev key and you ran the command without the --dev flag, which will move it to the require key.</warning>"},
		{true, true, "<warning>required/pkg is currently present in the require key and you ran the command with the --dev flag, which will move it to the require-dev key.</warning>"},
		{false, true, "<warning>required/pkg is currently present in the require-dev key and you ran the command without the --dev flag, which will move it to the require key.</warning>"},
	}
	for i, tt := range tests {
		t.Run(string(rune('0'+i)), func(t *testing.T) {
			currentKey, otherKey := "require-dev", "require"
			if tt.isDev {
				currentKey, otherKey = otherKey, currentKey
			}

			dir := commandtest.InitTempComposer(t, `{"repositories": {"packages": {"type": "package", "package": [
				{"name": "required/pkg", "version": "1.0.0"}]}},
				"`+currentKey+`": {"required/pkg": "^1.0"}}`, nil, nil, true)

			p := commandtest.GetPackage(t, "required/pkg", "1.0.0")
			if tt.isDev {
				commandtest.CreateComposerLock(t, nil, []pkg.PackageInterface{p})
				commandtest.CreateInstalledJSON(t, nil, []pkg.PackageInterface{p}, true)
			} else {
				commandtest.CreateComposerLock(t, []pkg.PackageInterface{p}, nil)
				commandtest.CreateInstalledJSON(t, []pkg.PackageInterface{p}, nil, true)
			}

			appTester := commandtest.GetApplicationTester(t)
			command := []any{
				"command", "require",
				"--no-audit", true,
				"--dev", tt.isDev,
				"--no-install", true,
				"packages",
				[]string{"required/pkg"},
			}

			if tt.isInteractive {
				appTester.SetInputs("yes")
			} else {
				command = append(command, "--no-interaction", true)
			}

			if _, err := appTester.RunArgs(commandtest.Options{}, command...); err != nil {
				t.Fatalf("%v\n%s", err, appTester.Display(true))
			}

			if out := appTester.Display(true); !strings.Contains(out, tt.expectedWarning) {
				t.Errorf("output does not contain %q:\n%s", tt.expectedWarning, out)
			}

			composerContent := readProjectJSON(t, filepath.Join(dir, "composer.json"))
			if !composerContent.Has(otherKey) {
				t.Errorf("composer.json has no %s key", otherKey)
			}
			if composerContent.Has(currentKey) {
				t.Errorf("composer.json still has the %s key", currentKey)
			}
		})
	}
}
