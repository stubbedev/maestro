// Ports tests/Composer/Test/Command/FundCommandTest.php.

package command_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

func TestFundCommand_FundCommand(t *testing.T) {
	tests := []struct {
		name         string
		composerJSON string
		command      gbKV
		funding      [][3]string // package, type, url
		expected     string
	}{
		{
			"no funding links present, locally or remotely",
			`{"repositories": [], "require": {"first/pkg": "^2.0"}, "require-dev": {"dev/pkg": "~4.0"}}`,
			nil,
			nil,
			"No funding links were found in your package dependencies. This doesn't mean they don't need your support!",
		},
		{
			"funding links set locally are used as fallback if not found remotely",
			`{"repositories": [], "require": {"first/pkg": "^2.0"}, "require-dev": {"dev/pkg": "~4.0"}}`,
			nil,
			[][3]string{
				{"first/pkg", "github", "https://github.com/composer-test-data"},
				{"dev/pkg", "github", "https://github.com/composer-test-data-dev"},
			},
			`The following packages were found in your dependencies which publish funding information:

dev
  pkg
    https://github.com/sponsors/composer-test-data-dev

first
    https://github.com/sponsors/composer-test-data

Please consider following these links and sponsoring the work of package authors!
Thank you!`,
		},
		{
			"funding links set remotely are used as primary if found",
			`{
				"repositories": [
					{
						"type": "package",
						"package": [
							{"name": "first/pkg", "version": "dev-foo", "funding": [{"type": "github", "url": "https://github.com/test-should-not-be-used"}]},
							{"name": "first/pkg", "version": "dev-main", "default-branch": true, "funding": [{"type": "custom", "url": "https://example.org"}]},
							{"name": "dev/pkg", "version": "dev-foo", "default-branch": true, "funding": [{"type": "github", "url": "https://github.com/org"}]},
							{"name": "stable/pkg", "version": "1.0.0", "funding": [{"type": "github", "url": "org2"}]}
						]
					}
				],
				"require": {"first/pkg": "^2.0", "stable/pkg": "^1.0"},
				"require-dev": {"dev/pkg": "~4.0"}
			}`,
			nil,
			[][3]string{
				{"first/pkg", "github", "https://github.com/composer-test-data"},
				{"dev/pkg", "github", "https://github.com/composer-test-data-dev"},
				{"stable/pkg", "github", "https://github.com/composer-test-data-stable"},
			},
			`The following packages were found in your dependencies which publish funding information:

dev
  pkg
    https://github.com/sponsors/org

first
    https://example.org

stable
    https://github.com/sponsors/composer-test-data-stable

Please consider following these links and sponsoring the work of package authors!
Thank you!`,
		},
		{
			"format funding links as JSON",
			`{"repositories": [], "require": {"first/pkg": "^2.0"}, "require-dev": {"dev/pkg": "~4.0"}}`,
			gbParams("--format", "json"),
			[][3]string{
				{"first/pkg", "github", "https://github.com/composer-test-data"},
				{"dev/pkg", "github", "https://github.com/composer-test-data-dev"},
			},
			`{
    "dev": {
        "https://github.com/sponsors/composer-test-data-dev": [
            "pkg"
        ]
    },
    "first": {
        "https://github.com/sponsors/composer-test-data": [
            "pkg"
        ]
    }
}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, tt.composerJSON, nil, nil, true)

			packages := map[string]*pkg.CompletePackage{
				"first/pkg":  commandtest.GetPackage(t, "first/pkg", "2.3.4"),
				"stable/pkg": commandtest.GetPackage(t, "stable/pkg", "1.0.0"),
			}
			devPackages := map[string]*pkg.CompletePackage{
				"dev/pkg": commandtest.GetPackage(t, "dev/pkg", "2.3.4.5"),
			}

			for _, f := range tt.funding {
				info := php.ListOf(php.ArrayOf("type", f[1], "url", f[2]))
				if p, ok := packages[f[0]]; ok {
					p.SetFunding(info)
				}
				if p, ok := devPackages[f[0]]; ok {
					p.SetFunding(info)
				}
			}

			commandtest.CreateInstalledJSON(t,
				[]pkg.PackageInterface{packages["first/pkg"], packages["stable/pkg"]},
				[]pkg.PackageInterface{devPackages["dev/pkg"]},
				true,
			)

			appTester := gbRun(t, gbMerge(gbParams("command", "fund"), tt.command, true))

			if code := appTester.StatusCode(); code != 0 {
				t.Errorf("command not successful: exit code %d", code)
			}
			gbAssertSame(t, php.Trim(tt.expected), gbTrim(appTester))
		})
	}
}
