package pkgtest

import (
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/util"
)

// Expectation is one command ProcessExecutorMock expects, as
// Composer\Test\Mock\ProcessExecutorMock takes them.
type Expectation struct {
	Cmd    []string
	Return int
	Stdout string
	Stderr string
}

// ProcessExecutorMock ports Composer's ProcessExecutorMock as a
// version.ProcessExecutor.
type ProcessExecutorMock struct {
	t            testing.TB
	mu           sync.Mutex
	expectations []Expectation
	configured   bool
	strict       bool
	def          Expectation
	log          []string
	errorOutput  string
}

// NewProcessExecutorMock returns a mock that accepts every command with
// exit code 0 until Expects configures it.
func NewProcessExecutorMock(t testing.TB) *ProcessExecutorMock {
	return &ProcessExecutorMock{t: t}
}

// Expects ports ProcessExecutorMock::expects: the commands expected in
// order; in strict mode any other command fails the test, else def
// answers it.
func (m *ProcessExecutorMock) Expects(expectations []Expectation, strict bool, def Expectation) {
	m.expectations = expectations
	m.configured = true
	m.strict = strict
	m.def = def
}

// AssertComplete ports ProcessExecutorMock::assertComplete.
func (m *ProcessExecutorMock) AssertComplete() {
	m.t.Helper()

	if m.configured && len(m.expectations) > 0 {
		var cmds []string
		for _, e := range m.expectations {
			cmds = append(cmds, strings.Join(e.Cmd, " "))
		}

		m.t.Errorf("There are still %d expected process calls which have not been consumed:\n%s\n\nReceived calls:\n%s",
			len(m.expectations), strings.Join(cmds, "\n"), strings.Join(m.log, "\n"))
	}
}

func (m *ProcessExecutorMock) run(command []string) Expectation {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.log = append(m.log, strings.Join(command, " "))

	var result Expectation

	switch {
	case len(m.expectations) > 0 && slices.Equal(command, m.expectations[0].Cmd):
		result = m.expectations[0]
		m.expectations = m.expectations[1:]
	case !m.strict:
		result = m.def
	default:
		next := "Expected no more calls at this point."
		if len(m.expectations) > 0 {
			next = "Expected " + strings.Join(m.expectations[0].Cmd, " ") + " at this point."
		}

		m.t.Errorf("Received unexpected command %q\n%s\nReceived calls:\n%s", command, next, strings.Join(m.log[:len(m.log)-1], "\n"))
	}

	m.errorOutput = result.Stderr

	return result
}

// Execute ports ProcessExecutorMock::execute.
func (m *ProcessExecutorMock) Execute(command []string, output *string, _ string) (int, error) {
	result := m.run(command)
	if output != nil {
		*output = result.Stdout
	}

	return result.Return, nil
}

// MockProcess is the finished process ExecuteAsync resolves with.
type MockProcess struct {
	Output   string
	ExitCode int
}

// IsSuccessful reports a zero exit code.
func (p MockProcess) IsSuccessful() bool { return p.ExitCode == 0 }

// GetOutput returns the standard output.
func (p MockProcess) GetOutput() string { return p.Output }

// ExecuteAsync ports ProcessExecutorMock::executeAsync: the command runs
// at once and the promise is already resolved.
func (m *ProcessExecutorMock) ExecuteAsync(command []string, _ string) (*util.Promise[version.ProcessResult], error) {
	result := m.run(command)

	return util.Resolved[version.ProcessResult](MockProcess{Output: result.Stdout, ExitCode: result.Return}), nil
}

// GetErrorOutput ports ProcessExecutor::getErrorOutput.
func (m *ProcessExecutorMock) GetErrorOutput() string { return m.errorOutput }

// SplitLines ports ProcessExecutor::splitLines.
func (m *ProcessExecutorMock) SplitLines(output string) []string {
	return util.NewProcessExecutor(nil).SplitLines(output)
}

// SetMaxJobs does nothing: the mock runs commands at once.
func (m *ProcessExecutorMock) SetMaxJobs(int) {}

// ResetMaxJobs does nothing.
func (m *ProcessExecutorMock) ResetMaxJobs() {}

// Wait does nothing: every command already finished.
func (m *ProcessExecutorMock) Wait() {}
