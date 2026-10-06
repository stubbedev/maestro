// Ports src/Tester/TesterTrait.php and ApplicationTester.php
// (symfony/console 5.4) and TestCase::getApplicationTester.

package commandtest

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
)

// Options are ApplicationTester::run()'s $options; nil pointers are unset.
type Options struct {
	Interactive *bool
	Decorated   *bool
	// Verbosity is a console.Verbosity* level; 0 is unset.
	Verbosity               int
	CaptureStderrSeparately bool
}

// ApplicationTester ports Symfony's ApplicationTester.
type ApplicationTester struct {
	t           testing.TB
	Application *command.Application

	output     console.Output
	outBuf     *bytes.Buffer
	errBuf     *bytes.Buffer
	inputs     []string
	input      *console.ArrayInput
	statusCode int
	executed   bool
	separately bool
}

// GetApplicationTester ports TestCase::getApplicationTester: a new
// Application with auto-exit and exception catching off.
func GetApplicationTester(t testing.TB) *ApplicationTester {
	t.Helper()
	app := NewApplication()
	app.SetAutoExit(false)
	app.SetCatchExceptions(false)

	return NewApplicationTester(t, app)
}

// NewApplicationTester ports new ApplicationTester($application).
func NewApplicationTester(t testing.TB, app *command.Application) *ApplicationTester {
	return &ApplicationTester{t: t, Application: app}
}

// Run ports run($input, $options): params are the ArrayInput parameters,
// in order (console.P("command", "show"), console.P("--all", true), ...).
// It returns the status code and the exception run() let through.
func (a *ApplicationTester) Run(params []console.Param, o Options) (int, error) {
	prev, hadPrev := os.LookupEnv("SHELL_VERBOSITY")
	defer func() {
		if hadPrev {
			_ = os.Setenv("SHELL_VERBOSITY", prev)
		} else {
			_ = os.Unsetenv("SHELL_VERBOSITY")
		}
	}()

	in, err := console.NewArrayInput(params, nil)
	if err != nil {
		return 0, err
	}
	a.input = in
	if o.Interactive != nil {
		in.SetInteractive(*o.Interactive)
	}
	if len(a.inputs) > 0 {
		in.SetStream(createStream(a.inputs))
	}
	a.initOutput(o)

	code, err := a.Application.Run(in, a.output)
	a.statusCode, a.executed = code, true

	return code, err
}

// RunArgs is Run with "key" => value pairs: RunArgs(o, "command", "show",
// "--all", true).
func (a *ApplicationTester) RunArgs(o Options, kv ...any) (int, error) {
	params := make([]console.Param, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		key, _ := kv[i].(string)
		params = append(params, console.P(key, kv[i+1]))
	}

	return a.Run(params, o)
}

// Input returns the input of the last run.
func (a *ApplicationTester) Input() console.Input { return a.input }

// Output returns the output of the last run.
func (a *ApplicationTester) Output() console.Output { return a.output }

// Display ports getDisplay($normalize): the captured output.
func (a *ApplicationTester) Display(normalize bool) string {
	if a.output == nil {
		a.t.Fatal("Output not initialized, did you execute the command before requesting the display?")
	}
	s := a.outBuf.String()
	if normalize {
		// str_replace(\PHP_EOL, "\n", $display)
		s = php.NormalizeEOL(s)
	}

	return s
}

// ErrorOutput ports getErrorOutput($normalize).
func (a *ApplicationTester) ErrorOutput(normalize bool) string {
	if !a.separately {
		a.t.Fatal(`The error output is not available when the tester is run without "capture_stderr_separately" option set.`)
	}
	s := a.errBuf.String()
	if normalize {
		// str_replace(\PHP_EOL, "\n", $display)
		s = php.NormalizeEOL(s)
	}

	return s
}

// StatusCode ports getStatusCode.
func (a *ApplicationTester) StatusCode() int {
	if !a.executed {
		a.t.Fatal("Status code not initialized, did you execute the command before requesting the status code?")
	}

	return a.statusCode
}

// SetInputs ports setInputs: the lines fed to the input stream.
func (a *ApplicationTester) SetInputs(inputs ...string) { a.inputs = inputs }

func createStream(inputs []string) *strings.Reader {
	var b strings.Builder
	for _, in := range inputs {
		b.WriteString(in)
		b.WriteByte('\n')
	}

	return strings.NewReader(b.String())
}

func (a *ApplicationTester) initOutput(o Options) {
	a.separately = o.CaptureStderrSeparately
	a.outBuf = &bytes.Buffer{}
	if !a.separately {
		out := console.NewStreamOutput(a.outBuf, console.VerbosityNormal, new(false), nil)
		if o.Decorated != nil {
			out.SetDecorated(*o.Decorated)
		}
		if o.Verbosity != 0 {
			out.SetVerbosity(o.Verbosity)
		}
		a.output = out

		return
	}

	verbosity := o.Verbosity
	if verbosity == 0 {
		verbosity = console.VerbosityNormal
	}
	a.errBuf = &bytes.Buffer{}
	out := console.NewConsoleOutputStreams(a.outBuf, a.errBuf, verbosity, o.Decorated, nil)
	errOut := console.NewStreamOutput(a.errBuf, console.VerbosityNormal, new(false), nil)
	errOut.SetFormatter(out.Formatter())
	errOut.SetVerbosity(out.Verbosity())
	errOut.SetDecorated(out.IsDecorated())
	out.SetErrorOutput(errOut)
	a.output = out
}
