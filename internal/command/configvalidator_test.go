// Ports tests/Composer/Test/Util/ConfigValidatorTest.php.

package command

import (
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/pkg/loader"
)

func validateWarnings(t *testing.T, file string) []string {
	t.Helper()
	v := NewConfigValidator(io.NewNullIO())
	_, _, warnings, err := v.Validate("testdata/Util/Fixtures/"+file, loader.CheckAll, ConfigValidatorCheckVersion)
	if err != nil {
		t.Fatal(err)
	}

	return warnings
}

func assertContains(t *testing.T, list []string, want string) {
	t.Helper()
	if !slices.Contains(list, want) {
		t.Errorf("%q not in %q", want, list)
	}
}

func TestConfigValidator_ConfigValidatorCommitRefWarning(t *testing.T) {
	warnings := validateWarnings(t, "composer_commit-ref.json")
	assertContains(t, warnings, `The package "some/package" is pointing to a commit-ref, this is bad practice and can cause unforeseen issues.`)
}

func TestConfigValidator_ConfigValidatorWarnsOnScriptDescriptionForNonexistentScript(t *testing.T) {
	warnings := validateWarnings(t, "composer_scripts-descriptions.json")
	assertContains(t, warnings, `Description for non-existent script "phpcsxxx" found in "scripts-descriptions"`)
}

func TestConfigValidator_ConfigValidatorWarnsOnScriptAliasForNonexistentScript(t *testing.T) {
	warnings := validateWarnings(t, "composer_scripts-aliases.json")
	assertContains(t, warnings, `Aliases for non-existent script "phpcsxxx" found in "scripts-aliases"`)
}

func TestConfigValidator_ConfigValidatorWarnsOnUnnecessaryProvideReplace(t *testing.T) {
	warnings := validateWarnings(t, "composer_provide-replace-requirements.json")
	assertContains(t, warnings, "The package a/a in require is also listed in provide which satisfies the requirement. Remove it from provide if you wish to install it.")
	assertContains(t, warnings, "The package b/b in require is also listed in replace which satisfies the requirement. Remove it from replace if you wish to install it.")
	assertContains(t, warnings, "The package c/c in require-dev is also listed in provide which satisfies the requirement. Remove it from provide if you wish to install it.")
}
