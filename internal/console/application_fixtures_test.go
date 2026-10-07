// Ports the command fixtures of Tests/Fixtures/*.php (symfony/console) used
// by ApplicationTest, CommandTest, ListCommandTest and HelpCommandTest, plus
// test helpers (a PHP-like exception and fixture assertions).

package console

import (
	"os"
	"testing"
)

// testException is a PHP exception thrown by test code: \Exception unless
// class says otherwise.
type testException struct {
	class, message, file string
	line, code           int
	prev                 error
}

func newTestException(message string, code int, prev error) *testException {
	return &testException{class: "Exception", message: message, file: "/tests/ApplicationTest.php", line: 1, code: code, prev: prev}
}

func (e *testException) Error() string      { return e.message }
func (e *testException) PHPClass() string   { return e.class }
func (e *testException) PHPCode() int       { return e.code }
func (e *testException) PHPPrevious() error { return e.prev }

// fixture reads testdata/Fixtures/name.
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/Fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func assertEqualsFile(t *testing.T, name, got string) {
	t.Helper()
	if want := fixture(t, name); want != got {
		t.Errorf("output does not equal %s:\nwant %q\ngot  %q", name, want, got)
	}
}

// fixtureCommand holds what the fixture commands record.
type fixtureCommand struct {
	*Command
	input  Input
	output Output
}

func newFixture(name, description string, aliases ...string) fixtureCommand {
	c := NewCommand(name).SetDescription(description)
	if aliases != nil {
		c.SetAliases(aliases...)
	}

	return fixtureCommand{Command: c}
}

type FooCommand struct{ fixtureCommand }

func newFooCommand() *FooCommand {
	c := &FooCommand{newFixture("foo:bar", "The foo:bar command", "afoobar")}
	c.SetImpl(c)

	return c
}

func (c *FooCommand) Interact(_ Input, out Output) error {
	out.Writeln("interact called")

	return nil
}

func (c *FooCommand) Execute(in Input, out Output) (int, error) {
	c.input, c.output = in, out
	out.Writeln("called")

	return 0, nil
}

type FooOptCommand struct{ fixtureCommand }

func newFooOptCommand() *FooOptCommand {
	c := &FooOptCommand{newFixture("foo:bar", "The foo:bar command", "afoobar")}
	c.AddOption("fooopt", "fo", OptionValueOptional, "fooopt description", nil)
	c.SetImpl(c)

	return c
}

func (c *FooOptCommand) Interact(_ Input, out Output) error {
	out.Writeln("interact called")

	return nil
}

func (c *FooOptCommand) Execute(in Input, out Output) (int, error) {
	c.input, c.output = in, out
	out.Writeln("called")
	out.Writeln(StringOption(in, "fooopt"))

	return 0, nil
}

type Foo1Command struct{ fixtureCommand }

func newFoo1Command() *Foo1Command {
	c := &Foo1Command{newFixture("foo:bar1", "The foo:bar1 command", "afoobar1")}
	c.SetImpl(c)

	return c
}

func (c *Foo1Command) Execute(in Input, out Output) (int, error) {
	c.input, c.output = in, out

	return 0, nil
}

type Foo2Command struct{ fixtureCommand }

func newFoo2Command() *Foo2Command {
	c := &Foo2Command{newFixture("foo1:bar", "The foo1:bar command", "afoobar2")}
	c.SetImpl(c)

	return c
}

func (*Foo2Command) Execute(Input, Output) (int, error) { return 0, nil }

type Foo3Command struct{ fixtureCommand }

func newFoo3Command() *Foo3Command {
	c := &Foo3Command{newFixture("foo3:bar", "The foo3:bar command")}
	c.SetImpl(c)

	return c
}

func (*Foo3Command) Execute(Input, Output) (int, error) {
	first := &testException{class: "Exception", message: "First exception <p>this is html</p>", file: "/Fixtures/Foo3Command.php", line: 21}
	second := &testException{class: "Exception", message: "Second exception <comment>comment</comment>", file: "/Fixtures/Foo3Command.php", line: 23, prev: first}

	return 0, &testException{class: "Exception", message: "Third exception <fg=blue;bg=red>comment</>", file: "/Fixtures/Foo3Command.php", line: 26, code: 404, prev: second}
}

type Foo4Command struct{ fixtureCommand }

func newFoo4Command() *Foo4Command {
	c := &Foo4Command{newFixture("foo3:bar:toh", "")}
	c.SetImpl(c)

	return c
}

// Foo5Command does not call the parent constructor.
type Foo5Command struct{ *Command }

func (*Foo5Command) PHPClass() string { return "Foo5Command" }

type Foo6Command struct{ fixtureCommand }

func newFoo6Command() *Foo6Command {
	c := &Foo6Command{newFixture("0foo:bar", "0foo:bar command")}
	c.SetImpl(c)

	return c
}

type FooSameCaseUppercaseCommand struct{ fixtureCommand }

func newFooSameCaseUppercaseCommand() *FooSameCaseUppercaseCommand {
	c := &FooSameCaseUppercaseCommand{newFixture("foo:BAR", "foo:BAR command")}
	c.SetImpl(c)

	return c
}

type FooSameCaseLowercaseCommand struct{ fixtureCommand }

func newFooSameCaseLowercaseCommand() *FooSameCaseLowercaseCommand {
	c := &FooSameCaseLowercaseCommand{newFixture("foo:bar", "foo:bar command")}
	c.SetImpl(c)

	return c
}

type FoobarCommand struct{ fixtureCommand }

func newFoobarCommand() *FoobarCommand {
	c := &FoobarCommand{newFixture("foobar:foo", "The foobar:foo command")}
	c.SetImpl(c)

	return c
}

func (c *FoobarCommand) Execute(in Input, out Output) (int, error) {
	c.input, c.output = in, out

	return 0, nil
}

type BarBucCommand struct{ fixtureCommand }

func newBarBucCommand() *BarBucCommand {
	c := &BarBucCommand{newFixture("bar:buc", "")}
	c.SetImpl(c)

	return c
}

type FooSubnamespaced1Command struct{ fixtureCommand }

func newFooSubnamespaced1Command() *FooSubnamespaced1Command {
	c := &FooSubnamespaced1Command{newFixture("foo:bar:baz", "The foo:bar:baz command", "foobarbaz")}
	c.SetImpl(c)

	return c
}

type FooSubnamespaced2Command struct{ fixtureCommand }

func newFooSubnamespaced2Command() *FooSubnamespaced2Command {
	c := &FooSubnamespaced2Command{newFixture("foo:go:bret", "The foo:bar:go command", "foobargo")}
	c.SetImpl(c)

	return c
}

type FooWithoutAliasCommand struct{ fixtureCommand }

func newFooWithoutAliasCommand() *FooWithoutAliasCommand {
	c := &FooWithoutAliasCommand{newFixture("foo", "The foo command")}
	c.SetImpl(c)

	return c
}

func (*FooWithoutAliasCommand) Execute(_ Input, out Output) (int, error) {
	out.Writeln("called")

	return 0, nil
}

type TestAmbiguousCommandRegistering struct{ fixtureCommand }

func newTestAmbiguousCommandRegistering() *TestAmbiguousCommandRegistering {
	c := &TestAmbiguousCommandRegistering{newFixture("test-ambiguous", "The test-ambiguous command", "test")}
	c.SetImpl(c)

	return c
}

func (*TestAmbiguousCommandRegistering) Execute(_ Input, out Output) (int, error) {
	out.Write("test-ambiguous", false, OutputNormal)

	return 0, nil
}

type TestAmbiguousCommandRegistering2 struct{ fixtureCommand }

func newTestAmbiguousCommandRegistering2() *TestAmbiguousCommandRegistering2 {
	c := &TestAmbiguousCommandRegistering2{newFixture("test-ambiguous2", "The test-ambiguous2 command")}
	c.SetImpl(c)

	return c
}

func (*TestAmbiguousCommandRegistering2) Execute(_ Input, out Output) (int, error) {
	out.Write("test-ambiguous2", false, OutputNormal)

	return 0, nil
}

type FooHiddenCommand struct{ fixtureCommand }

func newFooHiddenCommand() *FooHiddenCommand {
	c := &FooHiddenCommand{newFixture("foo:hidden", "", "afoohidden")}
	c.SetHidden(true)
	c.SetImpl(c)

	return c
}

func (*FooHiddenCommand) Execute(Input, Output) (int, error) { return 0, nil }

// TestCommand is Fixtures/TestCommand.php.
type TestCommand struct{ *Command }

func newTestCommand() *TestCommand {
	c := &TestCommand{NewCommand("")}
	c.SetName("namespace:name").SetAliases("name").SetDescription("description").SetHelp("help")
	c.SetImpl(c)

	return c
}

func (*TestCommand) Execute(_ Input, out Output) (int, error) {
	out.Writeln("execute called")

	return 0, nil
}

func (*TestCommand) Interact(_ Input, out Output) error {
	out.Writeln("interact called")

	return nil
}

// DisabledCommand is never added.
type DisabledCommand struct{ *Command }

func (*DisabledCommand) IsEnabled() bool { return false }
