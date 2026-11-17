package command_test

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/ui"
)

func TestUpdate_ResolutionFailureIsAReportWhenDecorated(t *testing.T) {
	run := func(decorated bool) commandtest.Streams {
		commandtest.InitTempComposer(t, updateRootDepAndTransitiveDep, nil, nil, true)
		tester := commandtest.GetApplicationTester(t)
		code, err := tester.RunArgs(commandtest.Options{Decorated: &decorated, CaptureStderrSeparately: true},
			"command", "update", "--dry-run", true, "--no-audit", true, "--with", []string{"dep/pkg:^2"})
		if err != nil {
			t.Fatal(err)
		}

		return commandtest.Streams{Code: code, Stdout: tester.Display(true), Stderr: tester.ErrorOutput(true)}
	}
	plain, decorated := run(false), run(true)
	if plain.Code != 2 || decorated.Code != plain.Code {
		t.Fatalf("exit code %d undecorated, %d decorated", plain.Code, decorated.Code)
	}

	composerText := `
  Problem 1
    - Root composer.json requires root/req 1.* -> satisfiable by root/req[1.0.0].
    - root/req 1.0.0 requires dep/pkg ^1 -> found dep/pkg[1.0.0, 1.0.1, 1.0.2] but it conflicts with your temporary update constraint (dep/pkg:^2).
`
	if !strings.Contains(plain.Stderr, composerText) || strings.Contains(plain.Stderr, "\x1b") {
		t.Errorf("undecorated stderr is not Composer's problem text:\n%s", plain.Stderr)
	}

	for _, styled := range []string{
		ui.RoleEmphasis.Styled("Problem 1"),
		ui.RoleEmphasis.Styled("root composer.json"),
		ui.RolePackage.Styled("root/req") + " " + ui.RoleVersion.Styled("1.0.0") + " " + ui.RoleMuted.Styled("requires") + " " + ui.RolePackage.Styled("dep/pkg") + " " + ui.RoleVersion.Styled("^1"),
		ui.RoleDanger.Styled(ui.GlyphCheckFailed.String()) + " found dep/pkg[1.0.0, 1.0.1, 1.0.2] but it conflicts with your temporary update constraint (dep/pkg:^2).",
	} {
		if !strings.Contains(decorated.Stderr, styled) {
			t.Errorf("decorated stderr does not show %q:\n%q", styled, decorated.Stderr)
		}
	}
	stripped := sgrOrLinkRe.ReplaceAllString(decorated.Stderr, "")
	if strings.Contains(stripped, "- Root composer.json requires") {
		t.Errorf("decorated stderr is Composer's problem text:\n%s", stripped)
	}
	for _, line := range []string{"Your requirements could not be resolved to an installable set of packages.", "    root composer.json requires root/req 1.*"} {
		if !strings.Contains(stripped, line+"\n") {
			t.Errorf("decorated stderr lacks the line %q:\n%s", line, stripped)
		}
	}
}
