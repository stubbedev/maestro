// Ports tests/Composer/Test/Console/ApplicationTest.php and
// tests/Composer/Test/Command/AboutCommandTest.php.

package command_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

func bufferedOutput() (*console.StreamOutput, *bytes.Buffer) {
	b := &bytes.Buffer{}

	return console.NewStreamOutput(b, console.VerbosityNormal, new(false), nil), b
}

func arrayInput(t *testing.T, params ...console.Param) *console.ArrayInput {
	t.Helper()
	in, err := console.NewArrayInput(params, nil)
	if err != nil {
		t.Fatal(err)
	}

	return in
}

func xdebugWarnOff(t *testing.T) {
	t.Helper()
	util.PutEnv("COMPOSER_DISABLE_XDEBUG_WARN", "1")
	t.Cleanup(func() { util.ClearEnv("COMPOSER_DISABLE_XDEBUG_WARN") })
}

// TestAboutCommand_About ports AboutCommandTest::testAbout, asserting
// the whole of stdout (Composer writes the three lines with IO::write)
// and an empty stderr, in a directory without composer.json: about never
// loads the project.
func TestAboutCommand_About(t *testing.T) {
	commandtest.InitTempDir(t)
	got := commandtest.GetApplicationTester(t).RunStreams("command", "about")
	want := "Composer - Dependency Manager for PHP - version " + composer.GetVersion() + "\n" +
		"Composer is a dependency manager tracking local dependencies of your projects and libraries.\n" +
		"See https://getcomposer.org/ for more information.\n"
	if got != (commandtest.Streams{Stdout: want}) {
		t.Errorf("about: %+v, want stdout %q only", got, want)
	}
}

// testDevWarning and testDevWarningSuppressedForSelfUpdate are not ported:
// maestro has no COMPOSER_DEV_WARNING_TIME (no dev builds of Composer), and
// its self-update is its own (SelfUpdateCommandTest covers it).

func TestApplication_ProcessIsolationWorksMultipleTimes(t *testing.T) {
	xdebugWarnOff(t)
	application := commandtest.NewApplication()
	if _, err := application.Add(command.NewAboutCommand()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		out, _ := bufferedOutput()
		if code, err := application.DoRun(arrayInput(t, console.P("command", "about")), out); err != nil || code != 0 {
			t.Fatalf("doRun: %d %v", code, err)
		}
	}
}

func TestApplication_NoPluginsDisablesPluginsWhenScriptCommandsExist(t *testing.T) {
	xdebugWarnOff(t)
	if !command.HasScriptAliasCommand() {
		t.Skip("ScriptAliasCommand not registered yet")
	}
	commandtest.InitTempComposer(t, `{"scripts": {"my-script": "echo hello"}}`, nil, nil, true)

	application := commandtest.NewApplication()
	application.SetAutoExit(false)
	application.SetCatchExceptions(false)

	// Run list command with --no-plugins, this triggers script command registration which previously
	// created a Composer instance with plugins enabled regardless of the --no-plugins flag
	out, _ := bufferedOutput()
	if _, err := application.DoRun(arrayInput(t, console.P("command", "list"), console.P("--no-plugins", true)), out); err != nil {
		t.Fatal(err)
	}

	c, err := application.GetComposer(false, nil, nil)
	if err != nil || c == nil {
		t.Fatalf("Composer instance should have been created during script command registration: %v", err)
	}
	pm, ok := c.PluginManager().(interface{ ArePluginsDisabled(string) bool })
	if !ok {
		t.Fatalf("plugin manager %T", c.PluginManager())
	}
	if !pm.ArePluginsDisabled("local") {
		t.Error("Plugins should be disabled when --no-plugins is used")
	}
	if !pm.ArePluginsDisabled("global") {
		t.Error("Global plugins should be disabled when --no-plugins is used")
	}
}

func TestApplication_ScriptCommandTakesPriorityOverAbbreviatedBuiltinCommand(t *testing.T) {
	xdebugWarnOff(t)
	if !command.HasScriptAliasCommand() {
		t.Skip("ScriptAliasCommand not registered yet")
	}
	commandtest.InitTempComposer(t, `{"scripts": {"check": "echo hello"}}`, nil, nil, true)

	application := commandtest.NewApplication()
	application.SetAutoExit(false)
	application.SetCatchExceptions(false)

	out, buf := bufferedOutput()
	exitCode, err := application.DoRun(arrayInput(t, console.P("command", "check")), out)
	if err != nil || exitCode != 0 {
		t.Fatalf("Script command should have run successfully: %d %v", exitCode, err)
	}
	if !strings.Contains(buf.String(), "hello") {
		t.Errorf(`The "check" script should have been executed instead of the check-platform-reqs command: %q`, buf.String())
	}
}

type pluginCommand struct{ *console.Command }

func (pluginCommand) PHPClass() string { return `Acme\Plugin\FooCommand` }

func TestApplication_GetTelemetryCommandName(t *testing.T) {
	cases := []struct {
		name    string
		command console.Commander
		want    string
	}{
		// built-in Composer command reports its own name
		{"composer command", command.NewAboutCommand(), "about"},
		// Symfony's built-in console commands report their own name
		{"symfony builtin", console.NewHelpCommand(), "help"},
		// anything else is a plugin
		{"plugin", pluginCommand{console.NewCommand("foo")}, "plugin"},
	}
	for _, tc := range cases {
		if got := command.TelemetryCommandName(tc.command); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestApplication_LongVersion(t *testing.T) {
	app := commandtest.NewApplication()
	want := "<info>Composer</info> version <comment>" + composer.GetVersion() + "</comment> " + composer.ReleaseDate
	if got := app.LongVersion(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestApplication_InvalidWorkingDir(t *testing.T) {
	xdebugWarnOff(t)
	app := commandtest.NewApplication()
	app.SetAutoExit(false)
	out, _ := bufferedOutput()
	_, err := app.DoRun(arrayInput(t, console.P("command", "about"), console.P("--working-dir", "/does/not/exist")), out)
	if err == nil || err.Error() != "Invalid working directory specified, /does/not/exist does not exist." {
		t.Fatalf("got %v", err)
	}
}

func statusBufferedOutput() *console.BufferedOutput {
	return console.NewBufferedOutput(console.VerbosityNormal, true, nil)
}

func TestApplication_CompletionProcessDoesNotReportStatus(t *testing.T) {
	xdebugWarnOff(t)
	originalArgv := os.Args
	os.Args = []string{"composer", "_complete"}
	t.Cleanup(func() { os.Args = originalArgv })

	t.Setenv("COMPOSER_TERMINAL_STATUS", "7501,9;4")
	application := commandtest.NewApplication()
	if _, err := application.Add(command.NewAboutCommand()); err != nil {
		t.Fatal(err)
	}
	output := statusBufferedOutput()
	if code, err := application.DoRun(arrayInput(t, console.P("command", "about"), console.P("--no-plugins", true)), output); err != nil || code != 0 {
		t.Fatalf("doRun: %d %v", code, err)
	}
	if written := output.Fetch(); strings.Contains(written, "\033]") {
		t.Errorf("wrote %q", written)
	}
	if got, _ := util.GetEnv("COMPOSER_TERMINAL_STATUS"); got != "7501,9;4" {
		t.Errorf("setting = %q", got)
	}
}

func TestApplication_NestedApplicationsDoNotOverwriteParentStatus(t *testing.T) {
	xdebugWarnOff(t)
	t.Setenv("COMPOSER_TERMINAL_STATUS", "7501")
	application := commandtest.NewApplication()
	nested := console.NewCommand("nested-status-test")
	nested.SetCode(func(in console.Input, out console.Output) (int, error) {
		child := commandtest.NewApplication()
		if _, err := child.Add(command.NewAboutCommand()); err != nil {
			return 0, err
		}
		result, err := child.DoRun(arrayInput(t, console.P("command", "about"), console.P("--no-plugins", true)), out)
		out.Writeln("Parent still running")

		return result, err
	})
	if _, err := application.Add(nested); err != nil {
		t.Fatal(err)
	}
	output := statusBufferedOutput()
	if code, err := application.DoRun(arrayInput(t, console.P("command", "nested-status-test"), console.P("--no-plugins", true)), output); err != nil || code != 0 {
		t.Fatalf("doRun: %d %v", code, err)
	}
	written := output.Fetch()
	if got := strings.Count(written, "\033]7501;"); got != 2 {
		t.Errorf("%d statuses in %q", got, written)
	}
	if !strings.Contains(written, "Composer - Dependency Manager for PHP") {
		t.Errorf("missing about output: %q", written)
	}
	if want := "Parent still running" + php.EOL + "\033]7501;state=done:app=composer\033\\"; !strings.HasSuffix(written, want) {
		t.Errorf("wrote %q, want it to end with %q", written, want)
	}
}

func TestApplication_TerminalStatusEnvironmentIsRestored(t *testing.T) {
	xdebugWarnOff(t)
	cases := []struct {
		setting *string
	}{
		{nil},
		{new("auto")},
		{new("7501,9;4")},
		{new("off")},
		{new("")},
	}
	for _, tc := range cases {
		if tc.setting == nil {
			util.ClearEnv("COMPOSER_TERMINAL_STATUS")
		} else {
			util.PutEnv("COMPOSER_TERMINAL_STATUS", *tc.setting)
		}
		t.Cleanup(func() { util.ClearEnv("COMPOSER_TERMINAL_STATUS") })
		application := commandtest.NewApplication()
		if _, err := application.Add(command.NewAboutCommand()); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			output := statusBufferedOutput()
			if code, err := application.DoRun(arrayInput(t, console.P("command", "about"), console.P("--no-plugins", true)), output); err != nil || code != 0 {
				t.Fatalf("doRun: %d %v", code, err)
			}
			got, ok := util.GetEnv("COMPOSER_TERMINAL_STATUS")
			if tc.setting == nil {
				if ok {
					t.Errorf("setting = %q, want it unset", got)
				}
			} else if got != *tc.setting {
				t.Errorf("setting = %q, want %q", got, *tc.setting)
			}
			want := 2
			if tc.setting != nil && *tc.setting == "off" {
				want = 0
			}
			if count := strings.Count(output.Fetch(), "\033]7501;"); count != want {
				t.Errorf("setting %v: %d statuses, want %d", tc.setting, count, want)
			}
		}
	}
}

func TestApplication_ProgramStatusReportsCommandOutcome(t *testing.T) {
	xdebugWarnOff(t)
	t.Setenv("COMPOSER_TERMINAL_STATUS", "7501")
	cases := []struct {
		exitCode int
		state    string
	}{
		{0, "done"},
		{1, "error"},
		{130, "idle"},
		{143, "idle"},
	}
	for _, tc := range cases {
		application := commandtest.NewApplication()
		cmd := console.NewCommand("status-test")
		cmd.SetCode(func(console.Input, console.Output) (int, error) { return tc.exitCode, nil })
		if _, err := application.Add(cmd); err != nil {
			t.Fatal(err)
		}
		output := statusBufferedOutput()
		code, err := application.DoRun(arrayInput(t, console.P("command", "status-test"), console.P("--no-plugins", true)), output)
		if err != nil || code != tc.exitCode {
			t.Fatalf("doRun = %d %v", code, err)
		}
		written := output.Fetch()
		if !strings.HasPrefix(written, "\033]7501;state=working:app=composer\033\\") {
			t.Errorf("exit %d: wrote %q", tc.exitCode, written)
		}
		if want := "\033]7501;state=" + tc.state + ":app=composer\033\\"; !strings.HasSuffix(written, want) {
			t.Errorf("exit %d: wrote %q, want it to end with %q", tc.exitCode, written, want)
		}
	}
}

func TestApplication_ProgramStatusReportsEarlyException(t *testing.T) {
	xdebugWarnOff(t)
	t.Setenv("COMPOSER_TERMINAL_STATUS", "7501")
	application := commandtest.NewApplication()
	output := statusBufferedOutput()
	if _, err := application.DoRun(arrayInput(t, console.P("--working-dir", "/does/not/exist")), output); err == nil || !strings.Contains(err.Error(), "Invalid working directory") {
		t.Fatalf("doRun = %v", err)
	}
	if want := "\033]7501;state=error:app=composer\033\\"; !strings.HasSuffix(output.Fetch(), want) {
		t.Errorf("wrote %q, want it to end with %q", output.Fetch(), want)
	}
	if got, _ := util.GetEnv("COMPOSER_TERMINAL_STATUS"); got != "7501" {
		t.Errorf("setting = %q", got)
	}
}

// TestApplication_MissingComposerFileReportsErrorBeforeExiting ports the
// status getComposer writes before it exits. Exit is captured instead of
// ending the test process.
func TestApplication_MissingComposerFileReportsErrorBeforeExiting(t *testing.T) {
	commandtest.InitTempDir(t)
	t.Setenv("COMPOSER_TERMINAL_STATUS", "7501,9;4")
	output := statusBufferedOutput()
	cio := io.NewConsoleIO(arrayInput(t), output, console.NewHelperSet(console.NewQuestionHelper()))
	application := commandtest.NewApplication()
	application.SetIO(cio)
	exitCode := 0
	application.Exit = func(code int) { exitCode = code }
	if _, err := application.GetComposer(true, nil, nil); err == nil || !strings.Contains(err.Error(), "Composer could not find a composer.json file") {
		t.Fatalf("getComposer = %v", err)
	}
	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1", exitCode)
	}
	if want := "\033]9;4;2\033\\\033]7501;state=error:app=composer\033\\"; !strings.HasSuffix(output.Fetch(), want) {
		t.Errorf("wrote %q, want it to end with %q", output.Fetch(), want)
	}
}
