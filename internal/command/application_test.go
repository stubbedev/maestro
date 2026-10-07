// Ports tests/Composer/Test/Console/ApplicationTest.php and
// tests/Composer/Test/Command/AboutCommandTest.php.

package command_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
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

func TestAboutCommand_About(t *testing.T) {
	composerVersion := composer.GetVersion()
	appTester := commandtest.GetApplicationTester(t)
	if code, err := appTester.RunArgs(commandtest.Options{}, "command", "about"); err != nil || code != 0 {
		t.Fatalf("run: %d %v", code, err)
	}
	for _, want := range []string{
		"Composer - Dependency Manager for PHP - version " + composerVersion,
		"Composer is a dependency manager tracking local dependencies of your projects and libraries.",
		"See https://getcomposer.org/ for more information.",
	} {
		if !strings.Contains(appTester.Display(false), want) {
			t.Errorf("display %q lacks %q", appTester.Display(false), want)
		}
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
