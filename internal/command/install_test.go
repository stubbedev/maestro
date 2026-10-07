// Ports tests/Composer/Test/Command/InstallCommandTest.php.

package command_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

// runAppTester runs the application tester and fails the test on an exception.
func runAppTester(t *testing.T, appTester *commandtest.ApplicationTester, params ...console.Param) int {
	t.Helper()
	code, err := appTester.Run(params, commandtest.Options{})
	if err != nil {
		t.Fatalf("unexpected exception: %v", err)
	}

	return code
}

func assertTrimmedDisplay(t *testing.T, appTester *commandtest.ApplicationTester, expected string) {
	t.Helper()
	if got := php.Trim(appTester.Display(true)); got != php.Trim(expected) {
		t.Errorf("output mismatch\nwant:\n%s\ngot:\n%s", php.Trim(expected), got)
	}
}

func TestInstallCommand_InstallCommandErrors(t *testing.T) {
	tests := []struct {
		name     string
		command  []console.Param
		expected string
	}{
		{
			"it writes an error when the dev flag is passed",
			[]console.Param{console.P("--dev", true)},
			`<warning>You are using the deprecated option "--dev". It has no effect and will break in Composer 3.</warning>
Installing dependencies from lock file (including require-dev)
Verifying lock file contents can be installed on current platform.
Nothing to install, update or remove
Generating autoload files`,
		},
		{
			"it writes an error when no-suggest flag passed",
			[]console.Param{console.P("--no-suggest", true)},
			`<warning>You are using the deprecated option "--no-suggest". It has no effect and will break in Composer 3.</warning>
Installing dependencies from lock file (including require-dev)
Verifying lock file contents can be installed on current platform.
Nothing to install, update or remove
Generating autoload files`,
		},
		{
			"it writes an error when packages passed",
			[]console.Param{console.P("packages", []string{"vendor/package"})},
			`Invalid argument vendor/package. Use "composer require vendor/package" instead to add packages to your composer.json.`,
		},
		{
			"it writes an error when no-install flag is passed",
			[]console.Param{console.P("--no-install", true)},
			`Invalid option "--no-install". Use "composer update --no-install" instead if you are trying to update the composer.lock file.`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, `{"repositories": []}`, nil, nil, true)

			packages := []pkg.PackageInterface{commandtest.GetPackage(t, "vendor/package", "1.2.3")}
			devPackages := []pkg.PackageInterface{commandtest.GetPackage(t, "vendor/devpackage", "2.3.4")}

			commandtest.CreateComposerLock(t, packages, devPackages)
			commandtest.CreateInstalledJSON(t, packages, devPackages, true)

			appTester := commandtest.GetApplicationTester(t)
			runAppTester(t, appTester, append([]console.Param{console.P("command", "install")}, tt.command...)...)

			assertTrimmedDisplay(t, appTester, tt.expected)
		})
	}
}

// rootMetapackages returns root/req and root/another as rootMetapackages, so the
// whole install process runs without downloading them.
func rootMetapackages(t *testing.T) (rootReq, another *pkg.CompletePackage) {
	t.Helper()
	rootReq = commandtest.GetPackage(t, "root/req", "1.0.0")
	another = commandtest.GetPackage(t, "root/another", "1.0.0")
	// Set as a metapackage so that we can do the whole post-remove update & install process without Composer trying to download them (DownloadManager::getDownloaderForPackage).
	rootReq.SetType("metapackage")
	another.SetType("metapackage")

	return rootReq, another
}

func TestInstallCommand_InstallFromEmptyVendor(t *testing.T) {
	commandtest.InitTempComposer(t, `{"require": {"root/req": "1.*"}, "require-dev": {"root/another": "1.*"}}`, nil, nil, true)
	rootReq, another := rootMetapackages(t)
	commandtest.CreateComposerLock(t, []pkg.PackageInterface{rootReq}, []pkg.PackageInterface{another})

	appTester := commandtest.GetApplicationTester(t)
	runAppTester(t, appTester, console.P("command", "install"), console.P("--no-progress", true))

	assertTrimmedDisplay(t, appTester, `Installing dependencies from lock file (including require-dev)
Verifying lock file contents can be installed on current platform.
Package operations: 2 installs, 0 updates, 0 removals
  - Installing root/another (1.0.0)
  - Installing root/req (1.0.0)
Generating autoload files`)
}

func TestInstallCommand_InstallFromEmptyVendorNoDev(t *testing.T) {
	commandtest.InitTempComposer(t, `{"require": {"root/req": "1.*"}, "require-dev": {"root/another": "1.*"}}`, nil, nil, true)
	rootReq, another := rootMetapackages(t)
	commandtest.CreateComposerLock(t, []pkg.PackageInterface{rootReq}, []pkg.PackageInterface{another})

	appTester := commandtest.GetApplicationTester(t)
	runAppTester(t, appTester, console.P("command", "install"), console.P("--no-progress", true), console.P("--no-dev", true))

	assertTrimmedDisplay(t, appTester, `Installing dependencies from lock file
Verifying lock file contents can be installed on current platform.
Package operations: 1 install, 0 updates, 0 removals
  - Installing root/req (1.0.0)
Generating autoload files`)
}

func TestInstallCommand_InstallNewPackagesWithExistingPartialVendor(t *testing.T) {
	commandtest.InitTempComposer(t, `{"require": {"root/req": "1.*", "root/another": "1.*"}}`, nil, nil, true)
	rootReq, another := rootMetapackages(t)
	commandtest.CreateComposerLock(t, []pkg.PackageInterface{rootReq, another}, nil)
	commandtest.CreateInstalledJSON(t, []pkg.PackageInterface{rootReq}, nil, true)

	appTester := commandtest.GetApplicationTester(t)
	runAppTester(t, appTester, console.P("command", "install"), console.P("--no-progress", true))

	assertTrimmedDisplay(t, appTester, `Installing dependencies from lock file (including require-dev)
Verifying lock file contents can be installed on current platform.
Package operations: 1 install, 0 updates, 0 removals
  - Installing root/another (1.0.0)
Generating autoload files`)
}

// installFromLockProject is the project of TestInstallCommand_InstallFromEmptyVendor:
// root/req in require and root/another in require-dev, both locked.
func installFromLockProject(t *testing.T) {
	t.Helper()
	commandtest.InitTempComposer(t, `{"require": {"root/req": "1.*"}, "require-dev": {"root/another": "1.*"}}`, nil, nil, true)
	rootReq, another := rootMetapackages(t)
	commandtest.CreateComposerLock(t, []pkg.PackageInterface{rootReq}, []pkg.PackageInterface{another})
}

const (
	installingReq       = "  - Installing root/req (1.0.0)"
	installingDev       = "  - Installing root/another (1.0.0)"
	installingNoDevHead = "Installing dependencies from lock file\n"
)

func TestInstallCommand_Options(t *testing.T) {
	runCommandCases(t, installFromLockProject, []commandCase{
		{
			name:     "--no-progress and --no-blocking are accepted",
			params:   cmd("install", "--no-progress", true, "--no-blocking", true),
			contains: []string{installingDev, installingReq},
		},
		{
			name:     "COMPOSER_NO_DEV is --no-dev",
			params:   cmd("install"),
			env:      map[string]string{"COMPOSER_NO_DEV": "1"},
			contains: []string{installingNoDevHead, installingReq},
			excludes: []string{installingDev},
		},
		{
			name:     "COMPOSER_IGNORE_PLATFORM_REQS is --ignore-platform-reqs, with a warning",
			params:   cmd("install", "--dry-run", true),
			env:      map[string]string{"COMPOSER_IGNORE_PLATFORM_REQS": "1"},
			contains: []string{"COMPOSER_IGNORE_PLATFORM_REQS is set. You may experience unexpected errors."},
		},
		{
			name:     "-o --strict-psr-autoloader on a clean project",
			params:   cmd("install", "-o", true, "--strict-psr-autoloader", true),
			contains: []string{installingReq},
		},
		{
			name:   "--strict-psr-autoloader needs an optimized autoloader",
			params: cmd("install", "--strict-psr-autoloader", true),
			err:    "--strict-psr-autoloader mode only works with optimized autoloader, use --optimize-autoloader or --classmap-authoritative if you want a strict return value.",
		},
		{
			name:   "--prefer-install takes dist, source or auto",
			params: cmd("install", "--prefer-install", "foo"),
			err:    `--prefer-install accepts one of "dist", "source" or "auto", got foo`,
		},
		{
			name:   "--prefer-source with --prefer-install",
			params: cmd("install", "--prefer-source", true, "--prefer-install", "dist"),
			err:    "--prefer-source can not be used together with --prefer-install",
		},
		{
			name:   "--prefer-dist with --prefer-install",
			params: cmd("install", "--prefer-dist", true, "--prefer-install", "source"),
			err:    "--prefer-dist can not be used together with --prefer-install",
		},
		{
			name:   "--audit-format takes a known format",
			params: cmd("install", "--audit-format", "xml"),
			err:    "--audit-format must be one of table, plain, json, summary.",
		},
	})
}
