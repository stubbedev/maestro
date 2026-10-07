// Ports tests/Composer/Test/Command/CheckPlatformReqsCommandTest.php.

package command_test

import (
	"fmt"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
)

func checkPlatformReqsPackages(t *testing.T) (packages, devPackages []pkg.PackageInterface) {
	t.Helper()

	return gbPkgs(commandtest.GetPackage(t, "ext-foobar", "2.3.4")),
		gbPkgs(commandtest.GetPackage(t, "ext-barbaz", "2.3.4.5"))
}

func TestCheckPlatformReqsCommand_PlatformReqsAreSatisfied(t *testing.T) {
	cases := []struct {
		name         string
		composerJSON string
		command      gbKV
		expected     string
		lock         bool
	}{
		{
			name:         "Disables checking of require-dev packages requirements.",
			composerJSON: `{"require": {"ext-foobar": "^2.0"}, "require-dev": {"ext-barbaz": "~4.0"}}`,
			command:      gbParams("--no-dev", true),
			expected:     "Checking non-dev platform requirements for packages in the vendor dir\next-foobar 2.3.4   success",
			lock:         true,
		},
		{
			name:         "Checks requirements only from the lock file, not from installed packages.",
			composerJSON: `{"require": {"ext-foobar": "^2.3"}, "require-dev": {"ext-barbaz": "~2.0"}}`,
			command:      gbParams("--lock", true),
			expected:     "Checking platform requirements using the lock file\next-barbaz 2.3.4.5   success \next-foobar 2.3.4     success",
			lock:         true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, tc.composerJSON, nil, nil, true)

			packages, devPackages := checkPlatformReqsPackages(t)
			commandtest.CreateInstalledJSON(t, packages, devPackages, true)
			if tc.lock {
				commandtest.CreateComposerLock(t, packages, devPackages)
			}

			appTester := gbRun(t, gbMerge(gbParams("command", "check-platform-reqs"), tc.command, true))
			if appTester.StatusCode() != 0 {
				t.Errorf("status %d", appTester.StatusCode())
			}
			gbAssertSame(t, tc.expected, gbTrim(appTester))
		})
	}
}

func TestCheckPlatformReqsCommand_ExceptionThrownIfNoLockfileFound(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	err := gbRunErr(t, gbParams("command", "check-platform-reqs"))
	if !phperr.InstanceOf(err, "LogicException") {
		t.Errorf("expected a LogicException, got %T", err)
	}
	gbAssertSame(t, "No lockfile found. Unable to read locked packages", err.Error())
}

func TestCheckPlatformReqsCommand_FailedPlatformRequirement(t *testing.T) {
	commandtest.InitTempComposer(t, `{"require": {"ext-foobar": "^0.3"}, "require-dev": {"ext-barbaz": "^2.3"}}`, nil, nil, true)

	packages, devPackages := checkPlatformReqsPackages(t)
	commandtest.CreateInstalledJSON(t, packages, devPackages, true)
	commandtest.CreateComposerLock(t, packages, devPackages)

	appTester := gbRun(t, gbParams("command", "check-platform-reqs", "--format", "json"))

	expected := `Checking platform requirements for packages in the vendor dir
[
    {
        "name": "ext-barbaz",
        "version": "2.3.4.5",
        "status": "success",
        "failed_requirement": null,
        "provider": null
    },
    {
        "name": "ext-foobar",
        "version": "2.3.4",
        "status": "failed",
        "failed_requirement": {
            "source": "__root__",
            "type": "requires",
            "target": "ext-foobar",
            "constraint": "^0.3"
        },
        "provider": null
    }
]`
	gbAssertSame(t, expected, gbTrim(appTester))
	if appTester.StatusCode() != 1 {
		t.Errorf("status %d, want 1", appTester.StatusCode())
	}
}

// cprPackage is a package with links configured as ConfigureLinks takes
// them (link type => target => constraint).
func cprPackage(t *testing.T, name, version, links string) pkg.PackageInterface {
	t.Helper()
	p := commandtest.GetPackage(t, name, version)
	commandtest.ConfigureLinks(t, p, links)

	return p
}

// TestCheckPlatformReqsCommand_Rows checks each source of requirements
// (vendor dir, lock file, the fallback from an empty vendor dir to the
// lock), with and without dev requirements, and each row status with the
// exit code it gives: success (also through a provider), failed (1) and
// missing (2, which outranks failed).
func TestCheckPlatformReqsCommand_Rows(t *testing.T) {
	type fixture struct {
		composerJSON string
		// vendor writes the packages to installed.json (an empty one
		// otherwise); the lock always holds them
		vendor bool
	}
	packages := func(t *testing.T) (prod, dev []pkg.PackageInterface) {
		return gbPkgs(
				commandtest.GetPackage(t, "ext-foobar", "2.3.4"),
				cprPackage(t, "x/poly", "1.0.0", `{"provide": {"ext-polyfill": "1.2.0"}}`),
			),
			gbPkgs(cprPackage(t, "x/devtool", "1.0.0", `{"require": {"ext-devonly": "^1.0"}}`))
	}
	satisfied := fixture{composerJSON: `{"require": {"ext-foobar": "^2.0", "ext-polyfill": "^1.0"}}`, vendor: true}
	fallback := fixture{composerJSON: satisfied.composerJSON}
	failed := fixture{composerJSON: `{"require": {"ext-foobar": "^0.3", "ext-polyfill": "^1.0"}}`, vendor: true}

	const (
		vendorHeader  = "Checking platform requirements for packages in the vendor dir\n"
		vendorNonDev  = "Checking non-dev platform requirements for packages in the vendor dir\n"
		satisfiedRows = "ext-foobar   2.3.4   success\next-polyfill 1.2.0   success provided by x/poly\n"
		// The third column is the failed link itself (Link::__toString,
		// with its normalised constraint), the fourth its pretty form.
		missingRows = "" +
			"ext-devonly  n/a   x/devtool requires ext-devonly ([>= 1.0.0.0-dev < 2.0.0.0-dev]) x/devtool requires ext-devonly (^1.0) missing\n" +
			"ext-foobar   2.3.4                                                                                                       success\n" +
			"ext-polyfill 1.2.0                                                                                                       success provided by x/poly\n"
		failedRows = "" +
			"ext-foobar   2.3.4 __root__ requires ext-foobar ([>= 0.3.0.0-dev < 0.4.0.0-dev]) __root__ requires ext-foobar (^0.3) failed\n" +
			"ext-polyfill 1.2.0                                                                                                   success provided by x/poly\n"
		missingAndFailedRows = "" +
			"ext-devonly  n/a   x/devtool requires ext-devonly ([>= 1.0.0.0-dev < 2.0.0.0-dev]) x/devtool requires ext-devonly (^1.0) missing\n" +
			"ext-foobar   2.3.4 __root__ requires ext-foobar ([>= 0.3.0.0-dev < 0.4.0.0-dev])   __root__ requires ext-foobar (^0.3)   failed\n" +
			"ext-polyfill 1.2.0                                                                                                       success provided by x/poly\n"
		// The ApplicationTester's output has none of the styles
		// Factory::createOutput adds, as in Composer's own tests.
		fallbackHeader = "<warning>No vendor dir present, checking %splatform requirements from the lock file</warning>\n"
	)
	cases := []struct {
		name    string
		fixture fixture
		args    []any
		code    int
		stdout  string
		stderr  string
	}{
		{
			name:    "a provider satisfies a requirement",
			fixture: satisfied,
			args:    []any{"--no-dev", true},
			stdout:  satisfiedRows,
			stderr:  vendorNonDev,
		},
		{
			name:    "a provider in json",
			fixture: satisfied,
			args:    []any{"-f", "json", "--no-dev", true},
			stdout: `[
    {
        "name": "ext-foobar",
        "version": "2.3.4",
        "status": "success",
        "failed_requirement": null,
        "provider": null
    },
    {
        "name": "ext-polyfill",
        "version": "1.2.0",
        "status": "success",
        "failed_requirement": null,
        "provider": "provided by x/poly"
    }
]
`,
			stderr: vendorNonDev,
		},
		{
			name:    "an unknown format renders the table",
			fixture: satisfied,
			args:    []any{"--format", "xml", "--no-dev", true},
			stdout:  satisfiedRows,
			stderr:  vendorNonDev,
		},
		{
			name:    "a dev requirement nothing provides is missing",
			fixture: satisfied,
			code:    2,
			stdout:  missingRows,
			stderr:  vendorHeader,
		},
		{
			name:    "an empty vendor dir falls back to the lock file",
			fixture: fallback,
			code:    2,
			stdout:  missingRows,
			stderr:  fmt.Sprintf(fallbackHeader, ""),
		},
		{
			name:    "the lock fallback without dev packages",
			fixture: fallback,
			args:    []any{"--no-dev", true},
			stdout:  satisfiedRows,
			stderr:  fmt.Sprintf(fallbackHeader, "non-dev "),
		},
		{
			name:    "the lock file without dev packages",
			fixture: satisfied,
			args:    []any{"--lock", true, "--no-dev", true},
			stdout:  satisfiedRows,
			stderr:  "Checking non-dev platform requirements using the lock file\n",
		},
		{
			name:    "a failed requirement",
			fixture: failed,
			args:    []any{"--no-dev", true},
			code:    1,
			stdout:  failedRows,
			stderr:  vendorNonDev,
		},
		{
			name:    "missing outranks failed",
			fixture: failed,
			code:    2,
			stdout:  missingAndFailedRows,
			stderr:  vendorHeader,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, tc.fixture.composerJSON, nil, nil, true)
			prod, dev := packages(t)
			if tc.fixture.vendor {
				commandtest.CreateInstalledJSON(t, prod, dev, true)
			} else {
				commandtest.CreateInstalledJSON(t, nil, nil, true)
			}
			commandtest.CreateComposerLock(t, prod, dev)

			got := commandtest.GetApplicationTester(t).RunStreams(append([]any{"command", "check-platform-reqs"}, tc.args...)...)
			if got.Err != nil {
				t.Fatal(got.Err)
			}
			if got.Code != tc.code {
				t.Errorf("exit code %d, want %d", got.Code, tc.code)
			}
			gbAssertSame(t, tc.stdout, commandtest.TrimLines(got.Stdout)+"\n")
			gbAssertSame(t, tc.stderr, got.Stderr)
		})
	}
}
