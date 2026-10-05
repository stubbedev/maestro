// Ports tests/Composer/Test/Command/HomeCommandTest.php.

package command_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

func TestHomeCommand_HomeCommandWithShowFlag(t *testing.T) {
	tests := []struct {
		name         string
		composerJSON string
		command      gbKV
		expected     string
		urls         map[string]string
	}{
		{
			"Invalid or missing repository URL",
			`{
				"repositories": {
					"packages": {
						"type": "package",
						"package": [
							{"name": "vendor/package", "description": "generic description", "version": "1.0.0"}
						]
					}
				},
				"require": {"vendor/package": "^1.0"}
			}`,
			gbParams("packages", []string{"vendor/package"}),
			`<warning>Invalid or missing repository URL for vendor/package</warning>`,
			nil,
		},
		{
			"No Packages Provided",
			`{"repositories": []}`,
			nil,
			`No package specified, opening homepage for the root package
<warning>Invalid or missing repository URL for __root__</warning>`,
			nil,
		},
		{
			"Package not found",
			`{"repositories": []}`,
			gbParams("packages", []string{"vendor/anotherpackage"}),
			`<warning>Package vendor/anotherpackage not found</warning>
<warning>Invalid or missing repository URL for vendor/anotherpackage</warning>`,
			nil,
		},
		{
			"A valid package URL",
			`{"repositories": []}`,
			gbParams("packages", []string{"vendor/package"}),
			`https://example.org`,
			map[string]string{"vendor/package": "https://example.org"},
		},
		{
			"A valid dev package URL",
			`{"repositories": []}`,
			gbParams("packages", []string{"vendor/devpackage"}),
			`https://example.org/dev`,
			map[string]string{"vendor/devpackage": "https://example.org/dev"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, tt.composerJSON, nil, nil, true)

			packages := map[string]*pkg.CompletePackage{
				"vendor/package": commandtest.GetPackage(t, "vendor/package", "1.2.3"),
			}
			devPackages := map[string]*pkg.CompletePackage{
				"vendor/devpackage": commandtest.GetPackage(t, "vendor/devpackage", "2.3.4"),
			}

			for name, url := range tt.urls {
				if p, ok := packages[name]; ok {
					p.SetHomepage(pkg.Str(url))
				}
				if p, ok := devPackages[name]; ok {
					p.SetHomepage(pkg.Str(url))
				}
			}

			commandtest.CreateInstalledJSON(t, gbPkgs(packages["vendor/package"]), gbPkgs(devPackages["vendor/devpackage"]), true)

			appTester := gbRun(t, gbMerge(gbParams("command", "home", "--show", true), tt.command, true))

			gbAssertSame(t, php.Trim(tt.expected), gbTrim(appTester))
		})
	}
}
