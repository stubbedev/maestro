package command_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/ui"
)

// Undecorated, diagnose writes Composer's lines and says which maestro
// ran; decorated, the same checks and findings as a check list, with
// warnings as diagnostics; the exit code is the same.
func TestDiagnoseCommand_Decorated(t *testing.T) {
	t.Setenv("COMPOSER_DISABLE_NETWORK", "1")
	commandtest.InitTempComposer(t, `{"name": "foo/bar", "description": "test pkg"}`, nil, nil, true)

	run := func(decorated bool) (int, string) {
		tester := commandtest.GetApplicationTester(t)
		code, err := tester.RunArgs(commandtest.Options{Decorated: &decorated}, "command", "diagnose")
		if err != nil {
			t.Fatal(err)
		}

		return code, tester.Display(true)
	}
	plainCode, plain := run(false)
	decoratedCode, decorated := run(true)
	if plainCode != 1 || decoratedCode != plainCode {
		t.Errorf("exit codes %d undecorated, %d decorated, want 1", plainCode, decoratedCode)
	}
	if !strings.Contains(plain, "Composer version: "+composer.Version+"\nMaestro version: ") {
		t.Errorf("undecorated output does not name maestro after Composer:\n%s", plain)
	}
	if !strings.Contains(plain, "Checking composer.json: <warning>WARNING</warning>\n<warning>No license specified") {
		t.Errorf("undecorated output lacks the composer.json warning:\n%s", plain)
	}

	shown := regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(decorated, "")
	if strings.Contains(shown, "Checking ") {
		t.Errorf("decorated output has Composer's lines:\n%s", shown)
	}
	for _, want := range []string{
		"Composer version", composer.Version, "Maestro version",
		ui.GlyphCheckWarning.String() + " composer.json",
		"Warning: No license specified",
		ui.GlyphCheckOK.String() + " disk free space",
		ui.GlyphCheckOK.String() + " http connectivity to packagist",
		"SKIP Network is disabled by COMPOSER_DISABLE_NETWORK.",
	} {
		if !strings.Contains(shown, want) {
			t.Errorf("decorated output lacks %q:\n%s", want, shown)
		}
	}
}
