// Ports tests/Composer/Test/Command/GlobalCommandTest.php.

package command_test

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

// globalTearDown ports GlobalCommandTest::tearDown.
func globalTearDown(t *testing.T) {
	t.Helper()
	prevCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevCwd)
		util.ClearEnv("COMPOSER_HOME")
		util.ClearEnv("COMPOSER")
	})
}

// globalNeedsCommand skips a test whose proxied command is not registered
// (yet) in this build.
func globalNeedsCommand(t *testing.T, name string) {
	t.Helper()
	if !commandtest.NewApplication().Has(name) {
		t.Skipf("the %s command is not available", name)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
}

func TestGlobalCommand_Global(t *testing.T) {
	globalTearDown(t)
	script := `@php -r "echo 'COMPOSER SCRIPT OUTPUT: '.getenv('COMPOSER') . PHP_EOL;"`
	fakeComposer := "TMP_COMPOSER.JSON"
	cj := php.NewArray()
	scripts := php.NewArray()
	scripts.Set("test-script", script)
	cj.Set("scripts", scripts)
	composerHome := commandtest.InitTempComposer(t, cj, nil, nil, true)

	util.PutEnv("COMPOSER_HOME", composerHome)
	util.PutEnv("COMPOSER", fakeComposer)

	dir := commandtest.UniqueTmpDirectory(t)
	chdir(t, dir)

	tester := commandtest.GetApplicationTester(t)
	if _, err := tester.RunArgs(commandtest.Options{}, "command", "global", "command-name", "test-script", "--no-interaction", true); err != nil {
		t.Fatal(err)
	}

	display := tester.Display(true)

	want := "Changed current directory to " + composerHome + "\n" +
		"COMPOSER SCRIPT OUTPUT: \n"
	if display != want {
		t.Errorf("display %q, want %q", display, want)
	}
}

func TestGlobalCommand_CannotCreateHome(t *testing.T) {
	globalTearDown(t)
	dir := commandtest.UniqueTmpDirectory(t)
	filename := dir + "/file"
	if err := os.WriteFile(filename, nil, 0o666); err != nil {
		t.Fatal(err)
	}

	util.PutEnv("COMPOSER_HOME", filename)

	tester := commandtest.GetApplicationTester(t)
	_, err := tester.RunArgs(commandtest.Options{}, "command", "global", "command-name", "test-script", "--no-interaction", true)
	want := filename + " exists and is not a directory."
	if err == nil || err.Error() != want {
		t.Fatalf("err %v, want %q", err, want)
	}
	if !phperr.InstanceOf(err, "RuntimeException") {
		t.Errorf("err %T is not a RuntimeException", err)
	}
}

func TestGlobalCommand_GlobalShow(t *testing.T) {
	globalNeedsCommand(t, "show")
	globalTearDown(t)
	composerHome := commandtest.InitTempComposer(t, `{
		"repositories": {"packages": {"type": "package", "package": [{"name": "vendor/global-tool", "version": "1.0.0"}]}},
		"require": {"vendor/global-tool": "1.0.0"}
	}`, nil, nil, true)

	p := commandtest.GetPackage(t, "vendor/global-tool", "1.0.0")
	p.SetDescription(pkg.Str("A globally installed tool"))
	commandtest.CreateInstalledJSON(t, []pkg.PackageInterface{p}, nil, true)

	util.PutEnv("COMPOSER_HOME", composerHome)

	chdir(t, commandtest.UniqueTmpDirectory(t))

	tester := commandtest.GetApplicationTester(t)
	tester.SetInputs("")
	if _, err := tester.RunArgs(commandtest.Options{}, "command", "global", "command-name", "show"); err != nil {
		t.Fatal(err)
	}

	output := tester.Display(true)
	if !strings.Contains(output, "vendor/global-tool") || !strings.Contains(output, "1.0.0") {
		t.Errorf("output %q", output)
	}
}

func TestGlobalCommand_GlobalShowWithoutPackages(t *testing.T) {
	globalNeedsCommand(t, "show")
	globalTearDown(t)
	composerHome := commandtest.InitTempComposer(t, nil, nil, nil, true)
	commandtest.CreateInstalledJSON(t, nil, nil, true)

	util.PutEnv("COMPOSER_HOME", composerHome)

	chdir(t, commandtest.UniqueTmpDirectory(t))

	tester := commandtest.GetApplicationTester(t)
	tester.SetInputs("")
	if _, err := tester.RunArgs(commandtest.Options{}, "command", "global", "command-name", "show"); err != nil {
		t.Fatal(err)
	}

	if code := tester.StatusCode(); code != 0 {
		t.Errorf("status %d", code)
	}
}

func TestGlobalCommand_GlobalRequire(t *testing.T) {
	globalNeedsCommand(t, "require")
	globalTearDown(t)
	_, thisFile, _, _ := runtime.Caller(0)
	pkgArray := php.NewArray()
	pkgArray.Set("name", "vendor/required-pkg")
	pkgArray.Set("version", "2.0.0")
	dist := php.NewArray()
	dist.Set("type", "file")
	dist.Set("url", thisFile)
	pkgArray.Set("dist", dist)
	repo := php.NewArray()
	repo.Set("type", "package")
	repo.Set("package", php.ListOf(pkgArray))
	repos := php.NewArray()
	repos.Set("packages", repo)
	cj := php.NewArray()
	cj.Set("repositories", repos)
	composerHome := commandtest.InitTempComposer(t, cj, nil, nil, true)

	util.PutEnv("COMPOSER_HOME", composerHome)

	chdir(t, commandtest.UniqueTmpDirectory(t))

	tester := commandtest.GetApplicationTester(t)
	tester.SetInputs("")
	if _, err := tester.RunArgs(commandtest.Options{}, "command", "global", "command-name", "require", "packages", []string{"vendor/required-pkg:2.0.0"}); err != nil {
		t.Fatal(err)
	}

	if code := tester.StatusCode(); code != 0 {
		t.Errorf("status %d:\n%s", code, tester.Display(true))
	}
	if !strings.Contains(tester.Display(true), "Installing vendor/required-pkg") {
		t.Errorf("output %q", tester.Display(true))
	}
}

func TestGlobalCommand_GlobalUpdate(t *testing.T) {
	globalNeedsCommand(t, "update")
	globalTearDown(t)
	composerHome := commandtest.InitTempComposer(t, `{
		"repositories": {"packages": {"type": "package", "package": [{"name": "vendor/pkg", "version": "1.0.0"}]}},
		"require": {"vendor/pkg": "1.0.0"}
	}`, nil, nil, true)

	p := commandtest.GetPackage(t, "vendor/pkg", "1.0.0")
	commandtest.CreateInstalledJSON(t, []pkg.PackageInterface{p}, nil, true)
	commandtest.CreateComposerLock(t, []pkg.PackageInterface{p}, nil)

	util.PutEnv("COMPOSER_HOME", composerHome)

	chdir(t, commandtest.UniqueTmpDirectory(t))

	tester := commandtest.GetApplicationTester(t)
	tester.SetInputs("")
	if _, err := tester.RunArgs(commandtest.Options{}, "command", "global", "command-name", "update"); err != nil {
		t.Fatal(err)
	}

	if code := tester.StatusCode(); code != 0 {
		t.Errorf("status %d:\n%s", code, tester.Display(true))
	}
}

func TestGlobalCommand_GlobalChangesDirectory(t *testing.T) {
	globalNeedsCommand(t, "config")
	globalTearDown(t)
	composerHome := commandtest.InitTempComposer(t, `{"name": "test/global"}`, nil, nil, true)

	util.PutEnv("COMPOSER_HOME", composerHome)

	chdir(t, commandtest.UniqueTmpDirectory(t))

	tester := commandtest.GetApplicationTester(t)
	tester.SetInputs("")
	if _, err := tester.RunArgs(commandtest.Options{}, "command", "global", "command-name", "config", "setting-key", "name"); err != nil {
		t.Fatal(err)
	}

	output := tester.Display(true)
	if !strings.Contains(output, "Changed current directory to "+composerHome) {
		t.Errorf("output %q", output)
	}
}

func TestGlobalCommand_GlobalMissingCommandName(t *testing.T) {
	globalTearDown(t)
	composerHome := commandtest.InitTempComposer(t, nil, nil, nil, true)

	util.PutEnv("COMPOSER_HOME", composerHome)

	tester := commandtest.GetApplicationTester(t)
	tester.SetInputs("")
	_, err := tester.RunArgs(commandtest.Options{}, "command", "global")
	if err == nil || !strings.Contains(err.Error(), `Not enough arguments (missing: "command-name")`) {
		t.Fatalf("err %v", err)
	}
}
