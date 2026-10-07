// Ports tests/Composer/Test/Command/SuggestsCommandTest.php.

package command_test

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
)

func TestSuggestsCommand_InstalledPackagesWithNoSuggestions(t *testing.T) {
	commandtest.InitTempComposer(t, `{
		"repositories": {
			"packages": {
				"type": "package",
				"package": [
					{"name": "vendor1/package1", "version": "1.0.0"},
					{"name": "vendor2/package2", "version": "1.0.0"}
				]
			}
		},
		"require": {"vendor1/package1": "1.*", "vendor2/package2": "1.*"}
	}`, nil, nil, true)

	packages := gbPkgs(
		commandtest.GetPackage(t, "vendor1/package1", "1.0.0"),
		commandtest.GetPackage(t, "vendor2/package2", "1.0.0"),
	)

	commandtest.CreateInstalledJSON(t, packages, nil, true)
	commandtest.CreateComposerLock(t, packages, nil)

	appTester := gbRun(t, gbParams("command", "suggest"))
	if code := appTester.StatusCode(); code != 0 {
		t.Errorf("exit code %d", code)
	}
	if d := appTester.Display(true); d != "" {
		t.Errorf("expected empty output, got %q", d)
	}
}

// suggestsLink is new Link($source, $target, getVersionConstraint('>=',
// '1.0'), $type, '^1.0').
func suggestsLink(t *testing.T, source, target, typ string) *pkg.Link {
	t.Helper()
	c := semver.NewConstraintOp(semver.OpGE, "1.0.0.0")
	c.SetPrettyString(">= 1.0")

	return pkg.NewLink(source, target, c, typ, pkg.Str("^1.0"))
}

// suggestsPackage ports getPackageWithSuggestAndRequires.
func suggestsPackage(t *testing.T, name string, suggests *php.Array, requires, requireDevs []*pkg.Link) *pkg.CompletePackage {
	t.Helper()
	p := commandtest.GetPackage(t, name, "1.0.0")
	p.SetSuggests(suggests)
	p.SetRequires(pkg.LinksOf(requires...))
	p.SetDevRequires(pkg.LinksOf(requireDevs...))

	return p
}

func TestSuggestsCommand_Suggest(t *testing.T) {
	tests := []struct {
		name        string
		hasLockFile bool
		command     gbKV
		expected    string
	}{
		{"with lockfile, show suggested", true, nil, `vendor1/package1 suggests:
 - vendor3/suggested: helpful for vendor1/package1

vendor2/package2 suggests:
 - vendor4/dev-suggested: helpful for vendor2/package2

2 additional suggestions by transitive dependencies can be shown with --all`},
		{"without lockfile, show suggested", false, nil, `vendor1/package1 suggests:
 - vendor3/suggested: helpful for vendor1/package1

vendor2/package2 suggests:
 - vendor4/dev-suggested: helpful for vendor2/package2

2 additional suggestions by transitive dependencies can be shown with --all`},
		{"with lockfile, show suggested (excluding dev)", true, gbParams("--no-dev", true), `vendor1/package1 suggests:
 - vendor3/suggested: helpful for vendor1/package1

1 additional suggestions by transitive dependencies can be shown with --all`},
		{"without lockfile, show suggested (excluding dev)", false, gbParams("--no-dev", true), `vendor1/package1 suggests:
 - vendor3/suggested: helpful for vendor1/package1

vendor2/package2 suggests:
 - vendor4/dev-suggested: helpful for vendor2/package2

2 additional suggestions by transitive dependencies can be shown with --all`},
		{"with lockfile, show all suggested", true, gbParams("--all", true), `vendor1/package1 suggests:
 - vendor3/suggested: helpful for vendor1/package1

vendor2/package2 suggests:
 - vendor4/dev-suggested: helpful for vendor2/package2

vendor5/dev-package suggests:
 - vendor8/dev-transitive: helpful for vendor5/dev-package

vendor6/package6 suggests:
 - vendor7/transitive: helpful for vendor6/package6`},
		{"without lockfile, show all suggested", false, gbParams("--all", true), `vendor1/package1 suggests:
 - vendor3/suggested: helpful for vendor1/package1

vendor2/package2 suggests:
 - vendor4/dev-suggested: helpful for vendor2/package2

vendor5/dev-package suggests:
 - vendor8/dev-transitive: helpful for vendor5/dev-package

vendor6/package6 suggests:
 - vendor7/transitive: helpful for vendor6/package6`},
		{"with lockfile, show all suggested (excluding dev)", true, gbParams("--all", true, "--no-dev", true), `vendor1/package1 suggests:
 - vendor3/suggested: helpful for vendor1/package1

vendor6/package6 suggests:
 - vendor7/transitive: helpful for vendor6/package6`},
		{"without lockfile, show all suggested (excluding dev)", false, gbParams("--all", true, "--no-dev", true), `vendor1/package1 suggests:
 - vendor3/suggested: helpful for vendor1/package1

vendor2/package2 suggests:
 - vendor4/dev-suggested: helpful for vendor2/package2

vendor5/dev-package suggests:
 - vendor8/dev-transitive: helpful for vendor5/dev-package

vendor6/package6 suggests:
 - vendor7/transitive: helpful for vendor6/package6`},
		{"with lockfile, show suggested grouped by package", true, gbParams("--by-package", true), `vendor1/package1 suggests:
 - vendor3/suggested: helpful for vendor1/package1

vendor2/package2 suggests:
 - vendor4/dev-suggested: helpful for vendor2/package2

2 additional suggestions by transitive dependencies can be shown with --all`},
		{"without lockfile, show suggested grouped by package", false, gbParams("--by-package", true), `vendor1/package1 suggests:
 - vendor3/suggested: helpful for vendor1/package1

vendor2/package2 suggests:
 - vendor4/dev-suggested: helpful for vendor2/package2

2 additional suggestions by transitive dependencies can be shown with --all`},
		{"with lockfile, show suggested grouped by package (excluding dev)", true, gbParams("--by-package", true, "--no-dev", true), `vendor1/package1 suggests:
 - vendor3/suggested: helpful for vendor1/package1

1 additional suggestions by transitive dependencies can be shown with --all`},
		{"without lockfile, show suggested grouped by package (excluding dev)", false, gbParams("--by-package", true, "--no-dev", true), `vendor1/package1 suggests:
 - vendor3/suggested: helpful for vendor1/package1

vendor2/package2 suggests:
 - vendor4/dev-suggested: helpful for vendor2/package2

2 additional suggestions by transitive dependencies can be shown with --all`},
		{"with lockfile, show suggested grouped by suggestion", true, gbParams("--by-suggestion", true), `vendor3/suggested is suggested by:
 - vendor1/package1: helpful for vendor1/package1

vendor4/dev-suggested is suggested by:
 - vendor2/package2: helpful for vendor2/package2

2 additional suggestions by transitive dependencies can be shown with --all`},
		{"without lockfile, show suggested grouped by suggestion", false, gbParams("--by-suggestion", true), `vendor3/suggested is suggested by:
 - vendor1/package1: helpful for vendor1/package1

vendor4/dev-suggested is suggested by:
 - vendor2/package2: helpful for vendor2/package2

2 additional suggestions by transitive dependencies can be shown with --all`},
		{"with lockfile, show suggested grouped by suggestion (excluding dev)", true, gbParams("--by-suggestion", true, "--no-dev", true), `vendor3/suggested is suggested by:
 - vendor1/package1: helpful for vendor1/package1

1 additional suggestions by transitive dependencies can be shown with --all`},
		{"without lockfile, show suggested grouped by suggestion (excluding dev)", false, gbParams("--by-suggestion", true, "--no-dev", true), `vendor3/suggested is suggested by:
 - vendor1/package1: helpful for vendor1/package1

vendor4/dev-suggested is suggested by:
 - vendor2/package2: helpful for vendor2/package2

2 additional suggestions by transitive dependencies can be shown with --all`},
		{"with lockfile, show suggested grouped by package and suggestion", true, gbParams("--by-package", true, "--by-suggestion", true), `vendor1/package1 suggests:
 - vendor3/suggested: helpful for vendor1/package1

vendor2/package2 suggests:
 - vendor4/dev-suggested: helpful for vendor2/package2

------------------------------------------------------------------------------
vendor3/suggested is suggested by:
 - vendor1/package1: helpful for vendor1/package1

vendor4/dev-suggested is suggested by:
 - vendor2/package2: helpful for vendor2/package2

2 additional suggestions by transitive dependencies can be shown with --all`},
		{"without lockfile, show suggested grouped by package and suggestion", false, gbParams("--by-package", true, "--by-suggestion", true), `vendor1/package1 suggests:
 - vendor3/suggested: helpful for vendor1/package1

vendor2/package2 suggests:
 - vendor4/dev-suggested: helpful for vendor2/package2

------------------------------------------------------------------------------
vendor3/suggested is suggested by:
 - vendor1/package1: helpful for vendor1/package1

vendor4/dev-suggested is suggested by:
 - vendor2/package2: helpful for vendor2/package2

2 additional suggestions by transitive dependencies can be shown with --all`},
		{"with lockfile, show suggested grouped by package and suggestion (excluding dev)", true, gbParams("--by-package", true, "--by-suggestion", true, "--no-dev", true), `vendor1/package1 suggests:
 - vendor3/suggested: helpful for vendor1/package1

------------------------------------------------------------------------------
vendor3/suggested is suggested by:
 - vendor1/package1: helpful for vendor1/package1

1 additional suggestions by transitive dependencies can be shown with --all`},
		{"without lockfile, show suggested grouped by package and suggestion (excluding dev)", false, gbParams("--by-package", true, "--by-suggestion", true, "--no-dev", true), `vendor1/package1 suggests:
 - vendor3/suggested: helpful for vendor1/package1

vendor2/package2 suggests:
 - vendor4/dev-suggested: helpful for vendor2/package2

------------------------------------------------------------------------------
vendor3/suggested is suggested by:
 - vendor1/package1: helpful for vendor1/package1

vendor4/dev-suggested is suggested by:
 - vendor2/package2: helpful for vendor2/package2

2 additional suggestions by transitive dependencies can be shown with --all`},
		{"with lockfile, show suggested for package", true, gbParams("packages", []string{"vendor2/package2"}), `vendor2/package2 suggests:
 - vendor4/dev-suggested: helpful for vendor2/package2`},
		{"without lockfile, show suggested for package", false, gbParams("packages", []string{"vendor2/package2"}), `vendor2/package2 suggests:
 - vendor4/dev-suggested: helpful for vendor2/package2`},
		{"with lockfile, list suggested", true, gbParams("--list", true), `vendor3/suggested
vendor4/dev-suggested`},
		{"without lockfile, list suggested", false, gbParams("--list", true), `vendor3/suggested
vendor4/dev-suggested`},
		{"with lockfile, list suggested with no transitive or no-dev dependencies", true, gbParams("--list", true, "--no-dev", true), `vendor3/suggested`},
		{"without lockfile, list suggested with no transitive or no-dev dependencies", false, gbParams("--list", true, "--no-dev", true), `vendor3/suggested
vendor4/dev-suggested`},
		{"with lockfile, list suggested with all dependencies including transitive and dev dependencies", true, gbParams("--list", true, "--all", true), `vendor3/suggested
vendor4/dev-suggested
vendor7/transitive
vendor8/dev-transitive`},
		{"without lockfile, list suggested with all dependencies including transitive and dev dependencies", false, gbParams("--list", true, "--all", true), `vendor3/suggested
vendor4/dev-suggested
vendor7/transitive
vendor8/dev-transitive`},
		{"with lockfile, list all suggested (excluding dev)", true, gbParams("--list", true, "--all", true, "--no-dev", true), `vendor3/suggested
vendor7/transitive`},
		{"without lockfile, list all suggested (excluding dev)", false, gbParams("--list", true, "--all", true, "--no-dev", true), `vendor3/suggested
vendor4/dev-suggested
vendor7/transitive
vendor8/dev-transitive`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestsFixture(t, tt.hasLockFile)

			appTester := gbRun(t, gbMerge(gbParams("command", "suggest"), tt.command, true))
			if code := appTester.StatusCode(); code != 0 {
				t.Errorf("exit code %d", code)
			}
			gbAssertSame(t, php.Trim(tt.expected), gbTrim(appTester))
		})
	}
}

// suggestsFixture is testSuggest's project: direct, dev and transitive
// suggestions, installed, and locked when hasLockFile.
func suggestsFixture(t *testing.T, hasLockFile bool) {
	t.Helper()
	commandtest.InitTempComposer(t, `{
			"repositories": {
				"packages": {
					"type": "package",
					"package": [
						{"name": "vendor1/package1", "version": "1.0.0", "suggests": {"vendor3/suggested": "helpful for vendor1/package1"}, "require": {"vendor6/package6": "^1.0"}, "require-dev": {"vendor3/suggested": "^1.0", "vendor4/dev-suggested": "^1.0"}},
						{"name": "vendor2/package2", "version": "1.0.0", "suggests": {"vendor4/dev-suggested": "helpful for vendor2/package2"}, "require": {"vendor5/dev-package": "^1.0"}},
						{"name": "vendor5/dev-package", "version": "1.0.0", "suggests": {"vendor8/dev-transitive": "helpful for vendor5/dev-package"}, "require-dev": {"vendor8/dev-transitive": "^1.0"}},
						{"name": "vendor6/package6", "version": "1.0.0", "suggests": {"vendor7/transitive": "helpful for vendor6/package6"}}
					]
				}
			},
			"require": {"vendor1/package1": "^1"},
			"require-dev": {"vendor2/package2": "^1"}
		}`, nil, nil, true)

	packages := gbPkgs(
		suggestsPackage(t, "vendor1/package1",
			php.ArrayOf("vendor3/suggested", "helpful for vendor1/package1"),
			[]*pkg.Link{suggestsLink(t, "vendor1/package1", "vendor6/package6", pkg.TypeRequire)},
			[]*pkg.Link{
				suggestsLink(t, "vendor1/package1", "vendor4/dev-suggested", pkg.TypeDevRequire),
				suggestsLink(t, "vendor1/package1", "vendor3/suggested", pkg.TypeDevRequire),
			},
		),
		suggestsPackage(t, "vendor6/package6",
			php.ArrayOf("vendor7/transitive", "helpful for vendor6/package6"),
			nil, nil,
		),
	)
	devPackages := gbPkgs(
		suggestsPackage(t, "vendor2/package2",
			php.ArrayOf("vendor4/dev-suggested", "helpful for vendor2/package2"),
			[]*pkg.Link{suggestsLink(t, "vendor2/package2", "vendor5/dev-package", pkg.TypeRequire)},
			nil,
		),
		suggestsPackage(t, "vendor5/dev-package",
			php.ArrayOf("vendor8/dev-transitive", "helpful for vendor5/dev-package"),
			nil,
			[]*pkg.Link{suggestsLink(t, "vendor5/dev-package", "vendor8/dev-transitive", pkg.TypeDevRequire)},
		),
	)

	commandtest.CreateInstalledJSON(t, packages, devPackages, true)
	if hasLockFile {
		commandtest.CreateComposerLock(t, packages, devPackages)
	}
}

// TestSuggestsCommand_Streams: the report, its transitive-suggestions
// hint included, goes to stdout; a filter naming no installed package is
// no error and reports nothing.
func TestSuggestsCommand_Streams(t *testing.T) {
	for _, c := range []struct {
		name   string
		args   []any
		stdout string
	}{
		{"report", nil, "vendor1/package1 suggests:\n" +
			" - vendor3/suggested: helpful for vendor1/package1\n\n" +
			"vendor2/package2 suggests:\n" +
			" - vendor4/dev-suggested: helpful for vendor2/package2\n\n" +
			"2 additional suggestions by transitive dependencies can be shown with --all\n"},
		{"unknown filter", []any{"packages", []string{"nope/nope"}}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			suggestsFixture(t, true)
			got := commandtest.GetApplicationTester(t).RunStreams(append([]any{"command", "suggests"}, c.args...)...)
			if got.Code != 0 || got.Err != nil || got.Stdout != c.stdout || strings.Contains(got.Stderr, "suggest") {
				t.Errorf("got %+v\nwant exit 0, stdout %q and no suggestion on stderr", got, c.stdout)
			}
		})
	}
}
