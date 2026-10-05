// Ports src/Composer/Util/ProcessExecutor.php.

package util

import (
	"errors"
	"os"
	"os/signal"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Command is what ProcessExecutor runs: a shell command line or an
// argument list (Composer's string|list<string>).
type Command struct {
	line  string
	args  []string
	shell bool
}

// Cmd is an argument-list command, run without a shell's parsing.
func Cmd(args ...string) Command {
	return Command{args: args}
}

// ShellCmd is a command line run through /bin/sh -c (cmd.exe on Windows).
func ShellCmd(line string) Command {
	return Command{line: line, shell: true}
}

// String renders the command the way Composer logs it: the line, or the
// arguments escaped with Escape.
func (c Command) String() string {
	if c.shell {
		return c.line
	}

	escaped := make([]string, len(c.args))
	for i, arg := range c.args {
		escaped[i] = Escape(arg)
	}

	return strings.Join(escaped, " ")
}

// cmdBuiltinCommands are ProcessExecutor::BUILTIN_CMD_COMMANDS, the same
// list as ExecutableFinder's.
var cmdBuiltinCommands = cmdBuiltins

// gitCmdsNeedGitDir is ProcessExecutor::GIT_CMDS_NEED_GIT_DIR.
var gitCmdsNeedGitDir = [][]string{
	{"show"},
	{"log"},
	{"branch"},
	{"remote", "set-url"},
}

// Async job states (ProcessExecutor::STATUS_*).
const (
	statusQueued = iota + 1
	statusStarted
	statusCompleted
	statusFailed
	statusAborted
)

var processTimeout atomic.Int64

func init() {
	processTimeout.Store(300)
}

// GetProcessTimeout ports ProcessExecutor::getTimeout, in seconds.
func GetProcessTimeout() int {
	return int(processTimeout.Load())
}

// SetProcessTimeout ports ProcessExecutor::setTimeout, in seconds; 0
// disables the timeout.
func SetProcessTimeout(seconds int) {
	processTimeout.Store(int64(seconds))
}

// ProcessExecutor ports Composer\Util\ProcessExecutor.
type ProcessExecutor struct {
	io IO

	mu          sync.Mutex
	idle        *sync.Cond
	errorOutput string
	allowAsync  bool
	maxJobs     int
	runningJobs int
	jobs        []*asyncJob
}

type asyncJob struct {
	status  int
	command Command
	cwd     string
	process *Process
	promise *Promise[*Process]
}

// NewProcessExecutor returns a ProcessExecutor writing to io, which may be
// nil.
func NewProcessExecutor(io IO) *ProcessExecutor {
	p := &ProcessExecutor{io: io}
	p.idle = sync.NewCond(&p.mu)
	p.ResetMaxJobs()

	return p
}

// Execute ports ProcessExecutor::execute. With a non-nil output the
// command's standard output is captured into it; otherwise it is written to
// the IO (or stdout). An empty cwd is the current directory.
func (p *ProcessExecutor) Execute(command Command, output *string, cwd string) (int, error) {
	return p.doExecute(command, cwd, false, output != nil, output, nil)
}

// ExecuteFunc ports ProcessExecutor::execute with a callable output: handler
// receives each chunk of output with its type (ProcessOut or ProcessErr).
func (p *ProcessExecutor) ExecuteFunc(command Command, handler func(typ, buffer string), cwd string) (int, error) {
	return p.doExecute(command, cwd, false, true, nil, handler)
}

// ExecuteTty ports ProcessExecutor::executeTty: in TTY mode when stdout is
// a terminal.
func (p *ProcessExecutor) ExecuteTty(command Command, cwd string) (int, error) {
	return p.doExecute(command, cwd, IsTty(nil), false, nil, nil)
}

func (p *ProcessExecutor) doExecute(command Command, cwd string, tty, capture bool, output *string, handler func(typ, buffer string)) (int, error) {
	p.outputCommandRun(command, cwd, false)

	p.mu.Lock()
	p.errorOutput = ""
	p.mu.Unlock()

	var env map[string]string

	// Composer's RequiresGitDirEnv never holds (see there), so this is
	// dormant in Composer too; it is kept for parity.
	if cwd != "" && p.RequiresGitDirEnv(command) && !isDir(strings.TrimRight(cwd, "/")+"/.git") {
		var configValue string

		if _, err := p.runProcess(Cmd("git", "config", "safe.bareRepository"), cwd, map[string]string{"GIT_DIR": cwd}, tty, capture, &configValue, nil); err != nil {
			return 0, err
		}

		if strings.Trim(configValue, phpTrimChars) == "explicit" {
			env = map[string]string{"GIT_DIR": cwd}
		}
	}

	return p.runProcess(command, cwd, env, tty, capture, output, handler)
}

func (p *ProcessExecutor) runProcess(command Command, cwd string, env map[string]string, tty, capture bool, output *string, handler func(typ, buffer string)) (int, error) {
	timeout := time.Duration(GetProcessTimeout()) * time.Second

	var process *Process

	if command.shell {
		line := command.line

		// On Windows Composer resolves the executable itself rather than
		// letting the OS look in the (untrusted) current directory. The
		// possessive {^([^:/\\]++) } also consumes spaces, so it never
		// matches and Composer never rewrites a command line.
		if IsWindows() {
			if run := strings.IndexAny(line, `:/\`); run > 0 && line[run] == ' ' {
				line = Escape(getExecutable(line[:run])) + line[run:]
			}
		}

		process = NewShellProcess(line, cwd, env, timeout)
	} else {
		args := command.args
		if IsWindows() && len(args) > 0 && !strings.ContainsAny(args[0], `:/\`) {
			args = slices.Clone(args)
			args[0] = getExecutable(args[0])
		}

		process = NewProcess(args, cwd, env, timeout)
	}

	if !IsWindows() && tty {
		// TTY enabling errors are ignored.
		_ = process.SetTty(true)
	}

	callback := handler
	if callback == nil && !capture {
		callback = p.outputHandler
	}

	stop := p.watchSignals()
	code, err := process.Run(callback)
	triggered := stop()

	if err != nil {
		var signaled *ProcessSignaledError
		if !errors.As(err, &signaled) {
			return code, err
		}

		if triggered != nil {
			// Exiting as we were signaled and the child process exited too
			// due to the signal.
			exitWithSignal(triggered)
		}

		return process.GetExitCode(), nil
	}

	if capture && handler == nil && output != nil {
		*output = process.GetOutput()
	}

	p.mu.Lock()
	p.errorOutput = process.GetErrorOutput()
	p.mu.Unlock()

	return process.GetExitCode(), nil
}

// watchSignals ports the SignalHandler Composer installs around a child
// process: SIGINT, SIGTERM and SIGHUP are logged instead of killing
// maestro, so it exits once the child is done. The returned function stops
// watching and returns the last signal received.
func (p *ProcessExecutor) watchSignals() func() os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, handledSignals...)

	var (
		last os.Signal
		mu   sync.Mutex
		done = make(chan struct{})
	)

	go func() {
		defer close(done)

		for sig := range ch {
			mu.Lock()
			last = sig
			mu.Unlock()

			if p.io != nil {
				p.io.WriteError("Received "+signalName(sig)+", aborting when child process is done", true, VerbosityDebug)
			}
		}
	}()

	return func() os.Signal {
		signal.Stop(ch)
		close(ch)
		<-done

		mu.Lock()
		defer mu.Unlock()

		return last
	}
}

// ExecuteAsync ports ProcessExecutor::executeAsync: the command runs once
// fewer than the maximum number of jobs are running, and the promise
// resolves with its finished Process.
func (p *ProcessExecutor) ExecuteAsync(command Command, cwd string) (*Promise[*Process], error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.allowAsync {
		return nil, errors.New(`You must use the ProcessExecutor instance which is part of a Composer\Loop instance to be able to run async processes`)
	}

	job := &asyncJob{status: statusQueued, command: command, cwd: cwd, promise: newPromise[*Process]()}
	job.promise.cancel = func() { p.cancelJob(job) }
	p.jobs = append(p.jobs, job)

	if p.runningJobs < p.maxJobs {
		p.startJob(job)
	}

	return job.promise, nil
}

// errAbortedProcess is the RuntimeException a cancelled job rejects with.
var errAbortedProcess = errors.New("Aborted process")

// cancelJob ports the promise canceller of executeAsync. A queued job is
// dropped; React would leave its promise pending, here it is rejected so
// waiters do not hang.
func (p *ProcessExecutor) cancelJob(job *asyncJob) {
	p.mu.Lock()

	switch job.status {
	case statusQueued:
		job.status = statusAborted
		p.idle.Broadcast()
		p.mu.Unlock()
		job.promise.reject(errAbortedProcess)

		return
	case statusStarted:
	default:
		p.mu.Unlock()

		return
	}

	job.status = statusAborted
	process := job.process
	p.mu.Unlock()

	// Signal can fail in various conditions, but that does not matter.
	_ = process.Signal(intSignal)
	process.Stop(time.Second)

	p.mu.Lock()
	job.status = statusFailed
	p.markJobDone()
	p.mu.Unlock()

	job.promise.reject(errAbortedProcess)
}

// startJob ports ProcessExecutor::startJob; p.mu is held.
func (p *ProcessExecutor) startJob(job *asyncJob) {
	if job.status != statusQueued {
		return
	}

	job.status = statusStarted
	p.runningJobs++

	p.outputCommandRun(job.command, job.cwd, true)

	timeout := time.Duration(GetProcessTimeout()) * time.Second

	var process *Process
	if job.command.shell {
		process = NewShellProcess(job.command.line, job.cwd, nil, timeout)
	} else {
		process = NewProcess(job.command.args, job.cwd, nil, timeout)
	}

	job.process = process

	if err := process.Start(nil); err != nil {
		job.status = statusFailed
		p.markJobDone()

		job.promise.reject(err)

		return
	}

	go func() {
		_, err := process.Wait()

		p.mu.Lock()
		if job.status != statusStarted {
			p.mu.Unlock()

			return
		}

		if err == nil && process.IsSuccessful() {
			job.status = statusCompleted
		} else {
			job.status = statusFailed
		}

		p.markJobDone()
		p.mu.Unlock()

		if err != nil {
			job.promise.reject(err)
		} else {
			job.promise.resolve(process)
		}
	}()
}

// markJobDone frees a running slot and starts the next queued jobs; p.mu
// is held.
func (p *ProcessExecutor) markJobDone() {
	p.runningJobs--

	for _, job := range p.jobs {
		if p.runningJobs >= p.maxJobs {
			break
		}

		if job.status == statusQueued {
			p.startJob(job)
		}
	}

	p.idle.Broadcast()
}

// SetMaxJobs ports ProcessExecutor::setMaxJobs.
func (p *ProcessExecutor) SetMaxJobs(maxJobs int) {
	p.mu.Lock()
	p.maxJobs = maxJobs
	p.mu.Unlock()
}

// ResetMaxJobs ports ProcessExecutor::resetMaxJobs: COMPOSER_MAX_PARALLEL_PROCESSES
// clamped to 1..50, or 10.
func (p *ProcessExecutor) ResetMaxJobs() {
	maxJobs := 10
	if v, ok := GetEnv("COMPOSER_MAX_PARALLEL_PROCESSES"); ok {
		if f, ok := phpNumeric(v); ok {
			maxJobs = int(max(1, min(50, f)))
		}
	}

	p.SetMaxJobs(maxJobs)
}

// Wait ports ProcessExecutor::wait: blocks until no job is queued or
// running.
func (p *ProcessExecutor) Wait() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for p.countActiveJobs() > 0 {
		p.idle.Wait()
	}
}

// EnableAsync ports ProcessExecutor::enableAsync.
func (p *ProcessExecutor) EnableAsync() {
	p.mu.Lock()
	p.allowAsync = true
	p.mu.Unlock()
}

// CountActiveJobs ports ProcessExecutor::countActiveJobs: the number of
// queued or running jobs.
func (p *ProcessExecutor) CountActiveJobs() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.countActiveJobs()
}

func (p *ProcessExecutor) countActiveJobs() int {
	active := 0

	p.jobs = slices.DeleteFunc(p.jobs, func(job *asyncJob) bool {
		if job.status < statusCompleted {
			active++

			return false
		}

		return true
	})

	return active
}

// SplitLines ports ProcessExecutor::splitLines.
func (p *ProcessExecutor) SplitLines(output string) []string {
	output = strings.Trim(output, phpTrimChars)
	if output == "" {
		return []string{}
	}

	return crlfSplit.Split(output, -1)
}

var crlfSplit = regexp.MustCompile(`\r?\n`)

// GetErrorOutput ports ProcessExecutor::getErrorOutput: the error output of
// the last command.
func (p *ProcessExecutor) GetErrorOutput() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.errorOutput
}

// outputHandler ports ProcessExecutor::outputHandler for uncaptured output.
func (p *ProcessExecutor) outputHandler(typ, buffer string) {
	if p.io == nil {
		_, _ = os.Stdout.WriteString(buffer)

		return
	}

	if typ == ProcessErr {
		p.io.WriteErrorRaw(buffer, false, VerbosityNormal)
	} else {
		p.io.WriteRaw(buffer, false, VerbosityNormal)
	}
}

var passwordArg = regexp.MustCompile(`--password (.*[^\\]') `)

// outputCommandRun ports ProcessExecutor::outputCommandRun: the debug line
// announcing a command, with credentials masked.
func (p *ProcessExecutor) outputCommandRun(command Command, cwd string, async bool) {
	if p.io == nil || !p.io.IsDebug() {
		return
	}

	safeCommand := passwordArg.ReplaceAllLiteralString(SanitizeURL(command.String()), `--password '***' `)

	mode := "Executing"
	if async {
		mode += " async"
	}

	if !phpTruthy(cwd) {
		cwd = "CWD"
	}

	p.io.WriteError(mode+" command ("+cwd+"): "+safeCommand, true, VerbosityNormal)
}

// Escape ports ProcessExecutor::escape: a shell argument for /bin/sh, or
// for cmd.exe with delayed expansion (/V:ON) on Windows.
func Escape(argument string) string {
	return escapeArgument(argument, IsWindows())
}

// windowsLookalikes are the characters cmd.exe's "Best Fit" conversion
// turns into quotes, colons and slashes, plus newlines, which break its
// parsing.
var windowsLookalikes = strings.NewReplacer(
	"\n", " ",
	"＂", `"`,
	"ʺ", `"`,
	"〝", `"`,
	"〞", `"`,
	"̎", `"`,
	"：", ":",
	"։", ":",
	"∶", ":",
	"／", "/",
	"⁄", "/",
	"∕", "/",
	"´", "/",
)

// escapeArgument ports ProcessExecutor::escapeArgument, modified from
// https://github.com/johnstevenson/winbox-args.
func escapeArgument(argument string, windows bool) string {
	if argument == "" {
		if windows {
			return `""`
		}

		return "''"
	}

	if !windows {
		return escapeShellArg(argument)
	}

	argument = windowsLookalikes.Replace(argument)

	// In addition to whitespace, commas need quoting to preserve paths.
	quote := strings.ContainsAny(argument, " \t,")
	dquotes := strings.Count(argument, `"`)
	argument = escapeQuotesBackslashes(argument)
	meta := dquotes > 0 || hasWindowsVariable(argument)

	if !meta && !quote {
		quote = strings.ContainsAny(argument, "^&|<>()")
	}

	if quote {
		argument = `"` + doubleTrailingBackslashes(argument) + `"`
	}

	if meta {
		var b strings.Builder

		for i := range len(argument) {
			switch c := argument[i]; c {
			case '"', '^', '&', '|', '<', '>', '(', ')', '%':
				b.WriteByte('^')
				b.WriteByte(c)
			case '!':
				b.WriteString("^^!")
			default:
				b.WriteByte(c)
			}
		}

		argument = b.String()
	}

	return argument
}

// hasWindowsVariable matches /%[^%]+%|![^!]+!/: two successive % (or !)
// with something between them.
func hasWindowsVariable(s string) bool {
	for _, delim := range []byte{'%', '!'} {
		prev := -1

		for i := range len(s) {
			if s[i] != delim {
				continue
			}

			if prev >= 0 && i > prev+1 {
				return true
			}

			prev = i
		}
	}

	return false
}

// RequiresGitDirEnv ports ProcessExecutor::requiresGitDirEnv. Composer
// compares array_intersect($cmd, $gitCmd), which keeps $cmd's keys, with
// $gitCmd strictly; as $cmd[0] is "git" the keys never line up, so this is
// always false, in Composer and here.
func (p *ProcessExecutor) RequiresGitDirEnv(command Command) bool {
	cmd := command.args
	if command.shell {
		cmd = strings.Split(command.line, " ")
	}

	if len(cmd) == 0 || cmd[0] != "git" {
		return false
	}

	for _, gitCmd := range gitCmdsNeedGitDir {
		// array_intersect($cmd, $gitCmd) === $gitCmd: same keys, values and
		// order.
		matched := 0
		equal := true

		for i, part := range cmd {
			if !slices.Contains(gitCmd, part) {
				continue
			}

			if matched >= len(gitCmd) || i != matched || gitCmd[matched] != part {
				equal = false
			}

			matched++
		}

		if equal && matched == len(gitCmd) {
			return true
		}
	}

	return false
}

var (
	executablesMu sync.Mutex
	executables   = map[string]string{}
)

// getExecutable ports ProcessExecutor::getExecutable, resolving Windows
// executable paths once.
func getExecutable(name string) string {
	if slices.Contains(cmdBuiltinCommands, strings.ToLower(name)) {
		return name
	}

	executablesMu.Lock()
	defer executablesMu.Unlock()

	if path, ok := executables[name]; ok {
		return path
	}

	if path, ok := NewExecutableFinder().Find(name); ok {
		executables[name] = path

		return path
	}

	return name
}
