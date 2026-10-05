// Ports Tests/Command/HelpCommandTest.php and ListCommandTest.php
// (symfony/console).
//
// Not ported: Tests/Command/SingleCommandApplicationTest.php
// (SingleCommandApplication is not part of the port; Composer only checks
// whether a script command extends it).

package console

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

func getCommand(t *testing.T, app *Application, name string) Commander {
	t.Helper()
	c, err := app.Get(name)
	if err != nil {
		t.Fatal(err)
	}

	return c
}

func assertContainsAll(t *testing.T, display string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(display, w) {
			t.Errorf("display lacks %q:\n%s", w, display)
		}
	}
}

func TestHelpCommand_ExecuteForCommandAlias(t *testing.T) {
	c := NewHelpCommand()
	c.SetApplication(newTestApp())
	tester := newCommandTester(c)
	if _, err := tester.Execute([]Param{P("command_name", "li")}, testerOptions{decorated: new(false)}); err != nil {
		t.Fatal(err)
	}
	assertContainsAll(t, tester.Display(), "list [options] [--] [<namespace>]", "format=FORMAT", "raw")
}

func TestHelpCommand_ExecuteForCommand(t *testing.T) {
	c := NewHelpCommand()
	tester := newCommandTester(c)
	c.SetCommand(NewListCommand())
	if _, err := tester.Execute(nil, testerOptions{decorated: new(false)}); err != nil {
		t.Fatal(err)
	}
	assertContainsAll(t, tester.Display(), "list [options] [--] [<namespace>]", "format=FORMAT", "raw")
}

func TestHelpCommand_ExecuteForCommandWithXmlOption(t *testing.T) {
	c := NewHelpCommand()
	tester := newCommandTester(c)
	c.SetCommand(NewListCommand())
	if _, err := tester.Execute([]Param{P("--format", "xml")}, testerOptions{}); err != nil {
		t.Fatal(err)
	}
	assertContainsAll(t, tester.Display(), "<command")
}

func TestHelpCommand_ExecuteForApplicationCommand(t *testing.T) {
	tester := newCommandTester(getCommand(t, newTestApp(), "help"))
	if _, err := tester.Execute([]Param{P("command_name", "list")}, testerOptions{}); err != nil {
		t.Fatal(err)
	}
	assertContainsAll(t, tester.Display(), "list [options] [--] [<namespace>]", "format=FORMAT", "raw")
}

func TestHelpCommand_ExecuteForApplicationCommandWithXmlOption(t *testing.T) {
	tester := newCommandTester(getCommand(t, newTestApp(), "help"))
	if _, err := tester.Execute([]Param{P("command_name", "list"), P("--format", "xml")}, testerOptions{}); err != nil {
		t.Fatal(err)
	}
	assertContainsAll(t, tester.Display(), "list [--raw] [--format FORMAT] [--short] [--] [&lt;namespace&gt;]", "<command")
}

func TestHelpCommand_ExecuteForUnknownCommand(t *testing.T) {
	tester := newCommandTester(getCommand(t, newTestApp(), "help"))
	_, err := tester.Execute([]Param{P("command_name", "nope")}, testerOptions{})
	expectError(t, err, KindCommandNotFound, `Command "nope" is not defined.`)
}

func TestHelpCommand_Complete(t *testing.T) {
	for _, c := range []struct {
		input, want []string
	}{
		{[]string{"--format", ""}, []string{"txt", "xml", "json", "md"}},
		{[]string{""}, []string{"completion", "help", "list", "foo:bar"}},
		{[]string{"f"}, []string{"completion", "help", "list", "foo:bar"}},
	} {
		app := newTestApp()
		mustAdd(t, app, newFooCommand())
		if got := (commandCompletionTester{getCommand(t, app, "help")}).complete(c.input); !slices.Equal(got, c.want) {
			t.Errorf("complete(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestListCommand_ExecuteListsCommands(t *testing.T) {
	tester := newCommandTester(getCommand(t, newTestApp(), "list"))
	if _, err := tester.Execute([]Param{P("command", "list")}, testerOptions{decorated: new(false)}); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`help\s{2,}Display help for a command`).MatchString(tester.Display()) {
		t.Error(tester.Display())
	}
}

func TestListCommand_ExecuteListsCommandsWithXmlOption(t *testing.T) {
	tester := newCommandTester(getCommand(t, newTestApp(), "list"))
	if _, err := tester.Execute([]Param{P("command", "list"), P("--format", "xml")}, testerOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tester.Display(), `<command id="list" name="list" hidden="0">`) {
		t.Error(tester.Display())
	}
}

func TestListCommand_ExecuteListsCommandsWithRawOption(t *testing.T) {
	tester := newCommandTester(getCommand(t, newTestApp(), "list"))
	if _, err := tester.Execute([]Param{P("command", "list"), P("--raw", true)}, testerOptions{}); err != nil {
		t.Fatal(err)
	}
	want := "completion   Dump the shell completion script\nhelp         Display help for a command\nlist         List commands\n"
	if tester.Display() != want {
		t.Errorf("display = %q", tester.Display())
	}
}

func TestListCommand_ExecuteListsCommandsWithNamespaceArgument(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newFooCommand())
	tester := newCommandTester(getCommand(t, app, "list"))
	if _, err := tester.Execute([]Param{P("command", "list"), P("namespace", "foo"), P("--raw", true)}, testerOptions{}); err != nil {
		t.Fatal(err)
	}
	if tester.Display() != "foo:bar   The foo:bar command\n" {
		t.Errorf("display = %q", tester.Display())
	}
}

func TestListCommand_ExecuteListsCommandsOrder(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newFoo6Command())
	tester := newCommandTester(getCommand(t, app, "list"))
	if _, err := tester.Execute([]Param{P("command", "list")}, testerOptions{decorated: new(false)}); err != nil {
		t.Fatal(err)
	}
	want := `Console Tool

Usage:
  command [options] [arguments]

Options:
  -h, --help            Display help for the given command. When no command is given display help for the list command
  -q, --quiet           Do not output any message
  -V, --version         Display this application version
      --ansi|--no-ansi  Force (or disable --no-ansi) ANSI output
  -n, --no-interaction  Do not ask any interactive question
  -v|vv|vvv, --verbose  Increase the verbosity of messages: 1 for normal output, 2 for more verbose output and 3 for debug

Available commands:
  completion  Dump the shell completion script
  help        Display help for a command
  list        List commands
 0foo
  0foo:bar    0foo:bar command`
	if got := strings.TrimSpace(tester.Display()); got != want {
		t.Errorf("display:\n%s", got)
	}
}

func TestListCommand_ExecuteListsCommandsOrderRaw(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newFoo6Command())
	tester := newCommandTester(getCommand(t, app, "list"))
	if _, err := tester.Execute([]Param{P("command", "list"), P("--raw", true)}, testerOptions{}); err != nil {
		t.Fatal(err)
	}
	want := "completion   Dump the shell completion script\nhelp         Display help for a command\nlist         List commands\n0foo:bar     0foo:bar command"
	if got := strings.TrimSpace(tester.Display()); got != want {
		t.Errorf("display = %q", got)
	}
}

func TestListCommand_Complete(t *testing.T) {
	for _, c := range []struct {
		input, want []string
	}{
		{[]string{"--format", ""}, []string{"txt", "xml", "json", "md"}},
		{[]string{""}, []string{"_global", "foo"}},
		{[]string{"f"}, []string{"_global", "foo"}},
	} {
		app := newTestApp()
		mustAdd(t, app, newFooCommand())
		if got := (commandCompletionTester{getCommand(t, app, "list")}).complete(c.input); !slices.Equal(got, c.want) {
			t.Errorf("complete(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestListCommand_ExecuteWithEmptyFormat(t *testing.T) {
	app := NewApplication("UNKNOWN", "UNKNOWN")
	tester := newCommandTester(getCommand(t, app, "list"))
	_, err := tester.Execute([]Param{P("--format", "")}, testerOptions{})
	if err == nil || err.Error() != `Unsupported format "".` {
		t.Fatalf("got %v", err)
	}
}
