// Ports tests/Composer/Test/Command/SearchCommandTest.php.

package command_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/php"
)

func TestSearchCommand_Search(t *testing.T) {
	tests := []struct {
		name     string
		command  gbKV
		expected string
	}{
		{
			"by name and description",
			gbParams("tokens", []string{"fancy"}),
			`bar/baz                <warning>! Abandoned !</warning> fancy baz
vendor-2/fancy-package`,
		},
		{
			"by name and description with multiple tokens",
			gbParams("tokens", []string{"fancy", "vendor"}),
			`vendor-1/package-1     generic description
bar/baz                <warning>! Abandoned !</warning> fancy baz
vendor-2/fancy-package`,
		},
		{
			"by name only",
			gbParams("tokens", []string{"fancy"}, "--only-name", true),
			`vendor-2/fancy-package`,
		},
		{
			"by vendor only",
			gbParams("tokens", []string{"bar"}, "--only-vendor", true),
			`bar`,
		},
		{
			"by type",
			gbParams("tokens", []string{"vendor"}, "--type", "foo"),
			`vendor-2/fancy-package`,
		},
		{
			"json format",
			gbParams("tokens", []string{"vendor-2/fancy"}, "--format", "json"),
			`[
    {
        "name": "vendor-2/fancy-package",
        "description": null
    }
]`,
		},
		{
			"no results",
			gbParams("tokens", []string{"invalid-package-name"}),
			``,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, `{
				"repositories": [
					{"packagist.org": false},
					{
						"type": "package",
						"package": [
							{"name": "vendor-1/package-1", "description": "generic description", "version": "1.0.0"},
							{"name": "foo/bar", "description": "generic description", "version": "1.0.0"},
							{"name": "bar/baz", "description": "fancy baz", "version": "1.0.0", "abandoned": true},
							{"name": "vendor-2/fancy-package", "0": "fancy description", "version": "1.0.0", "type": "foo"}
						]
					}
				]
			}`, nil, nil, true)

			appTester := gbRun(t, gbMerge(gbParams("command", "search"), tt.command, true))
			gbAssertSame(t, php.Trim(tt.expected), gbTrim(appTester))
		})
	}
}

func TestSearchCommand_InvalidFormat(t *testing.T) {
	commandtest.InitTempComposer(t, `{"repositories": {"packagist.org": false}}`, nil, nil, true)

	appTester := gbRun(t, gbParams("command", "search", "--format", "test-format", "tokens", []string{"test"}))
	if code := appTester.StatusCode(); code != 1 {
		t.Errorf("exit code %d, want 1", code)
	}
	gbAssertSame(t, `Unsupported format "test-format". See help for supported formats.`, gbTrim(appTester))
}

func TestSearchCommand_InvalidFlags(t *testing.T) {
	commandtest.InitTempComposer(t, `{"repositories": {"packagist.org": false}}`, nil, nil, true)

	err := gbRunErr(t, gbParams("command", "search", "--only-vendor", true, "--only-name", true, "tokens", []string{"test"}))
	if !gbIsInvalidArgument(err) {
		t.Fatalf("expected an InvalidArgumentException, got %v", err)
	}
	gbAssertSame(t, "--only-name and --only-vendor cannot be used together", err.Error())
}

func TestSearchCommand_VerboseOutput(t *testing.T) {
	commandtest.InitTempComposer(t, `{
		"repositories": [
			{"packagist.org": false},
			{
				"type": "package",
				"package": [
					{"name": "vendor-1/package-1", "description": "generic description", "version": "1.0.0"},
					{"name": "vendor-1/package-2", "description": "another package", "version": "1.0.0"},
					{"name": "foo/bar", "description": "generic description", "version": "1.0.0"}
				]
			}
		]
	}`, nil, nil, true)

	appTester := gbRun(t, gbParams("command", "search", "tokens", []string{"vendor-1"}, "-vvv", true))

	output := appTester.Display(true)
	gbContains(t, output, "Searched installed array repo (defining 0 package), found 0 result(s)")
	gbContains(t, output, "Searched platform repo, found 0 result(s)")
	gbContains(t, output, "Searched package repo (defining 3 packages), found 2 result(s)")
	gbContains(t, output, "vendor-1/package-1")
	gbContains(t, output, "vendor-1/package-2")
}
