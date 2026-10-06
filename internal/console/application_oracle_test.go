package console

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/testutil"
)

// ostr is an oracle string: a JSON string, or {"b64": ...} for bytes that
// are not valid UTF-8.
type ostr string

func (s *ostr) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '{' {
		var o struct{ B64 string }
		if err := json.Unmarshal(b, &o); err != nil {
			return err
		}
		d, err := base64.StdEncoding.DecodeString(o.B64)
		*s = ostr(d)

		return err
	}
	var v string
	err := json.Unmarshal(b, &v)
	*s = ostr(v)

	return err
}

type applicationOracle struct {
	Exceptions []struct {
		Class    string
		Message  ostr
		Code     int
		File     string
		Line     int
		Previous *int
	}
	Render []struct {
		Exception int
		Columns   int
		Verbosity int
		Decorated bool
		Output    ostr
		Error     *string
	}
	Find []struct {
		Set                   string
		Columns               int
		Name                  ostr
		Result                *string
		Error                 *string
		Message               ostr
		Alternatives          []string
		Namespace             *string
		NamespaceError        *ostr
		NamespaceAlternatives []string
	}
	Namespaces map[string][]string
	Run        []struct {
		Argv           []string
		Decorated      bool
		Code           int
		Stdout, Stderr ostr
		ShellVerbosity string
	}
}

var consoleExceptionKinds = map[string]Kind{
	`Symfony\Component\Console\Exception\CommandNotFoundException`:   KindCommandNotFound,
	`Symfony\Component\Console\Exception\NamespaceNotFoundException`: KindNamespaceNotFound,
	`Symfony\Component\Console\Exception\RuntimeException`:           KindRuntime,
	`Symfony\Component\Console\Exception\InvalidArgumentException`:   KindInvalidArgument,
	`Symfony\Component\Console\Exception\LogicException`:             KindLogic,
}

func TestOracle_ApplicationRenderThrowable(t *testing.T) {
	setOracleEnv(t)
	var o applicationOracle
	loadOracle(t, "application.json", &o)

	exceptions := make([]error, len(o.Exceptions))
	for i, e := range o.Exceptions {
		var prev error
		if e.Previous != nil {
			prev = exceptions[*e.Previous]
		}
		if kind, ok := consoleExceptionKinds[e.Class]; ok {
			exceptions[i] = &Error{Kind: kind, Message: string(e.Message), File: e.File, Line: e.Line, Prev: prev}

			continue
		}
		exceptions[i] = &testException{class: e.Class, message: string(e.Message), file: e.File, line: e.Line, code: e.Code, prev: prev}
	}

	// How errors are rendered is maestro's own (internal/ui, #13): the
	// rendering reports the messages of the exception and its previous
	// ones that Symfony's does, has escape sequences only when decorated,
	// and debugging details at -v. Symfony's errors about the terminal
	// width (c.Error) have no counterpart: maestro does not wrap.
	app := newTestApp()
	for _, c := range o.Render {
		t.Setenv("COLUMNS", strconv.Itoa(c.Columns))
		var buf bytes.Buffer
		out := NewStreamOutput(&buf, c.Verbosity, &c.Decorated, nil)
		app.RenderThrowable(exceptions[c.Exception], out)
		name := strconv.Itoa(c.Exception) + "/" + strconv.Itoa(c.Columns) + "/" + strconv.Itoa(c.Verbosity) + "/" + strconv.FormatBool(c.Decorated)
		got := php.NormalizeEOL(buf.String())
		if strings.Contains(got, "\x1b") != c.Decorated {
			t.Errorf("%s: escape sequences in %q, decorated %v", name, got, c.Decorated)
		}
		if strings.Contains(got, "Debug:") != (c.Verbosity >= VerbosityVerbose) {
			t.Errorf("%s: debugging details in %q at verbosity %d", name, got, c.Verbosity)
		}
		compact := testutil.CompactMessage(ansiEscape.ReplaceAllString(got, ""))
		for e := exceptions[c.Exception]; e != nil; {
			class, _, prev := throwableInfo(e)
			want := testutil.CompactMessage(e.Error())
			if want == "" {
				want = testutil.CompactMessage(class)
			}
			if !strings.Contains(compact, want) {
				t.Errorf("%s: %q does not report %q", name, got, e.Error())
			}
			e = prev
		}
	}
}

// ansiEscape matches an SGR escape sequence.
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

func oracleSetFromPHP(t *testing.T) map[string][][4]any {
	t.Helper()
	// Mirrors $sets in tools/oracle/console/application.php.
	return map[string][][4]any{
		"foo": {
			{"foo:bar", []string{"afoobar"}, "The foo:bar command", false},
			{"foo:bar1", []string{"afoobar1"}, "The foo:bar1 command", false},
			{"foo1:bar", []string{"afoobar2"}, "The foo1:bar command", false},
			{"foo3:bar", []string{}, "The foo3:bar command", false},
			{"foo3:bar:toh", []string{}, "", false},
			{"foo:hidden", []string{"afoohidden"}, "", true},
			{"bar:buc", []string{}, "", false},
			{"foobar:foo", []string{}, "The foobar:foo command", false},
			{"foo:bar:baz", []string{"foobarbaz"}, "The foo:bar:baz command", false},
			{"foo:go:bret", []string{"foobargo"}, "The foo:bar:go command", false},
			{"test-ambiguous", []string{"test"}, "The test-ambiguous command", false},
			{"test-ambiguous2", []string{}, "The test-ambiguous2 command", false},
		},
		"case": {
			{"foo:BAR", []string{}, "foo:BAR command", false},
			{"foo:bar", []string{}, "foo:bar command", false},
			{"Cache:Clear", []string{"cc"}, "Clears the cache with a really long description that will be truncated when the terminal is narrow enough", false},
			{"cache:warmup", []string{}, "Warms up the cache", false},
			{"cache:pool:clear", []string{}, "Clears pools", false},
			{"cache:pool:prune", []string{}, "Prunes pools", true},
			{"debug:router", []string{}, "Displays routes", false},
			{"débug:ünicode", []string{}, "Unicode name ✓", false},
		},
		"composer": {
			{"install", []string{"i"}, "Installs the project dependencies from the composer.lock file if present, or falls back on the composer.json", false},
			{"update", []string{"u", "upgrade"}, "Updates your dependencies to the latest version according to composer.json, and updates the composer.lock file", false},
			{"require", []string{"r"}, "Adds required packages to your composer.json and installs them", false},
			{"remove", []string{"rm", "uninstall"}, "Removes a package from the require or require-dev", false},
			{"run-script", []string{"run"}, "Runs the scripts defined in composer.json", false},
			{"dump-autoload", []string{"dumpautoload"}, "Dumps the autoloader", false},
			{"self-update", []string{"selfupdate"}, "Updates composer.phar to the latest version", false},
			{"show", []string{"info"}, "Shows information about packages", false},
			{"status", []string{}, "Shows a list of locally modified packages", false},
			{"search", []string{}, "Searches for packages", false},
			{"suggests", []string{}, "Shows package suggestions", false},
			{"config", []string{}, "Sets config options", false},
			{"create-project", []string{}, "Creates new project from a package into given directory", false},
			{"depends", []string{"why"}, "Shows which packages cause the given package to be installed", false},
			{"prohibits", []string{"why-not"}, "Shows which packages prevent the given package from being installed", false},
			{"diagnose", []string{}, "Diagnoses the system to identify common errors", false},
			{"global", []string{}, "Allows running commands in the global composer dir ($COMPOSER_HOME)", false},
			{"outdated", []string{}, "Shows a list of installed packages that have updates available, including their latest version", false},
			{"reinstall", []string{}, "Uninstalls and reinstalls the given package names", false},
			{"audit", []string{}, "Checks for security vulnerability advisories for installed packages", false},
			{"bump", []string{}, "Increases the lower limit of your composer.json requirements to the currently installed versions", false},
			{"check-platform-reqs", []string{}, "Check that platform requirements are satisfied", false},
			{"clear-cache", []string{"clearcache", "cc"}, "Clears composer's internal package cache", false},
			{"exec", []string{}, "Executes a vendored binary/script", false},
			{"fund", []string{}, "Discover how to help fund the maintenance of your dependencies", false},
			{"licenses", []string{}, "Shows information about licenses of dependencies", false},
			{"validate", []string{}, "Validates a composer.json and composer.lock", false},
			{"archive", []string{}, "Creates an archive of this composer package", false},
			{"browse", []string{"home"}, "Opens the package's repository URL or homepage in your browser", false},
		},
	}
}

func TestOracle_ApplicationFind(t *testing.T) {
	setOracleEnv(t)
	var o applicationOracle
	loadOracle(t, "application.json", &o)

	sets := oracleSetFromPHP(t)
	build := func(set string) *Application {
		app := newTestApp()
		for _, c := range sets[set] {
			register(t, app, c[0].(string)).SetAliases(c[1].([]string)...).SetDescription(c[2].(string)).SetHidden(c[3].(bool))
		}

		return app
	}

	for set, want := range o.Namespaces {
		if got := build(set).Namespaces(); !slices.Equal(got, want) {
			t.Errorf("%s: Namespaces() = %v, want %v", set, got, want)
		}
	}

	for _, c := range o.Find {
		t.Setenv("COLUMNS", strconv.Itoa(c.Columns))
		app := build(c.Set)
		name := c.Set + "/" + strconv.Itoa(c.Columns) + "/" + string(c.Name)

		cmd, err := app.Find(string(c.Name))
		switch {
		case c.Result != nil:
			if err != nil || cmd.Base().Name() != *c.Result {
				t.Errorf("%s: Find() = %v, %v; want %q", name, cmd, err, *c.Result)
			}
		default:
			var e *Error
			if !errors.As(err, &e) {
				t.Errorf("%s: want error %q, got %v", name, c.Message, err)

				break
			}
			if e.ThrowableClass() != *c.Error || e.Message != string(c.Message) || !slices.Equal(e.Alternatives, nilIfEmpty(c.Alternatives)) {
				t.Errorf("%s: Find() error\nwant %s %q %v\ngot  %s %q %v", name, *c.Error, c.Message, c.Alternatives, e.ThrowableClass(), e.Message, e.Alternatives)
			}
		}

		ns, err := app.FindNamespace(string(c.Name))
		switch {
		case c.Namespace != nil:
			if err != nil || ns != *c.Namespace {
				t.Errorf("%s: FindNamespace() = %q, %v; want %q", name, ns, err, *c.Namespace)
			}
		default:
			var e *Error
			if !errors.As(err, &e) || e.Message != string(*c.NamespaceError) || !slices.Equal(e.Alternatives, nilIfEmpty(c.NamespaceAlternatives)) {
				t.Errorf("%s: FindNamespace() error\nwant %q %v\ngot  %v", name, *c.NamespaceError, c.NamespaceAlternatives, err)
			}
		}
	}
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}

	return s
}

// oracleDumpCommand mirrors DumpCommand in tools/oracle/console/application.php.
type oracleDumpCommand struct{ *Command }

func newOracleDumpCommand() *oracleDumpCommand {
	c := &oracleDumpCommand{NewCommand("app:dump")}
	c.SetAliases("dump").
		SetDescription("Dumps its input").
		SetHelp("The <info>%command.name%</info> command dumps.\n\n  <info>%command.full_name% x</info>").
		AddArgument("first", ArgumentRequired, "The first argument", nil).
		AddArgument("rest", ArgumentIsArray, "More arguments", nil).
		AddOption("opt", "o", OptionValueRequired, "A value option", "def").
		AddOption("flag", "f", OptionValueNone, "A flag", nil).
		AddOption("multi", "m", OptionValueRequired|OptionValueIsArray, "Many values", nil).
		AddOption("maybe", "", OptionValueOptional, "An optional value", nil).
		AddOption("color", "", OptionValueNegatable, "Negatable", nil)
	c.SetImpl(c)

	return c
}

func oracleFmtv(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(x)
	case []string:
		parts := make([]string, len(x))
		for i, s := range x {
			parts[i] = oracleFmtv(s)
		}

		return "[" + strings.Join(parts, ",") + "]"
	case []any:
		parts := make([]string, len(x))
		for i, s := range x {
			parts[i] = oracleFmtv(s)
		}

		return "[" + strings.Join(parts, ",") + "]"
	}

	return `"` + phpToString(v) + `"`
}

func (*oracleDumpCommand) Execute(in Input, out Output) (int, error) {
	for _, a := range in.Arguments() {
		out.Writeln("arg " + a.Name + "=" + oracleFmtv(a.Value))
	}
	for _, o := range in.Options() {
		out.Writeln("opt " + o.Name + "=" + oracleFmtv(o.Value))
	}
	b := func(v bool) string {
		if v {
			return "1"
		}

		return "0"
	}
	out.Writeln("verbosity=" + strconv.Itoa(out.Verbosity()) + " decorated=" + b(out.IsDecorated()) + " interactive=" + b(in.IsInteractive()))
	out.Write("<info>styled</info>", true, VerbosityVerbose)

	return phpIntval(StringOption(in, "opt")), nil
}

type oracleFailCommand struct{ *Command }

func newOracleFailCommand() *oracleFailCommand {
	c := &oracleFailCommand{NewCommand("app:fail")}
	c.SetDescription("Fails").AddArgument("code", ArgumentOptional, "", "0")
	c.SetImpl(c)

	return c
}

func (*oracleFailCommand) Execute(in Input, out Output) (int, error) {
	out.Writeln("failing")

	return 0, &testException{class: "RuntimeException", message: "Something <info>bad</info> happened", file: "/app/src/FailCommand.php", line: 33, code: phpIntval(StringArgument(in, "code"))}
}

func TestOracle_ApplicationRun(t *testing.T) {
	setOracleEnv(t)
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")
	var o applicationOracle
	loadOracle(t, "application.json", &o)

	oldScript := ScriptName
	phpSelf()
	ScriptName = "bin/console"
	defer func() { ScriptName = oldScript }()

	for _, c := range o.Run {
		setColumns(t, "80")
		app := NewApplication("Oracle App", "1.2.3")
		app.SetAutoExit(false)
		mustAdd(t, app, newOracleDumpCommand(), newOracleFailCommand())
		in, _ := NewArgvInput(append([]string{"bin/console"}, c.Argv...), nil)
		in.SetInteractive(false)

		var stdout, stderr bytes.Buffer
		out := NewConsoleOutputStreams(&stdout, &stderr, VerbosityNormal, &c.Decorated, nil)
		errOut := NewStreamOutput(&stderr, VerbosityNormal, new(false), nil)
		errOut.SetFormatter(out.Formatter())
		errOut.SetVerbosity(out.Verbosity())
		errOut.SetDecorated(out.IsDecorated())
		out.SetErrorOutput(errOut)

		code, err := app.Run(in, out)
		name := path.Join(c.Argv...) + "/" + strconv.FormatBool(c.Decorated)
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if code != c.Code {
			t.Errorf("%s: exit code %d, want %d", name, code, c.Code)
		}
		if got := php.NormalizeEOL(stdout.String()); got != string(c.Stdout) {
			t.Errorf("%s: stdout\nwant %q\ngot  %q", name, c.Stdout, got)
		}
		compareStderr(t, name, string(c.Stderr), php.NormalizeEOL(stderr.String()))
		if got := os.Getenv("SHELL_VERBOSITY"); got != c.ShellVerbosity {
			t.Errorf("%s: SHELL_VERBOSITY=%q, want %q", name, got, c.ShellVerbosity)
		}
	}
}

// compareStderr compares stderr with PHP's: exactly, except that where PHP
// rendered an exception what it wrote before must start got as it is, and
// the rest must report the messages of the exception and its previous
// ones (testutil.ErrorRendering): their rendering is maestro's (#13).
func compareStderr(t *testing.T, name, want, got string) {
	t.Helper()
	messages, _ := testutil.ErrorRendering(ansiEscape.ReplaceAllString(want, ""))
	if len(messages) == 0 {
		if got != want {
			t.Errorf("%s: stderr\nwant %q\ngot  %q", name, want, got)
		}

		return
	}
	// escape sequences aside: decorated, the rendering is maestro's too
	want, got = ansiEscape.ReplaceAllString(want, ""), ansiEscape.ReplaceAllString(got, "")
	_, start := testutil.ErrorRendering(want)
	if !strings.HasPrefix(got, want[:start]) {
		t.Errorf("%s: stderr before the error\nwant %q\ngot  %q", name, want[:start], got)

		return
	}
	rest := testutil.CompactMessage(got[start:])
	for _, m := range messages {
		if !strings.Contains(rest, m) {
			t.Errorf("%s: stderr %q does not report %q", name, got, m)
		}
	}
}
