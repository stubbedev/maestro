// Ports Tests/Command/CommandTest.php (symfony/console).
//
// Not ported: the setCode() closure-binding tests
// (testSetCodeBindToClosure, testSetCodeWithStaticClosure,
// testSetCodeWithStaticAnonymousFunction, testSetCodeWithNonClosureCallable:
// Go funcs have no $this), testRunWithProcessTitle's process title check
// (not applied, see Command.Run) and the #[AsCommand] attribute test
// (testCommandAttribute; testDefaultCommand is TestApplication_DefaultCommandName).

package console

import (
	"slices"
	"strings"
	"testing"
)

func TestCommand_Constructor(t *testing.T) {
	if got := NewCommand("foo:bar").Name(); got != "foo:bar" {
		t.Error(got)
	}
}

func TestCommand_CommandNameCannotBeEmpty(t *testing.T) {
	_, err := newTestApp().Add(NewCommand(""))
	expectError(t, err, KindLogic, `The command defined in "Symfony\Component\Console\Command\Command" cannot have an empty name.`)
}

func TestCommand_SetApplication(t *testing.T) {
	app := newTestApp()
	c := newTestCommand()
	c.SetApplication(app)
	if c.Application() != app || c.HelperSet() != app.HelperSet() {
		t.Error("SetApplication()")
	}
}

func TestCommand_SetApplicationNull(t *testing.T) {
	c := newTestCommand()
	c.SetApplication(nil)
	if c.HelperSet() != nil {
		t.Error("HelperSet() is nil without application")
	}
}

func TestCommand_SetGetDefinition(t *testing.T) {
	c := newTestCommand()
	definition := &InputDefinition{}
	if ret := c.SetDefinition(definition); ret != c.Command {
		t.Error("SetDefinition() is fluent")
	}
	if c.Definition() != definition {
		t.Error("SetDefinition() sets the definition")
	}
	c.SetDefinitionItems(MustArgument("foo", 0, "", nil), MustOption("bar", "", 0, "", nil))
	if !c.Definition().HasArgument("foo") || !c.Definition().HasOption("bar") {
		t.Error("SetDefinitionItems()")
	}
}

func TestCommand_AddArgument(t *testing.T) {
	c := newTestCommand()
	if ret := c.AddArgument("foo", 0, "", nil); ret != c.Command {
		t.Error("AddArgument() is fluent")
	}
	if !c.Definition().HasArgument("foo") {
		t.Error("AddArgument()")
	}
}

func TestCommand_AddOption(t *testing.T) {
	c := newTestCommand()
	if ret := c.AddOption("foo", "", 0, "", nil); ret != c.Command {
		t.Error("AddOption() is fluent")
	}
	if !c.Definition().HasOption("foo") {
		t.Error("AddOption()")
	}
}

func TestCommand_SetHidden(t *testing.T) {
	c := newTestCommand()
	c.SetHidden(true)
	if !c.IsHidden() {
		t.Error("SetHidden()")
	}
}

func TestCommand_GetNamespaceGetNameSetName(t *testing.T) {
	c := newTestCommand()
	if c.Name() != "namespace:name" {
		t.Error(c.Name())
	}
	c.SetName("foo")
	if c.Name() != "foo" {
		t.Error(c.Name())
	}
	if ret := c.SetName("foobar:bar"); ret != c.Command || c.Name() != "foobar:bar" {
		t.Error(c.Name())
	}
}

func TestCommand_InvalidCommandNames(t *testing.T) {
	for _, name := range []string{"", "foo:", ":foo", "foo::bar"} {
		msg := expectPanic(t, func() { newTestCommand().SetName(name) })
		if want := `Command name "` + name + `" is invalid.`; msg != want {
			t.Errorf("SetName(%q) panics with %q", name, msg)
		}
	}
	for _, name := range []string{"foo", "foo:bar", "a:b:c", "foo\n", "foo:\n"} {
		newTestCommand().SetName(name)
	}
}

func TestCommand_GetSetDescription(t *testing.T) {
	c := newTestCommand()
	if c.Description() != "description" {
		t.Error(c.Description())
	}
	if ret := c.SetDescription("description1"); ret != c.Command || c.Description() != "description1" {
		t.Error(c.Description())
	}
}

func TestCommand_GetSetHelp(t *testing.T) {
	c := newTestCommand()
	if c.Help() != "help" {
		t.Error(c.Help())
	}
	if ret := c.SetHelp("help1"); ret != c.Command || c.Help() != "help1" {
		t.Error(c.Help())
	}
	c.SetHelp("")
	if c.Help() != "" {
		t.Error("Help() does not fall back to the description")
	}
}

func TestCommand_GetProcessedHelp(t *testing.T) {
	c := newTestCommand()
	c.SetHelp("The %command.name% command does... Example: %command.full_name%.")
	if h := c.ProcessedHelp(); !strings.Contains(h, "The namespace:name command does...") || strings.Contains(h, "%command.full_name%") {
		t.Error(h)
	}
	if want := "Example: " + phpSelf() + " namespace:name."; !strings.Contains(c.ProcessedHelp(), want) {
		t.Errorf("%q lacks %q", c.ProcessedHelp(), want)
	}

	c = newTestCommand()
	c.SetHelp("")
	if !strings.Contains(c.ProcessedHelp(), "description") {
		t.Error("ProcessedHelp() falls back to the description")
	}

	c = newTestCommand()
	c.SetHelp("The %command.name% command does... Example: %command.full_name%.")
	app := newTestApp()
	mustAdd(t, app, c)
	if err := app.SetDefaultCommand("namespace:name", true); err != nil {
		t.Fatal(err)
	}
	if h := c.ProcessedHelp(); !strings.Contains(h, "The namespace:name command does...") || strings.Contains(h, "%command.full_name%") {
		t.Error(h)
	}
	if want := "Example: " + phpSelf() + "."; !strings.Contains(c.ProcessedHelp(), want) {
		t.Errorf("single command: %q lacks %q", c.ProcessedHelp(), want)
	}
}

func TestCommand_GetSetAliases(t *testing.T) {
	c := newTestCommand()
	if !slices.Equal(c.Aliases(), []string{"name"}) {
		t.Error(c.Aliases())
	}
	if ret := c.SetAliases("name1"); ret != c.Command || !slices.Equal(c.Aliases(), []string{"name1"}) {
		t.Error(c.Aliases())
	}
	msg := expectPanic(t, func() { c.SetAliases("ok", "bad:") })
	if msg != `Command name "bad:" is invalid.` {
		t.Error(msg)
	}
}

func TestCommand_GetSynopsis(t *testing.T) {
	c := newTestCommand()
	c.AddOption("foo", "", 0, "", nil)
	c.AddArgument("bar", 0, "", nil)
	c.AddArgument("info", 0, "", nil)
	if got := c.Synopsis(false); got != "namespace:name [--foo] [--] [<bar> [<info>]]" {
		t.Error(got)
	}
	if got := c.Synopsis(true); got != "namespace:name [options] [--] [<bar> [<info>]]" {
		t.Error(got)
	}
}

func TestCommand_AddGetUsages(t *testing.T) {
	c := newTestCommand()
	c.AddUsage("foo1")
	c.AddUsage("foo2")
	c.AddUsage("namespace:name foo3")
	if want := []string{"namespace:name foo1", "namespace:name foo2", "namespace:name foo3"}; !slices.Equal(c.Usages(), want) {
		t.Error(c.Usages())
	}
}

func TestCommand_GetHelper(t *testing.T) {
	c := newTestCommand()
	c.SetApplication(newTestApp())
	h, err := c.Helper("formatter")
	if err != nil || h.Name() != (&FormatterHelper{}).Name() {
		t.Errorf("Helper(formatter) = %v, %v", h, err)
	}
}

func TestCommand_GetHelperWithoutHelperSet(t *testing.T) {
	_, err := newTestCommand().Helper("formatter")
	expectError(t, err, KindLogic, `Cannot retrieve helper "formatter" because there is no HelperSet defined.`)
}

func TestCommand_MergeApplicationDefinition(t *testing.T) {
	app := newTestApp()
	_ = app.Definition().AddArguments(MustArgument("foo", 0, "", nil))
	_ = app.Definition().AddOptions(MustOption("bar", "", 0, "", nil))
	c := newTestCommand()
	c.SetApplication(app)
	c.SetDefinition(MustDefinition(MustArgument("bar", 0, "", nil), MustOption("foo", "", 0, "", nil)))

	if err := c.MergeApplicationDefinition(true); err != nil {
		t.Fatal(err)
	}
	d := c.Definition()
	if !d.HasArgument("foo") || !d.HasArgument("bar") || !d.HasOption("foo") || !d.HasOption("bar") {
		t.Error("MergeApplicationDefinition() merges arguments and options")
	}

	if err := c.MergeApplicationDefinition(true); err != nil {
		t.Fatal(err)
	}
	if got := c.Definition().ArgumentCount(); got != 3 {
		t.Errorf("ArgumentCount() = %d", got)
	}
}

func TestCommand_MergeApplicationDefinitionWithoutArgsThenWithArgsAddsArgs(t *testing.T) {
	app := newTestApp()
	_ = app.Definition().AddArguments(MustArgument("foo", 0, "", nil))
	_ = app.Definition().AddOptions(MustOption("bar", "", 0, "", nil))
	c := newTestCommand()
	c.SetApplication(app)
	c.SetDefinition(MustDefinition())

	_ = c.MergeApplicationDefinition(false)
	if !c.Definition().HasOption("bar") || c.Definition().HasArgument("foo") {
		t.Error("MergeApplicationDefinition(false)")
	}

	_ = c.MergeApplicationDefinition(true)
	if !c.Definition().HasArgument("foo") {
		t.Error("MergeApplicationDefinition(true)")
	}

	_ = c.MergeApplicationDefinition(true)
	if got := c.Definition().ArgumentCount(); got != 2 {
		t.Errorf("ArgumentCount() = %d", got)
	}
}

func TestCommand_RunInteractive(t *testing.T) {
	tester := newCommandTester(newTestCommand())
	if _, err := tester.Execute(nil, testerOptions{interactive: new(true)}); err != nil {
		t.Fatal(err)
	}
	if tester.Display() != "interact called\nexecute called\n" {
		t.Errorf("display = %q", tester.Display())
	}
}

func TestCommand_RunNonInteractive(t *testing.T) {
	tester := newCommandTester(newTestCommand())
	if _, err := tester.Execute(nil, testerOptions{interactive: new(false)}); err != nil {
		t.Fatal(err)
	}
	if tester.Display() != "execute called\n" {
		t.Errorf("display = %q", tester.Display())
	}
}

func TestCommand_ExecuteMethodNeedsToBeOverridden(t *testing.T) {
	in, _ := NewStringInput("")
	_, err := NewCommand("foo").Run(in, NewNullOutput())
	expectError(t, err, KindLogic, "You must override the execute() method in the concrete command class.")
}

func TestCommand_RunWithInvalidOption(t *testing.T) {
	tester := newCommandTester(newTestCommand())
	_, err := tester.Execute([]Param{P("--bar", true)}, testerOptions{})
	expectError(t, err, KindInvalidOption, `The "--bar" option does not exist.`)
}

func TestCommand_RunWithApplication(t *testing.T) {
	c := newTestCommand()
	c.SetApplication(newTestApp())
	in, _ := NewStringInput("")
	if code, err := c.Run(in, NewNullOutput()); code != 0 || err != nil {
		t.Errorf("Run() = %d, %v", code, err)
	}
}

func TestCommand_RunReturnsAlwaysInteger(t *testing.T) {
	in, _ := NewStringInput("")
	if code, err := newTestCommand().Run(in, NewNullOutput()); code != 0 || err != nil {
		t.Errorf("Run() = %d, %v", code, err)
	}
}

func TestCommand_RunWithProcessTitle(t *testing.T) {
	c := newTestCommand()
	c.SetApplication(newTestApp())
	c.SetProcessTitle("foo")
	in, _ := NewStringInput("")
	if code, err := c.Run(in, NewNullOutput()); code != 0 || err != nil {
		t.Errorf("Run() = %d, %v", code, err)
	}
}

func TestCommand_SetCode(t *testing.T) {
	c := newTestCommand()
	ret := c.SetCode(func(_ Input, out Output) (int, error) {
		out.Writeln("from the code...")

		return 0, nil
	})
	if ret != c.Command {
		t.Error("SetCode() is fluent")
	}
	tester := newCommandTester(c)
	if _, err := tester.Execute(nil, testerOptions{}); err != nil {
		t.Fatal(err)
	}
	if tester.Display() != "interact called\nfrom the code...\n" {
		t.Errorf("display = %q", tester.Display())
	}
}

// The command argument is filled in when Run is called directly.
func TestCommand_RunSetsCommandArgument(t *testing.T) {
	c := newTestCommand()
	var got any
	c.SetCode(func(in Input, _ Output) (int, error) {
		got = in.Argument("command")

		return 0, nil
	})
	c.SetApplication(newTestApp())
	in, _ := NewStringInput("")
	if _, err := c.Run(in, NewNullOutput()); err != nil {
		t.Fatal(err)
	}
	if got != "namespace:name" {
		t.Errorf("command argument = %v", got)
	}
}
