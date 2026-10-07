// Ports tests/Composer/Test/Command/RemoveCommandTest.php.

package command_test

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
)

// readProjectJSON reads a JSON file of the test project as PHP's JsonFile::read.
func readProjectJSON(t *testing.T, path string) *php.Array {
	t.Helper()
	f, err := json.NewFile(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	v, err := f.Read()
	if err != nil {
		t.Fatal(err)
	}
	a, ok := v.(*php.Array)
	if !ok {
		t.Fatalf("%s is not an object: %#v", path, v)
	}

	return a
}

// projectJSONKey returns $data[$key] of a JSON file (nil when missing).
func projectJSONKey(t *testing.T, path, key string) any {
	t.Helper()
	v, _ := readProjectJSON(t, path).Get(key)

	return v
}

// assertJSONValue compares a PHP value with the expected one given as JSON
// (key order included).
func assertJSONValue(t *testing.T, expected string, actual any) {
	t.Helper()
	d, err := php.JSONDecode(expected, true)
	if err != nil {
		t.Fatal(err)
	}
	want, err := php.JSONEncode(d, 0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := php.JSONEncode(actual, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want != got {
		t.Errorf("want %s\ngot  %s", want, got)
	}
}

// assertEmptyValue is PHPUnit's assertEmpty.
func assertEmptyValue(t *testing.T, v any) {
	t.Helper()
	if a, ok := v.(*php.Array); ok && a.Len() == 0 {
		return
	}
	if !php.ToBool(v) {
		return
	}
	t.Errorf("expected empty, got %#v", v)
}

func runRemove(t *testing.T, kv ...any) (*commandtest.ApplicationTester, int) {
	t.Helper()
	appTester := commandtest.GetApplicationTester(t)
	code, err := appTester.RunArgs(commandtest.Options{}, append([]any{"command", "remove"}, kv...)...)
	if err != nil {
		t.Fatalf("%v\n%s", err, appTester.Display(true))
	}

	return appTester, code
}

func assertOutputContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("output does not contain %q:\n%s", needle, haystack)
	}
}

func metapackage(t *testing.T, name string) *pkg.CompletePackage {
	t.Helper()
	p := commandtest.GetPackage(t, name, "1.0.0")
	p.SetType("metapackage")

	return p
}

const removeTwoMetapackagesRepo = `"repositories": {"packages": {"type": "package", "package": [
	{"name": "root/req", "version": "1.0.0", "type": "metapackage"},
	{"name": "root/another", "version": "1.0.0", "type": "metapackage"}]}}`

func TestRemoveCommand_ExceptionRunningWithNoRemovePackages(t *testing.T) {
	appTester := commandtest.GetApplicationTester(t)
	_, err := appTester.RunArgs(commandtest.Options{}, "command", "remove")
	ce, ok := errors.AsType[*console.Error](err)
	if !ok || ce.Kind != console.KindInvalidArgument {
		t.Fatalf("expected InvalidArgumentException, got %v", err)
	}
	if err.Error() != `Not enough arguments (missing: "packages").` {
		t.Errorf("message %q", err.Error())
	}
}

func TestRemoveCommand_ExceptionWhenRunningUnusedWithoutLockFile(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)

	appTester := commandtest.GetApplicationTester(t)
	_, err := appTester.RunArgs(commandtest.Options{}, "command", "remove", "--unused", true)
	if !phperr.InstanceOf(err, "UnexpectedValueException") {
		t.Fatalf("expected UnexpectedValueException, got %v", err)
	}
	if err.Error() != "A valid composer.lock file is required to run this command with --unused" {
		t.Errorf("message %q", err.Error())
	}
}

func TestRemoveCommand_WarningWhenRemovingNonExistentPackage(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	commandtest.CreateInstalledJSON(t, nil, nil, true)

	appTester, code := runRemove(t, "packages", []string{"vendor1/package1"})
	if code != 0 {
		t.Errorf("exit code %d", code)
	}
	if out := strings.TrimSpace(appTester.Display(true)); !strings.HasPrefix(out, "<warning>vendor1/package1 is not required in your composer.json and has not been removed</warning>") {
		t.Errorf("output:\n%s", out)
	}
}

func TestRemoveCommand_WarningWhenRemovingPackageFromWrongType(t *testing.T) {
	commandtest.InitTempComposer(t, `{"require": {"root/req": "1.*"}}`, nil, nil, true)

	appTester, code := runRemove(t, "packages", []string{"root/req"}, "--dev", true, "--no-update", true, "--no-interaction", true)
	if code != 0 {
		t.Errorf("exit code %d", code)
	}
	want := "<warning>root/req could not be found in require-dev but it is present in require</warning>\n./composer.json has been updated"
	if out := strings.TrimSpace(appTester.Display(true)); out != want {
		t.Errorf("output:\n%s", out)
	}
	assertJSONValue(t, `{"require": {"root/req": "1.*"}}`, readProjectJSON(t, "./composer.json"))
}

func TestRemoveCommand_WarningWhenRemovingPackageWithDeprecatedDependenciesFlag(t *testing.T) {
	commandtest.InitTempComposer(t, `{"require": {"root/req": "1.*"}}`, nil, nil, true)

	appTester, code := runRemove(t, "packages", []string{"root/req"}, "--update-with-dependencies", true, "--no-update", true, "--no-interaction", true)
	if code != 0 {
		t.Errorf("exit code %d", code)
	}
	want := "<warning>You are using the deprecated option \"update-with-dependencies\". This is now default behaviour. The --no-update-with-dependencies option can be used to remove a package without its dependencies.</warning>\n./composer.json has been updated"
	if out := strings.TrimSpace(appTester.Display(true)); out != want {
		t.Errorf("output:\n%s", out)
	}
	assertEmptyValue(t, readProjectJSON(t, "./composer.json"))
}

func TestRemoveCommand_MessageOutputWhenNoUnusedPackagesToRemove(t *testing.T) {
	commandtest.InitTempComposer(t, `{"repositories": {"packages": {"type": "package", "package": [
		{"name": "root/req", "version": "1.0.0", "require": {"nested/req": "^1"}},
		{"name": "nested/req", "version": "1.1.0"}]}},
		"require": {"root/req": "1.*"}}`, nil, nil, true)

	requiredPackage := commandtest.GetPackage(t, "root/req", "1.0.0")
	commandtest.ConfigureLinks(t, requiredPackage, `{"require": {"nested/req": "^1"}}`)
	nestedPackage := commandtest.GetPackage(t, "nested/req", "1.1.0")

	commandtest.CreateInstalledJSON(t, []pkg.PackageInterface{requiredPackage, nestedPackage}, nil, true)
	commandtest.CreateComposerLock(t, []pkg.PackageInterface{requiredPackage, nestedPackage}, nil)

	appTester, code := runRemove(t, "--unused", true, "--no-audit", true, "--no-interaction", true)
	if code != 0 {
		t.Errorf("exit code %d", code)
	}
	if out := strings.TrimSpace(appTester.Display(true)); out != "No unused packages to remove" {
		t.Errorf("output:\n%s", out)
	}
}

func TestRemoveCommand_RemoveUnusedPackage(t *testing.T) {
	commandtest.InitTempComposer(t, `{"repositories": {"packages": {"type": "package", "package": [
		{"name": "root/req", "version": "1.0.0"},
		{"name": "not/req", "version": "1.0.0"}]}},
		"require": {"root/req": "1.*"}}`, nil, nil, true)

	requiredPackage := commandtest.GetPackage(t, "root/req", "1.0.0")
	extraneousPackage := commandtest.GetPackage(t, "not/req", "1.0.0")

	commandtest.CreateInstalledJSON(t, []pkg.PackageInterface{requiredPackage}, nil, true)
	commandtest.CreateComposerLock(t, []pkg.PackageInterface{requiredPackage, extraneousPackage}, nil)

	appTester, code := runRemove(t, "--unused", true, "--no-audit", true, "--no-interaction", true)
	if code != 0 {
		t.Errorf("exit code %d", code)
	}
	out := appTester.Display(true)
	if !strings.HasPrefix(out, "<warning>not/req is not required in your composer.json and has not been removed</warning>") {
		t.Errorf("output:\n%s", out)
	}
	assertOutputContains(t, out, "Running composer update not/req")
	assertOutputContains(t, out, "- Removing not/req (1.0.0)")
}

func TestRemoveCommand_RemovePackageByName(t *testing.T) {
	commandtest.InitTempComposer(t, `{`+removeTwoMetapackagesRepo+`, "require": {"root/req": "1.*", "root/another": "1.*"}}`, nil, nil, true)
	rootReqPackage := metapackage(t, "root/req")
	rootAnotherPackage := metapackage(t, "root/another")

	commandtest.CreateInstalledJSON(t, []pkg.PackageInterface{rootReqPackage, rootAnotherPackage}, nil, true)
	commandtest.CreateComposerLock(t, []pkg.PackageInterface{rootReqPackage, rootAnotherPackage}, nil)

	appTester, code := runRemove(t, "packages", []string{"root/req"}, "--no-audit", true, "--no-interaction", true)
	if code != 0 {
		t.Errorf("exit code %d", code)
	}
	out := strings.TrimSpace(appTester.Display(true))
	if !strings.HasPrefix(out, "./composer.json has been updated") {
		t.Errorf("output:\n%s", out)
	}
	assertOutputContains(t, out, "Running composer update root/req")
	assertOutputContains(t, out, "Lock file operations: 0 installs, 0 updates, 1 removal")
	assertOutputContains(t, out, "- Removing root/req (1.0.0)")
	assertOutputContains(t, out, "Package operations: 0 installs, 0 updates, 1 removal")
	assertJSONValue(t, `{"root/another": "1.*"}`, projectJSONKey(t, "./composer.json", "require"))
	assertJSONValue(t, `[{"name": "root/another", "version": "1.0.0", "type": "metapackage"}]`, projectJSONKey(t, "./composer.lock", "packages"))
}

func TestRemoveCommand_RemovePackageByNameWithDryRun(t *testing.T) {
	commandtest.InitTempComposer(t, `{`+removeTwoMetapackagesRepo+`, "require": {"root/req": "1.*", "root/another": "1.*"}}`, nil, nil, true)
	rootReqPackage := metapackage(t, "root/req")
	rootAnotherPackage := metapackage(t, "root/another")

	commandtest.CreateInstalledJSON(t, []pkg.PackageInterface{rootReqPackage, rootAnotherPackage}, nil, true)
	commandtest.CreateComposerLock(t, []pkg.PackageInterface{rootReqPackage, rootAnotherPackage}, nil)

	appTester, code := runRemove(t, "packages", []string{"root/req"}, "--dry-run", true, "--no-audit", true, "--no-interaction", true)
	if code != 0 {
		t.Errorf("exit code %d", code)
	}
	out := strings.TrimSpace(appTester.Display(true))
	assertOutputContains(t, out, "./composer.json has been updated")
	assertOutputContains(t, out, "Running composer update root/req")
	assertOutputContains(t, out, "Lock file operations: 0 installs, 0 updates, 1 removal")
	assertOutputContains(t, out, "- Removing root/req (1.0.0)")
	assertOutputContains(t, out, "Package operations: 0 installs, 0 updates, 1 removal")
	assertJSONValue(t, `{"root/req": "1.*", "root/another": "1.*"}`, projectJSONKey(t, "./composer.json", "require"))
	assertJSONValue(t, `[{"name": "root/another", "version": "1.0.0", "type": "metapackage"}, {"name": "root/req", "version": "1.0.0", "type": "metapackage"}]`, projectJSONKey(t, "./composer.lock", "packages"))
}

func TestRemoveCommand_RemoveAllowedPluginPackageWithNoOtherAllowedPlugins(t *testing.T) {
	commandtest.InitTempComposer(t, `{`+removeTwoMetapackagesRepo+`, "require": {"root/req": "1.*", "root/another": "1.*"},
		"config": {"allow-plugins": {"root/req": true}}}`, nil, nil, true)
	rootReqPackage := metapackage(t, "root/req")
	rootAnotherPackage := metapackage(t, "root/another")

	commandtest.CreateInstalledJSON(t, []pkg.PackageInterface{rootReqPackage, rootAnotherPackage}, nil, true)
	commandtest.CreateComposerLock(t, []pkg.PackageInterface{rootReqPackage, rootAnotherPackage}, nil)

	_, code := runRemove(t, "packages", []string{"root/req"}, "--no-audit", true, "--no-interaction", true)
	if code != 0 {
		t.Errorf("exit code %d", code)
	}
	assertJSONValue(t, `{"root/another": "1.*"}`, projectJSONKey(t, "./composer.json", "require"))
	assertEmptyValue(t, projectJSONKey(t, "./composer.json", "config"))
}

func TestRemoveCommand_RemoveAllowedPluginPackageWithOtherAllowedPlugins(t *testing.T) {
	commandtest.InitTempComposer(t, `{`+removeTwoMetapackagesRepo+`, "require": {"root/req": "1.*", "root/another": "1.*"},
		"config": {"allow-plugins": {"root/another": true, "root/req": true}}}`, nil, nil, true)

	_, code := runRemove(t, "packages", []string{"root/req"}, "--no-audit", true, "--no-interaction", true)
	if code != 0 {
		t.Errorf("exit code %d", code)
	}
	assertJSONValue(t, `{"root/another": "1.*"}`, projectJSONKey(t, "./composer.json", "require"))
	assertJSONValue(t, `{"allow-plugins": {"root/another": true}}`, projectJSONKey(t, "./composer.json", "config"))
}

const removeThreePackagesComposerJSON = `{"repositories": {"packages": {"type": "package", "package": [
	{"name": "root/req", "version": "1.0.0"},
	{"name": "root/another", "version": "1.0.0"},
	{"name": "another/req", "version": "1.0.0"}]}},
	"require": {"root/req": "1.*", "root/another": "1.*", "another/req": "1.*"}}`

func setupThreePackages(t *testing.T) {
	t.Helper()
	commandtest.InitTempComposer(t, removeThreePackagesComposerJSON, nil, nil, true)
	packages := []pkg.PackageInterface{
		commandtest.GetPackage(t, "root/req", "1.0.0"),
		commandtest.GetPackage(t, "root/another", "1.0.0"),
		commandtest.GetPackage(t, "another/req", "1.0.0"),
	}
	commandtest.CreateInstalledJSON(t, packages, nil, true)
	commandtest.CreateComposerLock(t, packages, nil)
}

func TestRemoveCommand_RemovePackagesByVendor(t *testing.T) {
	setupThreePackages(t)

	appTester, code := runRemove(t, "packages", []string{"root/*"}, "--no-install", true, "--no-audit", true, "--no-interaction", true)
	if code != 0 {
		t.Errorf("exit code %d", code)
	}
	out := appTester.Display(true)
	if !strings.HasPrefix(strings.TrimSpace(out), "./composer.json has been updated") {
		t.Errorf("output:\n%s", out)
	}
	assertOutputContains(t, out, "Running composer update root/*")
	assertOutputContains(t, out, "- Removing root/another (1.0.0)")
	assertOutputContains(t, out, "- Removing root/req (1.0.0)")
	assertOutputContains(t, out, "Writing lock file")
	assertJSONValue(t, `{"another/req": "1.*"}`, projectJSONKey(t, "./composer.json", "require"))
	assertJSONValue(t, `[{"name": "another/req", "version": "1.0.0", "type": "library"}]`, projectJSONKey(t, "./composer.lock", "packages"))
}

func TestRemoveCommand_RemovePackagesByVendorWithDryRun(t *testing.T) {
	setupThreePackages(t)

	appTester := commandtest.GetApplicationTester(t)
	if _, err := appTester.RunArgs(commandtest.Options{}, "command", "remove", "packages", []string{"root/*"}, "--dry-run", true, "--no-install", true, "--no-audit", true, "--no-interaction", true); err != nil {
		t.Fatal(err)
	}
	if code := appTester.StatusCode(); code != 0 {
		t.Errorf("exit code %d", code)
	}
	want := `./composer.json has been updated
Running composer update root/*
Loading composer repositories with package information
Updating dependencies
Lock file operations: 0 installs, 0 updates, 2 removals
  - Removing root/another (1.0.0)
  - Removing root/req (1.0.0)`
	out := appTester.Display(true)
	if strings.TrimSpace(out) != want {
		t.Errorf("output:\n%s", out)
	}
	if strings.Contains(out, "Writing lock file") {
		t.Error("lock file written")
	}
	assertJSONValue(t, `{"root/req": "1.*", "root/another": "1.*", "another/req": "1.*"}`, projectJSONKey(t, "./composer.json", "require"))
	assertJSONValue(t, `[{"name": "another/req", "version": "1.0.0", "type": "library"}, {"name": "root/another", "version": "1.0.0", "type": "library"}, {"name": "root/req", "version": "1.0.0", "type": "library"}]`, projectJSONKey(t, "./composer.lock", "packages"))
}

func TestRemoveCommand_WarningWhenRemovingPackagesByVendorFromWrongType(t *testing.T) {
	commandtest.InitTempComposer(t, `{"require": {"root/req": "1.*", "root/another": "1.*", "another/req": "1.*"}}`, nil, nil, true)

	appTester, code := runRemove(t, "packages", []string{"root/*"}, "--dev", true, "--no-interaction", true, "--no-update", true)
	if code != 0 {
		t.Errorf("exit code %d", code)
	}
	want := "<warning>root/req could not be found in require-dev but it is present in require</warning>\n<warning>root/another could not be found in require-dev but it is present in require</warning>\n./composer.json has been updated"
	if out := strings.TrimSpace(appTester.Display(true)); out != want {
		t.Errorf("output:\n%s", out)
	}
	assertJSONValue(t, `{"require": {"root/req": "1.*", "root/another": "1.*", "another/req": "1.*"}}`, readProjectJSON(t, "./composer.json"))
}

func TestRemoveCommand_PackageStillPresentErrorWhenNoInstallFlagUsed(t *testing.T) {
	commandtest.InitTempComposer(t, `{"require": {"root/req": "1.*"}}`, nil, nil, true)
	rootReqPackage := commandtest.GetPackage(t, "root/req", "1.0.0")

	commandtest.CreateInstalledJSON(t, []pkg.PackageInterface{rootReqPackage}, nil, true)
	commandtest.CreateComposerLock(t, []pkg.PackageInterface{rootReqPackage}, nil)

	appTester, code := runRemove(t, "packages", []string{"root/req"}, "--no-install", true, "--no-audit", true, "--no-interaction", true)
	if code != 2 {
		t.Errorf("exit code %d", code)
	}
	out := appTester.Display(true)
	assertOutputContains(t, out, "./composer.json has been updated")
	assertOutputContains(t, out, "Lock file operations: 0 installs, 0 updates, 1 removal")
	assertOutputContains(t, out, "- Removing root/req (1.0.0)")
	assertOutputContains(t, out, "Writing lock file")
	assertOutputContains(t, out, "Removal failed, root/req is still present, it may be required by another package. See `composer why root/req`")
	assertEmptyValue(t, readProjectJSON(t, "./composer.json"))
	assertEmptyValue(t, projectJSONKey(t, "./composer.lock", "packages"))
	assertJSONValue(t, `[{"name": "root/req", "version": "1.0.0", "version_normalized": "1.0.0.0", "type": "library", "install-path": "../root/req"}]`, projectJSONKey(t, "./vendor/composer/installed.json", "packages"))
}

func TestRemoveCommand_UpdateInheritedDependenciesFlagIsPassedToPostRemoveInstaller(t *testing.T) {
	for _, tt := range []struct{ name, installFlagName, expectedComposerUpdateCommand string }{
		{"update with all dependencies", "--update-with-all-dependencies", "Running composer update root/req --with-all-dependencies"},
		{"with all dependencies", "--with-all-dependencies", "Running composer update root/req --with-all-dependencies"},
		{"no update with dependencies", "--no-update-with-dependencies", "Running composer update root/req --with-dependencies"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, `{"repositories": {"packages": {"type": "package", "package": [
				{"name": "root/req", "version": "1.0.0", "type": "metapackage"}]}},
				"require": {"root/req": "1.*"}}`, nil, nil, true)
			rootReqPackage := metapackage(t, "root/req")

			commandtest.CreateInstalledJSON(t, []pkg.PackageInterface{rootReqPackage}, nil, true)
			commandtest.CreateComposerLock(t, []pkg.PackageInterface{rootReqPackage}, nil)

			appTester, code := runRemove(t, "packages", []string{"root/req"}, tt.installFlagName, true, "--no-audit", true, "--no-interaction", true)
			if code != 0 {
				t.Errorf("exit code %d", code)
			}
			out := appTester.Display(true)
			assertOutputContains(t, out, "./composer.json has been updated")
			assertOutputContains(t, out, tt.expectedComposerUpdateCommand)
			assertOutputContains(t, out, "Package operations: 0 installs, 0 updates, 1 removal")
			assertOutputContains(t, out, "- Removing root/req (1.0.0)")
			assertOutputContains(t, out, "Writing lock file")
			assertOutputContains(t, out, "Lock file operations: 0 installs, 0 updates, 1 removal")
			assertEmptyValue(t, projectJSONKey(t, "./composer.lock", "packages"))
		})
	}
}

// removeProject requires root/req and root/another, and root/dev in
// require-dev (plus extra requirements, a JSON fragment), all locked and
// installed metapackages.
func removeProject(extra string) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()
		commandtest.InitTempComposer(t, `{"repositories": {"packages": {"type": "package", "package": [
			{"name": "root/req", "version": "1.0.0", "type": "metapackage"},
			{"name": "root/another", "version": "1.0.0", "type": "metapackage"},
			{"name": "root/dev", "version": "1.0.0", "type": "metapackage"}]}},
			"require": {"root/req": "1.*", "root/another": "1.*"`+extra+`},
			"require-dev": {"root/dev": "1.*"}}`, nil, nil, true)
		packages := []pkg.PackageInterface{metapackage(t, "root/req"), metapackage(t, "root/another")}
		devPackages := []pkg.PackageInterface{metapackage(t, "root/dev")}
		commandtest.CreateInstalledJSON(t, packages, devPackages, true)
		commandtest.CreateComposerLock(t, packages, devPackages)
	}
}

const removingDev = "  - Removing root/dev (1.0.0)"

func TestRemoveCommand_Options(t *testing.T) {
	runCommandCases(t, removeProject(""), []commandCase{
		{
			name:     "dev packages stay installed",
			params:   cmd("remove", "packages", []string{"root/req"}, "--no-audit", true),
			contains: []string{"  - Removing root/req (1.0.0)"},
			excludes: []string{removingDev},
		},
		{
			name:     "--update-no-dev",
			params:   cmd("remove", "packages", []string{"root/req"}, "--no-audit", true, "--update-no-dev", true),
			contains: []string{"  - Removing root/req (1.0.0)", removingDev},
		},
		{
			name:     "COMPOSER_NO_DEV is --update-no-dev",
			params:   cmd("remove", "packages", []string{"root/req"}, "--no-audit", true),
			env:      map[string]string{"COMPOSER_NO_DEV": "1"},
			contains: []string{"  - Removing root/req (1.0.0)", removingDev},
		},
	})
}

func TestRemoveCommand_RevertsComposerJSONWhenTheUpdateFails(t *testing.T) {
	var original []byte
	runCommandCases(t, func(t *testing.T) {
		t.Helper()
		removeProject(`, "root/missing": "^1.0"`)(t)
		var err error
		if original, err = os.ReadFile("composer.json"); err != nil {
			t.Fatal(err)
		}
	}, []commandCase{{
		name:     "an unresolvable requirement",
		params:   cmd("remove", "packages", []string{"root/req"}, "--no-audit", true),
		code:     2,
		contains: []string{"Removal failed, reverting ./composer.json to its original content."},
		check: func(t *testing.T) {
			t.Helper()
			got, err := os.ReadFile("composer.json")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, original) {
				t.Errorf("composer.json not reverted:\n%s\nwant:\n%s", got, original)
			}
		},
	}})
}
