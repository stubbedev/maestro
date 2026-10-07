// Ports tests/Composer/Test/Command/ShowCommandTest.php.

package command_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/platform"
	"github.com/stubbedev/maestro/internal/repository"
)

func TestShowCommand_Show(t *testing.T) {
	cases := []struct {
		name     string
		command  gbKV
		expected string
		requires []string
	}{
		{"default shows installed with version and description", nil, `outdated/major 1.0.0
outdated/minor 1.0.0
outdated/patch 1.0.0
vendor/package 1.0.0 description of installed package`, nil},
		{"with -s and --installed shows list of installed + self package", gbParams("--installed", true, "--self", true), `outdated/major 1.0.0
outdated/minor 1.0.0
outdated/patch 1.0.0
root/pkg       1.2.3
vendor/package 1.0.0 description of installed package`, nil},
		{"with -s and --locked shows list of installed + self package", gbParams("--locked", true, "--self", true), `root/pkg      1.2.3
vendor/locked 3.0.0 description of locked package`, nil},
		{"with -a show available packages with description but no version", gbParams("-a", true), `outdated/major outdated/major v2.0.0 description
outdated/minor outdated/minor v1.1.1 description
outdated/patch outdated/patch v1.0.1 description
vendor/package generic description`, nil},
		{"show with --direct shows nothing if no deps", gbParams("--direct", true), ``, nil},
		{"show with --direct shows only root deps", gbParams("--direct", true), `outdated/major 1.0.0`, []string{"outdated/major", "*"}},
		{"outdated deps", gbParams("command", "outdated"), `Legend:
! patch or minor release available - update recommended
~ major release available - update possible

Direct dependencies required in composer.json:
Everything up to date

Transitive dependencies not required in composer.json:
outdated/major 1.0.0 ~ 2.0.0
outdated/minor 1.0.0 <highlight>! 1.1.1</highlight>
outdated/patch 1.0.0 <highlight>! 1.0.1</highlight>`, nil},
		{"outdated deps sorting by age", gbParams("command", "outdated", "--sort-by-age", true), `Legend:
! patch or minor release available - update recommended
~ major release available - update possible

Direct dependencies required in composer.json:
Everything up to date

Transitive dependencies not required in composer.json:
outdated/minor 1.0.0 <highlight>! 1.1.1</highlight> 2 years old
outdated/patch 1.0.0 <highlight>! 1.0.1</highlight> 2 weeks old
outdated/major 1.0.0 ~ 2.0.0 from today`, nil},
		{"outdated deps with --direct only show direct deps with updated", gbParams("command", "outdated", "--direct", true), `Legend:
! patch or minor release available - update recommended
~ major release available - update possible
outdated/major 1.0.0 ~ 2.0.0`, []string{"vendor/package", "*", "outdated/major", "*"}},
		{"outdated deps with --direct show msg if all up to date", gbParams("command", "outdated", "--direct", true), `All your direct dependencies are up to date`, []string{"vendor/package", "*"}},
		{"outdated deps with --major-only only shows major updates", gbParams("command", "outdated", "--major-only", true), `Legend:
! patch or minor release available - update recommended
~ major release available - update possible

Direct dependencies required in composer.json:
Everything up to date

Transitive dependencies not required in composer.json:
outdated/major 1.0.0 ~ 2.0.0`, nil},
		{"outdated deps with --minor-only only shows minor updates", gbParams("command", "outdated", "--minor-only", true), `Legend:
! patch or minor release available - update recommended
~ major release available - update possible

Direct dependencies required in composer.json:
outdated/minor 1.0.0 <highlight>! 1.1.1</highlight>

Transitive dependencies not required in composer.json:
outdated/major 1.0.0 <highlight>! 1.1.1</highlight>
outdated/patch 1.0.0 <highlight>! 1.0.1</highlight>`, []string{"outdated/minor", "*"}},
		{"outdated deps with --patch-only only shows patch updates", gbParams("command", "outdated", "--patch-only", true), `Legend:
! patch or minor release available - update recommended
~ major release available - update possible

Direct dependencies required in composer.json:
Everything up to date

Transitive dependencies not required in composer.json:
outdated/major 1.0.0 <highlight>! 1.0.1</highlight>
outdated/minor 1.0.0 <highlight>! 1.0.1</highlight>
outdated/patch 1.0.0 <highlight>! 1.0.1</highlight>`, nil},
	}

	// The release ages are computed in PHP's default time zone, as
	// Composer's are (its test creates the dates with new
	// DateTimeImmutable() in that zone).
	var snap *platform.Snapshot
	if view, _, err := commandtest.Runtime().ComposerView(); err == nil {
		snap = view
	}

	loc := command.PHPDefaultTimezone(snap)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A run that straddles midnight in that zone sees "today" turn
			// into "this week"; it is repeated once.
			for attempt := 0; ; attempt++ {
				now := time.Now().In(loc)
				got := runShowCase(t, tc.requires, tc.command, now)

				if attempt == 0 && now.Format("20060102") != time.Now().In(loc).Format("20060102") {
					continue
				}

				gbAssertSame(t, php.Trim(tc.expected), got)

				break
			}
		})
	}
}

// runShowCase is one TestShowCommand_Show case at the moment now: the
// project, then `show` with the case's options.
func runShowCase(t *testing.T, requires []string, cmd gbKV, now time.Time) string {
	t.Helper()

	commandtest.InitTempComposer(t, php.ArrayOf(
		"name", "root/pkg",
		"version", "1.2.3",
		"repositories", gbPackageRepo(
			gbRepoPackage("name", "vendor/package", "description", "generic description", "version", "v1.0.0"),

			gbRepoPackage("name", "outdated/major", "description", "outdated/major v1.0.0 description", "version", "v1.0.0"),
			gbRepoPackage("name", "outdated/major", "description", "outdated/major v1.0.1 description", "version", "v1.0.1"),
			gbRepoPackage("name", "outdated/major", "description", "outdated/major v1.1.0 description", "version", "v1.1.0"),
			gbRepoPackage("name", "outdated/major", "description", "outdated/major v1.1.1 description", "version", "v1.1.1"),
			gbRepoPackage("name", "outdated/major", "description", "outdated/major v2.0.0 description", "version", "v2.0.0"),

			gbRepoPackage("name", "outdated/minor", "description", "outdated/minor v1.0.0 description", "version", "1.0.0"),
			gbRepoPackage("name", "outdated/minor", "description", "outdated/minor v1.0.1 description", "version", "1.0.1"),
			gbRepoPackage("name", "outdated/minor", "description", "outdated/minor v1.1.0 description", "version", "1.1.0"),
			gbRepoPackage("name", "outdated/minor", "description", "outdated/minor v1.1.1 description", "version", "1.1.1"),

			gbRepoPackage("name", "outdated/patch", "description", "outdated/patch v1.0.0 description", "version", "1.0.0"),
			gbRepoPackage("name", "outdated/patch", "description", "outdated/patch v1.0.1 description", "version", "1.0.1"),
		),
		"require", gbRequireMap(requires...),
	), nil, nil, true)

	p := commandtest.GetPackage(t, "vendor/package", "v1.0.0")
	p.SetDescription(pkg.Str("description of installed package"))
	major := commandtest.GetPackage(t, "outdated/major", "v1.0.0")
	major.SetReleaseDate(now, true)
	minor := commandtest.GetPackage(t, "outdated/minor", "1.0.0")
	minor.SetReleaseDate(now.AddDate(-2, 0, 0), true)
	patch := commandtest.GetPackage(t, "outdated/patch", "1.0.0")
	// 14 days of 24 hours: AddDate would make it 13 days and 23
	// hours across a DST change ("last week").
	patch.SetReleaseDate(now.Add(-14*24*time.Hour), true)

	commandtest.CreateInstalledJSON(t, gbPkgs(p, major, minor, patch), nil, true)

	locked := commandtest.GetPackage(t, "vendor/locked", "3.0.0")
	locked.SetDescription(pkg.Str("description of locked package"))
	commandtest.CreateComposerLock(t, gbPkgs(locked), nil)

	appTester := gbRun(t, gbMerge(gbParams("command", "show"), cmd, true))

	return gbTrim(appTester)
}

func TestShowCommand_OutdatedFiltersAccordingToPlatformReqsAndWarns(t *testing.T) {
	commandtest.InitTempComposer(t, php.ArrayOf(
		"repositories", gbPackageRepo(
			gbRepoPackage("name", "vendor/package", "description", "generic description", "version", "1.0.0"),
			gbRepoPackage("name", "vendor/package", "description", "generic description", "version", "1.1.0", "require", php.ArrayOf("ext-missing", "3")),
			gbRepoPackage("name", "vendor/package", "description", "generic description", "version", "1.2.0", "require", php.ArrayOf("ext-missing", "3")),
			gbRepoPackage("name", "vendor/package", "description", "generic description", "version", "1.3.0", "require", php.ArrayOf("ext-missing", "3")),
		),
	), nil, nil, true)

	commandtest.CreateInstalledJSON(t, gbPkgs(commandtest.GetPackage(t, "vendor/package", "1.1.0")), nil, true)

	appTester := gbRun(t, gbParams("command", "outdated"))
	gbAssertSame(t, `<warning>Cannot use vendor/package 1.1.0 as it requires ext-missing 3 which is missing from your platform.
Legend:
! patch or minor release available - update recommended
~ major release available - update possible

Direct dependencies required in composer.json:
Everything up to date

Transitive dependencies not required in composer.json:
vendor/package 1.1.0 ~ 1.0.0`, gbTrim(appTester))

	appTester = gbRun(t, gbParams("command", "outdated", "--verbose", true))
	gbAssertSame(t, `<warning>Cannot use vendor/package's latest version 1.3.0 as it requires ext-missing 3 which is missing from your platform.
<warning>Cannot use vendor/package 1.2.0 as it requires ext-missing 3 which is missing from your platform.
<warning>Cannot use vendor/package 1.1.0 as it requires ext-missing 3 which is missing from your platform.
Legend:
! patch or minor release available - update recommended
~ major release available - update possible

Direct dependencies required in composer.json:
Everything up to date

Transitive dependencies not required in composer.json:
vendor/package 1.1.0 ~ 1.0.0`, gbTrim(appTester))
}

func TestShowCommand_OutdatedFiltersAccordingToPlatformReqsWithoutWarningForHigherVersions(t *testing.T) {
	commandtest.InitTempComposer(t, php.ArrayOf(
		"repositories", gbPackageRepo(
			gbRepoPackage("name", "vendor/package", "description", "generic description", "version", "1.0.0"),
			gbRepoPackage("name", "vendor/package", "description", "generic description", "version", "1.1.0"),
			gbRepoPackage("name", "vendor/package", "description", "generic description", "version", "1.2.0"),
			gbRepoPackage("name", "vendor/package", "description", "generic description", "version", "1.3.0", "require", php.ArrayOf("php", "^99")),
		),
	), nil, nil, true)

	commandtest.CreateInstalledJSON(t, gbPkgs(commandtest.GetPackage(t, "vendor/package", "1.1.0")), nil, true)

	appTester := gbRun(t, gbParams("command", "outdated"))
	gbAssertSame(t, `Legend:
! patch or minor release available - update recommended
~ major release available - update possible

Direct dependencies required in composer.json:
Everything up to date

Transitive dependencies not required in composer.json:
vendor/package 1.1.0 <highlight>! 1.2.0</highlight>`, gbTrim(appTester))
}

func TestShowCommand_ShowDirectWithNameDoesNotShowTransientDependencies(t *testing.T) {
	commandtest.InitTempComposer(t, php.ArrayOf(
		"repositories", php.NewArray(),
		"require", php.ArrayOf("direct/dependent", "*"),
	), nil, nil, true)

	direct := commandtest.GetPackage(t, "direct/dependent", "1.0.0")
	commandtest.CreateInstalledJSON(t, gbPkgs(direct, commandtest.GetPackage(t, "vendor/package", "1.0.0")), nil, true)

	commandtest.ConfigureLinks(t, direct, php.ArrayOf("require", php.ArrayOf("vendor/package", "*")))

	err := gbRunErr(t, gbParams("command", "show", "--direct", true, "package", "vendor/package"))
	if !gbIsInvalidArgument(err) {
		t.Errorf("expected an InvalidArgumentException, got %T", err)
	}
	gbAssertSame(t, `Package "vendor/package" is installed but not a direct dependent of the root package.`, err.Error())
}

func TestShowCommand_ShowDirectWithNameOnlyShowsDirectDependents(t *testing.T) {
	commandtest.InitTempComposer(t, php.ArrayOf(
		"repositories", php.NewArray(),
		"require", php.ArrayOf("direct/dependent", "*"),
		"require-dev", php.ArrayOf("direct/dependent2", "*"),
	), nil, nil, true)

	commandtest.CreateInstalledJSON(t, gbPkgs(
		commandtest.GetPackage(t, "direct/dependent", "1.0.0"),
		commandtest.GetPackage(t, "direct/dependent2", "1.0.0"),
	), nil, true)

	appTester := gbRun(t, gbParams("command", "show", "--direct", true, "package", "direct/dependent"))
	gbAssertSame(t, "0", gbItoa(appTester.StatusCode()))
	gbContains(t, appTester.Display(true), "name     : direct/dependent\n")

	appTester = gbRun(t, gbParams("command", "show", "--direct", true, "package", "direct/dependent2"))
	gbAssertSame(t, "0", gbItoa(appTester.StatusCode()))
	gbContains(t, appTester.Display(true), "name     : direct/dependent2\n")
}

var leadingWord = regexp.MustCompile(`(?m)^(\w+)`)

func assertOnlyPlatformPackages(t *testing.T, output string) {
	t.Helper()
	// PHP iterates Regex::matchAll(...)->matches, i.e. the list of full
	// matches and the list of group 1, and checks element [1] of each:
	// the second line's first word, twice.
	matches := leadingWord.FindAllStringSubmatch(output, -1)
	if len(matches) < 2 {
		t.Fatalf("expected at least two lines, got %q", output)
	}
	for _, m := range matches[1] {
		if !repository.IsPlatformPackage(m) {
			t.Errorf("%q is not a platform package", m)
		}
	}
}

func TestShowCommand_ShowPlatformOnlyShowsPlatformPackages(t *testing.T) {
	commandtest.InitTempComposer(t, php.ArrayOf(
		"repositories", gbPackageRepo(
			gbRepoPackage("name", "vendor/package", "description", "generic description", "version", "1.0.0"),
		),
	), nil, nil, true)

	commandtest.CreateInstalledJSON(t, gbPkgs(commandtest.GetPackage(t, "vendor/package", "1.0.0")), nil, true)

	appTester := gbRun(t, gbParams("command", "show", "-p", true))
	assertOnlyPlatformPackages(t, gbTrim(appTester))
}

func TestShowCommand_ShowPlatformWorksWithoutComposerJson(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	if err := os.Remove("./composer.json"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove("./auth.json"); err != nil {
		t.Fatal(err)
	}

	// listing packages
	appTester := commandtest.GetApplicationTester(t)
	if _, err := appTester.Run(gbParams("command", "show", "-p", true), commandtest.Options{}); err != nil {
		t.Fatal(err)
	}
	assertOnlyPlatformPackages(t, gbTrim(appTester))

	// getting a single package
	if code, err := appTester.Run(gbParams("command", "show", "-p", true, "package", "php"), commandtest.Options{}); err != nil || code != 0 {
		t.Fatalf("code %d, err %v", code, err)
	}
	if code, err := appTester.Run(gbParams("command", "show", "-p", true, "-f", "json", "package", "php"), commandtest.Options{}); err != nil || code != 0 {
		t.Fatalf("code %d, err %v", code, err)
	}
}

func TestShowCommand_OutdatedWithZeroMajor(t *testing.T) {
	commandtest.InitTempComposer(t, php.ArrayOf(
		"repositories", gbPackageRepo(
			gbRepoPackage("name", "zerozero/major", "description", "generic description", "version", "0.0.1"),
			gbRepoPackage("name", "zerozero/major", "description", "generic description", "version", "0.0.2"),
			gbRepoPackage("name", "zero/major", "description", "generic description", "version", "0.1.0"),
			gbRepoPackage("name", "zero/major", "description", "generic description", "version", "0.2.0"),
			gbRepoPackage("name", "zero/minor", "description", "generic description", "version", "0.1.0"),
			gbRepoPackage("name", "zero/minor", "description", "generic description", "version", "0.1.2"),
			gbRepoPackage("name", "zero/patch", "description", "generic description", "version", "0.1.2"),
			gbRepoPackage("name", "zero/patch", "description", "generic description", "version", "0.1.2.1"),
		),
		"require", php.ArrayOf(
			"zerozero/major", "^0.0.1",
			"zero/major", "^0.1",
			"zero/minor", "^0.1",
			"zero/patch", "^0.1",
		),
	), nil, nil, true)

	commandtest.CreateInstalledJSON(t, gbPkgs(
		commandtest.GetPackage(t, "zerozero/major", "0.0.1"),
		commandtest.GetPackage(t, "zero/major", "0.1.0"),
		commandtest.GetPackage(t, "zero/minor", "0.1.0"),
		commandtest.GetPackage(t, "zero/patch", "0.1.2"),
	), nil, true)

	appTester := gbRun(t, gbParams("command", "outdated", "--direct", true, "--patch-only", true))
	gbAssertSame(t, `Legend:
! patch or minor release available - update recommended
~ major release available - update possible
zero/patch 0.1.2 <highlight>! 0.1.2.1</highlight>`, gbTrim(appTester))

	appTester = gbRun(t, gbParams("command", "outdated", "--direct", true, "--minor-only", true))
	gbAssertSame(t, `Legend:
! patch or minor release available - update recommended
~ major release available - update possible
zero/minor 0.1.0 <highlight>! 0.1.2  </highlight>
zero/patch 0.1.2 <highlight>! 0.1.2.1</highlight>`, gbTrim(appTester))

	appTester = gbRun(t, gbParams("command", "outdated", "--direct", true, "--major-only", true))
	gbAssertSame(t, `Legend:
! patch or minor release available - update recommended
~ major release available - update possible
zero/major     0.1.0 ~ 0.2.0
zerozero/major 0.0.1 ~ 0.0.2`, gbTrim(appTester))
}

func TestShowCommand_ShowAllShowsAllSections(t *testing.T) {
	commandtest.InitTempComposer(t, php.ArrayOf(
		"repositories", gbPackageRepo(
			gbRepoPackage("name", "vendor/available", "description", "generic description", "version", "1.0.0"),
		),
	), nil, nil, true)

	installed := commandtest.GetPackage(t, "vendor/installed", "2.0.0")
	installed.SetDescription(pkg.Str("description of installed package"))
	commandtest.CreateInstalledJSON(t, gbPkgs(installed), nil, true)

	locked := commandtest.GetPackage(t, "vendor/locked", "3.0.0")
	locked.SetDescription(pkg.Str("description of locked package"))
	commandtest.CreateComposerLock(t, gbPkgs(locked), nil)

	appTester := gbRun(t, gbParams("command", "show", "--all", true))
	output, _, err := php.PregReplace(`{platform:(\n  .*)+}`, "platform: wiped", gbTrim(appTester), -1)
	if err != nil {
		t.Fatal(err)
	}

	gbAssertSame(t, `platform: wiped

locked:
  vendor/locked 3.0.0 description of locked package

available:
  vendor/available generic description

installed:
  vendor/installed 2.0.0 description of installed package`, output)
}

func TestShowCommand_LockedRequiresValidLockFile(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	err := gbRunErr(t, gbParams("command", "show", "--locked", true))
	gbContains(t, err.Error(), "A valid composer.json and composer.lock files is required to run this command with --locked")
}

func TestShowCommand_LockedShowsAllLocked(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)

	p := commandtest.GetPackage(t, "vendor/locked", "3.0.0")
	p.SetDescription(pkg.Str("description of locked package"))
	commandtest.CreateComposerLock(t, gbPkgs(p), nil)

	appTester := gbRun(t, gbParams("command", "show", "--locked", true))
	gbAssertSame(t, "vendor/locked 3.0.0 description of locked package", gbTrim(appTester))

	p2 := commandtest.GetPackage(t, "vendor/locked2", "2.0.0")
	p2.SetDescription(pkg.Str("description of locked2 package"))
	commandtest.CreateComposerLock(t, gbPkgs(p, p2), nil)

	appTester = gbRun(t, gbParams("command", "show", "--locked", true))
	gbAssertSame(t, `vendor/locked  3.0.0 description of locked package
vendor/locked2 2.0.0 description of locked2 package`, gbTrim(appTester))
}

func TestShowCommand_InvalidOptionCombinations(t *testing.T) {
	appTester := commandtest.GetApplicationTester(t)
	for _, ps := range []gbKV{
		gbParams("command", "show", "--direct", true, "--all", true),
		gbParams("command", "show", "--direct", true, "--available", true),
		gbParams("command", "show", "--direct", true, "--platform", true),
		gbParams("command", "show", "--tree", true, "--all", true),
		gbParams("command", "show", "--tree", true, "--available", true),
		gbParams("command", "show", "--tree", true, "--latest", true),
		gbParams("command", "show", "--tree", true, "--path", true),
		gbParams("command", "show", "--patch-only", true, "--minor-only", true),
		gbParams("command", "show", "--patch-only", true, "--major-only", true),
		gbParams("command", "show", "--minor-only", true, "--major-only", true),
		gbParams("command", "show", "--minor-only", true, "--major-only", true, "--patch-only", true),
		gbParams("command", "show", "--format", "test"),
	} {
		_, _ = appTester.Run(ps, commandtest.Options{})
		if appTester.StatusCode() != 1 {
			t.Errorf("%v: status %d, want 1", ps, appTester.StatusCode())
		}
	}
}

func TestShowCommand_IgnoredOptionCombinations(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)

	appTester := gbRun(t, gbParams("command", "show", "--installed", true))
	gbContains(t, appTester.Display(true), `You are using the deprecated option "installed".`)

	appTester = gbRun(t, gbParams("command", "show", "--ignore", []string{"vendor/package"}))
	gbContains(t, appTester.Display(true), `You are using the option "ignore"`)
}

func TestShowCommand_SelfAndNameOnly(t *testing.T) {
	commandtest.InitTempComposer(t, php.ArrayOf("name", "vendor/package", "version", "1.2.3"), nil, nil, true)

	appTester := gbRun(t, gbParams("command", "show", "--self", true, "--name-only", true))
	gbAssertSame(t, "vendor/package", gbTrim(appTester))
}

func TestShowCommand_SelfAndPackageCombination(t *testing.T) {
	commandtest.InitTempComposer(t, php.ArrayOf("name", "vendor/package"), nil, nil, true)

	err := gbRunErr(t, gbParams("command", "show", "--self", true, "package", "vendor/package"))
	if !gbIsInvalidArgument(err) {
		t.Errorf("expected an InvalidArgumentException, got %T: %v", err, err)
	}
}

func TestShowCommand_Self(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	commandtest.InitTempComposer(t, php.ArrayOf("name", "vendor/package", "version", "1.2.3", "time", today), nil, nil, true)

	appTester := gbRun(t, gbParams("command", "show", "--self", true))
	expected := [][2]string{
		{"name", "vendor/package"},
		{"descrip.", ""},
		{"keywords", ""},
		{"versions", "* 1.2.3"},
		{"released", today + ", today"},
		{"type", "library"},
		{"homepage", ""},
		{"source", "[]  "},
		{"dist", "[]  "},
		{"path", ""},
		{"names", "vendor/package"},
	}
	var lines []string
	for _, e := range expected {
		lines = append(lines, php.StrPad(e[0], 8, " ", php.StrPadRight)+" : "+e[1])
	}

	gbAssertSame(t, strings.Join(lines, "\n")+"\n", appTester.Display(true))
}

func TestShowCommand_NotInstalledError(t *testing.T) {
	commandtest.InitTempComposer(t, php.ArrayOf(
		"require", php.ArrayOf("vendor/package", "1.0.0"),
		"require-dev", php.ArrayOf("vendor/package-dev", "1.0.0"),
	), nil, nil, true)
	appTester := gbRun(t, gbParams("command", "show"))
	gbContains(t, gbTrim(appTester), "No dependencies installed. Try running composer install or update.")
}

func TestShowCommand_NoDevOption(t *testing.T) {
	commandtest.InitTempComposer(t, php.ArrayOf(
		"require", php.ArrayOf("vendor/package", "1.0.0"),
		"require-dev", php.ArrayOf("vendor/package-dev", "1.0.0"),
	), nil, nil, true)
	commandtest.CreateInstalledJSON(t, gbPkgs(
		commandtest.GetPackage(t, "vendor/package", "1.0.0"),
		commandtest.GetPackage(t, "vendor/package-dev", "1.0.0"),
	), nil, true)
	appTester := gbRun(t, gbParams("command", "show", "--no-dev", true))
	gbAssertSame(t, "vendor/package 1.0.0", gbTrim(appTester))
}

func TestShowCommand_PackageFilter(t *testing.T) {
	commandtest.InitTempComposer(t, php.ArrayOf(
		"require", php.ArrayOf(
			"vendor/package", "1.0.0",
			"vendor/other-package", "1.0.0",
			"company/package", "1.0.0",
			"company/other-package", "1.0.0",
		),
	), nil, nil, true)
	commandtest.CreateInstalledJSON(t, gbPkgs(
		commandtest.GetPackage(t, "vendor/package", "1.0.0"),
		commandtest.GetPackage(t, "vendor/other-package", "1.0.0"),
		commandtest.GetPackage(t, "company/package", "1.0.0"),
		commandtest.GetPackage(t, "company/other-package", "1.0.0"),
	), nil, true)

	output := gbTrim(gbRun(t, gbParams("command", "show", "package", "vendor/package")))
	gbContains(t, output, "vendor/package")
	gbNotContains(t, output, "vendor/other-package")
	gbNotContains(t, output, "company/package")
	gbNotContains(t, output, "company/other-package")

	output = gbTrim(gbRun(t, gbParams("command", "show", "package", "company/*", "--name-only", true)))
	gbNotContains(t, output, "vendor/package")
	gbNotContains(t, output, "vendor/other-package")
	gbContains(t, output, "company/package")
	gbContains(t, output, "company/other-package")
}

func TestShowCommand_NotExistingPackage(t *testing.T) {
	cases := []struct {
		name     string
		pkg      string
		options  gbKV
		expected string
	}{
		{"package with no options", "not/existing", nil, `Package "not/existing" not found, try using --available (-a) to show all available packages.`},
		{"package with --all option", "not/existing", gbParams("--all", true), `Package "not/existing" not found.`},
		{"package with --locked option", "not/existing", gbParams("--locked", true), `Package "not/existing" not found in lock file, try using --available (-a) to show all available packages.`},
		{"platform with --platform", "ext-nonexisting", gbParams("--platform", true), `Package "ext-nonexisting" not found, try using --available (-a) to show all available packages.`},
		{"platform without --platform", "ext-nonexisting", nil, `Package "ext-nonexisting" not found, try using --platform (-p) to show platform packages, try using --available (-a) to show all available packages.`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, php.ArrayOf("require", php.ArrayOf("vendor/package", "1.0.0")), nil, nil, true)
			p := commandtest.GetPackage(t, "vendor/package", "1.0.0")
			commandtest.CreateInstalledJSON(t, gbPkgs(p), nil, true)
			commandtest.CreateComposerLock(t, gbPkgs(p), nil)

			err := gbRunErr(t, gbMerge(gbParams("command", "show", "package", tc.pkg), tc.options, false))
			if !strings.HasPrefix(err.Error(), tc.expected) {
				t.Errorf("error %q does not start with %q", err.Error(), tc.expected)
			}
		})
	}
}

func TestShowCommand_NotExistingPackageWithWorkingDir(t *testing.T) {
	dir := commandtest.InitTempComposer(t, php.ArrayOf("require", php.ArrayOf("vendor/package", "1.0.0")), nil, nil, true)
	commandtest.CreateInstalledJSON(t, gbPkgs(commandtest.GetPackage(t, "vendor/package", "1.0.0")), nil, true)

	err := gbRunErr(t, gbParams("command", "show", "package", "not/existing", "--working-dir", dir))
	expected := `Package "not/existing" not found in ` + dir + `/composer.json, try using --available (-a) to show all available packages.`
	if !strings.HasPrefix(err.Error(), expected) {
		t.Errorf("error %q does not start with %q", err.Error(), expected)
	}
}

func TestShowCommand_SpecificPackageAndTree(t *testing.T) {
	cases := []struct {
		name     string
		packages func(t *testing.T) []pkg.PackageInterface
		options  gbKV
		expected string
	}{
		{"just package", func(t *testing.T) []pkg.PackageInterface {
			return gbPkgs(commandtest.GetPackage(t, "vendor/package", "1.0.0"))
		}, nil, "vendor/package 1.0.0"},
		{"package with one package requirement", func(t *testing.T) []pkg.PackageInterface {
			p := commandtest.GetPackage(t, "vendor/package", "1.0.0")
			p.SetRequires(pkg.LinksOf(gbLink(t, "vendor/package", "vendor/required-package", "1.0.0")))

			return gbPkgs(p)
		}, nil, "vendor/package 1.0.0\n`--vendor/required-package 1.0.0"},
		{"package with platform requirement", func(t *testing.T) []pkg.PackageInterface {
			p := commandtest.GetPackage(t, "vendor/package", "1.0.0")
			p.SetRequires(pkg.LinksOf(gbLink(t, "vendor/package", "php", "8.2.0")))

			return gbPkgs(p)
		}, nil, "vendor/package 1.0.0\n`--php 8.2.0"},
		{"package with json format", func(t *testing.T) []pkg.PackageInterface {
			return gbPkgs(commandtest.GetPackage(t, "vendor/package", "1.0.0"))
		}, gbParams("--format", "json"), `{
    "installed": [
        {
            "name": "vendor/package",
            "version": "1.0.0",
            "description": null
        }
    ]
}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, php.ArrayOf("require", php.ArrayOf("vendor/package", "1.0.0")), nil, nil, true)
			commandtest.CreateInstalledJSON(t, tc.packages(t), nil, true)

			appTester := gbRun(t, gbMerge(gbParams("command", "show", "package", "vendor/package", "--tree", true), tc.options, false))
			gbAssertSame(t, tc.expected, gbTrim(appTester))
		})
	}
}

func TestShowCommand_NameOnlyPrintsNoTrailingWhitespace(t *testing.T) {
	commandtest.InitTempComposer(t, php.ArrayOf(
		"repositories", gbPackageRepo(
			// CAUTION: package names matter - output is sorted, and we want shorter before longer ones
			gbRepoPackage("name", "vendor/apackage", "description", "generic description", "version", "1.0.0"),
			gbRepoPackage("name", "vendor/apackage", "description", "generic description", "version", "1.1.0"),
			gbRepoPackage("name", "vendor/longpackagename", "description", "generic description", "version", "1.0.0"),
			gbRepoPackage("name", "vendor/longpackagename", "description", "generic description", "version", "1.1.0"),
			gbRepoPackage("name", "vendor/somepackage", "description", "generic description", "version", "1.0.0"),
		),
	), nil, nil, true)

	commandtest.CreateInstalledJSON(t, gbPkgs(
		commandtest.GetPackage(t, "vendor/apackage", "1.0.0"),
		commandtest.GetPackage(t, "vendor/longpackagename", "1.0.0"),
		commandtest.GetPackage(t, "vendor/somepackage", "1.0.0"),
	), nil, true)

	appTester := gbRun(t, gbParams("command", "show", "-N", true))
	gbAssertSame(t, `vendor/apackage
vendor/longpackagename
vendor/somepackage`, gbTrim(appTester))

	appTester = gbRun(t, gbParams("command", "show", "--outdated", true, "-N", true))
	gbAssertSame(t, `Legend:
! patch or minor release available - update recommended
~ major release available - update possible
vendor/apackage
vendor/longpackagename`, gbTrim(appTester))
}

// A type exists in show's listing only once a package is added to it, as
// PHP's $packages[$type] does: a repository that contributes nothing (no
// packages, a filter or --direct matching none) leaves no empty type for
// --format=json to print or for -o/-l to report on.
func TestShowCommand_TypesWithoutPackagesAreLeftOut(t *testing.T) {
	commandtest.InitTempComposer(t, php.ArrayOf(
		"repositories", gbPackageRepo(
			gbRepoPackage("name", "vendor/package", "version", "1.0.0"),
			gbRepoPackage("name", "vendor/dependency", "version", "1.0.0"),
		),
		"require", php.ArrayOf("vendor/package", "1.0.0"),
	), nil, nil, true)
	commandtest.CreateInstalledJSON(t, gbPkgs(
		commandtest.GetPackage(t, "vendor/package", "1.0.0"),
		commandtest.GetPackage(t, "vendor/dependency", "1.0.0"),
	), nil, true)
	commandtest.CreateComposerLock(t, gbPkgs(
		commandtest.GetPackage(t, "vendor/package", "1.0.0"),
		commandtest.GetPackage(t, "vendor/dependency", "1.0.0"),
	), nil)

	for _, c := range []struct {
		params gbKV
		want   string
	}{
		{gbParams("command", "show", "package", "nomatch*", "--format", "json"), "[]"},
		{gbParams("command", "show", "package", "nomatch*", "--platform", true, "--format", "json"), "[]"},
		{gbParams("command", "show", "package", "nomatch*", "--locked", true, "--format", "json"), "[]"},
		{gbParams("command", "show", "package", "nomatch*", "--available", true, "--format", "json"), "[]"},
		{gbParams("command", "show", "package", "vendor/dep*", "--direct", true, "--format", "json"), "[]"},
		{gbParams("command", "show", "package", "nomatch/*", "--outdated", true), ""},
		{gbParams("command", "show", "package", "nomatch/*", "--latest", true), ""},
		// A package that is added, then skipped as up to date, keeps its
		// type.
		{gbParams("command", "outdated", "--format", "json"), "{\n    \"installed\": []\n}"},
		{gbParams("command", "outdated", "--locked", true, "--format", "json"), "{\n    \"locked\": []\n}"},
	} {
		if got := gbTrim(gbRun(t, c.params)); got != c.want {
			t.Errorf("%v: got %q, want %q", c.params, got, c.want)
		}
	}
}

func TestShowCommand_EmptyProjectListsNoType(t *testing.T) {
	commandtest.InitTempComposer(t, nil, nil, nil, true)

	for _, params := range []gbKV{
		gbParams("command", "show", "--format", "json"),
		gbParams("command", "outdated", "--format", "json"),
	} {
		if got := gbTrim(gbRun(t, params)); got != "[]" {
			t.Errorf("%v: got %q, want []", params, got)
		}
	}
}
