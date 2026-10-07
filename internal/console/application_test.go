// Ports Tests/ApplicationTest.php (symfony/console).
//
// Not ported (features outside the ported subset): the event dispatcher
// tests (testRunWithDispatcher, testRunWithExceptionAndDispatcher,
// testRunDispatchesAllEventsWithException*, testRunAllowsErrorListeners*,
// testConsoleErrorEventIsTriggeredOnCommandNotFound,
// testErrorIsRethrownIfNotHandledByConsoleErrorEvent*,
// testRunWithErrorAndDispatcher, testRunDispatchesAllEventsWithError,
// testRunWithErrorFailingStatusCode, testRunWithDispatcherSkippingCommand,
// testRunWithDispatcherAccessingInputOptions,
// testRunWithDispatcherAddingInputOptions, testThrowingErrorListener,
// testRunDispatchesIntegerExitCode,
// testRunDispatchesExitCodeOneForExceptionCodeZero), command loaders and
// LazyCommand (testAllWithCommandLoader, testHasGetWithCommandLoader,
// testFindWithCommandLoader, testFindAlternativeCommandsWithAnAlias,
// testRunLazyCommandService, testGetDisabledLazyCommand,
// testHasReturnsFalseForDisabledLazyCommand,
// testAllExcludesDisabledLazyCommand,
// testFindAlternativesDoesNotLoadSameNamespaceCommandsOnExactMatch,
// testCommandNameMismatchWithCommandLoaderKeyThrows), signals
// (testSignal*, testSetSignalsToDispatchEvent, testSignalable*) and PHP
// anonymous classes (testRenderAnonymousException,
// testRenderExceptionStackTraceContainsRootException).
//
// How errors are rendered is maestro's own (internal/ui): the
// renderException tests check the new rendering, with the information
// Symfony's fixtures show, and those about Symfony's box layout
// (testRenderExceptionEscapesLines, the double-width box padding) are
// gone.

package console

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
)

func newTestApp() *Application { return NewApplication("UNKNOWN", "UNKNOWN") }

func mustAdd(t *testing.T, app *Application, cmds ...Commander) {
	t.Helper()
	for _, c := range cmds {
		if _, err := app.Add(c); err != nil {
			t.Fatal(err)
		}
	}
}

func register(t *testing.T, app *Application, name string) *Command {
	t.Helper()
	c, err := app.Register(name)
	if err != nil {
		t.Fatal(err)
	}

	return c.Base()
}

func mustFind(t *testing.T, app *Application, name string) Commander {
	t.Helper()
	c, err := app.Find(name)
	if err != nil {
		t.Fatalf("Find(%q): %v", name, err)
	}

	return c
}

// expectError checks err is a console *Error of kind with the message
// (prefix when contains is false, substring when true).
func expectError(t *testing.T, err error, kind Kind, message string) *Error {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want %s error %q, got %v", kindClass[kind], message, err)
	}
	if !kindIsA(e.Kind, kind) {
		t.Errorf("want %s, got %s (%q)", kindClass[kind], kindClass[e.Kind], e.Message)
	}
	if !strings.Contains(e.Message, message) {
		t.Errorf("want message containing %q, got %q", message, e.Message)
	}

	return e
}

// kindIsA is instanceof for the exception classes.
func kindIsA(k, want Kind) bool {
	switch want {
	case KindCommandNotFound:
		return k == KindCommandNotFound || k == KindNamespaceNotFound
	case KindInvalidArgument:
		return k == KindInvalidArgument || k == KindInvalidOption || k == KindCommandNotFound || k == KindNamespaceNotFound
	case KindRuntime:
		return k == KindRuntime || k == KindMissingInput
	}

	return k == want
}

// expectPanic runs fn and returns the message of the *Error it panics with.
func expectPanic(t *testing.T, fn func()) (msg string) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic")
		}
		err, ok := r.(error)
		if !ok {
			t.Fatalf("panic with %v", r)
		}
		msg = err.Error()
	}()
	fn()

	return ""
}

func names(cmds []NamedCommand) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Name
	}

	return out
}

func setColumns(t *testing.T, columns string) {
	t.Helper()
	t.Setenv("COLUMNS", columns)
	t.Setenv("LINES", "50")
	t.Setenv("SHELL_VERBOSITY", "")
	os.Unsetenv("SHELL_VERBOSITY")
}

func TestApplication_Constructor(t *testing.T) {
	app := NewApplication("foo", "bar")
	if app.Name() != "foo" || app.Version() != "bar" {
		t.Errorf("name/version = %q/%q", app.Name(), app.Version())
	}
	if got := names(app.All("")); !slices.Equal(got, []string{"help", "list", "_complete", "completion"}) {
		t.Errorf("All() = %v", got)
	}
}

func TestApplication_SetGetName(t *testing.T) {
	app := newTestApp()
	app.SetName("foo")
	if app.Name() != "foo" {
		t.Error(app.Name())
	}
}

func TestApplication_SetGetVersion(t *testing.T) {
	app := newTestApp()
	app.SetVersion("bar")
	if app.Version() != "bar" {
		t.Error(app.Version())
	}
}

func TestApplication_GetLongVersion(t *testing.T) {
	if got := NewApplication("foo", "bar").LongVersion(); got != "foo <info>bar</info>" {
		t.Error(got)
	}
	if got := NewApplication("foo", "UNKNOWN").LongVersion(); got != "foo" {
		t.Error(got)
	}
}

func TestApplication_Help(t *testing.T) {
	assertEqualsFile(t, "application_gethelp.txt", newTestApp().Help())
}

func TestApplication_All(t *testing.T) {
	app := newTestApp()
	cmds := app.All("")
	if _, ok := cmds[0].Command.(*HelpCommand); !ok || cmds[0].Name != "help" {
		t.Errorf("All()[0] = %+v", cmds[0])
	}
	mustAdd(t, app, newFooCommand())
	if got := app.All("foo"); len(got) != 1 {
		t.Errorf("All(foo) = %v", names(got))
	}
}

func TestApplication_Register(t *testing.T) {
	if c := register(t, newTestApp(), "foo"); c.Name() != "foo" {
		t.Error(c.Name())
	}
}

func TestApplication_RegisterAmbiguous(t *testing.T) {
	code := func(_ Input, out Output) (int, error) {
		out.Writeln("It works!")

		return 0, nil
	}
	app := newTestApp()
	app.SetAutoExit(false)
	register(t, app, "test-foo").SetAliases("test").SetCode(code)
	register(t, app, "test-bar").SetCode(code)

	tester := newApplicationTester(app)
	tester.Run([]Param{PI(0, "test")}, testerOptions{})
	if !strings.Contains(tester.Display(), "It works!") {
		t.Error(tester.Display())
	}
}

func TestApplication_Add(t *testing.T) {
	app := newTestApp()
	foo := newFooCommand()
	mustAdd(t, app, foo)
	if c, _ := app.Get("foo:bar"); c != foo {
		t.Error("Add() did not register the command")
	}

	app = newTestApp()
	foo, foo1 := newFooCommand(), newFoo1Command()
	if err := app.AddCommands(foo, foo1); err != nil {
		t.Fatal(err)
	}
	c1, _ := app.Get("foo:bar")
	c2, _ := app.Get("foo:bar1")
	if c1 != foo || c2 != foo1 {
		t.Error("AddCommands() did not register the commands")
	}
}

func TestApplication_AddCommandWithEmptyConstructor(t *testing.T) {
	_, err := newTestApp().Add(&Foo5Command{})
	expectError(t, err, KindLogic, `Command class "Foo5Command" is not correctly initialized. You probably forgot to call the parent constructor.`)
}

func TestApplication_AddDisabled(t *testing.T) {
	app := newTestApp()
	c := &DisabledCommand{NewCommand("disabled")}
	got, err := app.Add(c)
	if err != nil || got != nil {
		t.Fatalf("Add(disabled) = %v, %v", got, err)
	}
	if app.Has("disabled") || c.Application() != nil {
		t.Error("disabled command was registered")
	}
}

func TestApplication_HasGet(t *testing.T) {
	app := newTestApp()
	if !app.Has("list") || app.Has("afoobar") {
		t.Error("Has()")
	}
	foo := newFooCommand()
	mustAdd(t, app, foo)
	if !app.Has("afoobar") {
		t.Error("Has(alias)")
	}
	if c, _ := app.Get("foo:bar"); c != foo {
		t.Error("Get(name)")
	}
	if c, _ := app.Get("afoobar"); c != foo {
		t.Error("Get(alias)")
	}

	app = newTestApp()
	mustAdd(t, app, newFooCommand())
	// simulate --help
	app.wantHelps = true
	c, err := app.Get("foo:bar")
	if _, ok := c.(*HelpCommand); err != nil || !ok {
		t.Errorf("Get() with --help = %T, %v", c, err)
	}
}

func TestApplication_SilentHelp(t *testing.T) {
	app := newTestApp()
	app.SetAutoExit(false)
	app.SetCatchExceptions(false)

	tester := newApplicationTester(app)
	if _, err := tester.Run([]Param{P("-h", true), P("-q", true)}, testerOptions{decorated: new(false)}); err != nil {
		t.Fatal(err)
	}
	if tester.Display() != "" {
		t.Errorf("display = %q", tester.Display())
	}
}

func TestApplication_GetInvalidCommand(t *testing.T) {
	_, err := newTestApp().Get("foofoo")
	expectError(t, err, KindCommandNotFound, `The command "foofoo" does not exist.`)
}

func TestApplication_GetNamespaces(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newFooCommand(), newFoo1Command())
	if got := app.Namespaces(); !slices.Equal(got, []string{"foo"}) {
		t.Error(got)
	}
}

func TestApplication_FindNamespace(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newFooCommand())
	for _, ns := range []string{"foo", "f"} {
		if got, err := app.FindNamespace(ns); err != nil || got != "foo" {
			t.Errorf("FindNamespace(%q) = %q, %v", ns, got, err)
		}
	}
	mustAdd(t, app, newFoo2Command())
	if got, err := app.FindNamespace("foo"); err != nil || got != "foo" {
		t.Errorf("FindNamespace(foo) = %q, %v", got, err)
	}
}

func TestApplication_FindNamespaceWithSubnamespaces(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newFooSubnamespaced1Command(), newFooSubnamespaced2Command())
	if got, err := app.FindNamespace("foo"); err != nil || got != "foo" {
		t.Errorf("FindNamespace(foo) = %q, %v", got, err)
	}
}

func TestApplication_FindAmbiguousNamespace(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newBarBucCommand(), newFooCommand(), newFoo2Command())
	_, err := app.FindNamespace("f")
	expectError(t, err, KindNamespaceNotFound, "The namespace \"f\" is ambiguous.\nDid you mean one of these?\n    foo\n    foo1")
}

func TestApplication_FindNonAmbiguous(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newTestAmbiguousCommandRegistering(), newTestAmbiguousCommandRegistering2())
	if got := mustFind(t, app, "test").Base().Name(); got != "test-ambiguous" {
		t.Error(got)
	}
}

func TestApplication_FindInvalidNamespace(t *testing.T) {
	_, err := newTestApp().FindNamespace("bar")
	expectError(t, err, KindNamespaceNotFound, `There are no commands defined in the "bar" namespace.`)
}

func TestApplication_FindUniqueNameButNamespaceName(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newFooCommand(), newFoo1Command(), newFoo2Command())
	_, err := app.Find("foo1")
	expectError(t, err, KindCommandNotFound, `Command "foo1" is not defined`)
}

func TestApplication_Find(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newFooCommand())
	for name, want := range map[string]string{"foo:bar": "*console.FooCommand", "h": "*console.HelpCommand", "f:bar": "*console.FooCommand", "f:b": "*console.FooCommand", "a": "*console.FooCommand"} {
		if got := typeName(mustFind(t, app, name)); got != want {
			t.Errorf("Find(%q) = %s, want %s", name, got, want)
		}
	}
}

func typeName(v any) string {
	switch v.(type) {
	case *FooCommand:
		return "*console.FooCommand"
	case *HelpCommand:
		return "*console.HelpCommand"
	}

	return "other"
}

func TestApplication_FindCaseSensitiveFirst(t *testing.T) {
	app := newTestApp()
	upper, lower := newFooSameCaseUppercaseCommand(), newFooSameCaseLowercaseCommand()
	mustAdd(t, app, upper, lower)
	for name, want := range map[string]Commander{"f:B": upper, "f:BAR": upper, "f:b": lower, "f:bar": lower} {
		if got := mustFind(t, app, name); got != want {
			t.Errorf("Find(%q) = %s", name, got.Base().Name())
		}
	}
}

func TestApplication_FindCaseInsensitiveAsFallback(t *testing.T) {
	app := newTestApp()
	lower := newFooSameCaseLowercaseCommand()
	mustAdd(t, app, lower)
	for _, name := range []string{"f:b", "f:B", "FoO:BaR"} {
		if got := mustFind(t, app, name); got != lower {
			t.Errorf("Find(%q) = %s", name, got.Base().Name())
		}
	}
}

func TestApplication_FindCaseInsensitiveSuggestions(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newFooSameCaseLowercaseCommand(), newFooSameCaseUppercaseCommand())
	_, err := app.Find("FoO:BaR")
	expectError(t, err, KindCommandNotFound, `Command "FoO:BaR" is ambiguous`)
}

func TestApplication_FindWithAmbiguousAbbreviations(t *testing.T) {
	setColumns(t, "120")
	for _, c := range []struct{ abbreviation, message string }{
		{"f", `Command "f" is not defined.`},
		{"a", "Command \"a\" is ambiguous.\nDid you mean one of these?\n" +
			"    afoobar  The foo:bar command\n" +
			"    afoobar1 The foo:bar1 command\n" +
			"    afoobar2 The foo1:bar command"},
		{"foo:b", "Command \"foo:b\" is ambiguous.\nDid you mean one of these?\n" +
			"    foo:bar  The foo:bar command\n" +
			"    foo:bar1 The foo:bar1 command\n" +
			"    foo1:bar The foo1:bar command"},
	} {
		app := newTestApp()
		mustAdd(t, app, newFooCommand(), newFoo1Command(), newFoo2Command())
		_, err := app.Find(c.abbreviation)
		expectError(t, err, KindCommandNotFound, c.message)
	}
}

func TestApplication_FindWithAmbiguousAbbreviationsFindsCommandIfAlternativesAreHidden(t *testing.T) {
	app := newTestApp()
	foo := newFooCommand()
	mustAdd(t, app, foo, newFooHiddenCommand())
	if mustFind(t, app, "foo:") != foo {
		t.Error("Find(foo:)")
	}
}

func TestApplication_FindCommandEqualNamespace(t *testing.T) {
	app := newTestApp()
	foo3, foo4 := newFoo3Command(), newFoo4Command()
	mustAdd(t, app, foo3, foo4)
	if mustFind(t, app, "foo3:bar") != foo3 || mustFind(t, app, "foo3:bar:toh") != foo4 {
		t.Error("Find()")
	}
}

func TestApplication_FindCommandWithAmbiguousNamespacesButUniqueName(t *testing.T) {
	app := newTestApp()
	foobar := newFoobarCommand()
	mustAdd(t, app, newFooCommand(), foobar)
	if mustFind(t, app, "f:f") != foobar {
		t.Error("Find(f:f)")
	}
}

func TestApplication_FindCommandWithMissingNamespace(t *testing.T) {
	app := newTestApp()
	foo4 := newFoo4Command()
	mustAdd(t, app, foo4)
	if mustFind(t, app, "f::t") != foo4 {
		t.Error("Find(f::t)")
	}
}

func TestApplication_FindAlternativeExceptionMessageSingle(t *testing.T) {
	for _, name := range []string{"foo3:barr", "fooo3:bar"} {
		app := newTestApp()
		mustAdd(t, app, newFoo3Command())
		_, err := app.Find(name)
		expectError(t, err, KindCommandNotFound, "Did you mean this")
	}
}

func TestApplication_DontRunAlternativeNamespaceName(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newFoo1Command())
	app.SetAutoExit(false)
	tester := newApplicationTester(app)
	tester.Run([]Param{P("command", "foos:bar1")}, testerOptions{decorated: new(false)})
	assertRendered(t, "\nError: There are no commands defined in the \"foos\" namespace.\n\n       Did you mean this?\n           foo\n", tester.Display())
}

func TestApplication_CanRunAlternativeCommandName(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newFooWithoutAliasCommand())
	app.SetAutoExit(false)
	tester := newApplicationTester(app)
	tester.SetInputs("y")
	tester.Run([]Param{P("command", "foos")}, testerOptions{decorated: new(false)})
	display := strings.TrimSpace(tester.Display())
	for _, want := range []string{`Command "foos" is not defined`, `Do you want to run "foo" instead?  (yes/no) [no]:`, "called"} {
		if !strings.Contains(display, want) {
			t.Errorf("display lacks %q:\n%s", want, display)
		}
	}
}

func TestApplication_DontRunAlternativeCommandName(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newFooWithoutAliasCommand())
	app.SetAutoExit(false)
	tester := newApplicationTester(app)
	tester.SetInputs("n")
	code, _ := tester.Run([]Param{P("command", "foos")}, testerOptions{decorated: new(false)})
	if code != 1 {
		t.Errorf("exit code %d", code)
	}
	display := strings.TrimSpace(tester.Display())
	for _, want := range []string{`Command "foos" is not defined`, `Do you want to run "foo" instead?  (yes/no) [no]:`} {
		if !strings.Contains(display, want) {
			t.Errorf("display lacks %q:\n%s", want, display)
		}
	}
}

func TestApplication_FindAlternativeExceptionMessageMultiple(t *testing.T) {
	setColumns(t, "120")
	app := newTestApp()
	mustAdd(t, app, newFooCommand(), newFoo1Command(), newFoo2Command())

	// Command + plural
	_, err := app.Find("foo:baR")
	e := expectError(t, err, KindCommandNotFound, "Did you mean one of these")
	for _, want := range []string{"foo1:bar", "foo:bar"} {
		if !strings.Contains(e.Message, want) {
			t.Errorf("message lacks %q: %q", want, e.Message)
		}
	}

	// Namespace + plural
	_, err = app.Find("foo2:bar")
	e = expectError(t, err, KindNamespaceNotFound, "Did you mean one of these")
	if !strings.Contains(e.Message, "foo1") {
		t.Error(e.Message)
	}

	mustAdd(t, app, newFoo3Command(), newFoo4Command())

	// Subnamespace + plural
	_, err = app.Find("foo3:")
	e = expectError(t, err, KindCommandNotFound, "foo3:bar")
	if !strings.Contains(e.Message, "foo3:bar:toh") {
		t.Error(e.Message)
	}
}

func TestApplication_FindAlternativeCommands(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newFooCommand(), newFoo1Command(), newFoo2Command())

	_, err := app.Find("Unknown command")
	e := expectError(t, err, KindCommandNotFound, `Command "Unknown command" is not defined.`)
	if e.Message != `Command "Unknown command" is not defined.` || len(e.Alternatives) != 0 {
		t.Errorf("%q %v", e.Message, e.Alternatives)
	}

	// Test if "bar1" command throw a "CommandNotFoundException" and does not contain
	// "foo:bar" as alternative because "bar1" is too far from "foo:bar"
	_, err = app.Find("bar1")
	e = expectError(t, err, KindCommandNotFound, `Command "bar1" is not defined.`)
	if !slices.Equal(e.Alternatives, []string{"afoobar1", "foo:bar1"}) {
		t.Error(e.Alternatives)
	}
	if strings.Contains(strings.ReplaceAll(e.Message, "foo:bar1", ""), "foo:bar") {
		t.Error(e.Message)
	}
}

func TestApplication_FindAlternativeNamespace(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newFooCommand(), newFoo1Command(), newFoo2Command(), newFoo3Command())

	_, err := app.Find("Unknown-namespace:Unknown-command")
	e := expectError(t, err, KindNamespaceNotFound, `There are no commands defined in the "Unknown-namespace" namespace.`)
	if len(e.Alternatives) != 0 || !phperr.InstanceOf(err, ClassCommandNotFound) {
		t.Error(e.Alternatives)
	}

	_, err = app.Find("foo2:command")
	e = expectError(t, err, KindNamespaceNotFound, `There are no commands defined in the "foo2" namespace.`)
	if !phperr.InstanceOf(err, ClassCommandNotFound) {
		t.Error("NamespaceNotFoundException extends CommandNotFoundException")
	}
	if len(e.Alternatives) != 3 || !slices.Contains(e.Alternatives, "foo") || !slices.Contains(e.Alternatives, "foo1") || !slices.Contains(e.Alternatives, "foo3") {
		t.Error(e.Alternatives)
	}
}

func TestApplication_FindAlternativesOutput(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newFooCommand(), newFoo1Command(), newFoo2Command(), newFoo3Command(), newFooHiddenCommand())
	_, err := app.Find("foo")
	e := expectError(t, err, KindCommandNotFound, `Command "foo" is not defined.`)
	if want := []string{"afoobar", "afoobar1", "afoobar2", "foo1:bar", "foo3:bar", "foo:bar", "foo:bar1"}; !slices.Equal(e.Alternatives, want) {
		t.Errorf("alternatives = %v", e.Alternatives)
	}
	if !strings.Contains(e.Message, "Did you mean one of these?") {
		t.Error(e.Message)
	}
}

func TestApplication_FindNamespaceDoesNotFailOnDeepSimilarNamespaces(t *testing.T) {
	// The PHP test mocks getNamespaces() to return ['foo:sublong', 'bar:sub'].
	app := newTestApp()
	register(t, app, "foo:sublong:x")
	register(t, app, "bar:sub:y")
	if got, err := app.FindNamespace("f:sub"); err != nil || got != "foo:sublong" {
		t.Errorf("FindNamespace(f:sub) = %q, %v", got, err)
	}
}

func TestApplication_FindWithDoubleColonInNameThrowsException(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newFooCommand(), newFoo4Command())
	_, err := app.Find("foo::bar")
	expectError(t, err, KindCommandNotFound, `Command "foo::bar" is not defined.`)
}

func TestApplication_FindHiddenWithExactName(t *testing.T) {
	app := newTestApp()
	hidden := newFooHiddenCommand()
	mustAdd(t, app, hidden)
	if mustFind(t, app, "foo:hidden") != hidden || mustFind(t, app, "afoohidden") != hidden {
		t.Error("hidden commands are found by exact name")
	}
}

func TestApplication_FindAmbiguousCommandsIfAllAlternativesAreHidden(t *testing.T) {
	app := newTestApp()
	foo := newFooCommand()
	mustAdd(t, app, foo, newFooHiddenCommand())
	if mustFind(t, app, "foo:") != foo {
		t.Error("Find(foo:)")
	}
}

// assertRendered checks an error's rendering by internal/ui (Symfony's
// exception boxes are not ported).
func assertRendered(t *testing.T, want, got string) {
	t.Helper()
	if got = php.NormalizeEOL(got); got != want {
		t.Errorf("rendered\n%s\nwant\n%s", got, want)
	}
}

func TestApplication_SetCatchExceptions(t *testing.T) {
	app := newTestApp()
	app.SetAutoExit(false)
	setColumns(t, "120")
	tester := newApplicationTester(app)

	app.SetCatchExceptions(true)
	if !app.AreExceptionsCaught() {
		t.Error("AreExceptionsCaught()")
	}

	const want = "\nError: Command \"foo\" is not defined.\n"
	tester.Run([]Param{P("command", "foo")}, testerOptions{decorated: new(false)})
	assertRendered(t, want, tester.Display())

	tester.Run([]Param{P("command", "foo")}, testerOptions{decorated: new(false), captureStderrSeparately: true})
	assertRendered(t, want, tester.ErrorOutput())
	if tester.Display() != "" {
		t.Error(tester.Display())
	}

	app.SetCatchExceptions(false)
	_, err := tester.Run([]Param{P("command", "foo")}, testerOptions{decorated: new(false)})
	if err == nil || err.Error() != `Command "foo" is not defined.` {
		t.Errorf("err = %v", err)
	}
}

func TestApplication_AutoExitSetting(t *testing.T) {
	app := newTestApp()
	if !app.IsAutoExitEnabled() {
		t.Error("auto exit is enabled by default")
	}
	app.SetAutoExit(false)
	if app.IsAutoExitEnabled() {
		t.Error("SetAutoExit(false)")
	}
}

// An uncaught error is its message, the messages of its previous errors,
// the command's usage when the input was wrong, and, at -v, the errors'
// types and the exit code; styled only when the output is decorated.
func TestApplication_RenderException(t *testing.T) {
	app := newTestApp()
	app.SetAutoExit(false)
	setColumns(t, "120")
	tester := newApplicationTester(app)

	tester.Run([]Param{P("command", "foo")}, testerOptions{decorated: new(false), captureStderrSeparately: true})
	assertRendered(t, "\nError: Command \"foo\" is not defined.\n", tester.ErrorOutput())

	tester.Run([]Param{P("command", "foo")}, testerOptions{decorated: new(false), verbosity: VerbosityVerbose, captureStderrSeparately: true})
	assertRendered(t, "\nError: Command \"foo\" is not defined.\n"+
		"  Debug: *console.Error [Symfony\\Component\\Console\\Exception\\CommandNotFoundException]\n"+
		"         exit code 1\n", tester.ErrorOutput())

	tester.Run([]Param{P("command", "list"), P("--foo", true)}, testerOptions{decorated: new(false), captureStderrSeparately: true})
	assertRendered(t, "\nError: The \"--foo\" option does not exist.\n"+
		"  Usage: list [--raw] [--format FORMAT] [--short] [--] [<namespace>]\n", tester.ErrorOutput())

	mustAdd(t, app, newFoo3Command())
	tester = newApplicationTester(app)
	// Messages are shown as they are: console tags in them are text.
	const chain = "\nError: Third exception <fg=blue;bg=red>comment</>\n" +
		"  Caused by: Second exception <comment>comment</comment>\n" +
		"  Caused by: First exception <p>this is html</p>\n"
	tester.Run([]Param{P("command", "foo3:bar")}, testerOptions{decorated: new(false), captureStderrSeparately: true})
	assertRendered(t, chain, tester.ErrorOutput())

	tester.Run([]Param{P("command", "foo3:bar")}, testerOptions{decorated: new(false), verbosity: VerbosityVerbose})
	assertRendered(t, chain+
		"  Debug: *console.testException [Exception, code 404]\n"+
		"         caused by *console.testException [Exception]\n"+
		"         caused by *console.testException [Exception]\n"+
		"         exit code 255\n", tester.Display())

	tester.Run([]Param{P("command", "foo3:bar")}, testerOptions{decorated: new(true), captureStderrSeparately: true})
	assertRendered(t, "\n\x1b[31;1mError:\x1b[39;22m \x1b[1mThird exception <fg=blue;bg=red>comment</>\x1b[22m\n"+
		"  \x1b[33mCaused by:\x1b[39m Second exception <comment>comment</comment>\n"+
		"  \x1b[33mCaused by:\x1b[39m First exception <p>this is html</p>\n", tester.ErrorOutput())
}

func TestApplication_RenderExceptionWithDoubleWidthCharacters(t *testing.T) {
	app := newTestApp()
	app.SetAutoExit(false)
	// Long messages are not wrapped: the terminal does.
	setColumns(t, "32")
	register(t, app, "foo").SetCode(func(Input, Output) (int, error) {
		return 0, newTestException("コマンドの実行中にエラーが発生しました。", 0, nil)
	})
	tester := newApplicationTester(app)
	tester.Run([]Param{P("command", "foo")}, testerOptions{decorated: new(false), captureStderrSeparately: true})
	assertRendered(t, "\nError: コマンドの実行中にエラーが発生しました。\n", tester.ErrorOutput())
}

func TestApplication_RenderExceptionLineBreaks(t *testing.T) {
	app := newTestApp()
	app.SetAutoExit(false)
	setColumns(t, "120")
	register(t, app, "foo").SetCode(func(Input, Output) (int, error) {
		e := newTestException("\n\nline 1 with extra spaces        \nline 2\n\nline 4\n", 0, nil)
		e.class = "InvalidArgumentException"

		return 0, e
	})
	tester := newApplicationTester(app)
	tester.Run([]Param{P("command", "foo")}, testerOptions{decorated: new(false)})
	assertRendered(t, "\nError: line 1 with extra spaces\n       line 2\n\n       line 4\n", tester.Display())
}

// An error without a message names its class.
func TestApplication_RenderExceptionWithoutMessage(t *testing.T) {
	app := newTestApp()
	app.SetAutoExit(false)
	register(t, app, "foo").SetCode(func(Input, Output) (int, error) {
		return 0, newTestException("", 0, nil)
	})
	tester := newApplicationTester(app)
	tester.Run([]Param{P("command", "foo")}, testerOptions{decorated: new(false)})
	assertRendered(t, "\nError: Exception (no message)\n", tester.Display())
}

// The usage escapes nothing: the synopsis is text.
func TestApplication_RenderExceptionEscapesLinesOfSynopsis(t *testing.T) {
	app := newTestApp()
	app.SetAutoExit(false)
	setColumns(t, "120")
	register(t, app, "foo").SetCode(func(Input, Output) (int, error) {
		return 0, newTestException("some exception", 0, nil)
	}).AddArgument("info", 0, "", nil)
	tester := newApplicationTester(app)
	tester.Run([]Param{P("command", "foo"), P("--nope", true)}, testerOptions{decorated: new(false)})
	assertRendered(t, "\nError: The \"--nope\" option does not exist.\n  Usage: foo [<info>]\n", tester.Display())

	// an error that is not about the input shows no usage
	tester.Run([]Param{P("command", "foo")}, testerOptions{decorated: new(false)})
	assertRendered(t, "\nError: some exception\n", tester.Display())
}

func TestApplication_Run(t *testing.T) {
	setColumns(t, "120")
	app := newTestApp()
	app.SetAutoExit(false)
	app.SetCatchExceptions(false)
	command := newFoo1Command()
	mustAdd(t, app, command)
	oldArgs := os.Args
	os.Args = []string{"cli.php", "foo:bar1"}
	_, err := app.Run(nil, nil)
	os.Args = oldArgs
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := command.input.(*ArgvInput); !ok {
		t.Errorf("Run() creates an ArgvInput by default, got %T", command.input)
	}
	if _, ok := command.output.(*ConsoleOutput); !ok {
		t.Errorf("Run() creates a ConsoleOutput by default, got %T", command.output)
	}

	app = newTestApp()
	app.SetAutoExit(false)
	app.SetCatchExceptions(false)
	ensureStaticCommandHelp(app)
	tester := newApplicationTester(app)

	run := func(params []Param, o testerOptions) {
		t.Helper()
		if _, err := tester.Run(params, o); err != nil {
			t.Fatal(err)
		}
	}
	plain := testerOptions{decorated: new(false)}

	run(nil, plain)
	assertEqualsFile(t, "application_run1.txt", tester.Display())

	run([]Param{P("--help", true)}, plain)
	assertEqualsFile(t, "application_run2.txt", tester.Display())

	run([]Param{P("-h", true)}, plain)
	assertEqualsFile(t, "application_run2.txt", tester.Display())

	run([]Param{P("command", "list"), P("--help", true)}, plain)
	assertEqualsFile(t, "application_run3.txt", tester.Display())

	run([]Param{P("command", "list"), P("-h", true)}, plain)
	assertEqualsFile(t, "application_run3.txt", tester.Display())

	run([]Param{P("--ansi", true)}, testerOptions{})
	if !tester.output.IsDecorated() {
		t.Error("--ansi forces color output")
	}

	run([]Param{P("--no-ansi", true)}, testerOptions{})
	if tester.output.IsDecorated() {
		t.Error("--no-ansi disables color output")
	}

	run([]Param{P("--version", true)}, plain)
	assertEqualsFile(t, "application_run4.txt", tester.Display())

	run([]Param{P("-V", true)}, plain)
	assertEqualsFile(t, "application_run4.txt", tester.Display())

	for _, q := range []string{"--quiet", "-q"} {
		run([]Param{P("command", "list"), P(q, true)}, testerOptions{})
		if tester.Display() != "" {
			t.Errorf("%s: display = %q", q, tester.Display())
		}
		if tester.input.IsInteractive() {
			t.Errorf("%s turns off interaction", q)
		}
	}

	for _, c := range []struct {
		param     Param
		verbosity int
	}{
		{P("--verbose", true), VerbosityVerbose},
		{P("--verbose", 1), VerbosityVerbose},
		{P("--verbose", 2), VerbosityVeryVerbose},
		{P("--verbose", 3), VerbosityDebug},
		{P("--verbose", 4), VerbosityVerbose},
		{P("-v", true), VerbosityVerbose},
		{P("-vv", true), VerbosityVeryVerbose},
		{P("-vvv", true), VerbosityDebug},
	} {
		run([]Param{P("command", "list"), c.param}, testerOptions{})
		if got := tester.output.Verbosity(); got != c.verbosity {
			t.Errorf("%s=%v: verbosity %d, want %d", c.param.Key, c.param.Value, got, c.verbosity)
		}
	}

	run([]Param{P("command", "help"), P("--help", true)}, plain)
	assertEqualsFile(t, "application_run5.txt", tester.Display())

	run([]Param{P("command", "help"), P("-h", true)}, plain)
	assertEqualsFile(t, "application_run5.txt", tester.Display())

	app = newTestApp()
	app.SetAutoExit(false)
	app.SetCatchExceptions(false)
	mustAdd(t, app, newFooCommand())
	tester = newApplicationTester(app)

	for _, n := range []string{"--no-interaction", "-n"} {
		run([]Param{P("command", "foo:bar"), P(n, true)}, plain)
		if tester.Display() != "called\n" {
			t.Errorf("%s: display = %q", n, tester.Display())
		}
	}
}

// ensureStaticCommandHelp replaces %command.full_name% with a static value.
func ensureStaticCommandHelp(app *Application) {
	for _, nc := range app.All("") {
		c := nc.Command.Base()
		c.SetHelp(strings.ReplaceAll(c.Help(), "%command.full_name%", "app/console %command.name%"))
	}
}

func TestApplication_RunWithGlobalOptionAndNoCommand(t *testing.T) {
	setColumns(t, "120")
	app := newTestApp()
	app.SetAutoExit(false)
	app.SetCatchExceptions(false)
	if err := app.Definition().AddOption(MustOption("foo", "f", OptionValueOptional, "", nil)); err != nil {
		t.Fatal(err)
	}
	in, _ := NewArgvInput([]string{"cli.php", "--foo", "bar"}, nil)
	code, err := app.Run(in, NewStreamOutput(&bytes.Buffer{}, VerbosityNormal, nil, nil))
	if code != 0 || err != nil {
		t.Errorf("Run() = %d, %v", code, err)
	}
}

// Issue #9285: a verbosity option before an argument must not take it as
// its value.
func TestApplication_VerboseValueNotBreakArguments(t *testing.T) {
	setColumns(t, "120")
	app := newTestApp()
	app.SetAutoExit(false)
	app.SetCatchExceptions(false)
	mustAdd(t, app, newFooCommand())
	out := NewStreamOutput(&bytes.Buffer{}, VerbosityNormal, nil, nil)

	for _, v := range []string{"-v", "--verbose"} {
		in, _ := NewArgvInput([]string{"cli.php", v, "foo:bar"}, nil)
		if _, err := app.Run(in, out); err != nil {
			t.Errorf("%s: %v", v, err)
		}
	}
}

// failingDoRunApp overrides doRun() to fail, like the PHP mocks.
type failingDoRunApp struct {
	*Application
	err error
}

func (a *failingDoRunApp) DoRun(Input, Output) (int, error) { return 0, a.err }

func TestApplication_RunReturnsExitCodeFromException(t *testing.T) {
	setColumns(t, "120")
	for _, c := range []struct{ code, want int }{{4, 4}, {0, 1}, {-1, 1}, {-32000, 1}} {
		app := &failingDoRunApp{newTestApp(), newTestException("", c.code, nil)}
		app.SetImpl(app)
		app.SetAutoExit(false)
		in, _ := NewArrayInput(nil, nil)
		got, err := app.Run(in, NewNullOutput())
		if err != nil || got != c.want {
			t.Errorf("code %d: Run() = %d, %v; want %d", c.code, got, err, c.want)
		}
	}
}

func TestApplication_AddingOptionWithDuplicateShortcut(t *testing.T) {
	setColumns(t, "120")
	app := newTestApp()
	app.SetAutoExit(false)
	app.SetCatchExceptions(false)
	if err := app.Definition().AddOption(MustOption("--env", "-e", OptionValueRequired, "Environment", nil)); err != nil {
		t.Fatal(err)
	}
	register(t, app, "foo").SetAliases("f").
		SetDefinitionItems(MustOption("survey", "e", OptionValueRequired, "My option with a shortcut.", nil)).
		SetCode(func(Input, Output) (int, error) { return 0, nil })

	in, _ := NewArrayInput([]Param{P("command", "foo")}, nil)
	_, err := app.Run(in, NewNullOutput())
	expectError(t, err, KindLogic, `An option with shortcut "e" already exists.`)
}

func TestApplication_AddingAlreadySetDefinitionElementData(t *testing.T) {
	setColumns(t, "120")
	for _, def := range []any{
		MustArgument("command", ArgumentRequired, "", nil),
		MustOption("quiet", "", OptionValueNone, "", nil),
		MustOption("query", "q", OptionValueNone, "", nil),
	} {
		app := newTestApp()
		app.SetAutoExit(false)
		app.SetCatchExceptions(false)
		register(t, app, "foo").SetDefinitionItems(def).SetCode(func(Input, Output) (int, error) { return 0, nil })
		in, _ := NewArrayInput([]Param{P("command", "foo")}, nil)
		_, err := app.Run(in, NewNullOutput())
		if !phperr.InstanceOf(err, "LogicException") {
			t.Errorf("%T: want a LogicException, got %v", def, err)
		}
	}
}

func TestApplication_GetDefaultHelperSetReturnsDefaultValues(t *testing.T) {
	if !newTestApp().HelperSet().Has("formatter") {
		t.Error("default helper set lacks formatter")
	}
}

func TestApplication_AddingSingleHelperSetOverwritesDefaultValues(t *testing.T) {
	app := newTestApp()
	app.SetHelperSet(NewHelperSet(&FormatterHelper{}))
	hs := app.HelperSet()
	if !hs.Has("formatter") || hs.Has("dialog") || hs.Has("progress") || hs.Has("question") {
		t.Error("helper set")
	}
}

// CustomApplication overwrites the default input definition and helper set.
type CustomApplication struct{ *Application }

func newCustomApplication() *CustomApplication {
	a := &CustomApplication{newTestApp()}
	a.SetImpl(a)

	return a
}

func (*CustomApplication) DefaultInputDefinition() *InputDefinition {
	return MustDefinition(MustOption("--custom", "-c", OptionValueNone, "Set the custom input definition.", nil))
}

func (*CustomApplication) DefaultHelperSet() *HelperSet {
	return NewHelperSet(&FormatterHelper{})
}

func TestApplication_OverwritingDefaultHelperSetOverwritesDefaultValues(t *testing.T) {
	app := newCustomApplication()
	app.SetHelperSet(NewHelperSet(&FormatterHelper{}))
	hs := app.HelperSet()
	if !hs.Has("formatter") || hs.Has("dialog") || hs.Has("progress") {
		t.Error("helper set")
	}
	if newCustomApplication().HelperSet().Has("question") {
		t.Error("DefaultHelperSet() override ignored")
	}
}

func TestApplication_GetDefaultInputDefinitionReturnsDefaultValues(t *testing.T) {
	d := newTestApp().Definition()
	if !d.HasArgument("command") {
		t.Error("command")
	}
	for _, o := range []string{"help", "quiet", "verbose", "version", "ansi", "no-interaction"} {
		if !d.HasOption(o) {
			t.Error(o)
		}
	}
	if !d.HasNegation("no-ansi") || d.HasOption("no-ansi") {
		t.Error("no-ansi")
	}
}

func assertCustomDefinition(t *testing.T, d *InputDefinition) {
	t.Helper()
	if d.HasArgument("command") {
		t.Error("command")
	}
	for _, o := range []string{"help", "quiet", "verbose", "version", "ansi", "no-interaction"} {
		if d.HasOption(o) {
			t.Error(o)
		}
	}
	if d.HasNegation("no-ansi") {
		t.Error("no-ansi")
	}
	if !d.HasOption("custom") {
		t.Error("custom")
	}
}

func TestApplication_OverwritingDefaultInputDefinitionOverwritesDefaultValues(t *testing.T) {
	assertCustomDefinition(t, newCustomApplication().Definition())
}

func TestApplication_SettingCustomInputDefinitionOverwritesDefaultValues(t *testing.T) {
	app := newTestApp()
	app.SetDefinition(MustDefinition(MustOption("--custom", "-c", OptionValueNone, "Set the custom input definition.", nil)))
	assertCustomDefinition(t, app.Definition())
}

func TestApplication_RunWithError(t *testing.T) {
	setColumns(t, "120")
	app := newTestApp()
	app.SetAutoExit(false)
	app.SetCatchExceptions(false)
	register(t, app, "dym").SetCode(func(_ Input, out Output) (int, error) {
		out.Write("dym.", false, OutputNormal)

		return 0, errors.New("dymerr")
	})
	tester := newApplicationTester(app)
	if _, err := tester.Run([]Param{P("command", "dym")}, testerOptions{}); err == nil || err.Error() != "dymerr" {
		t.Errorf("err = %v", err)
	}
}

// CustomDefaultCommandApplication sets a different default command.
type CustomDefaultCommandApplication struct{ *Application }

func newCustomDefaultCommandApplication(t *testing.T) *CustomDefaultCommandApplication {
	a := &CustomDefaultCommandApplication{newTestApp()}
	a.SetImpl(a)
	command := newFooCommand()
	mustAdd(t, a.Application, command)
	if err := a.SetDefaultCommand(command.Name(), false); err != nil {
		t.Fatal(err)
	}

	return a
}

func TestApplication_SetRunCustomDefaultCommand(t *testing.T) {
	setColumns(t, "120")
	command := newFooCommand()
	app := newTestApp()
	app.SetAutoExit(false)
	mustAdd(t, app, command)
	if err := app.SetDefaultCommand(command.Name(), false); err != nil {
		t.Fatal(err)
	}
	tester := newApplicationTester(app)
	tester.Run(nil, testerOptions{interactive: new(false)})
	if tester.Display() != "called\n" {
		t.Errorf("display = %q", tester.Display())
	}

	custom := newCustomDefaultCommandApplication(t)
	custom.SetAutoExit(false)
	tester = newApplicationTester(custom.Application)
	tester.Run(nil, testerOptions{interactive: new(false)})
	if tester.Display() != "called\n" {
		t.Errorf("display = %q", tester.Display())
	}
}

func TestApplication_SetRunCustomDefaultCommandWithOption(t *testing.T) {
	setColumns(t, "120")
	command := newFooOptCommand()
	app := newTestApp()
	app.SetAutoExit(false)
	mustAdd(t, app, command)
	if err := app.SetDefaultCommand(command.Name(), false); err != nil {
		t.Fatal(err)
	}
	tester := newApplicationTester(app)
	tester.Run([]Param{P("--fooopt", "opt")}, testerOptions{interactive: new(false)})
	if tester.Display() != "called\nopt\n" {
		t.Errorf("display = %q", tester.Display())
	}
}

func TestApplication_SetRunCustomSingleCommand(t *testing.T) {
	setColumns(t, "120")
	command := newFooCommand()
	app := newTestApp()
	app.SetAutoExit(false)
	mustAdd(t, app, command)
	if err := app.SetDefaultCommand(command.Name(), true); err != nil {
		t.Fatal(err)
	}
	tester := newApplicationTester(app)

	tester.Run(nil, testerOptions{})
	if !strings.Contains(tester.Display(), "called") {
		t.Errorf("display = %q", tester.Display())
	}

	tester.Run([]Param{P("--help", true)}, testerOptions{})
	if !strings.Contains(tester.Display(), "The foo:bar command") {
		t.Errorf("display = %q", tester.Display())
	}
}

// Ports CommandTest::testDefaultCommand (without PHP attributes).
func TestApplication_DefaultCommandName(t *testing.T) {
	app := newTestApp()
	_ = app.SetDefaultCommand("|foo|f", false)
	if app.defaultCommand != "foo" {
		t.Error(app.defaultCommand)
	}
	_ = app.SetDefaultCommand("foo2", false)
	if app.defaultCommand != "foo2" {
		t.Error(app.defaultCommand)
	}
}

func TestApplication_Abbreviations(t *testing.T) {
	got := Abbreviations([]string{"foo", "fop"})
	if !slices.Equal(got["fo"], []string{"foo", "fop"}) || !slices.Equal(got["foo"], []string{"foo"}) || len(got) != 4 {
		t.Error(got)
	}
}

func TestApplication_ExtractNamespace(t *testing.T) {
	for _, c := range []struct {
		name  string
		limit int
		want  string
	}{{"foo:bar:baz", 0, "foo:bar"}, {"foo:bar:baz", 1, "foo"}, {"foo", 0, ""}, {"a:b:c:d", 2, "a:b"}} {
		if got := ExtractNamespace(c.name, c.limit); got != c.want {
			t.Errorf("ExtractNamespace(%q, %d) = %q", c.name, c.limit, got)
		}
	}
}

func TestApplication_Complete(t *testing.T) {
	app := newTestApp()
	mustAdd(t, app, newFooCommand(), newFooHiddenCommand())
	in := CompletionInputFromTokens([]string{"bin/console", ""}, 1)
	_ = in.Bind(app.Definition())
	s := &CompletionSuggestions{}
	app.Complete(in, s)
	var got []string
	for _, v := range s.ValueSuggestions() {
		got = append(got, v.Value)
	}
	if want := []string{"help", "list", "completion", "foo:bar", "afoobar"}; !slices.Equal(got, want) {
		t.Errorf("suggestions = %v", got)
	}

	in = CompletionInputFromTokens([]string{"bin/console", "--"}, 1)
	_ = in.Bind(app.Definition())
	s = &CompletionSuggestions{}
	app.Complete(in, s)
	if len(s.OptionSuggestions()) != len(app.Definition().Options()) {
		t.Errorf("option suggestions = %d", len(s.OptionSuggestions()))
	}
}

// Symfony's run() putenv()s LINES and COLUMNS without $_SERVER, so the
// processes Composer starts don't inherit them: run() leaves maestro's
// environment, which is what those processes get, as it was.
func TestApplication_RunExportsNoTerminalSize(t *testing.T) {
	for _, name := range []string{"LINES", "COLUMNS"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	app := newTestApp()
	app.SetAutoExit(false)
	register(t, app, "foo").SetCode(func(_ Input, _ Output) (int, error) { return 0, nil })

	newApplicationTester(app).Run([]Param{PI(0, "foo")}, testerOptions{})
	for _, name := range []string{"LINES", "COLUMNS"} {
		if v, ok := os.LookupEnv(name); ok {
			t.Errorf("run() exported %s=%s", name, v)
		}
	}
}
