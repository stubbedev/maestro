// Ports Tests/Descriptor/{AbstractDescriptorTestCase,ObjectsProvider,
// TextDescriptorTest,JsonDescriptorTest,XmlDescriptorTest,
// MarkdownDescriptorTest,ApplicationDescriptionTest}.php,
// Tests/Helper/DescriptorHelperTest.php and the Descriptor* fixtures
// (symfony/console), plus the descriptor oracle goldens.

package console

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// Fixtures: Tests/Fixtures/Descriptor*.php.

func descCommand1() *Command {
	return NewCommand("descriptor:command1").
		SetAliases("alias1", "alias2").
		SetDescription("command 1 description").
		SetHelp("command 1 help")
}

func descCommand2() *Command {
	return NewCommand("descriptor:command2").
		SetDescription("command 2 description").
		SetHelp("command 2 help").
		AddUsage("-o|--option_name <argument_name>").
		AddUsage("<argument_name>").
		AddArgument("argument_name", ArgumentRequired, "", nil).
		AddOption("option_name", "o", OptionValueNone, "", nil)
}

func descCommand3() *Command {
	return NewCommand("descriptor:command3").
		SetDescription("command 3 description").
		SetHelp("command 3 help").
		SetHidden(true)
}

func descCommand4() *Command {
	return NewCommand("descriptor:command4").
		SetAliases("descriptor:alias_command4", "command4:descriptor")
}

func descCommandMbString() *Command {
	return NewCommand("descriptor:åèä").
		SetDescription("command åèä description").
		SetHelp("command åèä help").
		AddUsage("-o|--option_name <argument_name>").
		AddUsage("<argument_name>").
		AddArgument("argument_åèä", ArgumentRequired, "", nil).
		AddOption("option_åèä", "o", OptionValueNone, "", nil)
}

// descCommandExtra mirrors commandExtra() of tools/oracle/console/descriptor.php.
func descCommandExtra() *Command {
	c := NewCommand("extra:cmd").
		SetDescription("multi\nline & <special> \"chars\"").
		SetHelp("Help for %command.name%:\n  %command.full_name% --x\r\n\ttab").
		AddArgument("list", ArgumentIsArray, "list\n  description", []string{"a", "b<c>"}).
		AddOption("neg", "", OptionValueNegatable, "negatable", nil).
		AddOption("num", "N|M", OptionValueOptional, "number", 1.5).
		AddOption("int", "", OptionValueRequired, "int", 42).
		AddOption("t", "", OptionValueOptional, "bool default", true).
		AddOption("arr", "A", OptionValueRequired|OptionValueIsArray, "arr", []string{"x/y", "é"}).
		AddUsage("--neg extra")
	for _, u := range []string{"a &amp; b <c> &lt; &#65;&#x42;", "x & y", "p &foo; q", "t &amp", "a & b; c", "a &b c; d", "q &#xZZ; r", "q &#0; r", "&lt", "a &; b", "&amp;amp;", "x &#65 y", "&quot;&apos;"} {
		c.AddUsage(u)
	}

	return c
}

func descApp(t *testing.T, name, version string, commands ...*Command) *Application {
	t.Helper()
	app := NewApplication(name, version)
	for _, c := range commands {
		if _, err := app.Add(c); err != nil {
			t.Fatal(err)
		}
	}

	return app
}

func descApplication1(t *testing.T) *Application { return descApp(t, "UNKNOWN", "UNKNOWN") }

func descApplication2(t *testing.T) *Application {
	return descApp(t, "My Symfony application", "v1.0", descCommand1(), descCommand2(), descCommand3(), descCommand4())
}

func descApplicationMbString(t *testing.T) *Application {
	return descApp(t, "MbString åpplicätion", "UNKNOWN", descCommandMbString())
}

// ObjectsProvider.

type descObject struct {
	name   string
	object func(t *testing.T) any
}

func descInputArguments() []descObject {
	return []descObject{
		{"input_argument_1", func(*testing.T) any { return MustArgument("argument_name", ArgumentRequired, "", nil) }},
		{"input_argument_2", func(*testing.T) any {
			return MustArgument("argument_name", ArgumentIsArray, "argument description", nil)
		}},
		{"input_argument_3", func(*testing.T) any {
			return MustArgument("argument_name", ArgumentOptional, "argument description", "default_value")
		}},
		{"input_argument_4", func(*testing.T) any {
			return MustArgument("argument_name", ArgumentRequired, "multiline\nargument description", nil)
		}},
		{"input_argument_with_style", func(*testing.T) any {
			return MustArgument("argument_name", ArgumentOptional, "argument description", "<comment>style</>")
		}},
		{"input_argument_with_default_inf_value", func(*testing.T) any {
			return MustArgument("argument_name", ArgumentOptional, "argument description", math.Inf(1))
		}},
	}
}

func descInputOptions() []descObject {
	return []descObject{
		{"input_option_1", func(*testing.T) any { return MustOption("option_name", "o", OptionValueNone, "", nil) }},
		{"input_option_2", func(*testing.T) any {
			return MustOption("option_name", "o", OptionValueOptional, "option description", "default_value")
		}},
		{"input_option_3", func(*testing.T) any {
			return MustOption("option_name", "o", OptionValueRequired, "option description", nil)
		}},
		{"input_option_4", func(*testing.T) any {
			return MustOption("option_name", "o", OptionValueIsArray|OptionValueOptional, "option description", []string{})
		}},
		{"input_option_5", func(*testing.T) any {
			return MustOption("option_name", "o", OptionValueRequired, "multiline\noption description", nil)
		}},
		{"input_option_6", func(*testing.T) any {
			return MustOption("option_name", "o|O", OptionValueRequired, "option with multiple shortcuts", nil)
		}},
		{"input_option_with_style", func(*testing.T) any {
			return MustOption("option_name", "o", OptionValueRequired, "option description", "<comment>style</>")
		}},
		{"input_option_with_style_array", func(*testing.T) any {
			return MustOption("option_name", "o", OptionValueIsArray|OptionValueRequired, "option description", []string{"<comment>Hello</comment>", "<info>world</info>"})
		}},
		{"input_option_with_default_inf_value", func(*testing.T) any {
			return MustOption("option_name", "o", OptionValueOptional, "option description", math.Inf(1))
		}},
	}
}

func descInputDefinitions() []descObject {
	return []descObject{
		{"input_definition_1", func(*testing.T) any { return MustDefinition() }},
		{"input_definition_2", func(*testing.T) any {
			return MustDefinition(MustArgument("argument_name", ArgumentRequired, "", nil))
		}},
		{"input_definition_3", func(*testing.T) any {
			return MustDefinition(MustOption("option_name", "o", OptionValueNone, "", nil))
		}},
		{"input_definition_4", func(*testing.T) any {
			return MustDefinition(
				MustArgument("argument_name", ArgumentRequired, "", nil),
				MustOption("option_name", "o", OptionValueNone, "", nil),
			)
		}},
	}
}

func descCommands(withMbString bool) []descObject {
	out := []descObject{
		{"command_1", func(*testing.T) any { return descCommand1() }},
		{"command_2", func(*testing.T) any { return descCommand2() }},
	}
	if withMbString {
		out = append(out, descObject{"command_mbstring", func(*testing.T) any { return descCommandMbString() }})
	}

	return out
}

func descApplications(withMbString bool) []descObject {
	out := []descObject{
		{"application_1", func(t *testing.T) any { return descApplication1(t) }},
		{"application_2", func(t *testing.T) any { return descApplication2(t) }},
	}
	if withMbString {
		out = append(out, descObject{"application_mbstring", func(t *testing.T) any { return descApplicationMbString(t) }})
	}

	return out
}

// normalizeDescription is AbstractDescriptorTestCase::normalizeOutput().
func normalizeDescription(s string) string {
	self := phpSelf()
	full := phpRealpath(self)
	shell := ""
	if v, ok := os.LookupEnv("SHELL"); ok {
		shell = php.Basename(v, "")
	}
	s = strings.NewReplacer("%%PHP_SELF%%", self, "%%PHP_SELF_FULL%%", full, "%%COMMAND_NAME%%", php.Basename(self, ""), "%%SHELL%%", shell).Replace(s)

	return php.Trim(s)
}

func describeToString(t *testing.T, d Descriptor, object any, options DescriptorOptions) string {
	t.Helper()
	out := NewBufferedOutput(VerbosityNormal, true, nil)
	if options.RawOutput == nil {
		options.RawOutput = new(true)
	}
	if err := d.Describe(out, object, options); err != nil {
		t.Fatal(err)
	}

	return out.Fetch()
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "Fixtures", name))
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// normalizeJSONDescription is JsonDescriptorTest::normalizeOutput().
func normalizeJSONDescription(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("invalid json %q: %v", s, err)
	}

	var walk func(v any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				x[k] = walk(e)
			}
		case []any:
			for i, e := range x {
				x[i] = walk(e)
			}
		case string:
			return normalizeDescription(x)
		}

		return v
	}

	return walk(v)
}

// looseJSONEqual is PHPUnit's assertEquals() on decoded JSON: maps compare
// regardless of key order and scalars compare loosely (null == false).
func looseJSONEqual(want, have any, path string) (string, bool) {
	// json_decode($s, true) turns both [] and {} into an empty array.
	if isEmptyJSONContainer(want) && isEmptyJSONContainer(have) {
		return "", true
	}
	switch w := want.(type) {
	case map[string]any:
		h, ok := have.(map[string]any)
		if !ok || len(h) != len(w) {
			return path, false
		}
		for k, v := range w {
			if p, ok := looseJSONEqual(v, h[k], path+"."+k); !ok {
				return p, false
			}
		}

		return "", true
	case []any:
		h, ok := have.([]any)
		if !ok || len(h) != len(w) {
			return path, false
		}
		for i := range w {
			if p, ok := looseJSONEqual(w[i], h[i], path+"["+strconv.Itoa(i)+"]"); !ok {
				return p, false
			}
		}

		return "", true
	}
	switch have.(type) {
	case map[string]any, []any:
		return path, false
	}
	if want == nil || have == nil {
		return path, php.LooseEquals(want, have)
	}

	return path, reflect.DeepEqual(want, have) || php.LooseEquals(want, have)
}

func isEmptyJSONContainer(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		return len(x) == 0
	case []any:
		return len(x) == 0
	}

	return false
}

func runDescriptorCases(t *testing.T, d Descriptor, format string, objects []descObject) {
	t.Helper()
	for _, o := range objects {
		t.Run(o.name, func(t *testing.T) {
			expected := readFixture(t, o.name+"."+format)
			got := describeToString(t, d, o.object(t), DescriptorOptions{})
			if format == "json" {
				if path, ok := looseJSONEqual(normalizeJSONDescription(t, expected), normalizeJSONDescription(t, got), ""); !ok {
					t.Errorf("json mismatch at %s:\n%s", path, got)
				}

				return
			}
			if want, have := normalizeDescription(expected), normalizeDescription(got); want != have {
				t.Errorf("mismatch:\nwant %q\ngot  %q", want, have)
			}
		})
	}
}

func runAbstractDescriptorTests(t *testing.T, d Descriptor, format string, withMbString bool) {
	t.Run("DescribeInputArgument", func(t *testing.T) { runDescriptorCases(t, d, format, descInputArguments()) })
	t.Run("DescribeInputOption", func(t *testing.T) { runDescriptorCases(t, d, format, descInputOptions()) })
	t.Run("DescribeInputDefinition", func(t *testing.T) { runDescriptorCases(t, d, format, descInputDefinitions()) })
	t.Run("DescribeCommand", func(t *testing.T) { runDescriptorCases(t, d, format, descCommands(withMbString)) })
	t.Run("DescribeApplication", func(t *testing.T) { runDescriptorCases(t, d, format, descApplications(withMbString)) })
}

func TestTextDescriptor(t *testing.T) {
	runAbstractDescriptorTests(t, &TextDescriptor{}, "txt", true)
}

func TestTextDescriptor_DescribeApplicationWithFilteredNamespace(t *testing.T) {
	got := describeToString(t, &TextDescriptor{}, descApplication2(t), DescriptorOptions{Namespace: "command4"})
	if want, have := normalizeDescription(readFixture(t, "application_filtered_namespace.txt")), normalizeDescription(got); want != have {
		t.Errorf("mismatch:\nwant %q\ngot  %q", want, have)
	}
}

func TestJsonDescriptor(t *testing.T) {
	runAbstractDescriptorTests(t, &JSONDescriptor{}, "json", false)
}

func TestXmlDescriptor(t *testing.T) {
	runAbstractDescriptorTests(t, &XMLDescriptor{}, "xml", false)
}

func TestMarkdownDescriptor(t *testing.T) {
	runAbstractDescriptorTests(t, &MarkdownDescriptor{}, "md", true)
}

func TestDescriptorHelper_GetFormats(t *testing.T) {
	if got := NewDescriptorHelper().Formats(); !slices.Equal(got, []string{"txt", "xml", "json", "md"}) {
		t.Errorf("Formats() = %v", got)
	}
}

func TestDescriptorHelper_UnsupportedFormat(t *testing.T) {
	err := NewDescriptorHelper().Describe(NewBufferedOutput(0, false, nil), MustDefinition(), DescriptorOptions{Format: "yaml"})
	if err == nil || err.Error() != `Unsupported format "yaml".` {
		t.Errorf("err = %v", err)
	}
}

func TestDescriptor_NotDescribable(t *testing.T) {
	err := (&TextDescriptor{}).Describe(NewBufferedOutput(0, false, nil), 42, DescriptorOptions{})
	if err == nil || err.Error() != `Object of type "int" is not describable.` {
		t.Errorf("err = %v", err)
	}
}

func TestApplicationDescription_GetNamespaces(t *testing.T) {
	cases := []struct {
		expected []string
		names    []string
	}{
		{[]string{"_global"}, []string{"foobar"}},
		{[]string{"a", "b"}, []string{"b:foo", "a:foo", "b:bar"}},
		{[]string{"_global", "22", "33", "b", "z"}, []string{"z:foo", "1", "33:foo", "b:foo", "22:foo:bar"}},
	}
	for _, c := range cases {
		app := NewApplication("UNKNOWN", "UNKNOWN")
		app.SetImpl(noDefaultCommandsApp{})
		for _, name := range c.names {
			if _, err := app.Add(NewCommand(name)); err != nil {
				t.Fatal(err)
			}
		}
		namespaces, err := NewApplicationDescription(app, "", false).Namespaces()
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, len(namespaces))
		for i, ns := range namespaces {
			ids[i] = ns.ID
		}
		if !slices.Equal(ids, c.expected) {
			t.Errorf("%v: namespaces = %v, want %v", c.names, ids, c.expected)
		}
	}
}

// noDefaultCommandsApp is ApplicationDescriptionTest's TestApplication.
type noDefaultCommandsApp struct{}

func (noDefaultCommandsApp) DefaultCommands() []Commander { return nil }

// Oracle: tools/oracle/console/descriptor.php.

type descriptorOracleCase struct {
	Object    string  `json:"object"`
	Format    string  `json:"format"`
	Options   string  `json:"options"`
	Decorated bool    `json:"decorated"`
	Output    *string `json:"output"`
	Error     *string `json:"error"`
}

func descOracleObjects() map[string]func(t *testing.T) any {
	m := map[string]func(t *testing.T) any{}
	for _, list := range [][]descObject{descInputArguments(), descInputOptions(), descInputDefinitions(), descCommands(true), descApplications(true)} {
		for _, o := range list {
			m[o.name] = o.object
		}
	}
	m["input_argument_false"] = func(*testing.T) any { return MustArgument("argument_name", ArgumentOptional, "d", false) }
	m["input_argument_zero"] = func(*testing.T) any { return MustArgument("argument_name", ArgumentOptional, "d", "0") }
	m["input_option_negatable"] = func(*testing.T) any {
		return MustOption("option_name", "", OptionValueNegatable, "nega\ttable & <x>", nil)
	}
	m["input_option_float"] = func(*testing.T) any { return MustOption("option_name", "o", OptionValueOptional, "f", 0.1) }
	m["command_extra"] = func(*testing.T) any { return descCommandExtra() }
	m["application_extra"] = func(t *testing.T) any {
		return descApp(t, "Extra", "UNKNOWN", descCommandExtra(), descCommand4(),
			NewCommand("1").SetDescription("one"), NewCommand("22:foo"))
	}

	return m
}

func TestOracle_Descriptor(t *testing.T) {
	setOracleEnv(t)
	t.Setenv("COLUMNS", "80")
	phpSelf() // initialize once before overriding
	prevScript := ScriptName
	ScriptName = "/nonexistent/bin/composer"
	defer func() { ScriptName = prevScript }()
	prevArgs := os.Args
	os.Args = append([]string{"/nonexistent/bin/composer"}, os.Args[1:]...)
	defer func() { os.Args = prevArgs }()

	var cases []descriptorOracleCase
	loadOracle(t, "descriptor.json.gz", &cases)
	objects := descOracleObjects()
	helper := NewDescriptorHelper()

	for _, c := range cases {
		factory, ok := objects[c.Object]
		if !ok {
			t.Fatalf("unknown object %q", c.Object)
		}
		options := DescriptorOptions{Format: c.Format}
		switch c.Options {
		case "short":
			options.Short = true
		case "namespace":
			options.Namespace = "descriptor"
		case "raw_text":
			options.RawText = true
		case "pretty":
			options.JSONEncoding = php.JSONPrettyPrint
		}
		out := NewBufferedOutput(VerbosityNormal, c.Decorated, nil)
		err := helper.Describe(out, factory(t), options)
		got := out.Fetch()
		name := c.Object + "." + c.Format + "/" + c.Options
		if c.Decorated {
			name += "/decorated"
		}
		switch {
		case c.Error != nil:
			if err == nil || err.Error() != *c.Error {
				t.Errorf("%s: want error %q, got %v", name, *c.Error, err)
			}
		case err != nil:
			t.Errorf("%s: unexpected error %v", name, err)
		case got != *c.Output:
			t.Errorf("%s: %s", name, firstDifference(*c.Output, got))
		}
	}
}

// firstDifference describes where two strings start to differ.
func firstDifference(want, got string) string {
	i := 0
	for i < len(want) && i < len(got) && want[i] == got[i] {
		i++
	}
	from := max(0, i-80)

	return "at byte " + strconv.Itoa(i) + ":\nwant …" + strconv.Quote(want[from:min(len(want), i+80)]) + "\ngot  …" + strconv.Quote(got[from:min(len(got), i+80)])
}
