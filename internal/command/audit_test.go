// Ports tests/Composer/Test/Command/AuditCommandTest.php.

package command_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/policy"
)

func TestAuditCommand_SuccessfulResponseCodeWhenNoPackagesAreRequired(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)

	appTester := gbRun(t, gbParams("command", "audit"))
	if appTester.StatusCode() != 0 {
		t.Errorf("status %d", appTester.StatusCode())
	}
	gbAssertSame(t, "No packages - skipping audit.", gbTrim(appTester))
}

func TestAuditCommand_ErrorAuditingLockFileWhenItIsMissing(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	commandtest.CreateInstalledJSON(t, gbPkgs(commandtest.GetPackage(t, "dummy/pkg", "1.0.0")), nil, true)

	err := gbRunErr(t, gbParams("command", "audit", "--locked", true))
	if !phperr.InstanceOf(err, "UnexpectedValueException") {
		t.Errorf("expected an UnexpectedValueException, got %T", err)
	}
	gbAssertSame(t, "Valid composer.json and composer.lock files are required to run this command with --locked", err.Error())
}

func TestAuditCommand_AuditPackageWithNoSecurityVulnerabilities(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	packages := gbPkgs(commandtest.GetPackage(t, "dummy/pkg", "1.0.0"))
	commandtest.CreateInstalledJSON(t, packages, nil, true)
	commandtest.CreateComposerLock(t, packages, nil)

	appTester := gbRun(t, gbParams("command", "audit", "--locked", true))
	gbContains(t, gbTrim(appTester), "No security vulnerability advisories found.")
}

func TestAuditCommand_ErrorAuditWithNoInstalledPackages(t *testing.T) {
	commandtest.InitTempComposer(t, `{"require": {"dummy/pkg": "1.0.0"}}`, nil, nil, true)

	appTester := gbRun(t, gbParams("command", "audit"))
	if appTester.StatusCode() != advisory.StatusFailed {
		t.Errorf("status %d", appTester.StatusCode())
	}
	gbContains(t, gbTrim(appTester), `No installed packages found. Please run "composer install" before running "audit"`)
}

func TestAuditCommand_AuditPackageWithNoDevOptionPassed(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	devPackage := gbPkgs(commandtest.GetPackage(t, "dummy/pkg", "1.0.0"))
	commandtest.CreateInstalledJSON(t, nil, devPackage, true)
	commandtest.CreateComposerLock(t, nil, devPackage)

	appTester := gbRun(t, gbParams("command", "audit", "--no-dev", true))
	gbContains(t, gbTrim(appTester), "No packages - skipping audit.")
}

type auditTestPackage struct{ name, version string }

func auditPackageList(packages []auditTestPackage) *php.Array {
	list := php.NewArray()
	for _, p := range packages {
		list.Append(php.ArrayOf("name", p.name, "version", p.version))
	}

	return list
}

func auditFilterEntry(name, reason string) *php.Array {
	return php.ListOf(php.ArrayOf("package", name, "constraint", "*", "reason", reason))
}

func TestAuditCommand_AuditWithMalwareAndCustomListBothFail(t *testing.T) {
	packages := []auditTestPackage{{"safe/pkg", "1.0.0"}, {"malicious/pkg", "1.0.0"}, {"banned/pkg", "1.0.0"}}
	auditInitTempComposerWithFilterLists(t, packages)
	auditCreateComposerLockWithPackages(t, packages)

	appTester := commandtest.GetApplicationTester(t)
	exitCode, err := appTester.RunArgs(commandtest.Options{}, "command", "audit", "--locked", true)
	if err != nil {
		t.Fatal(err)
	}

	display := appTester.Display(true)
	gbContains(t, display, "Found 2 packages matching filters")
	gbContains(t, display, "malicious/pkg")
	gbContains(t, display, "banned/pkg")

	if exitCode != advisory.StatusFailed {
		t.Errorf("exit code %d", exitCode)
	}
}

func TestAuditCommand_AuditWithCustomListAuditReportDoesNotFail(t *testing.T) {
	packages := []auditTestPackage{{"safe/pkg", "1.0.0"}, {"banned/pkg", "1.0.0"}}
	commandtest.InitTempComposer(t, php.ArrayOf(
		"repositories", php.ArrayOf(
			"packages", php.ArrayOf(
				"type", "package",
				"package", auditPackageList(packages),
				"filter", php.ArrayOf(
					"company-banned", auditFilterEntry("banned/pkg", "company policy"),
				),
			),
		),
		"config", php.ArrayOf(
			"policy", php.ArrayOf(
				"company-banned", php.ArrayOf("audit", policy.AuditReport),
			),
		),
	), nil, nil, true)

	auditCreateComposerLockWithPackages(t, packages)

	appTester := commandtest.GetApplicationTester(t)
	exitCode, err := appTester.RunArgs(commandtest.Options{}, "command", "audit", "--locked", true)
	if err != nil {
		t.Fatal(err)
	}

	display := appTester.Display(true)
	gbContains(t, display, "Found 1 package matching filters")
	gbContains(t, display, "banned/pkg")
	if exitCode != advisory.StatusOK {
		t.Errorf("exit code %d", exitCode)
	}
}

func auditInitTempComposerWithFilterLists(t *testing.T, packages []auditTestPackage) {
	t.Helper()
	commandtest.InitTempComposer(t, php.ArrayOf(
		"repositories", php.ArrayOf(
			"packages", php.ArrayOf(
				"type", "package",
				"package", auditPackageList(packages),
				"filter", php.ArrayOf(
					"malware", auditFilterEntry("malicious/pkg", "malware sample"),
					"company-banned", auditFilterEntry("banned/pkg", "company policy"),
				),
			),
		),
		"config", php.ArrayOf(
			"policy", php.ArrayOf(
				"company-banned", true,
			),
		),
	), nil, nil, true)
}

func auditCreateComposerLockWithPackages(t *testing.T, packages []auditTestPackage) {
	t.Helper()
	list := make([]pkg.PackageInterface, len(packages))
	for i, p := range packages {
		list[i] = commandtest.GetPackage(t, p.name, p.version)
	}

	commandtest.CreateInstalledJSON(t, list, nil, true)
	commandtest.CreateComposerLock(t, list, nil)
}
