// Exported faces of ProcessExecutor and Process internals for the
// packages above util and their test doubles (Composer's
// ProcessExecutorMock).

package util

// Args is the argument list of an argument-list command (nil for a shell
// command line).
func (c Command) Args() []string { return c.args }

// Line is the command line of a shell command ("" for an argument list).
func (c Command) Line() string { return c.line }

// IsShell reports whether c is a shell command line (a PHP string command)
// rather than an argument list.
func (c Command) IsShell() bool { return c.shell }

// NewFinishedProcess returns a Process that already ran command and
// exited with exitCode after writing stdout and stderr, as test doubles of
// ProcessExecutor::executeAsync resolve with.
func NewFinishedProcess(command Command, exitCode int, stdout, stderr string) *Process {
	p := &Process{args: command.args, line: command.line, isShell: command.shell, started: true, exited: true, exitCode: exitCode, done: make(chan struct{})}
	close(p.done)
	p.stdout.WriteString(stdout)
	p.stderr.WriteString(stderr)

	return p
}
