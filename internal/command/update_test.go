// Ports tests/Composer/Test/Command/UpdateCommandTest.php.

package command_test

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
)

// matchesFormatPHPUnit is PHPUnit's assertStringMatchesFormat check.
func matchesFormatPHPUnit(t *testing.T, format, s string) bool {
	t.Helper()
	pattern := strings.NewReplacer(
		"%%", "%",
		"%e", `\/`,
		"%s", `[^\r\n]+`,
		"%S", `[^\r\n]*`,
		"%a", `.+`,
		"%A", `.*`,
		"%w", `\s*`,
		"%i", `[+-]?\d+`,
		"%d", `\d+`,
		"%x", `[0-9a-fA-F]+`,
		"%f", `[+-]?\.?\d+\.?\d*(?:[Ee][+-]?\d+)?`,
		"%c", `.`,
	).Replace(php.PregQuote(format, "/"))
	re, err := php.Compile("/^" + pattern + "$/s")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := re.IsMatch(strings.ReplaceAll(s, "\r\n", "\n"))

	return err == nil && ok
}

// assertDisplayMatchesFormat is
// assertStringMatchesFormat(trim($expected), trim($appTester->getDisplay(true))).
func assertDisplayMatchesFormat(t *testing.T, appTester *commandtest.ApplicationTester, expected string) {
	t.Helper()
	got := php.Trim(appTester.Display(true))
	if !matchesFormatPHPUnit(t, php.Trim(expected), got) {
		t.Errorf("output does not match format\nwant:\n%s\ngot:\n%s", php.Trim(expected), got)
	}
}

const updateRootDepAndTransitiveDep = `{
	"repositories": {"packages": {"type": "package", "package": [
		{"name": "root/req", "version": "1.0.0", "require": {"dep/pkg": "^1"}},
		{"name": "dep/pkg", "version": "1.0.0", "replace": {"replaced/pkg": "1.0.0"}},
		{"name": "dep/pkg", "version": "1.0.1", "replace": {"replaced/pkg": "1.0.1"}},
		{"name": "dep/pkg", "version": "1.0.2", "replace": {"replaced/pkg": "1.0.2"}}
	]}},
	"require": {"root/req": "1.*"}
}`

const updateTwoDepsThatMustBeUpdatedTogether = `{
	"type": "project",
	"repositories": {"packages": {"type": "package", "package": [
		{"name": "acme/foo", "version": "1.0.0"},
		{"name": "acme/foo", "version": "1.2.0"},
		{"name": "acme/bar", "version": "1.0.0", "require": {"acme/foo": "^1.0.0"}},
		{"name": "acme/bar", "version": "1.2.0", "require": {"acme/foo": "^1.2.0"}}
	]}},
	"require": {"acme/foo": "^1.0.0", "acme/bar": "^1.0.0"}
}`

func TestUpdateCommand_Update(t *testing.T) {
	tests := []struct {
		name               string
		composerJSON       string
		command            []console.Param
		expected           string
		createLockPackages []string // nil: no lock
	}{
		{
			"simple update", updateRootDepAndTransitiveDep, nil,
			`Loading composer repositories with package information
Updating dependencies
Lock file operations: 2 installs, 0 updates, 0 removals
  - Locking dep/pkg (1.0.2)
  - Locking root/req (1.0.0)
Installing dependencies from lock file (including require-dev)
Package operations: 2 installs, 0 updates, 0 removals
  - Installing dep/pkg (1.0.2)
  - Installing root/req (1.0.0)`, nil,
		},
		{
			"simple update with very verbose output", updateRootDepAndTransitiveDep,
			[]console.Param{console.P("-vv", true)},
			`Loading composer repositories with package information
Updating dependencies
Dependency resolution completed in %f seconds
Analyzed %d packages to resolve dependencies
Analyzed %d rules to resolve dependencies
Lock file operations: 2 installs, 0 updates, 0 removals
Installs: dep/pkg:1.0.2, root/req:1.0.0
  - Locking dep/pkg (1.0.2) from package repo (defining 4 packages)
  - Locking root/req (1.0.0) from package repo (defining 4 packages)
Installing dependencies from lock file (including require-dev)
Package operations: 2 installs, 0 updates, 0 removals
Installs: dep/pkg:1.0.2, root/req:1.0.0
  - Installing dep/pkg (1.0.2)
  - Installing root/req (1.0.0)`, nil,
		},
		{
			"update with temporary constraint + --no-install", updateRootDepAndTransitiveDep,
			[]console.Param{console.P("--with", []string{"dep/pkg:1.0.0"}), console.P("--no-install", true)},
			`Loading composer repositories with package information
Updating dependencies
Lock file operations: 2 installs, 0 updates, 0 removals
  - Locking dep/pkg (1.0.0)
  - Locking root/req (1.0.0)`, nil,
		},
		{
			"update with temporary constraint failing resolution", updateRootDepAndTransitiveDep,
			[]console.Param{console.P("--with", []string{"dep/pkg:^2"})},
			`Loading composer repositories with package information
Updating dependencies
Your requirements could not be resolved to an installable set of packages.

  Problem 1
    - Root composer.json requires root/req 1.* -> satisfiable by root/req[1.0.0].
    - root/req 1.0.0 requires dep/pkg ^1 -> found dep/pkg[1.0.0, 1.0.1, 1.0.2] but it conflicts with your temporary update constraint (dep/pkg:^2).`, nil,
		},
		{
			"update with temporary constraint failing resolution on root package", updateRootDepAndTransitiveDep,
			[]console.Param{console.P("--with", []string{"root/req:^2"})},
			"The temporary constraint \"^2\" for \"root/req\" must be a subset of the constraint in your composer.json (1.*)\nRun `composer require root/req` or `composer require root/req:^2` instead to replace the constraint", nil,
		},
		{
			"update & bump", updateRootDepAndTransitiveDep,
			[]console.Param{console.P("--bump-after-update", true)},
			`Loading composer repositories with package information
Updating dependencies
Lock file operations: 2 installs, 0 updates, 0 removals
  - Locking dep/pkg (1.0.2)
  - Locking root/req (1.0.0)
Installing dependencies from lock file (including require-dev)
Package operations: 2 installs, 0 updates, 0 removals
  - Installing dep/pkg (1.0.2)
  - Installing root/req (1.0.0)
Bumping dependencies
<warning>Warning: Bumping dependency constraints is not recommended for libraries as it will narrow down your dependencies and may cause problems for your users.</warning>
<warning>If your package is not a library, you can explicitly specify the "type" by using "composer config type project".</warning>
<warning>Alternatively you can use --bump-after-update=dev to only bump dependencies within "require-dev".</warning>
No requirements to update in ./composer.json.`,
			[]string{},
		},
		{
			"update & bump with lock", updateRootDepAndTransitiveDep,
			[]console.Param{console.P("--bump-after-update", true), console.P("--lock", true)},
			`Loading composer repositories with package information
Updating dependencies
Nothing to modify in lock file
Installing dependencies from lock file (including require-dev)
Nothing to install, update or remove`,
			[]string{},
		},
		{
			"update & bump dev only", updateRootDepAndTransitiveDep,
			[]console.Param{console.P("--bump-after-update", "dev")},
			`Loading composer repositories with package information
Updating dependencies
Lock file operations: 2 installs, 0 updates, 0 removals
  - Locking dep/pkg (1.0.2)
  - Locking root/req (1.0.0)
Installing dependencies from lock file (including require-dev)
Package operations: 2 installs, 0 updates, 0 removals
  - Installing dep/pkg (1.0.2)
  - Installing root/req (1.0.0)
Bumping dependencies
No requirements to update in ./composer.json.`,
			[]string{},
		},
		{
			"update & bump with failing update", updateRootDepAndTransitiveDep,
			[]console.Param{console.P("--with", []string{"dep/pkg:^2"}), console.P("--bump-after-update", true)},
			`Loading composer repositories with package information
Updating dependencies
Your requirements could not be resolved to an installable set of packages.

  Problem 1
    - Root composer.json requires root/req 1.* -> satisfiable by root/req[1.0.0].
    - root/req 1.0.0 requires dep/pkg ^1 -> found dep/pkg[1.0.0, 1.0.1, 1.0.2] but it conflicts with your temporary update constraint (dep/pkg:^2).`, nil,
		},
		{
			"update with replaced name filter fails to resolve", updateRootDepAndTransitiveDep,
			[]console.Param{console.P("--with", []string{"replaced/pkg:^2"})},
			`Loading composer repositories with package information
Updating dependencies
Your requirements could not be resolved to an installable set of packages.

  Problem 1
    - Root composer.json requires root/req 1.* -> satisfiable by root/req[1.0.0].
    - root/req 1.0.0 requires dep/pkg ^1 -> found dep/pkg[1.0.0, 1.0.1, 1.0.2] but it conflicts with your temporary update constraint (replaced/pkg:^2).`, nil,
		},
		{
			"update & bump without specifying package", updateTwoDepsThatMustBeUpdatedTogether,
			[]console.Param{console.P("--bump-after-update", true)},
			`Loading composer repositories with package information
Updating dependencies
Lock file operations: 0 installs, 2 updates, 0 removals
  - Upgrading acme/bar (1.0.0 => 1.2.0)
  - Upgrading acme/foo (1.0.0 => 1.2.0)
Installing dependencies from lock file (including require-dev)
Package operations: 2 installs, 0 updates, 0 removals
  - Installing acme/foo (1.2.0)
  - Installing acme/bar (1.2.0)
Bumping dependencies
./composer.json would be updated with:
 - require.acme/foo: ^1.2.0
 - require.acme/bar: ^1.2.0`,
			[]string{"acme/foo", "acme/bar"},
		},
		{
			`update & bump of single dependency without "--with-all-dependencies"`, updateTwoDepsThatMustBeUpdatedTogether,
			[]console.Param{console.P("packages", []string{"acme/bar:^1.2"}), console.P("--bump-after-update", true)},
			`Loading composer repositories with package information
Updating dependencies
Your requirements could not be resolved to an installable set of packages.

  Problem 1
    - Root composer.json requires acme/bar ^1.0.0 -> satisfiable by acme/bar[1.2.0].
    - acme/bar 1.2.0 requires acme/foo ^1.2.0 -> found acme/foo[1.2.0] but the package is fixed to 1.0.0 (lock file version) by a partial update and that version does not match. Make sure you list it as an argument for the update command.

Use the option --with-all-dependencies (-W) to allow upgrades, downgrades and removals for packages currently locked to specific versions.`,
			[]string{"acme/foo", "acme/bar"},
		},
		{
			`update & bump of single dependency with "--with-all-dependencies"`, updateTwoDepsThatMustBeUpdatedTogether,
			[]console.Param{console.P("packages", []string{"acme/bar:^1.2"}), console.P("--bump-after-update", true), console.P("--with-all-dependencies", true)},
			`Loading composer repositories with package information
Updating dependencies
Lock file operations: 0 installs, 2 updates, 0 removals
  - Upgrading acme/bar (1.0.0 => 1.2.0)
  - Upgrading acme/foo (1.0.0 => 1.2.0)
Installing dependencies from lock file (including require-dev)
Package operations: 2 installs, 0 updates, 0 removals
  - Installing acme/foo (1.2.0)
  - Installing acme/bar (1.2.0)
Bumping dependencies
./composer.json would be updated with:
 - require.acme/foo: ^1.2.0
 - require.acme/bar: ^1.2.0`,
			[]string{"acme/foo", "acme/bar"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, tt.composerJSON, nil, nil, true)

			if tt.createLockPackages != nil {
				packages := make([]pkg.PackageInterface, len(tt.createLockPackages))
				for i, name := range tt.createLockPackages {
					packages[i] = commandtest.GetPackage(t, name, "1.0.0")
				}
				commandtest.CreateComposerLock(t, packages, nil)
			}

			appTester := commandtest.GetApplicationTester(t)
			params := append([]console.Param{console.P("command", "update"), console.P("--dry-run", true), console.P("--no-audit", true)}, tt.command...)
			_, _ = appTester.Run(params, commandtest.Options{})

			assertDisplayMatchesFormat(t, appTester, tt.expected)
		})
	}
}

func TestUpdateCommand_UpdateWithPatchOnly(t *testing.T) {
	commandtest.InitTempComposer(t, `{
		"repositories": {"packages": {"type": "package", "package": [
			{"name": "root/req", "version": "1.0.0"},
			{"name": "root/req", "version": "1.0.1"},
			{"name": "root/req", "version": "1.1.0"},
			{"name": "root/req2", "version": "1.0.0"},
			{"name": "root/req2", "version": "1.0.1"},
			{"name": "root/req2", "version": "1.1.0"},
			{"name": "root/req3", "version": "1.0.0"},
			{"name": "root/req3", "version": "1.0.1"},
			{"name": "root/req3", "version": "1.1.0"}
		]}},
		"require": {"root/req": "1.*", "root/req2": "1.*", "root/req3": "1.*"}
	}`, nil, nil, true)

	commandtest.CreateComposerLock(t, []pkg.PackageInterface{
		commandtest.GetPackage(t, "root/req", "1.0.0"),
		commandtest.GetPackage(t, "root/req2", "1.0.0"),
		commandtest.GetPackage(t, "root/req3", "1.0.0"),
	}, nil)

	appTester := commandtest.GetApplicationTester(t)
	// root/req fails because of incompatible --with requirement
	_, _ = appTester.RunArgs(commandtest.Options{}, "command", "update", "--dry-run", true, "--no-audit", true, "--no-install", true, "--patch-only", true, "--with", []string{"root/req:^1.1"})

	assertDisplayMatchesFormat(t, appTester, `Loading composer repositories with package information
Updating dependencies
Your requirements could not be resolved to an installable set of packages.

  Problem 1
    - Root composer.json requires root/req 1.*, found root/req[1.0.0, 1.0.1, 1.1.0] but it conflicts with your temporary update constraint (root/req:[[>= 1.1.0.0-dev < 2.0.0.0-dev] [>= 1.0.0.0-dev < 1.1.0.0-dev]]).`)

	appTester = commandtest.GetApplicationTester(t)
	// root/req upgrades to 1.0.1 as that is compatible with the --with requirement now
	// root/req2 upgrades to 1.0.1 only due to --patch-only
	// root/req3 does not update as it is not in the allowlist
	_, _ = appTester.RunArgs(commandtest.Options{}, "command", "update", "--dry-run", true, "--no-audit", true, "--no-install", true, "--patch-only", true, "--with", []string{"root/req:^1.0.1"}, "packages", []string{"root/req", "root/req2"})

	assertDisplayMatchesFormat(t, appTester, `Loading composer repositories with package information
Updating dependencies
Lock file operations: 0 installs, 2 updates, 0 removals
  - Upgrading root/req (1.0.0 => 1.0.1)
  - Upgrading root/req2 (1.0.0 => 1.0.1)`)
}

func assertExceptionMessage(t *testing.T, err error, message string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an exception with message %q", message)
	}
	if !strings.Contains(err.Error(), message) {
		t.Fatalf("exception message %q does not contain %q", err.Error(), message)
	}
}

func TestUpdateCommand_InteractiveModeThrowsIfNoPackageToUpdate(t *testing.T) {
	commandtest.InitTempComposer(t, `{
		"repositories": {"packages": {"type": "package", "package": [{"name": "root/req", "version": "1.0.0"}]}},
		"require": {"root/req": "1.*"}
	}`, nil, nil, true)
	commandtest.CreateComposerLock(t, []pkg.PackageInterface{commandtest.GetPackage(t, "root/req", "1.0.0")}, nil)

	appTester := commandtest.GetApplicationTester(t)
	appTester.SetInputs("")
	_, err := appTester.RunArgs(commandtest.Options{}, "command", "update", "--interactive", true)
	assertExceptionMessage(t, err, "Could not find any package with new versions available")
}

func TestUpdateCommand_InteractiveModeThrowsIfNoPackageEntered(t *testing.T) {
	commandtest.InitTempComposer(t, `{
		"repositories": {"packages": {"type": "package", "package": [
			{"name": "root/req", "version": "1.0.0"},
			{"name": "root/req", "version": "1.0.1"}
		]}},
		"require": {"root/req": "1.*"}
	}`, nil, nil, true)
	commandtest.CreateComposerLock(t, []pkg.PackageInterface{commandtest.GetPackage(t, "root/req", "1.0.0")}, nil)

	appTester := commandtest.GetApplicationTester(t)
	appTester.SetInputs("")
	_, err := appTester.RunArgs(commandtest.Options{}, "command", "update", "--interactive", true)
	assertExceptionMessage(t, err, `No package named "" is installed.`)
}

func TestUpdateCommand_InteractiveTmp(t *testing.T) {
	tests := []struct {
		packageNames [][2]string
		expected     string
	}{
		{
			[][2]string{{"dep/pkg", "1.0.1"}},
			`Lock file operations: 1 install, 1 update, 0 removals
  - Locking another-dep/pkg (1.0.2)
  - Upgrading dep/pkg (1.0.1 => 1.0.2)
Installing dependencies from lock file (including require-dev)
Package operations: 1 install, 1 update, 0 removals
  - Upgrading dep/pkg (1.0.1 => 1.0.2)
  - Installing another-dep/pkg (1.0.2)`,
		},
		{
			[][2]string{{"dep/pkg", "1.0.1"}, {"another-dep/pkg", "1.0.2"}},
			`Lock file operations: 0 installs, 1 update, 0 removals
  - Upgrading dep/pkg (1.0.1 => 1.0.2)
Installing dependencies from lock file (including require-dev)
Package operations: 0 installs, 1 update, 0 removals
  - Upgrading dep/pkg (1.0.1 => 1.0.2)`,
		},
	}
	for i, tt := range tests {
		t.Run(php.ToString(i), func(t *testing.T) {
			commandtest.InitTempComposer(t, `{
				"repositories": {"packages": {"type": "package", "package": [
					{"name": "root/req", "version": "1.0.0", "require": {"dep/pkg": "^1"}},
					{"name": "dep/pkg", "version": "1.0.0"},
					{"name": "dep/pkg", "version": "1.0.1"},
					{"name": "dep/pkg", "version": "1.0.2"},
					{"name": "another-dep/pkg", "version": "1.0.2"}
				]}},
				"require": {"root/req": "1.*"}
			}`, nil, nil, true)

			rootPackage := commandtest.GetPackage(t, "root/req", "1.0.0")
			packages := []pkg.PackageInterface{rootPackage}
			inputs := make([]string, 0, len(tt.packageNames)+2)
			for _, nv := range tt.packageNames {
				packages = append(packages, commandtest.GetPackage(t, nv[0], nv[1]))
				inputs = append(inputs, nv[0])
			}

			rootPackage.SetRequires(pkg.LinksOf(
				pkg.NewLink("root/req", "dep/pkg", semver.NewMatchAllConstraint(), pkg.TypeRequire, pkg.Str("^1")),
				pkg.NewLink("root/req", "another-dep/pkg", semver.NewMatchAllConstraint(), pkg.TypeRequire, pkg.Str("^1")),
			))

			commandtest.CreateComposerLock(t, packages, nil)
			commandtest.CreateInstalledJSON(t, packages, nil, true)

			appTester := commandtest.GetApplicationTester(t)
			appTester.SetInputs(append(inputs, "", "yes")...)
			if _, err := appTester.RunArgs(commandtest.Options{}, "command", "update", "--interactive", true, "--no-audit", true, "--dry-run", true); err != nil {
				t.Fatalf("unexpected exception: %v", err)
			}

			display := php.Trim(appTester.Display(true))
			if !strings.HasSuffix(display, php.Trim(tt.expected)) {
				t.Errorf("output does not end with\n%s\ngot:\n%s", tt.expected, display)
			}
		})
	}
}

func TestUpdateCommand_NoSecurityBlockingAllowsInsecurePackages(t *testing.T) {
	commandtest.InitTempComposer(t, `{
		"repositories": {"packages": {
			"type": "package",
			"package": [
				{"name": "vulnerable/pkg", "version": "1.0.0"},
				{"name": "vulnerable/pkg", "version": "1.1.0"}
			],
			"security-advisories": {"vulnerable/pkg": [{
				"advisoryId": "PKSA-test-001",
				"packageName": "vulnerable/pkg",
				"remoteId": "CVE-2024-1234",
				"title": "Test Security Vulnerability",
				"link": "https://example.com/advisory",
				"cve": "CVE-2024-1234",
				"affectedVersions": ">=1.1.0,<2.0.0",
				"source": "test",
				"reportedAt": "2024-01-01 00:00:00",
				"composerRepository": "Package Repository",
				"severity": "high",
				"sources": [{"name": "test", "remoteId": "CVE-2024-1234"}]
			}]}
		}},
		"require": {"vulnerable/pkg": "^1.0"}
	}`, nil, nil, true)

	// Test 1: Without --no-security-blocking, the vulnerable version 1.1.0 should be filtered out
	appTester := commandtest.GetApplicationTester(t)
	_, _ = appTester.RunArgs(commandtest.Options{}, "command", "update", "--dry-run", true, "--no-audit", true, "--no-install", true)

	display := appTester.Display(true)
	// Should lock the secure version 1.0.0, not the vulnerable 1.1.0
	assertDisplayContains(t, display, "Locking vulnerable/pkg (1.0.0)", true)
	assertDisplayContains(t, display, "Locking vulnerable/pkg (1.1.0)", false)

	// Test 2: With --no-security-blocking, the vulnerable version 1.1.0 should be allowed
	appTester = commandtest.GetApplicationTester(t)
	_, _ = appTester.RunArgs(commandtest.Options{}, "command", "update", "--dry-run", true, "--no-audit", true, "--no-install", true, "--no-security-blocking", true)

	display = appTester.Display(true)
	// Should lock the latest version 1.1.0 even though it's vulnerable
	assertDisplayContains(t, display, "Locking vulnerable/pkg (1.1.0)", true)
	assertDisplayContains(t, display, "Locking vulnerable/pkg (1.0.0)", false)
}

func assertDisplayContains(t *testing.T, display, needle string, contains bool) {
	t.Helper()
	if strings.Contains(display, needle) != contains {
		t.Errorf("display contains %q = %v, want %v\n%s", needle, !contains, contains, display)
	}
}

func TestUpdateCommand_NoBlockingAllowsMalwareFlaggedPackages(t *testing.T) {
	commandtest.InitTempComposer(t, `{
		"repositories": {"packages": {
			"type": "package",
			"package": [
				{"name": "malicious/pkg", "version": "1.0.0"},
				{"name": "malicious/pkg", "version": "1.1.0"}
			],
			"filter": {"malware": [{"package": "malicious/pkg", "constraint": ">=1.1.0", "reason": "malware"}]}
		}},
		"require": {"malicious/pkg": "^1.0"}
	}`, nil, nil, true)

	// Without --no-blocking, the malware-flagged 1.1.0 must be filtered out.
	appTester := commandtest.GetApplicationTester(t)
	_, _ = appTester.RunArgs(commandtest.Options{}, "command", "update", "--dry-run", true, "--no-audit", true, "--no-install", true)

	display := appTester.Display(true)
	assertDisplayContains(t, display, "Locking malicious/pkg (1.0.0)", true)
	assertDisplayContains(t, display, "Locking malicious/pkg (1.1.0)", false)

	// With --no-blocking, the malware-flagged 1.1.0 must come through.
	appTester = commandtest.GetApplicationTester(t)
	_, _ = appTester.RunArgs(commandtest.Options{}, "command", "update", "--dry-run", true, "--no-audit", true, "--no-install", true, "--no-blocking", true)

	display = appTester.Display(true)
	assertDisplayContains(t, display, "Locking malicious/pkg (1.1.0)", true)
	assertDisplayContains(t, display, "Locking malicious/pkg (1.0.0)", false)
}

func TestUpdateCommand_BumpAfterUpdateWithoutLockfile(t *testing.T) {
	commandtest.InitTempComposer(t, `{
		"repositories": {"packages": {"type": "package", "package": [
			{"name": "root/a", "version": "1.0.0"},
			{"name": "root/a", "version": "1.1.0"}
		]}},
		"require-dev": {"root/a": "^1.0.0"},
		"config": {"lock": false}
	}`, nil, nil, true)

	appTester := commandtest.GetApplicationTester(t)
	_, _ = appTester.RunArgs(commandtest.Options{}, "command", "update", "--dry-run", true, "--no-audit", true, "--bump-after-update", "dev")

	assertDisplayMatchesFormat(t, appTester, `Loading composer repositories with package information
Updating dependencies
Package operations: 1 install, 0 updates, 0 removals
  - Installing root/a (1.1.0)
Bumping dependencies
./composer.json would be updated with:
 - require-dev.root/a: ^1.1.0`)
}

func TestUpdateCommand_UpdateWithTemporaryConstraintUsingWildcard(t *testing.T) {
	commandtest.InitTempComposer(t, `{
		"repositories": {"packages": {"type": "package", "package": [
			{"name": "root/a", "version": "1.0.0"},
			{"name": "root/a", "version": "2.0.0"},
			{"name": "root/ab", "version": "1.0.0"},
			{"name": "root/ab", "version": "2.0.0"},
			{"name": "root/abc", "version": "1.0.0"},
			{"name": "root/abc", "version": "2.0.0"}
		]}},
		"require": {"root/a": "^1 || ^2", "root/ab": "^1 || ^2", "root/abc": "^1 || ^2"}
	}`, nil, nil, true)

	commandtest.CreateComposerLock(t, []pkg.PackageInterface{
		commandtest.GetPackage(t, "root/a", "2.0.0"),
		commandtest.GetPackage(t, "root/ab", "2.0.0"),
		commandtest.GetPackage(t, "root/abc", "2.0.0"),
	}, nil)

	appTester := commandtest.GetApplicationTester(t)
	_, _ = appTester.RunArgs(commandtest.Options{}, "command", "update", "--dry-run", true, "--no-audit", true, "--no-install", true, "--with", []string{"root/*:^1"})

	assertDisplayMatchesFormat(t, appTester, `Loading composer repositories with package information
Updating dependencies
Lock file operations: 0 installs, 3 updates, 0 removals
  - Downgrading root/a (2.0.0 => 1.0.0)
  - Downgrading root/ab (2.0.0 => 1.0.0)
  - Downgrading root/abc (2.0.0 => 1.0.0)`)

	appTester = commandtest.GetApplicationTester(t)
	_, _ = appTester.RunArgs(commandtest.Options{}, "command", "update", "--dry-run", true, "--no-audit", true, "--no-install", true, "--with", []string{"root/ab*:^1"})

	assertDisplayMatchesFormat(t, appTester, `Loading composer repositories with package information
Updating dependencies
Lock file operations: 0 installs, 2 updates, 0 removals
  - Downgrading root/ab (2.0.0 => 1.0.0)
  - Downgrading root/abc (2.0.0 => 1.0.0)`)
}

func TestUpdateCommand_UpdateWithTemporaryConstraintWildcardFailsIntersection(t *testing.T) {
	commandtest.InitTempComposer(t, `{
		"repositories": {"packages": {"type": "package", "package": [
			{"name": "root/a", "version": "1.0.0"},
			{"name": "root/a", "version": "2.0.0"},
			{"name": "root/ab", "version": "1.0.0"},
			{"name": "root/ab", "version": "2.0.0"}
		]}},
		"require": {"root/a": "^1", "root/ab": "^1"}
	}`, nil, nil, true)

	commandtest.CreateComposerLock(t, []pkg.PackageInterface{
		commandtest.GetPackage(t, "root/a", "1.0.0"),
		commandtest.GetPackage(t, "root/ab", "1.0.0"),
	}, nil)

	appTester := commandtest.GetApplicationTester(t)
	_, _ = appTester.RunArgs(commandtest.Options{}, "command", "update", "--dry-run", true, "--no-audit", true, "--no-install", true, "--with", []string{"root/*:^2"})

	assertDisplayMatchesFormat(t, appTester, `The temporary constraint "^2" for "root/*" matching "root/a" must be a subset of the constraint in your composer.json (^1)`)
}

func TestUpdateCommand_UpdateWithTemporaryConstraintWildcardMatchingNothing(t *testing.T) {
	commandtest.InitTempComposer(t, `{
		"repositories": {"packages": {"type": "package", "package": [
			{"name": "root/a", "version": "1.0.0"},
			{"name": "root/a", "version": "2.0.0"}
		]}},
		"require": {"root/a": "^1 || ^2"}
	}`, nil, nil, true)

	commandtest.CreateComposerLock(t, []pkg.PackageInterface{commandtest.GetPackage(t, "root/a", "2.0.0")}, nil)

	appTester := commandtest.GetApplicationTester(t)
	_, _ = appTester.RunArgs(commandtest.Options{}, "command", "update", "--dry-run", true, "--no-audit", true, "--no-install", true, "--with", []string{"other/*:^1"})

	assertDisplayMatchesFormat(t, appTester, `Loading composer repositories with package information
Updating dependencies
Nothing to modify in lock file`)
}
