// Ports tests/Composer/Test/Command/ValidateCommandTest.php.

package command_test

import (
	"os"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// validateMinimalValidConfiguration is MINIMAL_VALID_CONFIGURATION; strip
// drops name, type, description and license (array_diff_key).
func validateMinimalValidConfiguration(strip bool) *php.Array {
	a := php.NewArray()
	if !strip {
		a.Set("name", "test/suite")
		a.Set("type", "library")
		a.Set("description", "A generical test suite")
		a.Set("license", "MIT")
	}
	a.Set("repositories", php.ArrayOf(
		"packages", php.ArrayOf(
			"type", "package",
			"package", php.ListOf(
				php.ArrayOf("name", "root/req", "version", "1.0.0", "require", php.ArrayOf("dep/pkg", "^1")),
				php.ArrayOf("name", "dep/pkg", "version", "1.0.0"),
				php.ArrayOf("name", "dep/pkg", "version", "1.0.1"),
				php.ArrayOf("name", "dep/pkg", "version", "1.0.2"),
			),
		),
	))
	a.Set("require", php.ArrayOf("root/req", "1.*"))

	return a
}

func TestValidateCommand_Validate(t *testing.T) {
	cases := []struct {
		name         string
		composerJSON *php.Array
		command      gbKV
		expected     string
	}{
		{
			name:         "validation passing",
			composerJSON: validateMinimalValidConfiguration(false),
			expected: `<warning>Composer could not detect the root package (test/suite) version, defaulting to '1.0.0'. See https://getcomposer.org/root-version</warning>
<warning>Composer could not detect the root package (test/suite) version, defaulting to '1.0.0'. See https://getcomposer.org/root-version</warning>
./composer.json is valid`,
		},
		{
			name:         "passing but with warnings",
			composerJSON: validateMinimalValidConfiguration(true),
			expected: `./composer.json is valid for simple usage with Composer but has
strict errors that make it unable to be published as a package
<warning>See https://getcomposer.org/doc/04-schema.md for details on the schema</warning>
# Publish errors
- name : The property name is required
- description : The property description is required
<warning># General warnings</warning>
- No license specified, it is recommended to do so. For closed-source software you may use "proprietary" as license.`,
		},
		{
			name:         "passing without publish-check",
			composerJSON: validateMinimalValidConfiguration(true),
			command:      gbParams("--no-check-publish", true),
			expected: `./composer.json is valid, but with a few warnings
<warning>See https://getcomposer.org/doc/04-schema.md for details on the schema</warning>
<warning># General warnings</warning>
- No license specified, it is recommended to do so. For closed-source software you may use "proprietary" as license.`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, tc.composerJSON, nil, nil, true)

			appTester := gbRun(t, gbMerge(gbParams("command", "validate"), tc.command, true))
			gbAssertSame(t, tc.expected, gbTrim(appTester))
		})
	}
}

func TestValidateCommand_ValidateOnFileIssues(t *testing.T) {
	directory := commandtest.InitTempComposer(t, validateMinimalValidConfiguration(false), nil, nil, true)
	if err := os.Remove(directory + "/composer.json"); err != nil {
		t.Fatal(err)
	}

	appTester := gbRun(t, gbParams("command", "validate"))
	gbAssertSame(t, "./composer.json not found.", gbTrim(appTester))
}

func TestValidateCommand_WithComposerLock(t *testing.T) {
	commandtest.InitTempComposer(t, validateMinimalValidConfiguration(false), nil, nil, true)
	commandtest.CreateComposerLock(t, nil, nil)

	appTester := gbRun(t, gbParams("command", "validate"))
	expected := `<warning>Composer could not detect the root package (test/suite) version, defaulting to '1.0.0'. See https://getcomposer.org/root-version</warning>
<warning>Composer could not detect the root package (test/suite) version, defaulting to '1.0.0'. See https://getcomposer.org/root-version</warning>
./composer.json is valid but your composer.lock has some errors
# Lock file errors
- Required package "root/req" is not present in the lock file.
This usually happens when composer files are incorrectly merged or the composer.json file is manually edited.
Read more about correctly resolving merge conflicts https://getcomposer.org/doc/articles/resolving-merge-conflicts.md
and prefer using the "require" command over editing the composer.json file directly https://getcomposer.org/doc/03-cli.md#require-r`
	gbAssertSame(t, expected, gbTrim(appTester))
}

func TestValidateCommand_UnaccessibleFile(t *testing.T) {
	if util.IsWindows() {
		t.Skip("Does not run on windows")
	}
	if os.Getuid() == 0 {
		t.Skip("Cannot run as root")
	}

	directory := commandtest.InitTempComposer(t, validateMinimalValidConfiguration(false), nil, nil, true)
	if err := os.Chmod(directory+"/composer.json", 0o200); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory+"/composer.json", 0o700) })

	appTester := gbRun(t, gbParams("command", "validate"))
	gbAssertSame(t, "./composer.json is not readable.", gbTrim(appTester))
	if appTester.StatusCode() != 3 {
		t.Errorf("status %d", appTester.StatusCode())
	}
}
