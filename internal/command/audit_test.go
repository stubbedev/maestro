// Ports tests/Composer/Test/Command/AuditCommandTest.php.

package command_test

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/semver"
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

// The empty-project shortcut runs before the options are validated, as in
// Composer: an invalid --format still skips the audit.
func TestAuditCommand_NoPackagesSkipsBeforeValidatingOptions(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)

	appTester := gbRun(t, gbParams("command", "audit", "--format", "xml"))
	if appTester.StatusCode() != advisory.StatusOK {
		t.Errorf("status %d", appTester.StatusCode())
	}
	gbAssertSame(t, "No packages - skipping audit.", gbTrim(appTester))
}

// An abandoned installed package fails the audit unless --abandoned
// reports or ignores it; failing is the default.
func TestAuditCommand_Abandoned(t *testing.T) {
	cases := []struct {
		abandoned string
		status    int
		listed    bool
	}{
		{"", advisory.StatusFailed, true},
		{policy.AuditFail, advisory.StatusFailed, true},
		{policy.AuditReport, advisory.StatusOK, true},
		{policy.AuditIgnore, advisory.StatusOK, false},
	}
	for _, c := range cases {
		t.Run("abandoned="+c.abandoned, func(t *testing.T) {
			commandtest.InitTempComposer(t, nil, nil, nil, true)
			abandoned := commandtest.GetPackage(t, "old/pkg", "1.0.0")
			abandoned.SetAbandoned("new/pkg")
			commandtest.CreateInstalledJSON(t, gbPkgs(abandoned), nil, true)

			params := gbParams("command", "audit")
			if c.abandoned != "" {
				params = gbParams("command", "audit", "--abandoned", c.abandoned)
			}
			appTester := gbRun(t, params)
			if appTester.StatusCode() != c.status {
				t.Errorf("status %d, want %d", appTester.StatusCode(), c.status)
			}
			display := gbTrim(appTester)
			gbContains(t, display, "No security vulnerability advisories found.")
			if got := strings.Contains(display, "Found 1 abandoned package:"); got != c.listed {
				t.Errorf("abandoned package listed: %v, want %v\n%s", got, c.listed, display)
			}
		})
	}
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
	auditInitTempComposerWithFilterLists(t, packages, nil)
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

func auditInitTempComposerWithFilterLists(t *testing.T, packages []auditTestPackage, extraConfig *php.Array) {
	t.Helper()
	config := php.ArrayOf(
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
	)
	// $extraConfig + [repositories => ..., config => ...]: the extra keys win
	if extraConfig != nil {
		merged := php.NewArray()
		for k, v := range extraConfig.All() {
			merged.SetKey(k, v)
		}
		for k, v := range config.All() {
			if _, ok := merged.Get(k.String()); !ok {
				merged.SetKey(k, v)
			}
		}
		config = merged
	}
	commandtest.InitTempComposer(t, config, nil, nil, true)
}

// TestAuditCommand_DirectSkipsTransitiveDependencies ports
// testAuditDirectSkipsTransitiveDependencies.
func TestAuditCommand_DirectSkipsTransitiveDependencies(t *testing.T) {
	packages := []auditTestPackage{{"safe/pkg", "1.0.0"}, {"malicious/pkg", "1.0.0"}}
	auditInitTempComposerWithFilterLists(t, packages, php.ArrayOf("require", php.ArrayOf("safe/pkg", "1.0.0")))
	auditCreateComposerLockWithPackages(t, packages)

	for _, locked := range []bool{false, true} {
		params := gbParams("command", "audit", "--direct", true)
		if locked {
			params = gbParams("command", "audit", "--direct", true, "--locked", true)
		}
		appTester := gbRun(t, params)

		if code := appTester.StatusCode(); code != advisory.StatusOK {
			t.Errorf("locked %v: exit code %d", locked, code)
		}
		gbNotContains(t, appTester.Display(true), "malicious/pkg")
	}

	appTester := gbRun(t, gbParams("command", "audit", "--locked", true))
	if code := appTester.StatusCode(); code != advisory.StatusFailed {
		t.Errorf("exit code %d", code)
	}
}

// TestAuditCommand_DirectIncludesReplacingPackage ports
// testAuditDirectIncludesReplacingPackage.
func TestAuditCommand_DirectIncludesReplacingPackage(t *testing.T) {
	auditInitTempComposerWithFilterLists(t, []auditTestPackage{{"malicious/pkg", "1.0.0"}}, php.ArrayOf("require", php.ArrayOf("replaced/pkg", "*")))

	p := commandtest.GetPackage(t, "malicious/pkg", "1.0.0")
	p.SetReplaces(pkg.LinksOf(pkg.NewLink("malicious/pkg", "replaced/pkg", semver.NewMatchAllConstraint(), pkg.TypeReplace, pkg.Str("*"))))
	commandtest.CreateInstalledJSON(t, gbPkgs(p), nil, true)

	appTester := gbRun(t, gbParams("command", "audit", "--direct", true))
	if code := appTester.StatusCode(); code != advisory.StatusFailed {
		t.Errorf("exit code %d", code)
	}
	gbContains(t, appTester.Display(true), "malicious/pkg")
}

// TestAuditCommand_DirectWithNoDirectPackagesInstalled ports
// testAuditDirectWithNoDirectPackagesInstalled.
func TestAuditCommand_DirectWithNoDirectPackagesInstalled(t *testing.T) {
	commandtest.InitTempComposer(t, php.ArrayOf("require", php.ArrayOf("php", "*")), nil, nil, true)
	commandtest.CreateInstalledJSON(t, gbPkgs(commandtest.GetPackage(t, "dummy/pkg", "1.0.0")), nil, true)

	appTester := gbRun(t, gbParams("command", "audit", "--direct", true))
	if code := appTester.StatusCode(); code != 0 {
		t.Errorf("exit code %d", code)
	}
	gbAssertSame(t, "No direct dependencies - skipping audit.", gbTrim(appTester))
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
