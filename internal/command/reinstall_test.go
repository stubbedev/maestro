// Ports tests/Composer/Test/Command/ReinstallCommandTest.php.

package command_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

// TestReinstallCommand_ReinstallCommand runs caseProvider's cases, with
// the status code the PHPUnit test leaves out, then maestro's: the input
// errors (an exception's message in err) and selections matching
// nothing.
func TestReinstallCommand_ReinstallCommand(t *testing.T) {
	const noneFound = `<warning>Found no packages to reinstall, aborting.</warning>`
	tests := []struct {
		name     string
		options  []console.Param
		expected string
		code     int
		err      string
	}{
		{
			name:    "reinstall a package by name",
			options: []console.Param{console.P("packages", []string{"root/req", "root/anotherreq*"})},
			expected: `- Removing root/req (1.0.0)
  - Removing root/anotherreq2 (1.0.0)
  - Removing root/anotherreq (1.0.0)
  - Installing root/anotherreq (1.0.0)
  - Installing root/anotherreq2 (1.0.0)
  - Installing root/req (1.0.0)`,
		},
		{
			name:    "reinstall packages by type",
			options: []console.Param{console.P("--type", []string{"metapackage"})},
			expected: `- Removing root/req (1.0.0)
  - Removing root/lala (1.0.0)
  - Removing root/anotherreq2 (1.0.0)
  - Removing root/anotherreq (1.0.0)
  - Installing root/anotherreq (1.0.0)
  - Installing root/anotherreq2 (1.0.0)
  - Installing root/lala (1.0.0)
  - Installing root/req (1.0.0)`,
		},
		{
			name:    "reinstall a package that is not installed",
			options: []console.Param{console.P("packages", []string{"root/unknownreq"})},
			expected: `<warning>Pattern "root/unknownreq" does not match any currently installed packages.</warning>
` + noneFound,
			code: 1,
		},
		{
			name:    "a wildcard matching nothing",
			options: []console.Param{console.P("packages", []string{"nope/*"})},
			expected: `<warning>Pattern "nope/*" does not match any currently installed packages.</warning>
` + noneFound,
			code: 1,
		},
		{
			name:     "a type no package has",
			options:  []console.Param{console.P("--type", []string{"nope"})},
			expected: noneFound,
			code:     1,
		},
		{
			name: "no package names",
			err:  "You must pass one or more package names to be reinstalled.",
		},
		{
			name:    "package names and a type",
			options: []console.Param{console.P("packages", []string{"root/req"}), console.P("--type", []string{"metapackage"})},
			err:     "You cannot specify package names and filter by type at the same time.",
		},
		{
			name:    "an invalid --prefer-install",
			options: []console.Param{console.P("packages", []string{"root/req"}), console.P("--prefer-install", "foo")},
			err:     `--prefer-install accepts one of "dist", "source" or "auto", got foo`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, `{
				"require": {"root/req": "1.*"},
				"require-dev": {"root/anotherreq": "2.*", "root/anotherreq2": "2.*", "root/lala": "2.*"}
			}`, nil, nil, true)

			rootReqPackage := commandtest.GetPackage(t, "root/req", "1.0.0")
			anotherReqPackage := commandtest.GetPackage(t, "root/anotherreq", "1.0.0")
			anotherReqPackage2 := commandtest.GetPackage(t, "root/anotherreq2", "1.0.0")
			anotherReqPackage3 := commandtest.GetPackage(t, "root/lala", "1.0.0")
			rootReqPackage.SetType("metapackage")
			anotherReqPackage.SetType("metapackage")
			anotherReqPackage2.SetType("metapackage")
			anotherReqPackage3.SetType("metapackage")

			packages := []pkg.PackageInterface{rootReqPackage}
			devPackages := []pkg.PackageInterface{anotherReqPackage, anotherReqPackage2, anotherReqPackage3}
			commandtest.CreateComposerLock(t, packages, devPackages)
			commandtest.CreateInstalledJSON(t, packages, devPackages, true)

			appTester := commandtest.GetApplicationTester(t)
			params := append([]console.Param{
				console.P("command", "reinstall"),
				console.P("--no-progress", true),
				console.P("--no-plugins", true),
			}, tt.options...)
			code, err := appTester.Run(params, commandtest.Options{})
			if tt.err != "" {
				if err == nil || err.Error() != tt.err {
					t.Fatalf("exception %v, want %q", err, tt.err)
				}

				return
			}
			if err != nil {
				t.Fatalf("unexpected exception: %v", err)
			}
			if code != tt.code {
				t.Errorf("status %d, want %d", code, tt.code)
			}
			if got := php.Trim(appTester.Display(true)); got != tt.expected {
				t.Errorf("output mismatch\nwant:\n%s\ngot:\n%s", tt.expected, got)
			}
		})
	}
}
