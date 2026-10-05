// Package processmock ports tests/Composer/Test/Mock/ProcessExecutorMock.php:
// a ProcessExecutor stand-in answering commands from expectations, for the
// tests of every package that runs processes.
package processmock

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// Expectation is one expected command: Cmd must equal the command run (a
// shell line only matches a shell line, an argument list only an identical
// list, as PHP's ===). Callback, when set, runs when the command is
// consumed.
type Expectation struct {
	Cmd      util.Command
	Return   int
	Stdout   string
	Stderr   string
	Callback func()
}

// Cmd is an expectation of the argument-list command args that succeeds
// without output (a bare list in a PHP expectations array).
func Cmd(args ...string) Expectation { return Expectation{Cmd: util.Cmd(args...)} }

// Shell is an expectation of the shell command line that succeeds without
// output (a bare string in a PHP expectations array).
func Shell(line string) Expectation { return Expectation{Cmd: util.ShellCmd(line)} }

// AssertionError is PHPUnit's AssertionFailedError the mock raises.
type AssertionError struct{ Message string }

func (e *AssertionError) Error() string { return e.Message }

// Mock is ProcessExecutorMock. It implements the execute, executeTty,
// executeAsync and getErrorOutput API of util.ProcessExecutor. It is safe
// for concurrent use.
type Mock struct {
	mu             sync.Mutex
	expectations   []Expectation
	configured     bool
	strict         bool
	defaultHandler Expectation
	log            []string
	errorOutput    string
}

// New returns a mock answering every command with exit code 0 and no
// output until Expects is called.
func New() *Mock { return &Mock{} }

// Expects is expects($expectations, $strict, $defaultHandler): commands
// must arrive in this order; with strict, any other command fails,
// otherwise it gets the default handler's answer (success without output
// when defaultHandler is nil). Set strict when listing *all* expected
// commands rather than the subset under test.
func (m *Mock) Expects(expectations []Expectation, strict bool, defaultHandler *Expectation) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.expectations = slices.Clone(expectations)
	m.configured = true
	m.strict = strict

	if defaultHandler != nil {
		m.defaultHandler = Expectation{Return: defaultHandler.Return, Stdout: defaultHandler.Stdout, Stderr: defaultHandler.Stderr}
	}
}

// AssertComplete is assertComplete(): an error when expected commands
// were not run.
func (m *Mock) AssertComplete() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// this was not configured to expect anything, so no need to react here
	if !m.configured || len(m.expectations) == 0 {
		return nil
	}

	cmds := make([]string, len(m.expectations))
	for i, e := range m.expectations {
		cmds[i] = commandString(e.Cmd)
	}

	return &AssertionError{Message: "There are still " + strconv.Itoa(len(m.expectations)) + " expected process calls which have not been consumed:\n" +
		strings.Join(cmds, "\n") + "\n\nReceived calls:\n" + strings.Join(m.log, "\n")}
}

// Log returns the commands received so far, as the mock logs them
// (arguments joined with spaces).
func (m *Mock) Log() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Clone(m.log)
}

// Execute is execute($command, $output, $cwd): with a non-nil output the
// expectation's stdout is stored into it. Uncaptured output is discarded.
func (m *Mock) Execute(command util.Command, output *string, cwd string) (int, error) {
	return m.doExecute(command, cwd, output, nil)
}

// ExecuteFunc is execute() with a callable output: handler receives the
// expectation's stdout and stderr.
func (m *Mock) ExecuteFunc(command util.Command, handler func(typ, buffer string), cwd string) (int, error) {
	return m.doExecute(command, cwd, nil, handler)
}

// ExecuteTty is executeTty($command, $cwd).
func (m *Mock) ExecuteTty(command util.Command, cwd string) (int, error) {
	return m.doExecute(command, cwd, nil, nil)
}

// ExecuteAsync is executeAsync($command, $cwd): the command is answered at
// once and the promise resolves with a finished Process carrying the
// expectation's exit code and output.
func (m *Mock) ExecuteAsync(command util.Command, cwd string) (*util.Promise[*util.Process], error) {
	var output string

	code, err := m.doExecute(command, cwd, &output, nil)
	if err != nil {
		return util.Rejected[*util.Process](err), nil
	}

	return util.Resolved(util.NewFinishedProcess(command, code, output, m.GetErrorOutput())), nil
}

// GetErrorOutput is getErrorOutput(): the stderr of the last command.
func (m *Mock) GetErrorOutput() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.errorOutput
}

func (m *Mock) doExecute(command util.Command, cwd string, output *string, handler func(typ, buffer string)) (int, error) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	m.mu.Lock()

	m.errorOutput = ""
	m.log = append(m.log, commandString(command))

	var expect Expectation

	switch {
	case m.configured && len(m.expectations) > 0 && sameCommand(command, m.expectations[0].Cmd):
		expect = m.expectations[0]
		m.expectations = m.expectations[1:]
	case !m.strict:
		expect = m.defaultHandler
	default:
		msg := "Received unexpected command " + exportCommand(command) + ` in "` + cwd + `"` + "\n"
		if m.configured && len(m.expectations) > 0 {
			msg += "Expected " + exportCommand(m.expectations[0].Cmd) + " at this point."
		} else {
			msg += "Expected no more calls at this point."
		}

		msg += "\nReceived calls:\n" + strings.Join(m.log[:len(m.log)-1], "\n")
		m.mu.Unlock()

		return 0, &AssertionError{Message: msg}
	}

	m.errorOutput = expect.Stderr
	m.mu.Unlock()

	if expect.Callback != nil {
		expect.Callback()
	}

	if handler != nil {
		if expect.Stdout != "" {
			handler(util.ProcessOut, expect.Stdout)
		}

		if expect.Stderr != "" {
			handler(util.ProcessErr, expect.Stderr)
		}
	}

	if output != nil {
		*output = expect.Stdout
	}

	return expect.Return, nil
}

// sameCommand is $command === $expectation['cmd'].
func sameCommand(a, b util.Command) bool {
	if a.IsShell() != b.IsShell() {
		return false
	}

	if a.IsShell() {
		return a.Line() == b.Line()
	}

	return slices.Equal(a.Args(), b.Args())
}

// commandString is is_array($command) ? implode(' ', $command) : $command.
func commandString(c util.Command) string {
	if c.IsShell() {
		return c.Line()
	}

	return strings.Join(c.Args(), " ")
}

// exportCommand is var_export($command, true).
func exportCommand(c util.Command) string {
	if c.IsShell() {
		return php.VarExport(c.Line())
	}

	return php.VarExport(php.StringList(c.Args()))
}
