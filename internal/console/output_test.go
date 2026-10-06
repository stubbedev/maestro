// Ports Tests/Output/{OutputTest,StreamOutputTest,ConsoleOutputTest,
// NullOutputTest}.php and Tests/TerminalTest.php (symfony/console).

package console

import (
	"bytes"
	"os"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// testOutput is OutputTest's TestOutput: an Output collecting its writes.
type testOutput struct {
	BaseOutput
	output string
}

func newTestOutput(verbosity int, decorated bool) *testOutput {
	o := &testOutput{}
	o.init(verbosity, decorated, nil, func(message string, newline bool) {
		o.output += message
		if newline {
			o.output += "\n"
		}
	})

	return o
}

func (o *testOutput) clear() { o.output = "" }

func TestOutput_Constructor(t *testing.T) {
	o := newTestOutput(VerbosityQuiet, true)
	eq(t, o.Verbosity(), VerbosityQuiet, "__construct() takes the verbosity as its first argument")
	eq(t, o.IsDecorated(), true, "__construct() takes the decorated flag as its second argument")
}

func TestOutput_SetIsDecorated(t *testing.T) {
	o := newTestOutput(0, false)
	o.SetDecorated(true)
	eq(t, o.IsDecorated(), true, "setDecorated() sets the decorated flag")
}

func TestOutput_SetGetVerbosity(t *testing.T) {
	o := newTestOutput(0, false)
	for _, c := range []struct {
		level                                  int
		quiet, verbose, veryVerbose, debugging bool
	}{
		{VerbosityQuiet, true, false, false, false},
		{VerbosityNormal, false, false, false, false},
		{VerbosityVerbose, false, true, false, false},
		{VerbosityVeryVerbose, false, true, true, false},
		{VerbosityDebug, false, true, true, true},
	} {
		o.SetVerbosity(c.level)
		eq(t, o.Verbosity(), c.level, "->setVerbosity() sets the verbosity")
		eq(t, o.IsQuiet(), c.quiet)
		eq(t, o.IsVerbose(), c.verbose)
		eq(t, o.IsVeryVerbose(), c.veryVerbose)
		eq(t, o.IsDebug(), c.debugging)
	}
}

func TestOutput_WriteWithVerbosityQuiet(t *testing.T) {
	o := newTestOutput(VerbosityQuiet, false)
	o.Writeln("foo")
	eq(t, o.output, "", "->writeln() outputs nothing if verbosity is set to VERBOSITY_QUIET")
}

func TestOutput_WriteAnArrayOfMessages(t *testing.T) {
	o := newTestOutput(0, false)
	WriteMessages(o, []string{"foo", "bar"}, true, 0)
	eq(t, o.output, "foo\nbar\n", "->writeln() can take an array of messages to output")
}

func TestOutput_WriteRawMessage(t *testing.T) {
	for _, c := range []struct {
		message  string
		typ      int
		expected string
	}{
		{"<info>foo</info>", OutputRaw, "<info>foo</info>\n"},
		{"<info>foo</info>", OutputPlain, "foo\n"},
	} {
		o := newTestOutput(0, false)
		o.Write(c.message, true, c.typ)
		eq(t, o.output, c.expected)
	}
}

func TestOutput_WriteWithDecorationTurnedOff(t *testing.T) {
	o := newTestOutput(0, false)
	o.SetDecorated(false)
	o.Writeln("<info>foo</info>")
	eq(t, o.output, "foo\n", "->writeln() strips decoration tags if decoration is set to false")
}

func TestOutput_WriteDecoratedMessage(t *testing.T) {
	o := newTestOutput(0, false)
	o.Formatter().SetStyle("FOO", MustStyle("yellow", "red", "blink"))
	o.SetDecorated(true)
	o.Writeln("<foo>foo</foo>")
	eq(t, o.output, "\033[33;41;5mfoo\033[39;49;25m\n", "->writeln() decorates the output")
}

func TestOutput_WriteWithInvalidStyle(t *testing.T) {
	o := newTestOutput(0, false)
	o.clear()
	o.Write("<bar>foo</bar>", false, 0)
	eq(t, o.output, "<bar>foo</bar>", "->write() do nothing when a style does not exist")
	o.clear()
	o.Writeln("<bar>foo</bar>")
	eq(t, o.output, "<bar>foo</bar>\n", "->writeln() do nothing when a style does not exist")
}

func TestOutput_WriteWithVerbosityOption(t *testing.T) {
	for _, c := range []struct {
		verbosity int
		expected  string
		msg       string
	}{
		{VerbosityQuiet, "2", "->write() in QUIET mode only outputs when an explicit QUIET verbosity is passed"},
		{VerbosityNormal, "123", "->write() in NORMAL mode outputs anything below an explicit VERBOSE verbosity"},
		{VerbosityVerbose, "1234", "->write() in VERBOSE mode outputs anything below an explicit VERY_VERBOSE verbosity"},
		{VerbosityVeryVerbose, "12345", "->write() in VERY_VERBOSE mode outputs anything below an explicit DEBUG verbosity"},
		{VerbosityDebug, "123456", "->write() in DEBUG mode outputs everything"},
	} {
		o := newTestOutput(0, false)
		o.SetVerbosity(c.verbosity)
		o.clear()
		o.Write("1", false, 0)
		o.Write("2", false, VerbosityQuiet)
		o.Write("3", false, VerbosityNormal)
		o.Write("4", false, VerbosityVerbose)
		o.Write("5", false, VerbosityVeryVerbose)
		o.Write("6", false, VerbosityDebug)
		eq(t, o.output, c.expected, c.msg)
	}
}

func TestStreamOutput_Constructor(t *testing.T) {
	o := NewStreamOutput(&bytes.Buffer{}, VerbosityQuiet, new(true), nil)
	eq(t, o.Verbosity(), VerbosityQuiet, "__construct() takes the verbosity as its first argument")
	eq(t, o.IsDecorated(), true, "__construct() takes the decorated flag as its second argument")
}

func TestStreamOutput_GetStream(t *testing.T) {
	buf := &bytes.Buffer{}
	o := NewStreamOutput(buf, 0, nil, nil)
	if o.Stream() != buf {
		t.Error("->getStream() returns the current stream")
	}
}

func TestStreamOutput_DoWrite(t *testing.T) {
	buf := &bytes.Buffer{}
	o := NewStreamOutput(buf, 0, nil, nil)
	o.Writeln("foo")
	eq(t, buf.String(), "foo"+php.EOL, "->doWrite() writes to the stream")
}

func TestStreamOutput_DoWriteOnFailure(t *testing.T) {
	f, err := os.Open("testdata/stream_output_file.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	o := NewStreamOutput(f, 0, nil, nil)
	o.Writeln("foo") // writing to a read-only file fails silently
	data, _ := os.ReadFile("testdata/stream_output_file.txt")
	eq(t, string(data), "")
}

func TestStreamOutput_HasColorSupport(t *testing.T) {
	unsetEnv(t, "NO_COLOR", "MSYSTEM", "TERM_PROGRAM", "COLORTERM", "ANSICON", "ConEmuANSI", "TERM")
	buf := &bytes.Buffer{}
	eq(t, HasColorSupport(buf), false, "a non-tty stream has no color support")

	// msysgit/mingw are assumed to be a tty.
	t.Setenv("MSYSTEM", "mingw64")
	eq(t, HasColorSupport(buf), false, "no TERM")
	t.Setenv("TERM", "xterm")
	eq(t, HasColorSupport(buf), true)
	t.Setenv("TERM", "dumb")
	eq(t, HasColorSupport(buf), false)
	t.Setenv("COLORTERM", "")
	eq(t, HasColorSupport(buf), true, "COLORTERM set, even empty")
	os.Unsetenv("COLORTERM")
	t.Setenv("NO_COLOR", "1")
	eq(t, HasColorSupport(buf), false, "NO_COLOR")
	t.Setenv("NO_COLOR", "")
	t.Setenv("ConEmuANSI", "ON")
	eq(t, HasColorSupport(buf), true, "ConEmuANSI")
	os.Unsetenv("ConEmuANSI")
	t.Setenv("TERM_PROGRAM", "Hyper")
	eq(t, HasColorSupport(buf), true, "Hyper")
}

func TestStreamOutput_TermSupportsColor(t *testing.T) {
	for _, c := range []struct {
		term string
		want bool
	}{
		{"", false},
		{"xterm", true},
		{"xterm-256color", true},
		{"screen.xterm", true},
		{"linux", true},
		{"vt100", true},
		{"cygwin", true},
		{"foo-256", true},
		{"foo-256color", true},
		{"foo-256-bce", true},
		{"foo-256color-bce", true},
		{"foo-256x", false},
		{"vt52", false},
		{"a\nb-256", true},
		{"b-256\n", true},
		{"b-256\n\n", false},
		{"Xterm", false},
	} {
		eq(t, termSupportsColor(c.term), c.want, c.term)
	}
}

func TestConsoleOutput_ConstructorWithoutFormatter(t *testing.T) {
	o := NewConsoleOutput(VerbosityQuiet, new(true), nil)
	eq(t, o.Verbosity(), VerbosityQuiet, "__construct() takes the verbosity as its first argument")
	if o.Formatter() == o.ErrorOutput().Formatter() {
		t.Error("ErrorOutput should use it own formatter")
	}
}

func TestConsoleOutput_ConstructorWithFormatter(t *testing.T) {
	f := NewOutputFormatter(false)
	o := NewConsoleOutput(VerbosityQuiet, new(true), f)
	eq(t, o.Verbosity(), VerbosityQuiet, "__construct() takes the verbosity as its first argument")
	eq(t, o.Formatter(), Formatter(f))
	eq(t, o.ErrorOutput().Formatter(), Formatter(f), "Output and ErrorOutput should use the same provided formatter")
}

func TestConsoleOutput_SetFormatter(t *testing.T) {
	o := NewConsoleOutput(0, nil, nil)
	f := NewOutputFormatter(false)
	o.SetFormatter(f)
	eq(t, o.Formatter(), Formatter(f))
	eq(t, o.ErrorOutput().Formatter(), Formatter(f))
}

func TestConsoleOutput_SetVerbosity(t *testing.T) {
	o := NewConsoleOutput(0, nil, nil)
	o.SetVerbosity(VerbosityVerbose)
	eq(t, o.Verbosity(), VerbosityVerbose)
	eq(t, o.ErrorOutput().Verbosity(), VerbosityVerbose)
}

func TestConsoleOutput_DecoratedFromBothStreams(t *testing.T) {
	unsetEnv(t, "NO_COLOR", "MSYSTEM")
	var out, errOut bytes.Buffer
	o := NewConsoleOutputStreams(&out, &errOut, 0, nil, NewOutputFormatter(true))
	eq(t, o.IsDecorated(), false, "non-tty streams are not decorated")
	o.Writeln("<info>x</info>")
	o.ErrorOutput().Writeln("<info>y</info>")
	eq(t, out.String(), "x"+php.EOL)
	eq(t, errOut.String(), "y"+php.EOL)
}

func TestBufferedOutput_Fetch(t *testing.T) {
	o := NewBufferedOutput(0, false, nil)
	o.Writeln("<info>foo</info>")
	o.Write("bar", false, 0)
	eq(t, o.Fetch(), "foo"+php.EOL+"bar")
	eq(t, o.Fetch(), "")
}

// TestOutputs_WindowsEOL: StreamOutput, BufferedOutput and
// TrimmedBufferOutput end a written line with PHP_EOL, "\r\n" on Windows,
// and OutputStyle::newLine() repeats it; a "\n" in the message stays.
func TestOutputs_WindowsEOL(t *testing.T) {
	php.SetEOLForTest(t, "\r\n")

	buf := &bytes.Buffer{}
	stream := NewStreamOutput(buf, 0, new(false), nil)
	stream.Writeln("a\nb")
	stream.Write("c", true, OutputRaw)
	stream.Write("d", false, OutputNormal)
	NewOutputStyle(stream).NewLine(2)
	eq(t, buf.String(), "a\nb\r\nc\r\nd\r\n\r\n")

	buffered := NewBufferedOutput(0, false, nil)
	buffered.Writeln("foo")
	buffered.Write("bar", true, 0)
	eq(t, buffered.Fetch(), "foo\r\nbar\r\n")

	trimmed, err := NewTrimmedBufferOutput(4, 0, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	trimmed.Writeln("foo")
	trimmed.Writeln("x")
	eq(t, trimmed.Fetch(), "\nx\r\n")
}

func TestNullOutput_Constructor(t *testing.T) {
	o := NewNullOutput()
	o.Write("foo", false, 0)
	eq(t, o.IsDecorated(), false, "->isDecorated() returns false")
}

func TestNullOutput_Verbosity(t *testing.T) {
	o := NewNullOutput()
	eq(t, o.Verbosity(), VerbosityQuiet, "->getVerbosity() returns VERBOSITY_QUIET for NullOutput by default")
	o.SetVerbosity(VerbosityVerbose)
	eq(t, o.Verbosity(), VerbosityQuiet, "->getVerbosity() always returns VERBOSITY_QUIET for NullOutput")
}

func TestNullOutput_GetFormatter(t *testing.T) {
	o := NewNullOutput()
	f := o.Formatter()
	if _, ok := f.(*NullOutputFormatter); !ok {
		t.Fatalf("want *NullOutputFormatter, got %T", f)
	}
	eq(t, o.Formatter(), f)
}

func TestNullOutput_SetFormatter(t *testing.T) {
	o := NewNullOutput()
	f := NewOutputFormatter(false)
	o.SetFormatter(f)
	if o.Formatter() == Formatter(f) {
		t.Error("NullOutput must ignore SetFormatter")
	}
}

func TestNullOutput_SetVerbosity(t *testing.T) {
	o := NewNullOutput()
	o.SetVerbosity(VerbosityNormal)
	eq(t, o.Verbosity(), VerbosityQuiet)
}

func TestNullOutput_SetDecorated(t *testing.T) {
	o := NewNullOutput()
	o.SetDecorated(true)
	eq(t, o.IsDecorated(), false)
}

func TestNullOutput_IsQuiet(t *testing.T)       { eq(t, NewNullOutput().IsQuiet(), true) }
func TestNullOutput_IsVerbose(t *testing.T)     { eq(t, NewNullOutput().IsVerbose(), false) }
func TestNullOutput_IsVeryVerbose(t *testing.T) { eq(t, NewNullOutput().IsVeryVerbose(), false) }
func TestNullOutput_IsDebug(t *testing.T)       { eq(t, NewNullOutput().IsDebug(), false) }

func TestTerminal_(t *testing.T) {
	t.Setenv("COLUMNS", "100")
	t.Setenv("LINES", "50")
	eq(t, Terminal{}.Width(), 100)
	eq(t, Terminal{}.Height(), 50)
	t.Setenv("COLUMNS", "120")
	t.Setenv("LINES", "60")
	eq(t, Terminal{}.Width(), 120)
	eq(t, Terminal{}.Height(), 60)
	t.Setenv("COLUMNS", " 7x ")
	eq(t, Terminal{}.Width(), 7, "(int) trim()")
}

func TestTerminal_ZeroValues(t *testing.T) {
	t.Setenv("COLUMNS", "0")
	t.Setenv("LINES", "0")
	eq(t, Terminal{}.Width(), 0)
	eq(t, Terminal{}.Height(), 0)
}

func TestTerminal_Fallback(t *testing.T) {
	unsetEnv(t, "COLUMNS", "LINES")
	// Without a terminal on stdin the detected size is unknown.
	if HasSttyAvailable() {
		t.Skip("stdin is a terminal")
	}
	eq(t, Terminal{}.Width(), 80)
	eq(t, Terminal{}.Height(), 50)
}
