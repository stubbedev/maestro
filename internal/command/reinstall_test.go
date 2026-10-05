// Ports tests/Composer/Test/Command/ReinstallCommandTest.php.

package command_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

func TestReinstallCommand_ReinstallCommand(t *testing.T) {
	tests := []struct {
		name     string
		options  []console.Param
		expected string
	}{
		{
			"reinstall a package by name",
			[]console.Param{console.P("packages", []string{"root/req", "root/anotherreq*"})},
			`- Removing root/req (1.0.0)
  - Removing root/anotherreq2 (1.0.0)
  - Removing root/anotherreq (1.0.0)
  - Installing root/anotherreq (1.0.0)
  - Installing root/anotherreq2 (1.0.0)
  - Installing root/req (1.0.0)`,
		},
		{
			"reinstall packages by type",
			[]console.Param{console.P("--type", []string{"metapackage"})},
			`- Removing root/req (1.0.0)
  - Removing root/lala (1.0.0)
  - Removing root/anotherreq2 (1.0.0)
  - Removing root/anotherreq (1.0.0)
  - Installing root/anotherreq (1.0.0)
  - Installing root/anotherreq2 (1.0.0)
  - Installing root/lala (1.0.0)
  - Installing root/req (1.0.0)`,
		},
		{
			"reinstall a package that is not installed",
			[]console.Param{console.P("packages", []string{"root/unknownreq"})},
			`<warning>Pattern "root/unknownreq" does not match any currently installed packages.</warning>
<warning>Found no packages to reinstall, aborting.</warning>`,
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
			if _, err := appTester.Run(params, commandtest.Options{}); err != nil {
				t.Fatalf("unexpected exception: %v", err)
			}

			if got := php.Trim(appTester.Display(true)); got != tt.expected {
				t.Errorf("output mismatch\nwant:\n%s\ngot:\n%s", tt.expected, got)
			}
		})
	}
}
