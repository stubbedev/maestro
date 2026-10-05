// Ports Tests/Input/*.php (symfony/console).

package console

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// nvs builds an ordered name => value list from alternating names and values.
func nvs(kv ...any) []NamedValue {
	out := make([]NamedValue, 0, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		out = append(out, NamedValue{kv[i].(string), kv[i+1]})
	}

	return out
}

func fmtNVs(l []NamedValue) string {
	parts := make([]string, len(l))
	for i, nv := range l {
		parts[i] = fmt.Sprintf("%s=%#v", nv.Name, nv.Value)
	}

	return "[" + strings.Join(parts, ", ") + "]"
}

// eqValues asserts two ordered value lists are identical (PHP ===, with
// []string and []any lists compared element-wise).
func eqValues(t *testing.T, got, want []NamedValue, msg string) {
	t.Helper()
	ok := len(got) == len(want)
	for i := 0; ok && i < len(got); i++ {
		ok = got[i].Name == want[i].Name && phpIdentical(got[i].Value, want[i].Value)
	}
	if !ok {
		t.Errorf("%s\nwant %s\ngot  %s", msg, fmtNVs(want), fmtNVs(got))
	}
}

func eqValue(t *testing.T, got, want any, msg ...string) {
	t.Helper()
	if !phpIdentical(got, want) {
		t.Errorf("%s\nwant %#v\ngot  %#v", strings.Join(msg, " "), want, got)
	}
}

func mustArgv(t *testing.T, argv ...string) *ArgvInput {
	t.Helper()
	in, err := NewArgvInput(argv, nil)
	if err != nil {
		t.Fatal(err)
	}

	return in
}

func opt(name, shortcut string, mode int, def ...any) *InputOption {
	var d any
	if len(def) > 0 {
		d = def[0]
	}

	return MustOption(name, shortcut, mode, "", d)
}

func arg(name string, mode int, def ...any) *InputArgument {
	var d any
	if len(def) > 0 {
		d = def[0]
	}

	return MustArgument(name, mode, "", d)
}

// --- ArgvInputTest ---

func TestArgvInput_Constructor(t *testing.T) {
	saved := os.Args
	defer func() { os.Args = saved }()
	os.Args = []string{"cli.php", "foo"}
	in, _ := NewArgvInput(nil, nil)
	eq(t, strings.Join(in.Tokens(), ","), "foo", "__construct() automatically get its input from the argv server variable")
}

func TestArgvInput_ParseArguments(t *testing.T) {
	in := mustArgv(t, "cli.php", "foo")
	_ = in.Bind(MustDefinition(arg("name", 0)))
	eqValues(t, in.Arguments(), nvs("name", "foo"), "->parse() parses required arguments")
	_ = in.Bind(MustDefinition(arg("name", 0)))
	eqValues(t, in.Arguments(), nvs("name", "foo"), "->parse() is stateless")
}

type argvOptionCase struct {
	argv     []string
	items    []any
	expected []NamedValue
	message  string
}

func argvOptionCases() []argvOptionCase {
	return []argvOptionCase{
		{[]string{"cli.php", "--foo"}, []any{opt("foo", "", 0)}, nvs("foo", true), "->parse() parses long options without a value"},
		{[]string{"cli.php", "--foo=bar"}, []any{opt("foo", "f", OptionValueRequired)}, nvs("foo", "bar"), "->parse() parses long options with a required value (with a = separator)"},
		{[]string{"cli.php", "--foo", "bar"}, []any{opt("foo", "f", OptionValueRequired)}, nvs("foo", "bar"), "->parse() parses long options with a required value (with a space separator)"},
		{[]string{"cli.php", "--foo="}, []any{opt("foo", "f", OptionValueOptional)}, nvs("foo", ""), "->parse() parses long options with optional value which is empty (with a = separator) as empty string"},
		{[]string{"cli.php", "--foo=", "bar"}, []any{opt("foo", "f", OptionValueOptional), arg("name", ArgumentRequired)}, nvs("foo", ""), "->parse() parses long options with optional value without value specified or an empty string (with a = separator) followed by an argument as empty string"},
		{[]string{"cli.php", "bar", "--foo"}, []any{opt("foo", "f", OptionValueOptional), arg("name", ArgumentRequired)}, nvs("foo", nil), "->parse() parses long options with optional value which is empty (with a = separator) preceded by an argument"},
		{[]string{"cli.php", "--foo", "", "bar"}, []any{opt("foo", "f", OptionValueOptional), arg("name", ArgumentRequired)}, nvs("foo", ""), "->parse() parses long options with optional value which is empty as empty string even followed by an argument"},
		{[]string{"cli.php", "--foo"}, []any{opt("foo", "f", OptionValueOptional)}, nvs("foo", nil), "->parse() parses long options with optional value specified with no separator and no value as null"},
		{[]string{"cli.php", "-f"}, []any{opt("foo", "f", 0)}, nvs("foo", true), "->parse() parses short options without a value"},
		{[]string{"cli.php", "-fbar"}, []any{opt("foo", "f", OptionValueRequired)}, nvs("foo", "bar"), "->parse() parses short options with a required value (with no separator)"},
		{[]string{"cli.php", "-f", "bar"}, []any{opt("foo", "f", OptionValueRequired)}, nvs("foo", "bar"), "->parse() parses short options with a required value (with a space separator)"},
		{[]string{"cli.php", "-f", ""}, []any{opt("foo", "f", OptionValueOptional)}, nvs("foo", ""), "->parse() parses short options with an optional empty value"},
		{[]string{"cli.php", "-f", "", "foo"}, []any{arg("name", 0), opt("foo", "f", OptionValueOptional)}, nvs("foo", ""), "->parse() parses short options with an optional empty value followed by an argument"},
		{[]string{"cli.php", "-f", "", "-b"}, []any{opt("foo", "f", OptionValueOptional), opt("bar", "b", 0)}, nvs("foo", "", "bar", true), "->parse() parses short options with an optional empty value followed by an option"},
		{[]string{"cli.php", "-f", "-b", "foo"}, []any{arg("name", 0), opt("foo", "f", OptionValueOptional), opt("bar", "b", 0)}, nvs("foo", nil, "bar", true), "->parse() parses short options with an optional value which is not present"},
		{[]string{"cli.php", "-fb"}, []any{opt("foo", "f", 0), opt("bar", "b", 0)}, nvs("foo", true, "bar", true), "->parse() parses short options when they are aggregated as a single one"},
		{[]string{"cli.php", "-fb", "bar"}, []any{opt("foo", "f", 0), opt("bar", "b", OptionValueRequired)}, nvs("foo", true, "bar", "bar"), "->parse() parses short options when they are aggregated as a single one and the last one has a required value"},
		{[]string{"cli.php", "-fb", "bar"}, []any{opt("foo", "f", 0), opt("bar", "b", OptionValueOptional)}, nvs("foo", true, "bar", "bar"), "->parse() parses short options when they are aggregated as a single one and the last one has an optional value"},
		{[]string{"cli.php", "-fbbar"}, []any{opt("foo", "f", 0), opt("bar", "b", OptionValueOptional)}, nvs("foo", true, "bar", "bar"), "->parse() parses short options when they are aggregated as a single one and the last one has an optional value with no separator"},
		{[]string{"cli.php", "-fbbar"}, []any{opt("foo", "f", OptionValueOptional), opt("bar", "b", OptionValueOptional)}, nvs("foo", "bbar", "bar", nil), "->parse() parses short options when they are aggregated as a single one and one of them takes a value"},
	}
}

func TestArgvInput_ParseOptions(t *testing.T) {
	for _, c := range argvOptionCases() {
		in := mustArgv(t, c.argv...)
		if err := in.Bind(MustDefinition(c.items...)); err != nil {
			t.Fatalf("%s: %v", c.message, err)
		}
		eqValues(t, in.Options(), c.expected, c.message)
	}
}

func TestArgvInput_ParseOptionsNegatable(t *testing.T) {
	for _, c := range []argvOptionCase{
		{[]string{"cli.php", "--foo"}, []any{opt("foo", "", OptionValueNegatable)}, nvs("foo", true), "->parse() parses long options without a value"},
		{[]string{"cli.php", "--foo"}, []any{opt("foo", "", OptionValueNone|OptionValueNegatable)}, nvs("foo", true), "->parse() parses long options without a value"},
		{[]string{"cli.php", "--no-foo"}, []any{opt("foo", "", OptionValueNegatable)}, nvs("foo", false), "->parse() parses long options without a value"},
		{[]string{"cli.php", "--no-foo"}, []any{opt("foo", "", OptionValueNone|OptionValueNegatable)}, nvs("foo", false), "->parse() parses long options without a value"},
		{[]string{"cli.php"}, []any{opt("foo", "", OptionValueNegatable)}, nvs("foo", nil), "->parse() parses long options without a value"},
		{[]string{"cli.php"}, []any{opt("foo", "", OptionValueNone|OptionValueNegatable)}, nvs("foo", nil), "->parse() parses long options without a value"},
		{[]string{"cli.php"}, []any{opt("foo", "", OptionValueNegatable, false)}, nvs("foo", false), "->parse() parses long options without a value"},
	} {
		in := mustArgv(t, c.argv...)
		if err := in.Bind(MustDefinition(c.items...)); err != nil {
			t.Fatalf("%s: %v", c.message, err)
		}
		eqValues(t, in.Options(), c.expected, c.message)
	}
}

type invalidArgvCase struct {
	argv    []string
	def     *InputDefinition
	message string
}

func TestArgvInput_InvalidInput(t *testing.T) {
	for _, c := range []invalidArgvCase{
		{[]string{"cli.php", "--foo"}, MustDefinition(opt("foo", "f", OptionValueRequired)), `The "--foo" option requires a value.`},
		{[]string{"cli.php", "-f"}, MustDefinition(opt("foo", "f", OptionValueRequired)), `The "--foo" option requires a value.`},
		{[]string{"cli.php", "-ffoo"}, MustDefinition(opt("foo", "f", OptionValueNone)), `The "-o" option does not exist.`},
		{[]string{"cli.php", "--foo=bar"}, MustDefinition(opt("foo", "f", OptionValueNone)), `The "--foo" option does not accept a value.`},
		{[]string{"cli.php", "foo", "bar"}, MustDefinition(), `No arguments expected, got "foo".`},
		{[]string{"cli.php", "foo", "bar"}, MustDefinition(arg("number", 0)), `Too many arguments, expected arguments "number".`},
		{[]string{"cli.php", "foo", "bar", "zzz"}, MustDefinition(arg("number", 0), arg("county", 0)), `Too many arguments, expected arguments "number" "county".`},
		{[]string{"cli.php", "--foo"}, MustDefinition(), `The "--foo" option does not exist.`},
		{[]string{"cli.php", "-f"}, MustDefinition(), `The "-f" option does not exist.`},
		{[]string{"cli.php", "-1"}, MustDefinition(arg("number", 0)), `The "-1" option does not exist.`},
		{[]string{"cli.php", "-fЩ"}, MustDefinition(opt("foo", "f", OptionValueNone)), `The "-Щ" option does not exist.`},
		{[]string{"cli.php", "acme:foo", "bar"}, MustDefinition(arg("command", ArgumentRequired)), `No arguments expected for "acme:foo" command, got "bar".`},
		{[]string{"cli.php", "acme:foo", "bar"}, MustDefinition(arg("name", ArgumentRequired)), `Too many arguments, expected arguments "name".`},
	} {
		in := mustArgv(t, c.argv...)
		wantKind(t, in.Bind(c.def), KindRuntime, c.message)
	}
}

func TestArgvInput_InvalidInputNegatable(t *testing.T) {
	for _, c := range []invalidArgvCase{
		{[]string{"cli.php", "--no-foo=bar"}, MustDefinition(opt("foo", "f", OptionValueNegatable)), `The "--no-foo" option does not accept a value.`},
		{[]string{"cli.php", "--no-foo="}, MustDefinition(opt("foo", "f", OptionValueNegatable)), `The "--no-foo" option does not accept a value.`},
		{[]string{"cli.php", "--no-foo=bar"}, MustDefinition(opt("foo", "f", OptionValueNone|OptionValueNegatable)), `The "--no-foo" option does not accept a value.`},
		{[]string{"cli.php", "--no-foo="}, MustDefinition(opt("foo", "f", OptionValueNone|OptionValueNegatable)), `The "--no-foo" option does not accept a value.`},
	} {
		in := mustArgv(t, c.argv...)
		wantKind(t, in.Bind(c.def), KindRuntime, c.message)
	}
}

func TestArgvInput_ParseArrayArgument(t *testing.T) {
	in := mustArgv(t, "cli.php", "foo", "bar", "baz", "bat")
	_ = in.Bind(MustDefinition(arg("name", ArgumentIsArray)))
	eqValues(t, in.Arguments(), nvs("name", []string{"foo", "bar", "baz", "bat"}), "->parse() parses array arguments")
}

func TestArgvInput_ParseArrayOption(t *testing.T) {
	def := func() *InputDefinition {
		return MustDefinition(opt("name", "", OptionValueOptional|OptionValueIsArray))
	}
	in := mustArgv(t, "cli.php", "--name=foo", "--name=bar", "--name=baz")
	_ = in.Bind(def())
	eqValues(t, in.Options(), nvs("name", []any{"foo", "bar", "baz"}), `->parse() parses array options ("--option=value" syntax)`)

	in = mustArgv(t, "cli.php", "--name", "foo", "--name", "bar", "--name", "baz")
	_ = in.Bind(def())
	eqValues(t, in.Options(), nvs("name", []any{"foo", "bar", "baz"}), `->parse() parses array options ("--option value" syntax)`)

	in = mustArgv(t, "cli.php", "--name=foo", "--name=bar", "--name=")
	_ = in.Bind(def())
	eqValues(t, in.Options(), nvs("name", []any{"foo", "bar", ""}), `->parse() parses empty array options as null ("--option=value" syntax)`)

	in = mustArgv(t, "cli.php", "--name", "foo", "--name", "bar", "--name", "--anotherOption")
	_ = in.Bind(MustDefinition(opt("name", "", OptionValueOptional|OptionValueIsArray), opt("anotherOption", "", OptionValueNone)))
	eqValues(t, in.Options(), nvs("name", []any{"foo", "bar", nil}, "anotherOption", true), `->parse() parses empty array options ("--option value" syntax)`)
}

func TestArgvInput_ParseNegativeNumberAfterDoubleDash(t *testing.T) {
	in := mustArgv(t, "cli.php", "--", "-1")
	_ = in.Bind(MustDefinition(arg("number", 0)))
	eqValues(t, in.Arguments(), nvs("number", "-1"), "->parse() parses arguments with leading dashes as arguments after having encountered a double-dash sequence")

	in = mustArgv(t, "cli.php", "-f", "bar", "--", "-1")
	_ = in.Bind(MustDefinition(arg("number", 0), opt("foo", "f", OptionValueOptional)))
	eqValues(t, in.Options(), nvs("foo", "bar"), "->parse() parses arguments with leading dashes as options before having encountered a double-dash sequence")
	eqValues(t, in.Arguments(), nvs("number", "-1"), "->parse() parses arguments with leading dashes as arguments after having encountered a double-dash sequence")
}

func TestArgvInput_ParseEmptyStringArgument(t *testing.T) {
	in := mustArgv(t, "cli.php", "-f", "bar", "")
	_ = in.Bind(MustDefinition(arg("empty", 0), opt("foo", "f", OptionValueOptional)))
	eqValues(t, in.Arguments(), nvs("empty", ""), "->parse() parses empty string arguments")
}

func TestArgvInput_GetFirstArgument(t *testing.T) {
	in := mustArgv(t, "cli.php", "-fbbar")
	eq(t, in.FirstArgument(), "", "->getFirstArgument() returns null when there is no arguments")

	in = mustArgv(t, "cli.php", "-fbbar", "foo")
	eq(t, in.FirstArgument(), "foo", "->getFirstArgument() returns the first argument from the raw input")

	in = mustArgv(t, "cli.php", "--foo", "fooval", "bar")
	_ = in.Bind(MustDefinition(opt("foo", "f", OptionValueOptional), arg("arg", 0)))
	eq(t, in.FirstArgument(), "bar")

	in = mustArgv(t, "cli.php", "-bf", "fooval", "argval")
	_ = in.Bind(MustDefinition(opt("bar", "b", OptionValueNone), opt("foo", "f", OptionValueOptional), arg("arg", 0)))
	eq(t, in.FirstArgument(), "argval")
}

func TestArgvInput_HasParameterOption(t *testing.T) {
	in := mustArgv(t, "cli.php", "-f", "foo")
	eq(t, in.HasParameterOption([]string{"-f"}, false), true, "->hasParameterOption() returns true if the given short option is in the raw input")

	in = mustArgv(t, "cli.php", "-etest")
	eq(t, in.HasParameterOption([]string{"-e"}, false), true, "->hasParameterOption() returns true if the given short option is in the raw input")
	eq(t, in.HasParameterOption([]string{"-s"}, false), false, "->hasParameterOption() returns true if the given short option is in the raw input")

	in = mustArgv(t, "cli.php", "--foo", "foo")
	eq(t, in.HasParameterOption([]string{"--foo"}, false), true, "->hasParameterOption() returns true if the given short option is in the raw input")

	in = mustArgv(t, "cli.php", "foo")
	eq(t, in.HasParameterOption([]string{"--foo"}, false), false, "->hasParameterOption() returns false if the given short option is not in the raw input")

	in = mustArgv(t, "cli.php", "--foo=bar")
	eq(t, in.HasParameterOption([]string{"--foo"}, false), true, "->hasParameterOption() returns true if the given option with provided value is in the raw input")
}

func TestArgvInput_HasParameterOptionOnlyOptions(t *testing.T) {
	in := mustArgv(t, "cli.php", "-f", "foo")
	eq(t, in.HasParameterOption([]string{"-f"}, true), true, "->hasParameterOption() returns true if the given short option is in the raw input")

	in = mustArgv(t, "cli.php", "--foo", "--", "foo")
	eq(t, in.HasParameterOption([]string{"--foo"}, true), true, "->hasParameterOption() returns true if the given long option is in the raw input")

	in = mustArgv(t, "cli.php", "--foo=bar", "foo")
	eq(t, in.HasParameterOption([]string{"--foo"}, true), true, "->hasParameterOption() returns true if the given long option with provided value is in the raw input")

	in = mustArgv(t, "cli.php", "--", "--foo")
	eq(t, in.HasParameterOption([]string{"--foo"}, true), false, "->hasParameterOption() returns false if the given option is in the raw input but after an end of options signal")
}

func TestArgvInput_HasParameterOptionEdgeCasesAndLimitations(t *testing.T) {
	in := mustArgv(t, "cli.php", "-fh")
	eq(t, in.HasParameterOption([]string{"-h"}, false), false)
	eq(t, in.HasParameterOption([]string{"-f"}, false), true)
	eq(t, in.HasParameterOption([]string{"-fh"}, false), true)
	eq(t, in.HasParameterOption([]string{"-hf"}, false), false)

	in = mustArgv(t, "cli.php", "-f", "-h")
	eq(t, in.HasParameterOption([]string{"-fh"}, false), false)
}

func TestArgvInput_NoWarningOnInvalidParameterOption(t *testing.T) {
	in := mustArgv(t, "cli.php", "-edev")
	eq(t, in.HasParameterOption([]string{"-e", ""}, false), true)
	eq(t, in.HasParameterOption([]string{"-m", ""}, false), false)
	eqValue(t, in.ParameterOption([]string{"-e", ""}, false, false), "dev")
	eqValue(t, in.ParameterOption([]string{"-m", ""}, false, false), false)
}

func TestArgvInput_ToString(t *testing.T) {
	in := mustArgv(t, "cli.php", "-f", "foo")
	eq(t, in.String(), "-f foo")

	in = mustArgv(t, "cli.php", "-f", "--bar=foo", "a b c d", "A\nB'C")
	eq(t, in.String(), "-f --bar=foo "+escapeShellArg("a b c d")+" "+escapeShellArg("A\nB'C"))
}

func TestArgvInput_GetParameterOptionEqualSign(t *testing.T) {
	for _, c := range []struct {
		argv       []string
		key        []string
		def        any
		onlyParams bool
		expected   any
	}{
		{[]string{"app/console", "foo:bar"}, []string{"-e"}, "default", false, "default"},
		{[]string{"app/console", "foo:bar", "-e", "dev"}, []string{"-e"}, "default", false, "dev"},
		{[]string{"app/console", "foo:bar", "--env=dev"}, []string{"--env"}, "default", false, "dev"},
		{[]string{"app/console", "foo:bar", "-e", "dev"}, []string{"-e", "--env"}, "default", false, "dev"},
		{[]string{"app/console", "foo:bar", "--env=dev"}, []string{"-e", "--env"}, "default", false, "dev"},
		{[]string{"app/console", "foo:bar", "--env=dev", "--en=1"}, []string{"--en"}, "default", false, "1"},
		{[]string{"app/console", "foo:bar", "--env=dev", "", "--en=1"}, []string{"--en"}, "default", false, "1"},
		{[]string{"app/console", "foo:bar", "--env", "val"}, []string{"--env"}, "default", false, "val"},
		{[]string{"app/console", "foo:bar", "--env", "val", "--dummy"}, []string{"--env"}, "default", false, "val"},
		{[]string{"app/console", "foo:bar", "--", "--env=dev"}, []string{"--env"}, "default", false, "dev"},
		{[]string{"app/console", "foo:bar", "--", "--env=dev"}, []string{"--env"}, "default", true, "default"},
	} {
		in := mustArgv(t, c.argv...)
		eqValue(t, in.ParameterOption(c.key, c.def, c.onlyParams), c.expected, strings.Join(c.argv, " "))
	}
}

func TestArgvInput_ParseSingleDashAsArgument(t *testing.T) {
	in := mustArgv(t, "cli.php", "-")
	_ = in.Bind(MustDefinition(arg("file", 0)))
	eqValues(t, in.Arguments(), nvs("file", "-"), "->parse() parses single dash as an argument")
}

func TestArgvInput_ParseOptionWithValueOptionalGivenEmptyAndRequiredArgument(t *testing.T) {
	for _, mode := range []int{ArgumentRequired, ArgumentOptional} {
		in := mustArgv(t, "cli.php", "--foo=", "bar")
		_ = in.Bind(MustDefinition(opt("foo", "f", OptionValueOptional), arg("name", mode)))
		// PHP's assertEquals(null) accepts the "" this yields.
		eqValues(t, in.Options(), nvs("foo", ""), "->parse() parses optional options with empty value as null")
		eqValues(t, in.Arguments(), nvs("name", "bar"), "->parse() parses required arguments")

		in = mustArgv(t, "cli.php", "--foo=0", "bar")
		_ = in.Bind(MustDefinition(opt("foo", "f", OptionValueOptional), arg("name", mode)))
		eqValues(t, in.Options(), nvs("foo", "0"), "->parse() parses optional options with empty value as null")
		eqValues(t, in.Arguments(), nvs("name", "bar"), "->parse() parses required arguments")
	}
}

// --- ArrayInputTest ---

func mustArray(t *testing.T, params []Param, def *InputDefinition) *ArrayInput {
	t.Helper()
	in, err := NewArrayInput(params, def)
	if err != nil {
		t.Fatal(err)
	}

	return in
}

func TestArrayInput_GetFirstArgument(t *testing.T) {
	eq(t, mustArray(t, nil, nil).FirstArgument(), "", "->getFirstArgument() returns null if no argument were passed")
	eq(t, mustArray(t, []Param{P("name", "Fabien")}, nil).FirstArgument(), "Fabien", "->getFirstArgument() returns the first passed argument")
	eq(t, mustArray(t, []Param{P("--foo", "bar"), P("name", "Fabien")}, nil).FirstArgument(), "Fabien", "->getFirstArgument() returns the first passed argument")
}

func TestArrayInput_HasParameterOption(t *testing.T) {
	in := mustArray(t, []Param{P("name", "Fabien"), P("--foo", "bar")}, nil)
	eq(t, in.HasParameterOption([]string{"--foo"}, false), true, "->hasParameterOption() returns true if an option is present in the passed parameters")
	eq(t, in.HasParameterOption([]string{"--bar"}, false), false, "->hasParameterOption() returns false if an option is not present in the passed parameters")

	in = mustArray(t, []Param{PI(0, "--foo")}, nil)
	eq(t, in.HasParameterOption([]string{"--foo"}, false), true, "->hasParameterOption() returns true if an option is present in the passed parameters")

	in = mustArray(t, []Param{PI(0, "--foo"), PI(1, "--"), PI(2, "--bar")}, nil)
	eq(t, in.HasParameterOption([]string{"--bar"}, false), true, "->hasParameterOption() returns true if an option is present in the passed parameters")
	eq(t, in.HasParameterOption([]string{"--bar"}, true), false, "->hasParameterOption() returns false if an option is present in the passed parameters after an end of options signal")
}

func TestArrayInput_GetParameterOption(t *testing.T) {
	in := mustArray(t, []Param{P("name", "Fabien"), P("--foo", "bar")}, nil)
	eqValue(t, in.ParameterOption([]string{"--foo"}, false, false), "bar", "->getParameterOption() returns the option of specified name")
	eqValue(t, in.ParameterOption([]string{"--bar"}, "default", false), "default", "->getParameterOption() returns the default value if an option is not present in the passed parameters")

	in = mustArray(t, []Param{PI(0, "Fabien"), P("--foo", "bar")}, nil)
	eqValue(t, in.ParameterOption([]string{"--foo"}, false, false), "bar", "->getParameterOption() returns the option of specified name")

	in = mustArray(t, []Param{PI(0, "--foo"), PI(1, "--"), P("--bar", "woop")}, nil)
	eqValue(t, in.ParameterOption([]string{"--bar"}, false, false), "woop", "->getParameterOption() returns the correct value if an option is present in the passed parameters")
	eqValue(t, in.ParameterOption([]string{"--bar"}, "default", true), "default", "->getParameterOption() returns the default value if an option is present in the passed parameters after an end of options signal")
}

func TestArrayInput_ParseArguments(t *testing.T) {
	in := mustArray(t, []Param{P("name", "foo")}, MustDefinition(arg("name", 0)))
	eqValues(t, in.Arguments(), nvs("name", "foo"), "->parse() parses required arguments")
}

func TestArrayInput_ParseOptions(t *testing.T) {
	for _, c := range []struct {
		params   []Param
		items    []any
		expected []NamedValue
		message  string
	}{
		{[]Param{P("--foo", "bar")}, []any{opt("foo", "", 0)}, nvs("foo", "bar"), "->parse() parses long options"},
		{[]Param{P("--foo", "bar")}, []any{opt("foo", "f", OptionValueOptional, "default")}, nvs("foo", "bar"), "->parse() parses long options with a default value"},
		{nil, []any{opt("foo", "f", OptionValueOptional, "default")}, nvs("foo", "default"), "->parse() uses the default value for long options with value optional which are not passed"},
		{[]Param{P("--foo", nil)}, []any{opt("foo", "f", OptionValueOptional, "default")}, nvs("foo", nil), "->parse() parses long options with a default value"},
		{[]Param{P("-f", "bar")}, []any{opt("foo", "f", 0)}, nvs("foo", "bar"), "->parse() parses short options"},
		{[]Param{P("--", nil), P("-f", "bar")}, []any{opt("foo", "f", OptionValueOptional, "default")}, nvs("foo", "default"), "->parse() does not parse opts after an end of options signal"},
		{[]Param{P("--", nil)}, nil, nvs(), "->parse() does not choke on end of options signal"},
	} {
		in := mustArray(t, c.params, MustDefinition(c.items...))
		eqValues(t, in.Options(), c.expected, c.message)
	}
}

func TestArrayInput_ParseInvalidInput(t *testing.T) {
	for _, c := range []struct {
		params  []Param
		def     *InputDefinition
		kind    Kind
		message string
	}{
		{[]Param{P("foo", "foo")}, MustDefinition(arg("name", 0)), KindInvalidArgument, `The "foo" argument does not exist.`},
		{[]Param{P("--foo", nil)}, MustDefinition(opt("foo", "f", OptionValueRequired)), KindInvalidOption, `The "--foo" option requires a value.`},
		{[]Param{P("--foo", "foo")}, MustDefinition(), KindInvalidOption, `The "--foo" option does not exist.`},
		{[]Param{P("-o", "foo")}, MustDefinition(), KindInvalidOption, `The "-o" option does not exist.`},
	} {
		_, err := NewArrayInput(c.params, c.def)
		wantKind(t, err, c.kind, c.message)
	}
}

func TestArrayInput_ToString(t *testing.T) {
	in := mustArray(t, []Param{P("-f", nil), P("-b", "bar"), P("--foo", "b a z"), P("--lala", nil), P("test", "Foo"), P("test2", "A\nB'C")}, nil)
	eq(t, in.String(), "-f -b bar --foo="+escapeShellArg("b a z")+" --lala Foo "+escapeShellArg("A\nB'C"))

	in = mustArray(t, []Param{P("-b", []string{"bval_1", "bval_2"}), P("--f", []string{"fval_1", "fval_2"})}, nil)
	eq(t, in.String(), "-b bval_1 -b bval_2 --f=fval_1 --f=fval_2")

	in = mustArray(t, []Param{P("array_arg", []string{"val_1", "val_2"})}, nil)
	eq(t, in.String(), "val_1 val_2")
}

// The PHP array semantics of the parameters: numeric string keys are
// integer keys, repeated keys keep their first position, and integer keyed
// values are appended to getArguments() like array_merge() does.
func TestArrayInput_PHPArrayKeys(t *testing.T) {
	def := MustDefinition(arg("a", 0), arg("b", 0))
	in := mustArray(t, []Param{P("1", "x"), P("a", "y"), P("a", "z"), PI(0, "w")}, def)
	eqValues(t, in.Arguments(), nvs("a", "z", "b", nil, "0", "x", "1", "w"), "getArguments()")
	eq(t, in.FirstArgument(), "x")
	eq(t, in.String(), "x z w")

	_, err := NewArrayInput([]Param{PI(2, "x")}, def)
	wantKind(t, err, KindInvalidArgument, `The "2" argument does not exist.`)

	// A negative integer key starts with "-" once converted to a string.
	in = mustArray(t, []Param{PI(-1, "v")}, MustDefinition(opt("one", "1", OptionValueRequired)))
	eqValues(t, in.Options(), nvs("one", "v"), "negative key")

	// getParameterOption() compares keys loosely.
	in = mustArray(t, []Param{P("1e1", "v")}, nil)
	eqValue(t, in.ParameterOption([]string{"10"}, false, false), "v")
}

// --- StringInputTest ---

func TestStringInput_Tokenize(t *testing.T) {
	for _, c := range []struct {
		input   string
		tokens  []string
		message string
	}{
		{"", nil, "->tokenize() parses an empty string"},
		{"foo", []string{"foo"}, "->tokenize() parses arguments"},
		{"  foo  bar  ", []string{"foo", "bar"}, "->tokenize() ignores whitespaces between arguments"},
		{`"quoted"`, []string{"quoted"}, "->tokenize() parses quoted arguments"},
		{"'quoted'", []string{"quoted"}, "->tokenize() parses quoted arguments"},
		{"'a\rb\nc\td'", []string{"a\rb\nc\td"}, "->tokenize() parses whitespace chars in strings"},
		{"'a'\r'b'\n'c'\t'd'", []string{"a", "b", "c", "d"}, "->tokenize() parses whitespace chars between args as spaces"},
		{`\"quoted\"`, []string{`"quoted"`}, "->tokenize() parses escaped-quoted arguments"},
		{`\'quoted\'`, []string{`'quoted'`}, "->tokenize() parses escaped-quoted arguments"},
		{"-a", []string{"-a"}, "->tokenize() parses short options"},
		{"-azc", []string{"-azc"}, "->tokenize() parses aggregated short options"},
		{"-awithavalue", []string{"-awithavalue"}, "->tokenize() parses short options with a value"},
		{`-a"foo bar"`, []string{"-afoo bar"}, "->tokenize() parses short options with a value"},
		{`-a"foo bar""foo bar"`, []string{"-afoo barfoo bar"}, "->tokenize() parses short options with a value"},
		{`-a'foo bar'`, []string{"-afoo bar"}, "->tokenize() parses short options with a value"},
		{`-a'foo bar''foo bar'`, []string{"-afoo barfoo bar"}, "->tokenize() parses short options with a value"},
		{`-a'foo bar'"foo bar"`, []string{"-afoo barfoo bar"}, "->tokenize() parses short options with a value"},
		{"--long-option", []string{"--long-option"}, "->tokenize() parses long options"},
		{"--long-option=foo", []string{"--long-option=foo"}, "->tokenize() parses long options with a value"},
		{`--long-option="foo bar"`, []string{"--long-option=foo bar"}, "->tokenize() parses long options with a value"},
		{`--long-option="foo bar""another"`, []string{"--long-option=foo baranother"}, "->tokenize() parses long options with a value"},
		{`--long-option='foo bar'`, []string{"--long-option=foo bar"}, "->tokenize() parses long options with a value"},
		{`--long-option='foo bar''another'`, []string{"--long-option=foo baranother"}, "->tokenize() parses long options with a value"},
		{`--long-option='foo bar'"another"`, []string{"--long-option=foo baranother"}, "->tokenize() parses long options with a value"},
		{"foo -a -ffoo --long bar", []string{"foo", "-a", "-ffoo", "--long", "bar"}, "->tokenize() parses when several arguments and options"},
		{`--arg=\"'Jenny'\''s'\"`, []string{`--arg="Jenny's"`}, "->tokenize() parses quoted quotes"},
	} {
		in, err := NewStringInput(c.input)
		if err != nil {
			t.Fatalf("%q: %v", c.input, err)
		}
		eq(t, fmt.Sprintf("%q", in.Tokens()), fmt.Sprintf("%q", c.tokens), c.message, c.input)
	}
}

func TestStringInput_InputOptionWithGivenString(t *testing.T) {
	in, _ := NewStringInput("--foo=bar")
	_ = in.Bind(MustDefinition(opt("foo", "", OptionValueRequired)))
	eqValue(t, in.Option("foo"), "bar")
}

func TestStringInput_ToString(t *testing.T) {
	in, _ := NewStringInput("-f foo")
	eq(t, in.String(), "-f foo")
	in, _ = NewStringInput(`-f --bar=foo "a b c d"`)
	eq(t, in.String(), "-f --bar=foo "+escapeShellArg("a b c d"))
	in, _ = NewStringInput("-f --bar=foo 'a b c d' 'A\nB\\'C'")
	eq(t, in.String(), "-f --bar=foo "+escapeShellArg("a b c d")+" "+escapeShellArg("A\nB'C"))
}

// --- InputTest ---

func TestInput_Constructor(t *testing.T) {
	in := mustArray(t, []Param{P("name", "foo")}, MustDefinition(arg("name", 0)))
	eqValue(t, in.Argument("name"), "foo", "->__construct() takes a InputDefinition as an argument")
}

func TestInput_Options(t *testing.T) {
	in := mustArray(t, []Param{P("--name", "foo")}, MustDefinition(opt("name", "", 0)))
	eqValue(t, in.Option("name"), "foo", "->getOption() returns the value for the given option")
	in.SetOption("name", "bar")
	eqValue(t, in.Option("name"), "bar", "->setOption() sets the value for a given option")
	eqValues(t, in.Options(), nvs("name", "bar"), "->getOptions() returns all option values")

	in = mustArray(t, []Param{P("--name", "foo")}, MustDefinition(opt("name", "", 0), opt("bar", "", OptionValueOptional, "default")))
	eqValue(t, in.Option("bar"), "default", "->getOption() returns the default value for optional options")
	eqValues(t, in.Options(), nvs("name", "foo", "bar", "default"), "->getOptions() returns all option values, even optional ones")

	in = mustArray(t, []Param{P("--name", "foo"), P("--bar", "")}, MustDefinition(opt("name", "", 0), opt("bar", "", OptionValueOptional, "default")))
	eqValue(t, in.Option("bar"), "", "->getOption() returns null for options explicitly passed without value (or an empty value)")
	eqValues(t, in.Options(), nvs("name", "foo", "bar", ""), "->getOptions() returns all option values.")

	in = mustArray(t, []Param{P("--name", "foo"), P("--bar", nil)}, MustDefinition(opt("name", "", 0), opt("bar", "", OptionValueOptional, "default")))
	eqValue(t, in.Option("bar"), nil, "->getOption() returns null for options explicitly passed without value (or an empty value)")
	eqValues(t, in.Options(), nvs("name", "foo", "bar", nil), "->getOptions() returns all option values")

	in = mustArray(t, []Param{P("--name", nil)}, MustDefinition(opt("name", "", OptionValueNegatable)))
	eq(t, in.HasOption("name"), true)
	eq(t, in.HasOption("no-name"), true)
	eqValue(t, in.Option("name"), true)
	eqValue(t, in.Option("no-name"), false)

	in = mustArray(t, []Param{P("--no-name", nil)}, MustDefinition(opt("name", "", OptionValueNegatable)))
	eqValue(t, in.Option("name"), false)
	eqValue(t, in.Option("no-name"), true)

	in = mustArray(t, nil, MustDefinition(opt("name", "", OptionValueNegatable)))
	eqValue(t, in.Option("name"), nil)
	eqValue(t, in.Option("no-name"), nil)
}

func TestInput_SetInvalidOption(t *testing.T) {
	in := mustArray(t, []Param{P("--name", "foo")}, MustDefinition(opt("name", "", 0), opt("bar", "", OptionValueOptional, "default")))
	wantKind(t, catchPanic(t, func() { in.SetOption("foo", "bar") }), KindInvalidArgument, `The "foo" option does not exist.`)
}

func TestInput_GetInvalidOption(t *testing.T) {
	in := mustArray(t, []Param{P("--name", "foo")}, MustDefinition(opt("name", "", 0), opt("bar", "", OptionValueOptional, "default")))
	wantKind(t, catchPanic(t, func() { in.Option("foo") }), KindInvalidArgument, `The "foo" option does not exist.`)
}

func TestInput_Arguments(t *testing.T) {
	in := mustArray(t, []Param{P("name", "foo")}, MustDefinition(arg("name", 0)))
	eqValue(t, in.Argument("name"), "foo", "->getArgument() returns the value for the given argument")
	in.SetArgument("name", "bar")
	eqValue(t, in.Argument("name"), "bar", "->setArgument() sets the value for a given argument")
	eqValues(t, in.Arguments(), nvs("name", "bar"), "->getArguments() returns all argument values")

	in = mustArray(t, []Param{P("name", "foo")}, MustDefinition(arg("name", 0), arg("bar", ArgumentOptional, "default")))
	eqValue(t, in.Argument("bar"), "default", "->getArgument() returns the default value for optional arguments")
	eqValues(t, in.Arguments(), nvs("name", "foo", "bar", "default"), "->getArguments() returns all argument values, even optional ones")
}

func TestInput_SetInvalidArgument(t *testing.T) {
	in := mustArray(t, []Param{P("name", "foo")}, MustDefinition(arg("name", 0), arg("bar", ArgumentOptional, "default")))
	wantKind(t, catchPanic(t, func() { in.SetArgument("foo", "bar") }), KindInvalidArgument, `The "foo" argument does not exist.`)
}

func TestInput_GetInvalidArgument(t *testing.T) {
	in := mustArray(t, []Param{P("name", "foo")}, MustDefinition(arg("name", 0), arg("bar", ArgumentOptional, "default")))
	wantKind(t, catchPanic(t, func() { in.Argument("foo") }), KindInvalidArgument, `The "foo" argument does not exist.`)
}

func TestInput_ValidateWithMissingArguments(t *testing.T) {
	in := mustArray(t, nil, nil)
	_ = in.Bind(MustDefinition(arg("name", ArgumentRequired)))
	wantKind(t, in.Validate(), KindRuntime, `Not enough arguments (missing: "name").`)
}

func TestInput_ValidateWithMissingRequiredArguments(t *testing.T) {
	in := mustArray(t, []Param{P("bar", "baz")}, nil)
	_ = in.Bind(MustDefinition(arg("name", ArgumentRequired), arg("bar", ArgumentOptional)))
	wantKind(t, in.Validate(), KindRuntime, `Not enough arguments (missing: "name").`)
}

func TestInput_Validate(t *testing.T) {
	in := mustArray(t, []Param{P("name", "foo")}, nil)
	_ = in.Bind(MustDefinition(arg("name", ArgumentRequired)))
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestInput_SetGetInteractive(t *testing.T) {
	in := mustArray(t, nil, nil)
	eq(t, in.IsInteractive(), true, "->isInteractive() returns whether the input should be interactive or not")
	in.SetInteractive(false)
	eq(t, in.IsInteractive(), false, "->setInteractive() changes the interactive flag")
}

func TestInput_SetGetStream(t *testing.T) {
	in := mustArray(t, nil, nil)
	stream := strings.NewReader("")
	in.SetStream(stream)
	if in.Stream() != stream {
		t.Error("Stream() must return the stream set")
	}
}

// --- InputArgumentTest ---

func TestInputArgument_Constructor(t *testing.T) {
	eq(t, arg("foo", 0).Name(), "foo", "__construct() takes a name as its first argument")
}

func TestInputArgument_Modes(t *testing.T) {
	eq(t, arg("foo", 0).IsRequired(), false, `__construct() gives a "InputArgument::OPTIONAL" mode by default`)
	eq(t, arg("foo", ArgumentOptional).IsRequired(), false, `__construct() can take "InputArgument::OPTIONAL" as its mode`)
	eq(t, arg("foo", ArgumentRequired).IsRequired(), true, `__construct() can take "InputArgument::REQUIRED" as its mode`)
}

func TestInputArgument_InvalidModes(t *testing.T) {
	_, err := NewInputArgument("foo", -1, "", nil)
	wantKind(t, err, KindInvalidArgument, `Argument mode "-1" is not valid.`)
}

func TestInputArgument_IsArray(t *testing.T) {
	eq(t, arg("foo", ArgumentIsArray).IsArray(), true, "->isArray() returns true if the argument can be an array")
	eq(t, arg("foo", ArgumentOptional|ArgumentIsArray).IsArray(), true, "->isArray() returns true if the argument can be an array")
	eq(t, arg("foo", ArgumentOptional).IsArray(), false, "->isArray() returns false if the argument cannot be an array")
}

func TestInputArgument_GetDescription(t *testing.T) {
	eq(t, MustArgument("foo", 0, "Some description", nil).Description(), "Some description", "->getDescription() return the message description")
}

func TestInputArgument_GetDefault(t *testing.T) {
	eqValue(t, arg("foo", ArgumentOptional, "default").Default(), "default", "->getDefault() return the default value")
}

func TestInputArgument_SetDefault(t *testing.T) {
	a := arg("foo", ArgumentOptional, "default")
	_ = a.SetDefault(nil)
	eqValue(t, a.Default(), nil, "->setDefault() can reset the default value by passing null")
	_ = a.SetDefault("another")
	eqValue(t, a.Default(), "another", "->setDefault() changes the default value")

	a = arg("foo", ArgumentOptional|ArgumentIsArray)
	_ = a.SetDefault([]any{1, 2})
	eqValue(t, a.Default(), []any{1, 2}, "->setDefault() changes the default value")
}

func TestInputArgument_SetDefaultWithRequiredArgument(t *testing.T) {
	wantKind(t, arg("foo", ArgumentRequired).SetDefault("default"), KindLogic, "Cannot set a default value except for InputArgument::OPTIONAL mode.")
}

func TestInputArgument_SetDefaultWithRequiredArrayArgument(t *testing.T) {
	wantKind(t, arg("foo", ArgumentRequired|ArgumentIsArray).SetDefault([]string{}), KindLogic, "Cannot set a default value except for InputArgument::OPTIONAL mode.")
}

func TestInputArgument_SetDefaultWithArrayArgument(t *testing.T) {
	wantKind(t, arg("foo", ArgumentIsArray).SetDefault("default"), KindLogic, "A default value for an array argument must be an array.")
}

// --- InputOptionTest ---

func TestInputOption_Constructor(t *testing.T) {
	eq(t, opt("foo", "", 0).Name(), "foo", "__construct() takes a name as its first argument")
	eq(t, opt("--foo", "", 0).Name(), "foo", "__construct() removes the leading -- of the option name")
}

func TestInputOption_ArrayModeWithoutValue(t *testing.T) {
	_, err := NewInputOption("foo", "f", OptionValueIsArray, "", nil)
	wantKind(t, err, KindInvalidArgument, "Impossible to have an option mode VALUE_IS_ARRAY if the option does not accept a value.")
}

func TestInputOption_BooleanWithRequired(t *testing.T) {
	_, err := NewInputOption("foo", "f", OptionValueRequired|OptionValueNegatable, "", nil)
	wantKind(t, err, KindInvalidArgument, "Impossible to have an option mode VALUE_NEGATABLE if the option also accepts a value.")
}

func TestInputOption_BooleanWithOptional(t *testing.T) {
	_, err := NewInputOption("foo", "f", OptionValueOptional|OptionValueNegatable, "", nil)
	wantKind(t, err, KindInvalidArgument, "Impossible to have an option mode VALUE_NEGATABLE if the option also accepts a value.")
}

func TestInputOption_Shortcut(t *testing.T) {
	eq(t, opt("foo", "f", 0).Shortcut(), "f", "__construct() can take a shortcut as its second argument")
	eq(t, opt("foo", "-f|-ff|fff", 0).Shortcut(), "f|ff|fff", "__construct() removes the leading - of the shortcuts")
	// PHP's array form is implode('|', ...) of the parts.
	eq(t, opt("foo", strings.Join([]string{"f", "ff", "-fff"}, "|"), 0).Shortcut(), "f|ff|fff", "__construct() removes the leading - of the shortcuts")
	eq(t, opt("foo", "", 0).Shortcut(), "", "__construct() makes the shortcut null by default")
	eq(t, opt("foo", strings.Join([]string{"f", "", "fff"}, "|"), 0).Shortcut(), "f|fff", "__construct() removes empty shortcuts")
	eq(t, opt("foo", "f||fff", 0).Shortcut(), "f|fff", "__construct() removes empty shortcuts")
	eq(t, opt("foo", "0", 0).Shortcut(), "0", "-0 is an acceptable shortcut value")
	eq(t, opt("foo", strings.Join([]string{"0", "z"}, "|"), 0).Shortcut(), "0|z", "-0 is an acceptable shortcut value when embedded in an array")
	eq(t, opt("foo", "0|z", 0).Shortcut(), "0|z", "-0 is an acceptable shortcut value when embedded in a string-list")
}

func TestInputOption_Modes(t *testing.T) {
	check := func(o *InputOption, accept, required, optional bool, msg string) {
		t.Helper()
		eq(t, o.AcceptValue(), accept, msg)
		eq(t, o.IsValueRequired(), required, msg)
		eq(t, o.IsValueOptional(), optional, msg)
	}
	check(opt("foo", "f", 0), false, false, false, `__construct() gives a "InputOption::VALUE_NONE" mode by default`)
	check(opt("foo", "f", OptionValueNone), false, false, false, `__construct() can take "InputOption::VALUE_NONE" as its mode`)
	check(opt("foo", "f", OptionValueRequired), true, true, false, `__construct() can take "InputOption::VALUE_REQUIRED" as its mode`)
	check(opt("foo", "f", OptionValueOptional), true, false, true, `__construct() can take "InputOption::VALUE_OPTIONAL" as its mode`)
}

func TestInputOption_InvalidModes(t *testing.T) {
	_, err := NewInputOption("foo", "f", -1, "", nil)
	wantKind(t, err, KindInvalidArgument, `Option mode "-1" is not valid.`)
	_, err = NewInputOption("foo", "f", OptionValueNegatable<<1, "", nil)
	wantKind(t, err, KindInvalidArgument, `Option mode "32" is not valid.`)
}

func TestInputOption_EmptyNameIsInvalid(t *testing.T) {
	_, err := NewInputOption("", "", 0, "", nil)
	wantKind(t, err, KindInvalidArgument, "An option name cannot be empty.")
	_, err = NewInputOption("0", "", 0, "", nil)
	wantKind(t, err, KindInvalidArgument, "An option name cannot be empty.")
}

func TestInputOption_DoubleDashNameIsInvalid(t *testing.T) {
	_, err := NewInputOption("--", "", 0, "", nil)
	wantKind(t, err, KindInvalidArgument, "An option name cannot be empty.")
}

func TestInputOption_SingleDashOptionIsInvalid(t *testing.T) {
	_, err := NewInputOption("foo", "-", 0, "", nil)
	wantKind(t, err, KindInvalidArgument, "An option shortcut cannot be empty.")
}

func TestInputOption_IsArray(t *testing.T) {
	eq(t, opt("foo", "", OptionValueOptional|OptionValueIsArray).IsArray(), true, "->isArray() returns true if the option can be an array")
	eq(t, opt("foo", "", OptionValueNone).IsArray(), false, "->isArray() returns false if the option cannot be an array")
}

func TestInputOption_GetDescription(t *testing.T) {
	eq(t, MustOption("foo", "f", 0, "Some description", nil).Description(), "Some description", "->getDescription() returns the description message")
}

func TestInputOption_GetDefault(t *testing.T) {
	eqValue(t, opt("foo", "", OptionValueOptional, "default").Default(), "default", "->getDefault() returns the default value")
	eqValue(t, opt("foo", "", OptionValueRequired, "default").Default(), "default", "->getDefault() returns the default value")
	eqValue(t, opt("foo", "", OptionValueRequired).Default(), nil, "->getDefault() returns null if no default value is configured")
	eqValue(t, opt("foo", "", OptionValueOptional|OptionValueIsArray).Default(), []string{}, "->getDefault() returns an empty array if option is an array")
	eqValue(t, opt("foo", "", OptionValueNone).Default(), false, "->getDefault() returns false if the option does not take a value")
}

func TestInputOption_SetDefault(t *testing.T) {
	o := opt("foo", "", OptionValueRequired, "default")
	_ = o.SetDefault(nil)
	eqValue(t, o.Default(), nil, "->setDefault() can reset the default value by passing null")
	_ = o.SetDefault("another")
	eqValue(t, o.Default(), "another", "->setDefault() changes the default value")

	o = opt("foo", "", OptionValueRequired|OptionValueIsArray)
	_ = o.SetDefault([]any{1, 2})
	eqValue(t, o.Default(), []any{1, 2}, "->setDefault() changes the default value")
}

func TestInputOption_DefaultValueWithValueNoneMode(t *testing.T) {
	wantKind(t, opt("foo", "f", OptionValueNone).SetDefault("default"), KindLogic, "Cannot set a default value when using InputOption::VALUE_NONE mode.")
}

func TestInputOption_DefaultValueWithIsArrayMode(t *testing.T) {
	wantKind(t, opt("foo", "f", OptionValueOptional|OptionValueIsArray).SetDefault("default"), KindLogic, "A default value for an array option must be an array.")
}

func TestInputOption_Equals(t *testing.T) {
	eq(t, MustOption("foo", "f", 0, "Some description", nil).Equals(MustOption("foo", "f", 0, "Alternative description", nil)), true)
	eq(t, MustOption("foo", "f", OptionValueOptional, "Some description", nil).Equals(MustOption("foo", "f", OptionValueOptional, "Some description", true)), false)
	eq(t, MustOption("foo", "f", 0, "Some description", nil).Equals(MustOption("bar", "f", 0, "Some description", nil)), false)
	eq(t, MustOption("foo", "f", 0, "Some description", nil).Equals(MustOption("foo", "", 0, "Some description", nil)), false)
	eq(t, MustOption("foo", "f", 0, "Some description", nil).Equals(MustOption("foo", "f", OptionValueOptional, "Some description", nil)), false)
}

// Composer's InputOption/InputArgument suggested-values backport.
func TestInputOption_SuggestedValues(t *testing.T) {
	_, err := opt("foo", "", OptionValueNone).WithSuggestedValues("a")
	wantKind(t, err, KindLogic, "Cannot set suggested values if the option does not accept a value.")
	o, err := opt("foo", "", OptionValueRequired).WithSuggestedValues("a", "b")
	if err != nil || !o.HasCompletion() {
		t.Fatalf("WithSuggestedValues: %v", err)
	}
	s := &CompletionSuggestions{}
	o.Complete(nil, s)
	eq(t, fmt.Sprint(s.ValueSuggestions()), "[a b]")

	a := arg("bar", 0).WithSuggestFunc(func(*CompletionInput, *CompletionSuggestions) []Suggestion { return []Suggestion{{"x"}} })
	s = &CompletionSuggestions{}
	a.Complete(nil, s)
	eq(t, fmt.Sprint(s.ValueSuggestions()), "[x]")
}

// --- InputDefinitionTest ---

func argFixtures() (foo, bar, foo1, foo2 *InputArgument) {
	return arg("foo", 0), arg("bar", 0), arg("foo", 0), arg("foo2", ArgumentRequired)
}

func optFixtures() (foo, bar, foo1, foo2, multi *InputOption) {
	return opt("foo", "f", 0), opt("bar", "b", 0), opt("fooBis", "f", 0), opt("foo", "p", 0), opt("multi", "m|mm|mmm", 0)
}

func argNames(d *InputDefinition) string {
	var n []string
	for _, a := range d.Arguments() {
		n = append(n, a.Name())
	}

	return strings.Join(n, ",")
}

func optNames(d *InputDefinition) string {
	var n []string
	for _, o := range d.Options() {
		n = append(n, o.Name()+"/"+o.Shortcut())
	}

	return strings.Join(n, ",")
}

func TestInputDefinition_ConstructorArguments(t *testing.T) {
	foo, bar, _, _ := argFixtures()
	eq(t, argNames(MustDefinition()), "", "__construct() creates a new InputDefinition object")
	d := MustDefinition(foo, bar)
	eq(t, argNames(d), "foo,bar", "__construct() takes an array of InputArgument objects as its first argument")
	if d.Arguments()[0] != foo {
		t.Error("same argument expected")
	}
}

func TestInputDefinition_ConstructorOptions(t *testing.T) {
	foo, bar, _, _, _ := optFixtures()
	eq(t, optNames(MustDefinition()), "")
	eq(t, optNames(MustDefinition(foo, bar)), "foo/f,bar/b")
}

func TestInputDefinition_SetArguments(t *testing.T) {
	foo, bar, _, _ := argFixtures()
	d := MustDefinition()
	_ = d.SetArguments(foo)
	eq(t, argNames(d), "foo", "->setArguments() sets the array of InputArgument objects")
	_ = d.SetArguments(bar)
	eq(t, argNames(d), "bar", "->setArguments() clears all InputArgument objects")
}

func TestInputDefinition_AddArguments(t *testing.T) {
	foo, bar, _, _ := argFixtures()
	d := MustDefinition()
	_ = d.AddArguments(foo)
	eq(t, argNames(d), "foo")
	_ = d.AddArguments(bar)
	eq(t, argNames(d), "foo,bar", "->addArguments() does not clear existing InputArgument objects")
}

func TestInputDefinition_AddArgument(t *testing.T) {
	foo, bar, _, _ := argFixtures()
	d := MustDefinition()
	_ = d.AddArgument(foo)
	eq(t, argNames(d), "foo")
	_ = d.AddArgument(bar)
	eq(t, argNames(d), "foo,bar")
}

func TestInputDefinition_ArgumentsMustHaveDifferentNames(t *testing.T) {
	foo, _, foo1, _ := argFixtures()
	d := MustDefinition()
	_ = d.AddArgument(foo)
	wantKind(t, d.AddArgument(foo1), KindLogic, `An argument with name "foo" already exists.`)
}

func TestInputDefinition_ArrayArgumentHasToBeLast(t *testing.T) {
	d := MustDefinition()
	_ = d.AddArgument(arg("fooarray", ArgumentIsArray))
	wantKind(t, d.AddArgument(arg("anotherbar", 0)), KindLogic, `Cannot add a required argument "anotherbar" after an array argument "fooarray".`)
}

func TestInputDefinition_RequiredArgumentCannotFollowAnOptionalOne(t *testing.T) {
	foo, _, _, foo2 := argFixtures()
	d := MustDefinition()
	_ = d.AddArgument(foo)
	wantKind(t, d.AddArgument(foo2), KindLogic, `Cannot add a required argument "foo2" after an optional one "foo".`)
}

func TestInputDefinition_GetArgument(t *testing.T) {
	foo, _, _, _ := argFixtures()
	d := MustDefinition()
	_ = d.AddArguments(foo)
	got, err := d.Argument("foo")
	if err != nil || got != foo {
		t.Fatal("->getArgument() returns a InputArgument by its name")
	}
}

func TestInputDefinition_GetInvalidArgument(t *testing.T) {
	foo, _, _, _ := argFixtures()
	d := MustDefinition(foo)
	_, err := d.Argument("bar")
	wantKind(t, err, KindInvalidArgument, `The "bar" argument does not exist.`)
}

func TestInputDefinition_HasArgument(t *testing.T) {
	foo, _, _, _ := argFixtures()
	d := MustDefinition(foo)
	eq(t, d.HasArgument("foo"), true)
	eq(t, d.HasArgument("bar"), false)
}

func TestInputDefinition_GetArgumentRequiredCount(t *testing.T) {
	foo, _, _, foo2 := argFixtures()
	d := MustDefinition()
	_ = d.AddArgument(foo2)
	eq(t, d.ArgumentRequiredCount(), 1)
	_ = d.AddArgument(foo)
	eq(t, d.ArgumentRequiredCount(), 1)
}

func TestInputDefinition_GetArgumentCount(t *testing.T) {
	foo, _, _, foo2 := argFixtures()
	d := MustDefinition()
	_ = d.AddArgument(foo2)
	eq(t, d.ArgumentCount(), 1)
	_ = d.AddArgument(foo)
	eq(t, d.ArgumentCount(), 2)
	_ = d.AddArgument(arg("rest", ArgumentIsArray))
	eq(t, d.ArgumentCount(), 1<<63-1)
}

func TestInputDefinition_GetArgumentDefaults(t *testing.T) {
	d := MustDefinition(arg("foo1", ArgumentOptional), arg("foo2", ArgumentOptional, "default"), arg("foo3", ArgumentOptional|ArgumentIsArray))
	eqValues(t, d.ArgumentDefaults(), nvs("foo1", nil, "foo2", "default", "foo3", []string{}), "->getArgumentDefaults() return the default values for each argument")
	d = MustDefinition(arg("foo4", ArgumentOptional|ArgumentIsArray, []any{1, 2}))
	eqValues(t, d.ArgumentDefaults(), nvs("foo4", []any{1, 2}), "->getArgumentDefaults() return the default values for each argument")
}

func TestInputDefinition_SetOptions(t *testing.T) {
	foo, bar, _, _, _ := optFixtures()
	d := MustDefinition(foo)
	eq(t, optNames(d), "foo/f")
	_ = d.SetOptions(bar)
	eq(t, optNames(d), "bar/b", "->setOptions() clears all InputOption objects")
}

func TestInputDefinition_SetOptionsClearsOptions(t *testing.T) {
	foo, bar, _, _, _ := optFixtures()
	d := MustDefinition(foo)
	_ = d.SetOptions(bar)
	_, err := d.OptionForShortcut("f")
	wantKind(t, err, KindInvalidArgument, `The "-f" option does not exist.`)
}

func TestInputDefinition_AddOptions(t *testing.T) {
	foo, bar, _, _, _ := optFixtures()
	d := MustDefinition(foo)
	_ = d.AddOptions(bar)
	eq(t, optNames(d), "foo/f,bar/b", "->addOptions() does not clear existing InputOption objects")
}

func TestInputDefinition_AddOption(t *testing.T) {
	foo, bar, _, _, _ := optFixtures()
	d := MustDefinition()
	_ = d.AddOption(foo)
	eq(t, optNames(d), "foo/f")
	_ = d.AddOption(bar)
	eq(t, optNames(d), "foo/f,bar/b")
}

func TestInputDefinition_AddDuplicateOption(t *testing.T) {
	foo, _, _, foo2, _ := optFixtures()
	d := MustDefinition()
	_ = d.AddOption(foo)
	wantKind(t, d.AddOption(foo2), KindLogic, `An option named "foo" already exists.`)
}

func TestInputDefinition_AddDuplicateNegatedOption(t *testing.T) {
	d := MustDefinition()
	_ = d.AddOption(opt("no-foo", "", 0))
	wantKind(t, d.AddOption(opt("foo", "", OptionValueNegatable)), KindLogic, `An option named "no-foo" already exists.`)
}

func TestInputDefinition_AddDuplicateNegatedReverseOption(t *testing.T) {
	d := MustDefinition()
	_ = d.AddOption(opt("foo", "", OptionValueNegatable))
	wantKind(t, d.AddOption(opt("no-foo", "", 0)), KindLogic, `An option named "no-foo" already exists.`)
}

func TestInputDefinition_AddDuplicateShortcutOption(t *testing.T) {
	foo, _, foo1, _, _ := optFixtures()
	d := MustDefinition()
	_ = d.AddOption(foo)
	wantKind(t, d.AddOption(foo1), KindLogic, `An option with shortcut "f" already exists.`)
}

func TestInputDefinition_GetOption(t *testing.T) {
	foo, _, _, _, _ := optFixtures()
	got, err := MustDefinition(foo).Option("foo")
	if err != nil || got != foo {
		t.Fatal("->getOption() returns a InputOption by its name")
	}
}

func TestInputDefinition_GetInvalidOption(t *testing.T) {
	foo, _, _, _, _ := optFixtures()
	_, err := MustDefinition(foo).Option("bar")
	wantKind(t, err, KindInvalidArgument, `The "--bar" option does not exist.`)
}

func TestInputDefinition_HasOption(t *testing.T) {
	foo, _, _, _, _ := optFixtures()
	d := MustDefinition(foo)
	eq(t, d.HasOption("foo"), true)
	eq(t, d.HasOption("bar"), false)
}

func TestInputDefinition_HasShortcut(t *testing.T) {
	foo, _, _, _, _ := optFixtures()
	d := MustDefinition(foo)
	eq(t, d.HasShortcut("f"), true)
	eq(t, d.HasShortcut("b"), false)
	// "0" is a falsy shortcut: PHP's addOption() does not register it.
	eq(t, MustDefinition(opt("zero", "0", 0)).HasShortcut("0"), false)
}

func TestInputDefinition_GetOptionForShortcut(t *testing.T) {
	foo, _, _, _, _ := optFixtures()
	got, err := MustDefinition(foo).OptionForShortcut("f")
	if err != nil || got != foo {
		t.Fatal("->getOptionForShortcut() returns a InputOption by its shortcut")
	}
}

func TestInputDefinition_GetOptionForMultiShortcut(t *testing.T) {
	_, _, _, _, multi := optFixtures()
	d := MustDefinition(multi)
	for _, s := range []string{"m", "mmm"} {
		got, err := d.OptionForShortcut(s)
		if err != nil || got != multi {
			t.Fatalf("->getOptionForShortcut(%s) returns a InputOption by its shortcut", s)
		}
	}
}

func TestInputDefinition_GetOptionForInvalidShortcut(t *testing.T) {
	foo, _, _, _, _ := optFixtures()
	_, err := MustDefinition(foo).OptionForShortcut("l")
	wantKind(t, err, KindInvalidArgument, `The "-l" option does not exist.`)
}

func TestInputDefinition_GetOptionDefaults(t *testing.T) {
	d := MustDefinition(
		opt("foo1", "", OptionValueNone),
		opt("foo2", "", OptionValueRequired),
		opt("foo3", "", OptionValueRequired, "default"),
		opt("foo4", "", OptionValueOptional),
		opt("foo5", "", OptionValueOptional, "default"),
		opt("foo6", "", OptionValueOptional|OptionValueIsArray),
		opt("foo7", "", OptionValueOptional|OptionValueIsArray, []any{1, 2}),
	)
	eqValues(t, d.OptionDefaults(), nvs("foo1", false, "foo2", nil, "foo3", "default", "foo4", nil, "foo5", "default", "foo6", []string{}, "foo7", []any{1, 2}),
		"->getOptionDefaults() returns the default values for all options")
}

func TestInputDefinition_GetSynopsis(t *testing.T) {
	for _, c := range []struct {
		def      *InputDefinition
		expected string
		message  string
	}{
		{MustDefinition(opt("foo", "", 0)), "[--foo]", "puts optional options in square brackets"},
		{MustDefinition(opt("foo", "f", 0)), "[-f|--foo]", "separates shortcut with a pipe"},
		{MustDefinition(opt("foo", "f", OptionValueRequired)), "[-f|--foo FOO]", "uses shortcut as value placeholder"},
		{MustDefinition(opt("foo", "f", OptionValueOptional)), "[-f|--foo [FOO]]", "puts optional values in square brackets"},
		{MustDefinition(arg("foo", ArgumentRequired)), "<foo>", "puts arguments in angle brackets"},
		{MustDefinition(arg("foo", 0)), "[<foo>]", "puts optional arguments in square brackets"},
		{MustDefinition(arg("foo", 0), arg("bar", 0)), "[<foo> [<bar>]]", "chains optional arguments inside brackets"},
		{MustDefinition(arg("foo", ArgumentIsArray)), "[<foo>...]", "uses an ellipsis for array arguments"},
		{MustDefinition(arg("foo", ArgumentRequired|ArgumentIsArray)), "<foo>...", "uses an ellipsis for required array arguments"},
		{MustDefinition(opt("foo", "", 0), arg("foo", ArgumentRequired)), "[--foo] [--] <foo>", "puts [--] between options and arguments"},
		{MustDefinition(opt("foo", "", OptionValueNegatable)), "[--foo|--no-foo]", "shows negations"},
		{MustDefinition(opt("foo", "0", 0)), "[--foo]", "hides the falsy shortcut 0"},
	} {
		eq(t, c.def.Synopsis(false), c.expected, "->getSynopsis() "+c.message)
	}
}

func TestInputDefinition_GetShortSynopsis(t *testing.T) {
	d := MustDefinition(opt("foo", "", 0), opt("bar", "", 0), arg("cat", 0))
	eq(t, d.Synopsis(true), "[options] [--] [<cat>]", "->getSynopsis(true) groups options in [options]")
}

func TestArgvInput_InvalidUTF8ShortOptionSet(t *testing.T) {
	in := mustArgv(t, "cli.php", "-f\xff")
	wantKind(t, in.Bind(MustDefinition(opt("foo", "f", OptionValueNone))), KindRuntime, "The \"-\xff\" option does not exist.")
	// mb_substr() is given the byte offset as a character offset.
	in = mustArgv(t, "cli.php", "-ffé")
	wantKind(t, in.Bind(MustDefinition(opt("foo", "f", OptionValueNone))), KindRuntime, `The "-é" option does not exist.`)
}

func TestArgvInput_ToStringRejectsNUL(t *testing.T) {
	in := mustArgv(t, "cli.php", "a\x00b")
	wantKind(t, catchPanic(t, func() { _ = in.String() }), KindValueError, "escapeshellarg(): Argument #1 ($arg) must not contain any null bytes")
}
