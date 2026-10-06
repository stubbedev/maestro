// Ports Tests/Completion/CompletionInputTest.php,
// Tests/Completion/Output/{CompletionOutputTestCase,BashCompletionOutputTest}.php,
// Tests/Command/{CompleteCommandTest,DumpCompletionCommandTest}.php and
// src/Tester/CommandCompletionTester.php (symfony/console), plus the
// completion oracle goldens.

package console

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

func completionTestDefinition() *InputDefinition {
	return MustDefinition(
		MustOption("with-required-value", "r", OptionValueRequired, "", nil),
		MustOption("with-optional-value", "o", OptionValueOptional, "", nil),
		MustOption("without-value", "n", OptionValueNone, "", nil),
		MustArgument("required-arg", ArgumentRequired, "", nil),
		MustArgument("optional-arg", ArgumentOptional, "", nil),
	)
}

func TestCompletionInput_Bind(t *testing.T) {
	cases := []struct {
		name          string
		input         *CompletionInput
		expectedType  string
		expectedName  string // "" means null
		expectedValue string
	}{
		// option names
		{"optname-minimal-input", CompletionInputFromTokens([]string{"bin/console", "-"}, 1), CompletionTypeOptionName, "", "-"},
		{"optname-partial", CompletionInputFromTokens([]string{"bin/console", "--with"}, 1), CompletionTypeOptionName, "", "--with"},

		// option values
		{"optval-short", CompletionInputFromTokens([]string{"bin/console", "-r"}, 1), CompletionTypeOptionValue, "with-required-value", ""},
		{"optval-short-partial", CompletionInputFromTokens([]string{"bin/console", "-rsymf"}, 1), CompletionTypeOptionValue, "with-required-value", "symf"},
		{"optval-short-space", CompletionInputFromTokens([]string{"bin/console", "-r"}, 2), CompletionTypeOptionValue, "with-required-value", ""},
		{"optval-short-space-partial", CompletionInputFromTokens([]string{"bin/console", "-r", "symf"}, 2), CompletionTypeOptionValue, "with-required-value", "symf"},
		{"optval-short-before-arg", CompletionInputFromTokens([]string{"bin/console", "-r", "symfony"}, 1), CompletionTypeOptionValue, "with-required-value", ""},
		{"optval-long", CompletionInputFromTokens([]string{"bin/console", "--with-required-value="}, 1), CompletionTypeOptionValue, "with-required-value", ""},
		{"optval-long-partial", CompletionInputFromTokens([]string{"bin/console", "--with-required-value=symf"}, 1), CompletionTypeOptionValue, "with-required-value", "symf"},
		{"optval-long-space", CompletionInputFromTokens([]string{"bin/console", "--with-required-value"}, 2), CompletionTypeOptionValue, "with-required-value", ""},
		{"optval-long-space-partial", CompletionInputFromTokens([]string{"bin/console", "--with-required-value", "symf"}, 2), CompletionTypeOptionValue, "with-required-value", "symf"},

		{"optval-short-optional", CompletionInputFromTokens([]string{"bin/console", "-o"}, 1), CompletionTypeOptionValue, "with-optional-value", ""},
		{"optval-short-space-optional", CompletionInputFromTokens([]string{"bin/console", "-o"}, 2), CompletionTypeOptionValue, "with-optional-value", ""},
		{"optval-long-optional", CompletionInputFromTokens([]string{"bin/console", "--with-optional-value="}, 1), CompletionTypeOptionValue, "with-optional-value", ""},
		{"optval-long-space-optional", CompletionInputFromTokens([]string{"bin/console", "--with-optional-value"}, 2), CompletionTypeOptionValue, "with-optional-value", ""},

		// arguments
		{"arg-minimal-input", CompletionInputFromTokens([]string{"bin/console"}, 1), CompletionTypeArgumentValue, "required-arg", ""},
		{"arg-optional", CompletionInputFromTokens([]string{"bin/console", "symfony"}, 2), CompletionTypeArgumentValue, "optional-arg", ""},
		{"arg-partial", CompletionInputFromTokens([]string{"bin/console", "symf"}, 1), CompletionTypeArgumentValue, "required-arg", "symf"},
		{"arg-optional-partial", CompletionInputFromTokens([]string{"bin/console", "symfony", "sen"}, 2), CompletionTypeArgumentValue, "optional-arg", "sen"},

		{"arg-after-option", CompletionInputFromTokens([]string{"bin/console", "--without-value"}, 2), CompletionTypeArgumentValue, "required-arg", ""},
		{"arg-after-optional-value-option", CompletionInputFromTokens([]string{"bin/console", "--with-optional-value", "--"}, 3), CompletionTypeArgumentValue, "required-arg", ""},

		// end of definition
		{"end", CompletionInputFromTokens([]string{"bin/console", "symfony", "sensiolabs"}, 3), CompletionTypeNone, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.input.Bind(completionTestDefinition()); err != nil {
				t.Fatal(err)
			}
			if got := c.input.CompletionType(); got != c.expectedType {
				t.Errorf("type = %q, want %q", got, c.expectedType)
			}
			if got := c.input.CompletionName(); got != c.expectedName {
				t.Errorf("name = %q, want %q", got, c.expectedName)
			}
			if got := c.input.CompletionValue(); got != c.expectedValue {
				t.Errorf("value = %q, want %q", got, c.expectedValue)
			}
		})
	}
}

func TestCompletionInput_BindWithLastArrayArgument(t *testing.T) {
	cases := []struct {
		input         *CompletionInput
		expectedValue string
	}{
		{CompletionInputFromTokens([]string{"bin/console"}, 1), ""},
		{CompletionInputFromTokens([]string{"bin/console", "symfony", "sensiolabs"}, 3), ""},
		{CompletionInputFromTokens([]string{"bin/console", "symfony", "sen"}, 2), "sen"},
	}
	for _, c := range cases {
		definition := MustDefinition(MustArgument("list-arg", ArgumentIsArray|ArgumentRequired, "", nil))
		if err := c.input.Bind(definition); err != nil {
			t.Fatal(err)
		}
		if got := c.input.CompletionType(); got != CompletionTypeArgumentValue {
			t.Errorf("%s: type = %q", c.input, got)
		}
		if got := c.input.CompletionName(); got != "list-arg" {
			t.Errorf("%s: name = %q", c.input, got)
		}
		if got := c.input.CompletionValue(); got != c.expectedValue {
			t.Errorf("%s: value = %q, want %q", c.input, got, c.expectedValue)
		}
	}
}

func TestCompletionInput_BindArgumentWithDefault(t *testing.T) {
	definition := MustDefinition(MustArgument("arg-with-default", ArgumentOptional, "", "default"))
	input := CompletionInputFromTokens([]string{"bin/console"}, 1)
	if err := input.Bind(definition); err != nil {
		t.Fatal(err)
	}
	if input.CompletionType() != CompletionTypeArgumentValue || input.CompletionName() != "arg-with-default" || input.CompletionValue() != "" {
		t.Errorf("got %q %q %q", input.CompletionType(), input.CompletionName(), input.CompletionValue())
	}
}

func TestCompletionInput_FromString(t *testing.T) {
	cases := []struct {
		input    string
		expected []string
	}{
		{"bin/console cache:clear", []string{"bin/console", "cache:clear"}},
		{"bin/console --env prod", []string{"bin/console", "--env", "prod"}},
		{"bin/console --env=prod", []string{"bin/console", "--env=prod"}},
		{"bin/console -eprod", []string{"bin/console", "-eprod"}},
		{`bin/console cache:clear "multi word string"`, []string{"bin/console", "cache:clear", `"multi word string"`}},
		{`bin/console cache:clear 'multi word string'`, []string{"bin/console", "cache:clear", `'multi word string'`}},
	}
	for _, c := range cases {
		if got := CompletionInputFromString(c.input, 1).tokens; !slices.Equal(got, c.expected) {
			t.Errorf("fromString(%q) = %q, want %q", c.input, got, c.expected)
		}
	}
}

func TestCompletionInput_ToString(t *testing.T) {
	cases := []struct {
		current  int
		expected string
	}{
		{0, "foo| bar baz"},
		{1, "foo bar| baz"},
		{2, "foo bar baz|"},
		{11, "foo bar baz |"},
	}
	for _, c := range cases {
		if got := CompletionInputFromTokens([]string{"foo", "bar", "baz"}, c.current).String(); got != c.expected {
			t.Errorf("String() = %q, want %q", got, c.expected)
		}
	}
}

// CompletionOutputTestCase / BashCompletionOutputTest.

func TestBashCompletionOutput_OptionsOutput(t *testing.T) {
	suggestions := &CompletionSuggestions{}
	suggestions.SuggestOptions(
		MustOption("option1", "o", OptionValueNone, "", nil),
		MustOption("negatable", "", OptionValueNegatable, "", nil),
	)
	var buf bytes.Buffer
	BashCompletionOutput{}.Write(suggestions, NewStreamOutput(&buf, VerbosityNormal, new(false), nil))
	if want := "--option1\n--negatable\n--no-negatable" + php.EOL; buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

func TestBashCompletionOutput_ValuesOutput(t *testing.T) {
	suggestions := &CompletionSuggestions{}
	suggestions.SuggestStrings("Green", "Red", "Yellow")
	var buf bytes.Buffer
	BashCompletionOutput{}.Write(suggestions, NewStreamOutput(&buf, VerbosityNormal, new(false), nil))
	if want := "Green\nRed\nYellow" + php.EOL; buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

// commandCompletionTester ports src/Tester/CommandCompletionTester.php.
type commandCompletionTester struct {
	command Commander
}

func (ct commandCompletionTester) complete(input []string) []string {
	currentIndex := len(input)
	if len(input) > 0 && input[len(input)-1] == "" {
		input = input[:len(input)-1]
	}
	input = append([]string{ct.command.Base().Name()}, input...)

	completionInput := CompletionInputFromTokens(input, currentIndex)
	if err := completionInput.Bind(ct.command.Base().Definition()); err != nil {
		panic(err)
	}
	suggestions := &CompletionSuggestions{}

	ct.command.Complete(completionInput, suggestions)

	out := make([]string, 0, len(suggestions.OptionSuggestions())+len(suggestions.ValueSuggestions()))
	for _, option := range suggestions.OptionSuggestions() {
		out = append(out, "--"+option.Name())
	}
	for _, v := range suggestions.ValueSuggestions() {
		out = append(out, v.String())
	}

	return out
}

func TestDumpCompletionCommand_Complete(t *testing.T) {
	got := commandCompletionTester{NewDumpCompletionCommand()}.complete([]string{""})
	if !slices.Equal(got, []string{"bash"}) {
		t.Errorf("got %q", got)
	}
}

func TestDumpCompletionCommand_Execute(t *testing.T) {
	app := NewApplication("app", "1.2.3")
	cmd := NewDumpCompletionCommand()
	if _, err := app.Add(cmd); err != nil {
		t.Fatal(err)
	}
	tester := newCommandTester(cmd)
	if _, err := tester.Execute([]Param{P("shell", "bash")}, testerOptions{}); err != nil {
		t.Fatal(err)
	}
	display := tester.Display()
	if strings.Contains(display, "{{ VERSION }}") || !strings.Contains(display, "1.2.3") {
		t.Errorf("placeholders not replaced:\n%s", display)
	}

	tester = newCommandTester(cmd)
	t.Setenv("SHELL", "/bin/fish")
	code, err := tester.Execute(nil, testerOptions{})
	if err != nil || code != 2 {
		t.Fatalf("code = %d, err = %v", code, err)
	}
	if want := "Detected shell \"fish\", which is not supported by Symfony shell completion (supported shells: \"bash\").\n"; tester.Display() != want {
		t.Errorf("got %q", tester.Display())
	}

	tester = newCommandTester(cmd)
	t.Setenv("SHELL", "")
	if code, _ := tester.Execute(nil, testerOptions{}); code != 2 {
		t.Fatalf("code = %d", code)
	}
	if want := "Shell not detected, Symfony shell completion only supports \"bash\").\n"; tester.Display() != want {
		t.Errorf("got %q", tester.Display())
	}
}

// CompleteCommandTest.

// completeTestHelloCommand is CompleteCommandTest_HelloCommand.
type completeTestHelloCommand struct {
	*Command
}

func newCompleteTestHelloCommand() *completeTestHelloCommand {
	c := &completeTestHelloCommand{Command: NewCommand("hello")}
	c.SetAliases("ahoy").AddArgument("name", ArgumentRequired, "", nil)

	return c
}

func (*completeTestHelloCommand) Complete(input *CompletionInput, suggestions *CompletionSuggestions) {
	if input.MustSuggestArgumentValuesFor("name") {
		suggestions.SuggestStrings("Fabien", "Robin", "Wouter")
	}
}

type completeCommandTest struct {
	t           *testing.T
	command     *CompleteCommand
	application *Application
	tester      *CommandTester
}

func setUpCompleteCommandTest(t *testing.T) *completeCommandTest {
	t.Helper()
	ct := &completeCommandTest{t: t, command: NewCompleteCommand(), application: NewApplication("UNKNOWN", "UNKNOWN")}
	if _, err := ct.application.Add(newCompleteTestHelloCommand()); err != nil {
		t.Fatal(err)
	}
	ct.command.SetApplication(ct.application)
	ct.tester = newCommandTester(ct.command)

	return ct
}

// execute runs in verbose mode to assert exceptions.
func (ct *completeCommandTest) execute(input []Param) error {
	if len(input) > 0 && !slices.ContainsFunc(input, func(p Param) bool { return p.Key == "--shell" }) {
		input = append(input, P("--shell", "bash"))
	}
	_, err := ct.tester.Execute(input, testerOptions{verbosity: VerbosityDebug})

	return err
}

func expectErrorContaining(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), msg) {
		t.Errorf("err = %v, want message containing %q", err, msg)
	}
}

func TestCompleteCommand_RequiredShellOption(t *testing.T) {
	ct := setUpCompleteCommandTest(t)
	expectErrorContaining(t, ct.execute(nil), `The "--shell" option must be set.`)
}

func TestCompleteCommand_UnsupportedShellOption(t *testing.T) {
	ct := setUpCompleteCommandTest(t)
	expectErrorContaining(t, ct.execute([]Param{P("--shell", "unsupported")}), `Shell completion is not supported for your shell: "unsupported" (supported: "bash").`)
}

func TestCompleteCommand_AdditionalShellSupport(t *testing.T) {
	ct := setUpCompleteCommandTest(t)
	ct.command = NewCompleteCommand(ShellCompletionOutput{"supported", BashCompletionOutput{}})
	ct.command.SetApplication(ct.application)
	ct.tester = newCommandTester(ct.command)

	if err := ct.execute([]Param{P("--shell", "supported"), P("--current", "1"), P("--input", []string{"bin/console"})}); err != nil {
		t.Fatal(err)
	}

	// verify that the default set of shells is still supported
	if err := ct.execute([]Param{P("--shell", "bash"), P("--current", "1"), P("--input", []string{"bin/console"})}); err != nil {
		t.Fatal(err)
	}
}

func TestCompleteCommand_InputAndCurrentOptionValidation(t *testing.T) {
	cases := []struct {
		input            []Param
		exceptionMessage string
	}{
		{nil, `The "--current" option must be set and it must be an integer`},
		{[]Param{P("--current", "a")}, `The "--current" option must be set and it must be an integer`},
		{[]Param{P("--current", "1"), P("--input", []string{"bin/console"})}, ""},
		{[]Param{P("--current", "2"), P("--input", []string{"bin/console"})}, "Current index is invalid, it must be the number of input tokens or one more."},
		{[]Param{P("--current", "1"), P("--input", []string{"bin/console", "cache:clear"})}, ""},
		{[]Param{P("--current", "2"), P("--input", []string{"bin/console", "cache:clear"})}, ""},
	}
	for _, c := range cases {
		ct := setUpCompleteCommandTest(t)
		err := ct.execute(append(slices.Clone(c.input), P("--shell", "bash")))
		if c.exceptionMessage != "" {
			expectErrorContaining(t, err, c.exceptionMessage)

			continue
		}
		if err != nil || ct.tester.StatusCode() != 0 {
			t.Errorf("%v: err = %v, status = %d", c.input, err, ct.tester.StatusCode())
		}
	}
}

func TestCompleteCommand_CompleteCommandName(t *testing.T) {
	cases := []struct {
		name        string
		input       []string
		suggestions []string
	}{
		{"empty", []string{"bin/console"}, []string{"help", "list", "completion", "hello", "ahoy"}},
		{"partial", []string{"bin/console", "he"}, []string{"help", "list", "completion", "hello", "ahoy"}},
		{"complete-shortcut-name", []string{"bin/console", "hell"}, []string{"hello", "ahoy"}},
		{"complete-aliases", []string{"bin/console", "ah"}, []string{"hello", "ahoy"}},
	}
	for _, c := range cases {
		ct := setUpCompleteCommandTest(t)
		if err := ct.execute([]Param{P("--current", "1"), P("--input", c.input)}); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if want := strings.Join(c.suggestions, "\n") + "\n"; ct.tester.Display() != want {
			t.Errorf("%s: got %q, want %q", c.name, ct.tester.Display(), want)
		}
	}
}

func TestCompleteCommand_CompleteCommandInputDefinition(t *testing.T) {
	cases := []struct {
		name        string
		input       []string
		suggestions []string
	}{
		{"definition", []string{"bin/console", "hello", "-"}, []string{"--help", "--quiet", "--verbose", "--version", "--ansi", "--no-ansi", "--no-interaction"}},
		{"custom", []string{"bin/console", "hello"}, []string{"Fabien", "Robin", "Wouter"}},
		{"definition-aliased", []string{"bin/console", "ahoy", "-"}, []string{"--help", "--quiet", "--verbose", "--version", "--ansi", "--no-ansi", "--no-interaction"}},
		{"custom-aliased", []string{"bin/console", "ahoy"}, []string{"Fabien", "Robin", "Wouter"}},
	}
	for _, c := range cases {
		ct := setUpCompleteCommandTest(t)
		if err := ct.execute([]Param{P("--current", "2"), P("--input", c.input)}); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if want := strings.Join(c.suggestions, "\n") + "\n"; ct.tester.Display() != want {
			t.Errorf("%s: got %q, want %q", c.name, ct.tester.Display(), want)
		}
	}
}

// Oracle: tools/oracle/console/completion.php.

type completionOracle struct {
	FromString []struct {
		Input  string   `json:"input"`
		Tokens []string `json:"tokens"`
	} `json:"fromString"`
	Bind []struct {
		Tokens  []string `json:"tokens"`
		Current int      `json:"current"`
		Type    string   `json:"type"`
		Name    *string  `json:"name"`
		Value   *string  `json:"value"`
		String  string   `json:"string"`
		Error   *string  `json:"error"`
	} `json:"bind"`
}

func completionOracleDefinition() *InputDefinition {
	return MustDefinition(
		MustOption("with-required-value", "r", OptionValueRequired, "", nil),
		MustOption("with-optional-value", "o", OptionValueOptional, "", nil),
		MustOption("without-value", "n", OptionValueNone, "", nil),
		MustOption("array", "a", OptionValueRequired|OptionValueIsArray, "", nil),
		MustOption("neg", "", OptionValueNegatable, "", nil),
		MustArgument("required-arg", ArgumentRequired, "", nil),
		MustArgument("optional-arg", ArgumentOptional, "", nil),
		MustArgument("list", ArgumentIsArray, "", nil),
	)
}

func TestOracle_Completion(t *testing.T) {
	var o completionOracle
	loadOracle(t, "completion.json", &o)

	for _, c := range o.FromString {
		got := CompletionInputFromString(c.Input, 1).tokens
		if len(got) == 0 && len(c.Tokens) == 0 {
			continue
		}
		if !slices.Equal(got, c.Tokens) {
			t.Errorf("fromString(%q) = %q, want %q", c.Input, got, c.Tokens)
		}
	}

	for _, c := range o.Bind {
		input := CompletionInputFromTokens(c.Tokens, c.Current)
		if err := input.Bind(completionOracleDefinition()); err != nil {
			t.Fatalf("%q@%d: %v", c.Tokens, c.Current, err)
		}
		name, value := "", ""
		if c.Name != nil {
			name = *c.Name
		}
		if c.Value != nil {
			value = *c.Value
		}
		if input.CompletionType() != c.Type || input.CompletionName() != name || input.CompletionValue() != value {
			t.Errorf("%q@%d: got (%q, %q, %q), want (%q, %q, %q)", c.Tokens, c.Current,
				input.CompletionType(), input.CompletionName(), input.CompletionValue(), c.Type, name, value)
		}
		if (c.Name != nil) != input.MustSuggestArgumentValuesFor(name) && c.Type == CompletionTypeArgumentValue {
			t.Errorf("%q@%d: name nullness mismatch", c.Tokens, c.Current)
		}
		if got := input.String(); got != c.String {
			t.Errorf("%q@%d: String() = %q, want %q", c.Tokens, c.Current, got, c.String)
		}
	}
}
