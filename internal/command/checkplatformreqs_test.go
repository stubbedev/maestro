// Ports tests/Composer/Test/Command/CheckPlatformReqsCommandTest.php.

package command_test

import (
	"errors"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
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
	if _, ok := errors.AsType[*util.LogicError](err); !ok {
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
}
