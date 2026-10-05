// Ports src/Output/OutputInterface.php, Output.php, StreamOutput.php,
// ConsoleOutputInterface.php, ConsoleOutput.php, BufferedOutput.php and
// NullOutput.php (symfony/console).

package console

import (
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"

	"github.com/stubbedev/maestro/internal/php"
)

// Verbosity levels and output types (OutputInterface constants).
const (
	VerbosityQuiet       = 16
	VerbosityNormal      = 32
	VerbosityVerbose     = 64
	VerbosityVeryVerbose = 128
	VerbosityDebug       = 256

	OutputNormal = 1
	OutputRaw    = 2
	OutputPlain  = 4
)

// Output is OutputInterface. Write and Writeln take one message; PHP's
// iterable form is a loop over Write (see WriteMessages).
type Output interface {
	Write(message string, newline bool, options int)
	Writeln(message string)
	SetVerbosity(level int)
	Verbosity() int
	IsQuiet() bool
	IsVerbose() bool
	IsVeryVerbose() bool
	IsDebug() bool
	SetDecorated(decorated bool)
	IsDecorated() bool
	SetFormatter(formatter Formatter)
	Formatter() Formatter
}

// ConsoleOutputInterface is an Output with a separate error output.
type ConsoleOutputInterface interface {
	Output
	ErrorOutput() Output
	SetErrorOutput(out Output)
}

// WriteMessages writes each message like PHP's write(array).
func WriteMessages(out Output, messages []string, newline bool, options int) {
	for _, m := range messages {
		out.Write(m, newline, options)
	}
}

// ErrorOutputOf returns the error output of a ConsoleOutputInterface, or out.
func ErrorOutputOf(out Output) Output {
	if c, ok := out.(ConsoleOutputInterface); ok {
		return c.ErrorOutput()
	}

	return out
}

// BaseOutput is the abstract Output class. Concrete outputs embed it and
// supply the doWrite function.
type BaseOutput struct {
	verbosity int
	formatter Formatter
	doWrite   func(message string, newline bool)
}

func (o *BaseOutput) init(verbosity int, decorated bool, formatter Formatter, doWrite func(string, bool)) {
	if verbosity == 0 {
		verbosity = VerbosityNormal
	}
	o.verbosity = verbosity
	if formatter == nil {
		formatter = NewOutputFormatter(false)
	}
	o.formatter = formatter
	o.formatter.SetDecorated(decorated)
	o.doWrite = doWrite
}

// SetFormatter implements Output.
func (o *BaseOutput) SetFormatter(formatter Formatter) { o.formatter = formatter }

// Formatter implements Output.
func (o *BaseOutput) Formatter() Formatter { return o.formatter }

// SetDecorated implements Output.
func (o *BaseOutput) SetDecorated(decorated bool) { o.formatter.SetDecorated(decorated) }

// IsDecorated implements Output.
func (o *BaseOutput) IsDecorated() bool { return o.formatter.IsDecorated() }

// SetVerbosity implements Output.
func (o *BaseOutput) SetVerbosity(level int) { o.verbosity = level }

// Verbosity implements Output.
func (o *BaseOutput) Verbosity() int { return o.verbosity }

// IsQuiet implements Output.
func (o *BaseOutput) IsQuiet() bool { return o.verbosity == VerbosityQuiet }

// IsVerbose implements Output.
func (o *BaseOutput) IsVerbose() bool { return o.verbosity >= VerbosityVerbose }

// IsVeryVerbose implements Output.
func (o *BaseOutput) IsVeryVerbose() bool { return o.verbosity >= VerbosityVeryVerbose }

// IsDebug implements Output.
func (o *BaseOutput) IsDebug() bool { return o.verbosity >= VerbosityDebug }

// Writeln implements Output.
func (o *BaseOutput) Writeln(message string) { o.Write(message, true, OutputNormal) }

// Write implements Output.
func (o *BaseOutput) Write(message string, newline bool, options int) {
	typ := (OutputNormal | OutputRaw | OutputPlain) & options
	if typ == 0 {
		typ = OutputNormal
	}
	verbosity := (VerbosityQuiet | VerbosityNormal | VerbosityVerbose | VerbosityVeryVerbose | VerbosityDebug) & options
	if verbosity == 0 {
		verbosity = VerbosityNormal
	}
	if verbosity > o.verbosity {
		return
	}

	switch typ {
	case OutputNormal:
		message = o.formatter.Format(message)
	case OutputRaw:
	case OutputPlain:
		message = StripTags(o.formatter.Format(message))
	}

	o.doWrite(message, newline)
}

// StreamOutput writes to an io.Writer.
type StreamOutput struct {
	BaseOutput
	stream io.Writer
	mu     sync.Mutex
}

// NewStreamOutput mirrors new StreamOutput($stream, $verbosity, $decorated,
// $formatter). A nil decorated detects colour support from the stream.
func NewStreamOutput(stream io.Writer, verbosity int, decorated *bool, formatter Formatter) *StreamOutput {
	o := &StreamOutput{stream: stream}
	d := false
	if decorated != nil {
		d = *decorated
	} else {
		d = HasColorSupport(stream)
	}
	o.init(verbosity, d, formatter, o.write)

	return o
}

// Stream returns the underlying writer.
func (o *StreamOutput) Stream() io.Writer { return o.stream }

func (o *StreamOutput) write(message string, newline bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if newline {
		_, _ = io.WriteString(o.stream, message+"\n")

		return
	}
	_, _ = io.WriteString(o.stream, message)
}

// fdWriter is implemented by *os.File.
type fdWriter interface{ Fd() uintptr }

// IsTTY ports stream_isatty() / posix_isatty() for a stream: only file
// descriptors referring to a terminal qualify.
func IsTTY(stream any) bool {
	f, ok := stream.(fdWriter)
	if !ok {
		return false
	}

	return term.IsTerminal(int(f.Fd()))
}

// HasColorSupport ports StreamOutput::hasColorSupport().
func HasColorSupport(stream io.Writer) bool {
	// Follow https://no-color.org/
	if os.Getenv("NO_COLOR") != "" {
		return false
	}

	// Detect msysgit/mingw and assume this is a tty because detection
	// does not work correctly, see https://github.com/composer/composer/issues/9690
	if !IsTTY(stream) {
		msystem := php.Strtoupper(os.Getenv("MSYSTEM"))
		if msystem != "MINGW32" && msystem != "MINGW64" {
			return false
		}
	}

	if windowsVT100Support(stream) {
		return true
	}

	if os.Getenv("TERM_PROGRAM") == "Hyper" {
		return true
	}
	if _, ok := os.LookupEnv("COLORTERM"); ok {
		return true
	}
	if _, ok := os.LookupEnv("ANSICON"); ok {
		return true
	}
	if os.Getenv("ConEmuANSI") == "ON" {
		return true
	}

	t := os.Getenv("TERM")
	if t == "dumb" {
		return false
	}

	// See https://github.com/chalk/supports-color/blob/d4f413efaf8da045c5ab440ed418ef02dbb28bf1/index.js#L157
	return termSupportsColor(t)
}

// termSupportsColor is
// preg_match('/^((screen|xterm|vt100|vt220|putty|rxvt|ansi|cygwin|linux).*)|(.*-256(color)?(-bce)?)$/', $term).
func termSupportsColor(t string) bool {
	for _, p := range []string{"screen", "xterm", "vt100", "vt220", "putty", "rxvt", "ansi", "cygwin", "linux"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	// The second branch is unanchored at the start, and "$" also matches
	// before a final newline.
	t = strings.TrimSuffix(t, "\n")
	for _, s := range []string{"-256", "-256color", "-256-bce", "-256color-bce"} {
		if strings.HasSuffix(t, s) {
			return true
		}
	}

	return false
}

// ConsoleOutput writes to stdout, with a separate stderr output.
type ConsoleOutput struct {
	*StreamOutput
	stderr Output
}

// NewConsoleOutput mirrors new ConsoleOutput($verbosity, $decorated,
// $formatter) on os.Stdout and os.Stderr.
func NewConsoleOutput(verbosity int, decorated *bool, formatter Formatter) *ConsoleOutput {
	return NewConsoleOutputStreams(os.Stdout, os.Stderr, verbosity, decorated, formatter)
}

// NewConsoleOutputStreams is NewConsoleOutput on explicit streams.
func NewConsoleOutputStreams(stdout, stderr io.Writer, verbosity int, decorated *bool, formatter Formatter) *ConsoleOutput {
	o := &ConsoleOutput{StreamOutput: NewStreamOutput(stdout, verbosity, decorated, formatter)}
	if formatter == nil {
		// for BC reasons, stdErr has it own Formatter only when user don't inject a specific formatter.
		o.stderr = NewStreamOutput(stderr, verbosity, decorated, nil)

		return o
	}

	actualDecorated := o.IsDecorated()
	errOut := NewStreamOutput(stderr, verbosity, decorated, o.Formatter())
	o.stderr = errOut
	if decorated == nil {
		o.SetDecorated(actualDecorated && errOut.IsDecorated())
	}

	return o
}

// SetDecorated implements Output.
func (o *ConsoleOutput) SetDecorated(decorated bool) {
	o.StreamOutput.SetDecorated(decorated)
	o.stderr.SetDecorated(decorated)
}

// SetFormatter implements Output.
func (o *ConsoleOutput) SetFormatter(formatter Formatter) {
	o.StreamOutput.SetFormatter(formatter)
	o.stderr.SetFormatter(formatter)
}

// SetVerbosity implements Output.
func (o *ConsoleOutput) SetVerbosity(level int) {
	o.StreamOutput.SetVerbosity(level)
	o.stderr.SetVerbosity(level)
}

// ErrorOutput implements ConsoleOutputInterface.
func (o *ConsoleOutput) ErrorOutput() Output { return o.stderr }

// SetErrorOutput implements ConsoleOutputInterface.
func (o *ConsoleOutput) SetErrorOutput(out Output) { o.stderr = out }

// BufferedOutput collects output in memory.
type BufferedOutput struct {
	BaseOutput
	buffer strings.Builder
}

// NewBufferedOutput mirrors new BufferedOutput($verbosity, $decorated, $formatter).
func NewBufferedOutput(verbosity int, decorated bool, formatter Formatter) *BufferedOutput {
	o := &BufferedOutput{}
	o.init(verbosity, decorated, formatter, o.write)

	return o
}

func (o *BufferedOutput) write(message string, newline bool) {
	o.buffer.WriteString(message)
	if newline {
		o.buffer.WriteByte('\n')
	}
}

// Fetch returns and empties the buffer.
func (o *BufferedOutput) Fetch() string {
	s := o.buffer.String()
	o.buffer.Reset()

	return s
}

// NullOutput discards everything.
type NullOutput struct {
	formatter Formatter
}

// NewNullOutput returns a NullOutput.
func NewNullOutput() *NullOutput { return &NullOutput{} }

// Write implements Output.
func (*NullOutput) Write(string, bool, int) {}

// Writeln implements Output.
func (*NullOutput) Writeln(string) {}

// SetVerbosity implements Output.
func (*NullOutput) SetVerbosity(int) {}

// Verbosity implements Output.
func (*NullOutput) Verbosity() int { return VerbosityQuiet }

// IsQuiet implements Output.
func (*NullOutput) IsQuiet() bool { return true }

// IsVerbose implements Output.
func (*NullOutput) IsVerbose() bool { return false }

// IsVeryVerbose implements Output.
func (*NullOutput) IsVeryVerbose() bool { return false }

// IsDebug implements Output.
func (*NullOutput) IsDebug() bool { return false }

// SetDecorated implements Output.
func (*NullOutput) SetDecorated(bool) {}

// IsDecorated implements Output.
func (*NullOutput) IsDecorated() bool { return false }

// SetFormatter implements Output.
func (*NullOutput) SetFormatter(Formatter) {}

// Formatter implements Output.
func (o *NullOutput) Formatter() Formatter {
	if o.formatter == nil {
		o.formatter = &NullOutputFormatter{}
	}

	return o.formatter
}
