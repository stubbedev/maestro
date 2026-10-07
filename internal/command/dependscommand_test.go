// Ports tests/Composer/Test/Command/BaseDependencyCommandTest.php (it
// exercises DependsCommand and ProhibitsCommand).

package command_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
)

func TestBaseDependencyCommandTest_ExceptionWhenNoRequiredParameters(t *testing.T) {
	cases := []struct {
		name       string
		command    string
		parameters gbKV
		expected   string
	}{
		{"`why` command without package parameter", "why", nil, `Not enough arguments (missing: "package").`},
		{"`why-not` command without package and version parameters", "why-not", nil, `Not enough arguments (missing: "package, version").`},
		{"`why-not` command without package parameter", "why-not", gbParams("version", "*"), `Not enough arguments (missing: "package").`},
		{"`why-not` command without version parameter", "why-not", gbParams("package", "vendor1/package1"), `Not enough arguments (missing: "version").`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := gbRunErr(t, gbMerge(gbParams("command", tc.command), tc.parameters, false))
			if !phperr.InstanceOf(err, "RuntimeException") {
				t.Errorf("expected a RuntimeException, got %T: %v", err, err)
			}
			gbAssertSame(t, tc.expected, err.Error())
		})
	}
}

var dependsCases = []struct {
	name       string
	command    string
	parameters gbKV
}{
	{"`why` command", "why", gbParams("package", "vendor1/package1")},
	{"`why-not` command", "why-not", gbParams("package", "vendor1/package1", "version", "1.*")},
}

func TestBaseDependencyCommandTest_ExceptionWhenRunningLockedWithoutLockFile(t *testing.T) {
	for _, tc := range dependsCases {
		t.Run(tc.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, nil, nil, nil, true)

			err := gbRunErr(t, gbMerge(gbMerge(gbParams("command", tc.command), tc.parameters, false), gbParams("--locked", true), false))
			if !phperr.InstanceOf(err, "UnexpectedValueException") {
				t.Errorf("expected an UnexpectedValueException, got %T: %v", err, err)
			}
			gbAssertSame(t, "A valid composer.lock file is required to run this command with --locked", err.Error())
		})
	}
}

func TestBaseDependencyCommandTest_ExceptionWhenItCouldNotFoundThePackage(t *testing.T) {
	for _, tc := range dependsCases {
		t.Run(tc.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, nil, nil, nil, true)

			err := gbRunErr(t, gbMerge(gbParams("command", tc.command), tc.parameters, false))
			if !gbIsInvalidArgument(err) {
				t.Errorf("expected an InvalidArgumentException, got %T: %v", err, err)
			}
			gbAssertSame(t, `Could not find package "vendor1/package1" in your project`, err.Error())
		})
	}
}

func TestBaseDependencyCommandTest_ExceptionWhenPackageWasNotFoundInProject(t *testing.T) {
	for _, tc := range dependsCases {
		t.Run(tc.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, php.ArrayOf(
				"require", php.ArrayOf(
					"vendor1/package2", "1.*",
					"vendor2/package1", "2.*",
				),
			), nil, nil, true)

			first := commandtest.GetPackage(t, "vendor1/package2", "1.0.0")
			second := commandtest.GetPackage(t, "vendor2/package1", "1.0.0")

			commandtest.CreateInstalledJSON(t, gbPkgs(first, second), nil, true)
			commandtest.CreateComposerLock(t, gbPkgs(first, second), nil)

			err := gbRunErr(t, gbMerge(gbParams("command", tc.command), tc.parameters, false))
			if !gbIsInvalidArgument(err) {
				t.Errorf("expected an InvalidArgumentException, got %T: %v", err, err)
			}
			gbAssertSame(t, `Could not find package "vendor1/package1" in your project`, err.Error())
		})
	}
}

func TestBaseDependencyCommandTest_WarningWhenDependenciesAreNotInstalled(t *testing.T) {
	for _, tc := range dependsCases {
		t.Run(tc.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, php.ArrayOf(
				"require", php.ArrayOf("vendor1/package1", "1.*"),
				"require-dev", php.ArrayOf("vendor2/package1", "2.*"),
			), nil, nil, true)

			required := commandtest.GetPackage(t, "vendor1/package1", "1.0.0")
			devRequired := commandtest.GetPackage(t, "vendor2/package1", "1.0.0")

			commandtest.CreateComposerLock(t, gbPkgs(required), gbPkgs(devRequired))

			// Composer's test asserts the text; the line also goes to
			// stdout, not stderr, and the run fails
			appTester := commandtest.GetApplicationTester(t)
			code, err := appTester.Run(gbMerge(gbParams("command", tc.command), tc.parameters, false), commandtest.Options{CaptureStderrSeparately: true})
			if err != nil {
				t.Fatal(err)
			}
			if code != 1 {
				t.Errorf("status %d, want 1", code)
			}
			gbAssertSame(t, "<warning>No dependencies installed. Try running composer install or update, or use --locked.</warning>", gbTrim(appTester))
			gbAssertSame(t, "", appTester.ErrorOutput(true))
		})
	}
}

func dependsMatchAllLink(source, target, prettyConstraint string) *pkg.Link {
	return pkg.NewLink(source, target, semver.NewMatchAllConstraint(), pkg.TypeRequire, pkg.Str(prettyConstraint))
}

func TestBaseDependencyCommandTest_WhyCommandOutputs(t *testing.T) {
	cases := []struct {
		name       string
		parameters gbKV
		expected   string
		status     int
	}{
		{"there is no installed package depending on the package", gbParams("package", "vendor1/package1"), `There is no installed package depending on "vendor1/package1"`, 1},
		{"a nested package dependency", gbParams("package", "vendor1/package3"), `__root__         -     requires vendor1/package3 (2.3.0)
vendor1/package2 2.3.0 requires vendor1/package3 (^1)`, 0},
		{"a nested package dependency (tree mode)", gbParams("package", "vendor1/package3", "--tree", true), "vendor1/package3 2.1.0\n" +
			"|--__root__ (requires vendor1/package3 2.3.0)\n" +
			"`--vendor1/package2 2.3.0 (requires vendor1/package3 ^1)\n" +
			"   |--__root__ (requires vendor1/package2 1.3.0)\n" +
			"   `--vendor1/package1 1.3.0 (requires vendor1/package2 ^2)", 0},
		{"a nested package dependency (recursive mode)", gbParams("package", "vendor1/package3", "--recursive", true), `__root__         -     requires vendor1/package2 (1.3.0)
vendor1/package1 1.3.0 requires vendor1/package2 (^2)
__root__         -     requires vendor1/package3 (2.3.0)
vendor1/package2 2.3.0 requires vendor1/package3 (^1)`, 0},
		{"a simple package dev dependency", gbParams("package", "vendor2/package1"), `__root__ - requires (for development) vendor2/package1 (2.*)`, 0},
	}
	// Composer runs the why alias: each name depends registers gives the
	// same output
	depends := command.NewDependsCommand()
	for _, tc := range cases {
		for _, name := range append([]string{depends.Name()}, depends.Aliases()...) {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				var pkgName any
				renderAsTree, renderRecursively := false, false
				for _, p := range tc.parameters {
					switch p.Key {
					case "package":
						pkgName = p.Value
					case "--tree":
						renderAsTree, _ = p.Value.(bool)
					case "--recursive":
						renderRecursively, _ = p.Value.(bool)
					}
				}

				commandtest.InitTempComposer(t, php.ArrayOf(
					"repositories", gbPackageRepo(
						gbRepoPackage("name", "vendor1/package1", "version", "1.3.0", "require", php.ArrayOf("vendor1/package2", "^2")),
						gbRepoPackage("name", "vendor1/package2", "version", "2.3.0", "require", php.ArrayOf("vendor1/package3", "^1")),
						gbRepoPackage("name", "vendor1/package3", "version", "2.1.0"),
					),
					"require", php.ArrayOf(
						"vendor1/package2", "1.3.0",
						"vendor1/package3", "2.3.0",
					),
					"require-dev", php.ArrayOf("vendor2/package1", "2.*"),
				), nil, nil, true)

				first := commandtest.GetPackage(t, "vendor1/package1", "1.3.0")
				first.SetRequires(pkg.LinksOf(dependsMatchAllLink("vendor1/package1", "vendor1/package2", "^2")))
				second := commandtest.GetPackage(t, "vendor1/package2", "2.3.0")
				second.SetRequires(pkg.LinksOf(dependsMatchAllLink("vendor1/package2", "vendor1/package3", "^1")))
				third := commandtest.GetPackage(t, "vendor1/package3", "2.1.0")
				dev := commandtest.GetPackage(t, "vendor2/package1", "1.0.0")
				commandtest.CreateComposerLock(t, gbPkgs(first, second, third), gbPkgs(dev))
				commandtest.CreateInstalledJSON(t, gbPkgs(first, second, third), gbPkgs(dev), true)

				appTester := commandtest.GetApplicationTester(t)
				_, _ = appTester.Run(gbParams(
					"command", name,
					"package", pkgName,
					"--tree", renderAsTree,
					"--recursive", renderRecursively,
					"--locked", true,
				), commandtest.Options{})

				if appTester.StatusCode() != tc.status {
					t.Errorf("status %d, want %d", appTester.StatusCode(), tc.status)
				}
				gbAssertSame(t, php.Trim(tc.expected), commandtest.TrimLines(appTester.Display(true)))
			})
		}
	}
}

func TestBaseDependencyCommandTest_WhyNotCommandOutputs(t *testing.T) {
	cases := []struct {
		name     string
		pkg      string
		version  string
		expected string
		status   int
	}{
		{"it could not found the package with a specific version", "vendor1/package1", "3.*", `Package "vendor1/package1" could not be found with constraint "3.*", results below will most likely be incomplete.
__root__ - requires vendor1/package1 (1.*)
Not finding what you were looking for? Try calling ` + "`composer require \"vendor1/package1:3.*\" --dry-run`" + ` to get another view on the problem.`, 1},
		{"it could not found the package and there is no installed package with a specific version", "vendor1/package1", "^1.4", `Package "vendor1/package1" could not be found with constraint "^1.4", results below will most likely be incomplete.
There is no installed package depending on "vendor1/package1" in versions not matching ^1.4
Not finding what you were looking for? Try calling ` + "`composer require \"vendor1/package1:^1.4\" --dry-run`" + ` to get another view on the problem.`, 0},
		{"Package is already installed!", "vendor1/package1", "^1.3", `Package "vendor1/package1" 1.3.0 is already installed! To find out why, run ` + "`composer why vendor1/package1`", 0},
		{"an installed package requires an incompatible version of the inspected package", "vendor2/package3", "1.5.0", `vendor2/package2 1.0.0 requires vendor2/package3 (1.4.*)
Not finding what you were looking for? Try calling ` + "`composer update \"vendor2/package3:1.5.0\" --dry-run`" + ` to get another view on the problem.`, 1},
		{"all compatible with the inspected platform package (range matching installed)", "php", "^8", `Package "php ^8" found in version "8.3.2" (version provided by config.platform).
There is no installed package depending on "php" in versions not matching ^8`, 0},
		{"an installed package requires an incompatible version of the inspected platform package (fixed non-matching package)", "php", "9.1.0", `__root__         -     requires php (^8)
vendor2/package2 1.0.0 requires php (^8.2)`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, php.ArrayOf(
				"repositories", gbPackageRepo(
					gbRepoPackage("name", "vendor1/package1", "version", "1.3.0"),
					gbRepoPackage("name", "vendor2/package1", "version", "2.0.0"),
					gbRepoPackage("name", "vendor2/package2", "version", "1.0.0", "require", php.ArrayOf("vendor2/package3", "1.4.*", "php", "^8.2")),
					gbRepoPackage("name", "vendor2/package3", "version", "1.4.0"),
					gbRepoPackage("name", "vendor2/package3", "version", "1.5.0"),
				),
				"require", php.ArrayOf(
					"vendor1/package1", "1.*",
					"php", "^8",
				),
				"require-dev", php.ArrayOf(
					"vendor2/package1", "2.*",
					"vendor2/package2", "^1",
				),
				"config", php.ArrayOf("platform", php.ArrayOf("php", "8.3.2")),
			), nil, nil, true)

			required := commandtest.GetPackage(t, "vendor1/package1", "1.3.0")
			firstDev := commandtest.GetPackage(t, "vendor2/package1", "2.0.0")
			secondDev := commandtest.GetPackage(t, "vendor2/package2", "1.0.0")
			lower := semver.NewConstraintOp(semver.OpGE, "8.2.0.0")
			lower.SetPrettyString(">= 8.2.0.0")
			upper := semver.NewConstraintOp(semver.OpLT, "9.0.0.0-dev")
			upper.SetPrettyString("< 9.0.0.0-dev")
			phpConstraint, err := semver.NewMultiConstraint([]semver.ConstraintInterface{lower, upper}, true)
			if err != nil {
				t.Fatal(err)
			}
			secondDev.SetRequires(pkg.LinksOf(
				dependsMatchAllLink("vendor2/package2", "vendor2/package3", "1.4.*"),
				pkg.NewLink("vendor2/package2", "php", phpConstraint, pkg.TypeRequire, pkg.Str("^8.2")),
			))
			nested := commandtest.GetPackage(t, "vendor2/package3", "1.4.0")

			commandtest.CreateComposerLock(t, gbPkgs(required), gbPkgs(firstDev, secondDev))
			commandtest.CreateInstalledJSON(t, gbPkgs(required), gbPkgs(firstDev, secondDev, nested), true)

			appTester := commandtest.GetApplicationTester(t)
			_, _ = appTester.Run(gbParams(
				"command", "why-not",
				"package", tc.pkg,
				"version", tc.version,
			), commandtest.Options{})

			if appTester.StatusCode() != tc.status {
				t.Errorf("status %d, want %d", appTester.StatusCode(), tc.status)
			}
			gbAssertSame(t, php.Trim(tc.expected), commandtest.TrimLines(appTester.Display(true)))
		})
	}
}
