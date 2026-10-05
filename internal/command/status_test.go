// Ports tests/Composer/Test/Command/StatusCommandTest.php.

package command_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

func TestStatusCommand_NoLocalChanges(t *testing.T) {
	commandtest.InitTempComposer(t, `{"require": {"root/req": "1.*"}}`, nil, nil, true)

	p := commandtest.GetPackage(t, "root/req", "1.0.0")
	p.SetType("metapackage")

	commandtest.CreateComposerLock(t, []pkg.PackageInterface{p}, nil)
	commandtest.CreateInstalledJSON(t, []pkg.PackageInterface{p}, nil, true)

	appTester := commandtest.GetApplicationTester(t)
	if _, err := appTester.RunArgs(commandtest.Options{}, "command", "status"); err != nil {
		t.Fatal(err)
	}

	if got := php.Trim(appTester.Display(true)); got != "No local changes" {
		t.Errorf("got %q", got)
	}
}

// TestStatusCommand_LocallyModifiedPackages installs real packages from
// GitHub as the PHP test does, so it needs the network: it runs only with
// MAESTRO_NETWORK_TESTS=1.
func TestStatusCommand_LocallyModifiedPackages(t *testing.T) {
	if os.Getenv("MAESTRO_NETWORK_TESTS") != "1" {
		t.Skip("needs the network (set MAESTRO_NETWORK_TESTS=1)")
	}
	cases := []struct {
		name               string
		composerJSON       string
		commandFlags       []console.Param
		pkgName, version   string
		installationSource string
		typ, url, ref      string
	}{
		{
			name:               "locally modified package from source",
			composerJSON:       `{"require": {"composer/class-map-generator": "^1.0"}}`,
			pkgName:            "composer/class-map-generator",
			version:            "1.1",
			installationSource: "source",
			typ:                "git",
			url:                "https://github.com/composer/class-map-generator.git",
			ref:                "953cc4ea32e0c31f2185549c7d216d7921f03da9",
		},
		{
			name:               "locally modified package from dist",
			composerJSON:       `{"require": {"composer/ca-bundle": "^1.5"}}`,
			commandFlags:       []console.Param{console.P("--verbose", true)},
			pkgName:            "composer/ca-bundle",
			version:            "1.5.12",
			installationSource: "dist",
			typ:                "zip",
			url:                "https://api.github.com/repos/composer/ca-bundle/zipball/00a2f4201641d5c53f7fc0195e6c8d9fcc321a78",
			ref:                "00a2f4201641d5c53f7fc0195e6c8d9fcc321a78",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, tc.composerJSON, nil, nil, true)

			p := commandtest.GetPackage(t, tc.pkgName, tc.version)
			p.SetInstallationSource(pkg.Str(tc.installationSource))
			if tc.installationSource == "source" {
				p.SetSourceType(pkg.Str(tc.typ))
				p.SetSourceURL(pkg.Str(tc.url))
				p.SetSourceReference(pkg.Str(tc.ref))
			}
			if tc.installationSource == "dist" {
				p.SetDistType(pkg.Str(tc.typ))
				p.SetDistURL(pkg.Str(tc.url))
				p.SetDistReference(pkg.Str(tc.ref))
			}

			commandtest.CreateComposerLock(t, []pkg.PackageInterface{p}, nil)

			appTester := commandtest.GetApplicationTester(t)
			if _, err := appTester.RunArgs(commandtest.Options{}, "command", "install"); err != nil {
				t.Fatal(err)
			}

			cwd, _ := os.Getwd()
			if err := os.WriteFile(cwd+"/vendor/"+tc.pkgName+"/composer.json", []byte("{}"), 0o666); err != nil {
				t.Fatal(err)
			}

			params := append([]console.Param{console.P("command", "status")}, tc.commandFlags...)
			if _, err := appTester.Run(params, commandtest.Options{}); err != nil {
				t.Fatal(err)
			}

			actual := php.Trim(appTester.Display(true))
			for _, want := range []string{"You have changes in the following dependencies:", tc.pkgName} {
				if !strings.Contains(actual, want) {
					t.Errorf("%q does not contain %q", actual, want)
				}
			}
		})
	}
}
