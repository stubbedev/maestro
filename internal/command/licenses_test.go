// Ports tests/Composer/Test/Command/LicensesCommandTest.php.

package command_test

import (
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

// licensesRun runs the tester and fails the test on an exception.
func licensesRun(t *testing.T, appTester *commandtest.ApplicationTester, params ...console.Param) int {
	t.Helper()
	code, err := appTester.Run(params, commandtest.Options{})
	if err != nil {
		t.Fatalf("unexpected exception: %v", err)
	}

	return code
}

func licensesSetUp(t *testing.T) {
	t.Helper()
	commandtest.InitTempComposer(t, `{
		"name": "test/pkg",
		"version": "1.2.3",
		"license": "MIT",
		"require": {"first/pkg": "^2.0", "second/pkg": "3.*", "third/pkg": "^1.3"},
		"require-dev": {"dev/pkg": "~2.0"}
	}`, nil, nil, true)

	first := commandtest.GetPackage(t, "first/pkg", "2.3.4")
	first.SetLicense(php.ListOf("MIT"))

	second := commandtest.GetPackage(t, "second/pkg", "3.4.0")
	second.SetLicense(php.ListOf("LGPL-2.0-only"))
	second.SetHomepage(pkg.Str("https://example.org"))

	third := commandtest.GetPackage(t, "third/pkg", "1.5.4")

	dev := commandtest.GetPackage(t, "dev/pkg", "2.3.4.5")
	dev.SetLicense(php.ListOf("MIT"))

	packages := []pkg.PackageInterface{first, second, third}
	devPackages := []pkg.PackageInterface{dev}
	commandtest.CreateInstalledJSON(t, packages, devPackages, true)
	commandtest.CreateComposerLock(t, packages, devPackages)
}

// assertLicenseLines checks each non-blank output line against the
// expected cells joined by \s+ (preg_quote'd).
func assertLicenseLines(t *testing.T, display string, expected [][]string) {
	t.Helper()
	for i, line := range strings.Split(display, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if i >= len(expected) {
			t.Fatalf("Got more output lines than expected")
		}
		quoted := make([]string, len(expected[i]))
		for j, v := range expected[i] {
			quoted[j] = regexp.QuoteMeta(v)
		}
		re := regexp.MustCompile(strings.Join(quoted, `\s+`))
		if !re.MatchString(line) {
			t.Errorf("line %d %q does not match %q", i, line, re)
		}
	}
}

var licensesAllLines = [][]string{
	{"Name:", "test/pkg"},
	{"Version:", "1.2.3"},
	{"Licenses:", "MIT"},
	{"Dependencies:"},
	{},
	{"Name", "Version", "Licenses"},
	{"dev/pkg", "2.3.4.5", "MIT"},
	{"first/pkg", "2.3.4", "MIT"},
	{"second/pkg", "3.4.0", "LGPL-2.0-only"},
	{"third/pkg", "1.5.4", "none"},
}

var licensesNoDevLines = [][]string{
	{"Name:", "test/pkg"},
	{"Version:", "1.2.3"},
	{"Licenses:", "MIT"},
	{"Dependencies:"},
	{},
	{"Name", "Version", "Licenses"},
	{"first/pkg", "2.3.4", "MIT"},
	{"second/pkg", "3.4.0", "LGPL-2.0-only"},
	{"third/pkg", "1.5.4", "none"},
}

func TestLicensesCommand_BasicRun(t *testing.T) {
	licensesSetUp(t)
	appTester := commandtest.GetApplicationTester(t)
	if code := licensesRun(t, appTester, console.P("command", "license")); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	assertLicenseLines(t, appTester.Display(false), licensesAllLines)
}

func TestLicensesCommand_NoDev(t *testing.T) {
	licensesSetUp(t)
	appTester := commandtest.GetApplicationTester(t)
	if code := licensesRun(t, appTester, console.P("command", "license"), console.P("--no-dev", true)); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	assertLicenseLines(t, appTester.Display(false), licensesNoDevLines)
}

func TestLicensesCommand_FormatJson(t *testing.T) {
	licensesSetUp(t)
	appTester := commandtest.GetApplicationTester(t)
	code, err := appTester.Run([]console.Param{console.P("command", "license"), console.P("--format", "json")}, commandtest.Options{CaptureStderrSeparately: true})
	if err != nil || code != 0 {
		t.Fatalf("code %d, err %v", code, err)
	}

	expected := `{
    "name": "test/pkg",
    "version": "1.2.3",
    "license": [
        "MIT"
    ],
    "dependencies": {
        "dev/pkg": {
            "version": "2.3.4.5",
            "license": [
                "MIT"
            ]
        },
        "first/pkg": {
            "version": "2.3.4",
            "license": [
                "MIT"
            ]
        },
        "second/pkg": {
            "version": "3.4.0",
            "license": [
                "LGPL-2.0-only"
            ]
        },
        "third/pkg": {
            "version": "1.5.4",
            "license": []
        }
    }
}`
	if got := strings.TrimSpace(appTester.Display(false)); got != expected {
		t.Errorf("got:\n%s\nwant:\n%s", got, expected)
	}
}

func TestLicensesCommand_FormatSummary(t *testing.T) {
	licensesSetUp(t)
	appTester := commandtest.GetApplicationTester(t)
	if code := licensesRun(t, appTester, console.P("command", "license"), console.P("--format", "summary")); code != 0 {
		t.Fatalf("exit code %d", code)
	}

	expected := [][2]string{
		{"-", "-"},
		{"License", "Number of dependencies"},
		{"-", "-"},
		{"MIT", "2"},
		{"LGPL-2.0-only", "1"},
		{"none", "1"},
		{"-", "-"},
	}

	lines := strings.Split(appTester.Display(false), "\n")
	for i, e := range expected {
		re := regexp.MustCompile(e[0] + `\s+` + e[1])
		if !re.MatchString(lines[i]) {
			t.Errorf("line %d %q does not match %q", i, lines[i], re)
		}
	}
}

func TestLicensesCommand_FormatUnknown(t *testing.T) {
	licensesSetUp(t)
	appTester := commandtest.GetApplicationTester(t)
	_, err := appTester.Run([]console.Param{console.P("command", "license"), console.P("--format", "unknown")}, commandtest.Options{})
	if _, ok := errors.AsType[*util.RuntimeError](err); !ok {
		t.Fatalf("expected a RuntimeException, got %v", err)
	}
}

func TestLicensesCommand_Locked(t *testing.T) {
	licensesSetUp(t)
	appTester := commandtest.GetApplicationTester(t)
	if code := licensesRun(t, appTester, console.P("command", "license"), console.P("--locked", true)); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	assertLicenseLines(t, appTester.Display(false), licensesAllLines)
}

func TestLicensesCommand_LockedNoDev(t *testing.T) {
	licensesSetUp(t)
	appTester := commandtest.GetApplicationTester(t)
	if code := licensesRun(t, appTester, console.P("command", "license"), console.P("--locked", true), console.P("--no-dev", true)); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	assertLicenseLines(t, appTester.Display(false), licensesNoDevLines)
}

func TestLicensesCommand_LockedWithoutLockFile(t *testing.T) {
	licensesSetUp(t)

	// Remove the lock file
	_ = os.Remove("./composer.lock")

	appTester := commandtest.GetApplicationTester(t)
	_, err := appTester.Run([]console.Param{console.P("command", "license"), console.P("--locked", true)}, commandtest.Options{})
	e, ok := errors.AsType[*util.UnexpectedValueError](err)
	if !ok {
		t.Fatalf("expected an UnexpectedValueException, got %v", err)
	}
	if e.Message != "Valid composer.json and composer.lock files are required to run this command with --locked" {
		t.Errorf("message %q", e.Message)
	}
}
