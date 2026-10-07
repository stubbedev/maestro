// Ports tests/Composer/Test/CompletionFunctionalTest.php (with Symfony's
// CommandCompletionTester). Like Composer's, it completes against
// Composer's own project (.ref/composer, see docs/PORTING.md) and packagist,
// so it only runs with MAESTRO_E2E=1.

package command_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/switches"
)

// completeCommand ports CommandCompletionTester::complete.
func completeCommand(t *testing.T, cmd console.Commander, input []string) []string {
	t.Helper()
	currentIndex := len(input)
	if len(input) > 0 && input[len(input)-1] == "" {
		input = input[:len(input)-1]
	}
	input = append([]string{cmd.Base().Name()}, input...)

	completionInput := console.CompletionInputFromTokens(input, currentIndex)
	if err := completionInput.Bind(cmd.Base().Definition()); err != nil {
		t.Fatal(err)
	}
	suggestions := &console.CompletionSuggestions{}
	cmd.Complete(completionInput, suggestions)

	var out []string
	for _, o := range suggestions.OptionSuggestions() {
		out = append(out, "--"+o.Name())
	}
	for _, v := range suggestions.ValueSuggestions() {
		out = append(out, v.Value)
	}

	return out
}

func TestCompletionFunctional_Complete(t *testing.T) {
	if !switches.On(switches.E2E) {
		t.Skip("set MAESTRO_E2E=1 to complete against .ref/composer and packagist")
	}
	if _, err := os.Stat("../../.ref/composer/composer.json"); err != nil {
		t.Skip(".ref/composer is missing (run ref-sync)")
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../../.ref/composer"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	randomVendor := []string{"a/"}
	installedPackages := []string{"composer/semver", "psr/log"}
	preferInstall := []string{"dist", "source", "auto"}

	cases := []struct {
		input string
		want  []string
	}{
		{"archive ", randomVendor},
		{"archive symfony/http-", []string{"symfony/http-kernel", "symfony/http-foundation"}},
		{"archive --format ", []string{"tar", "zip"}},
		{"create-project ", randomVendor},
		{"create-project symfony/skeleton --prefer-install ", preferInstall},
		{"depends ", installedPackages},
		{"why ", installedPackages},
		// Composer expects phpstan, phpstan.phar and simple-phpunit too: its
		// CI installs the dev requirements, which ref-sync does not.
		{"exec ", []string{"composer", "jsonlint", "validate-json"}},
		{"browse ", installedPackages},
		{"home -H ", installedPackages},
		{"init --require ", randomVendor},
		{"init --require-dev foo/bar --require-dev ", randomVendor},
		{"install --prefer-install ", preferInstall},
		{"install ", nil},
		{"outdated ", installedPackages},
		{"prohibits ", randomVendor},
		{"why-not symfony/http-ker", []string{"symfony/http-kernel"}},
		{"reinstall --prefer-install ", preferInstall},
		{"reinstall ", installedPackages},
		{"remove ", installedPackages},
		{"require --prefer-install ", preferInstall},
		{"require ", randomVendor},
		{"require --dev symfony/http-", []string{"symfony/http-kernel", "symfony/http-foundation"}},
		{"run-script ", []string{"compile", "test", "phpstan"}},
		{"run-script test ", nil},
		{"search --format ", []string{"text", "json"}},
		{"show --format ", []string{"text", "json"}},
		{"info ", installedPackages},
		{"suggests ", installedPackages},
		{"update --prefer-install ", preferInstall},
		{"update ", installedPackages},
		{"config --list ", nil},
		{"config --editor ", nil},
		{"config --auth ", nil},
		{"config ", []string{"bin-compat", "extra", "extra.branch-alias", "home", "name", "repositories", "repositories.packagist.org", "suggest", "suggest.ext-zip", "type", "version"}},
		{"config bin", []string{"bin-dir"}},
		{"config nam", []string{"name"}},
		{"config ver", []string{"version"}},
		{"config repo", []string{"repositories", "repositories.packagist.org"}},
		{"config repositories.", []string{"repositories.packagist.org"}},
		{"config sug", []string{"suggest", "suggest.ext-zip"}},
		{"config suggest.ext-", []string{"suggest.ext-zip"}},
		{"config ext", []string{"extra", "extra.branch-alias", "extra.branch-alias.dev-main"}},
		{"config --unset ", []string{"extra", "extra.branch-alias", "extra.branch-alias.dev-main", "name", "suggest", "suggest.ext-zip", "type"}},
		{"config --unset bin-dir", nil},
		{"config --unset nam", []string{"name"}},
		{"config --unset version", nil},
		{"config --unset extra.", []string{"extra.branch-alias", "extra.branch-alias.dev-main"}},
		{"config --global ", []string{"bin-compat", "home", "repositories", "repositories.packagist.org"}},
		{"config --global repo", []string{"repositories", "repositories.packagist.org"}},
		{"config --global repositories.", []string{"repositories.packagist.org"}},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			input := strings.Split(tc.input, " ")
			app := commandtest.NewApplication()
			cmd, err := app.Get(input[0])
			if err != nil {
				t.Fatal(err)
			}
			suggestions := completeCommand(t, cmd, input[1:])
			if tc.want == nil {
				if len(suggestions) != 0 {
					t.Errorf("expected no suggestions, got %q", suggestions)
				}

				return
			}
			for _, w := range tc.want {
				if !slices.Contains(suggestions, w) {
					t.Errorf("Suggestions must contain %q. Got %q.", w, suggestions)
				}
			}
		})
	}
}
