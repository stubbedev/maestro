// Ports src/Tester/TesterTrait.php, ApplicationTester.php and
// CommandTester.php (symfony/console), the test drivers the ported
// PHPUnit tests rely on.

package console

import (
	"bytes"
	"os"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// testerOptions are the $options of ApplicationTester::run() and
// CommandTester::execute(); nil pointers mean "not set".
type testerOptions struct {
	interactive             *bool
	decorated               *bool
	verbosity               int
	captureStderrSeparately bool
}

type tester struct {
	output     Output
	outBuf     *bytes.Buffer
	errBuf     *bytes.Buffer
	inputs     []string
	input      Input
	statusCode int
	executed   bool
	separately bool
}

// Display returns the captured output (getDisplay(true)).
func (t *tester) Display() string {
	if t.output == nil {
		panic("Output not initialized, did you execute the command before requesting the display?")
	}

	return php.NormalizeEOL(t.outBuf.String())
}

// ErrorOutput returns the captured error output.
func (t *tester) ErrorOutput() string {
	if !t.separately {
		panic(`The error output is not available when the tester is run without "capture_stderr_separately" option set.`)
	}

	return php.NormalizeEOL(t.errBuf.String())
}

// StatusCode returns the last status code.
func (t *tester) StatusCode() int {
	if !t.executed {
		panic("Status code not initialized, did you execute the command before requesting the status code?")
	}

	return t.statusCode
}

// SetInputs sets the lines fed to the input stream.
func (t *tester) SetInputs(inputs ...string) { t.inputs = inputs }

func (t *tester) initOutput(o testerOptions) {
	t.separately = o.captureStderrSeparately
	t.outBuf = &bytes.Buffer{}
	if !t.separately {
		out := NewStreamOutput(t.outBuf, VerbosityNormal, new(false), nil)
		if o.decorated != nil {
			out.SetDecorated(*o.decorated)
		}
		if o.verbosity != 0 {
			out.SetVerbosity(o.verbosity)
		}
		t.output = out

		return
	}

	verbosity := o.verbosity
	if verbosity == 0 {
		verbosity = VerbosityNormal
	}
	t.errBuf = &bytes.Buffer{}
	out := NewConsoleOutputStreams(t.outBuf, t.errBuf, verbosity, o.decorated, nil)
	errOut := NewStreamOutput(t.errBuf, VerbosityNormal, new(false), nil)
	errOut.SetFormatter(out.Formatter())
	errOut.SetVerbosity(out.Verbosity())
	errOut.SetDecorated(out.IsDecorated())
	out.SetErrorOutput(errOut)
	t.output = out
}

func createStream(inputs []string) *strings.Reader {
	var b strings.Builder
	for _, in := range inputs {
		b.WriteString(in)
		b.WriteByte('\n')
	}

	return strings.NewReader(b.String())
}

// ApplicationTester runs an application with an ArrayInput.
type ApplicationTester struct {
	tester
	application *Application
}

func newApplicationTester(app *Application) *ApplicationTester {
	return &ApplicationTester{application: app}
}

// Run runs the application, restoring SHELL_VERBOSITY afterwards.
func (t *ApplicationTester) Run(params []Param, o testerOptions) (int, error) {
	prev, hadPrev := os.LookupEnv("SHELL_VERBOSITY")
	defer func() {
		if hadPrev {
			_ = os.Setenv("SHELL_VERBOSITY", prev)
		} else {
			_ = os.Unsetenv("SHELL_VERBOSITY")
		}
	}()

	in, err := NewArrayInput(params, nil)
	if err != nil {
		return 0, err
	}
	t.input = in
	if o.interactive != nil {
		in.SetInteractive(*o.interactive)
	}
	if len(t.inputs) > 0 {
		in.SetStream(createStream(t.inputs))
	}
	t.initOutput(o)

	code, err := t.application.Run(in, t.output)
	t.statusCode, t.executed = code, true

	return code, err
}

// CommandTester runs a command with an ArrayInput.
type CommandTester struct {
	tester
	command Commander
}

func newCommandTester(command Commander) *CommandTester {
	return &CommandTester{command: command}
}

// Execute runs the command.
func (t *CommandTester) Execute(params []Param, o testerOptions) (int, error) {
	// set the command name automatically if the application requires
	// this argument and no command name was passed
	hasCommand := false
	for _, p := range params {
		if !p.Positional && p.Key == "command" {
			hasCommand = true
		}
	}
	if app := t.command.Base().Application(); !hasCommand && app != nil && app.Definition().HasArgument("command") {
		params = append([]Param{P("command", t.command.Base().Name())}, params...)
	}

	in, err := NewArrayInput(params, nil)
	if err != nil {
		return 0, err
	}
	t.input = in
	// Use an in-memory input stream even if no inputs are set so that
	// QuestionHelper::ask() does not rely on the blocking STDIN.
	in.SetStream(createStream(t.inputs))
	if o.interactive != nil {
		in.SetInteractive(*o.interactive)
	}
	if o.decorated == nil {
		o.decorated = new(false)
	}
	t.initOutput(o)

	code, err := t.command.Run(in, t.output)
	t.statusCode, t.executed = code, true

	return code, err
}
