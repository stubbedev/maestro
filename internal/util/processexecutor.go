// Ports src/Composer/Util/ProcessExecutor.php.

package util

import (
	"errors"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
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
	errorOutput string
	allowAsync  bool
	maxJobs     int
	runningJobs int
	jobs        []*asyncJob
	// sched runs the completions of async jobs (a Loop shares its
	// HttpDownloader's).
	sched *Scheduler
	// settles are the promise settlements decided with mu held, run once
	// it is released: their callbacks may call the executor again.
	settles []func()
	// prefetched are the commands Prefetch started, by prefetchKey.
	prefetched map[string]*prefetchedProcess
}

// prefetchedProcess is a command started ahead of the Execute that is to
// take it, with the environment it was started in.
type prefetchedProcess struct {
	process *Process
	environ []string
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
	p := &ProcessExecutor{io: io, sched: NewScheduler()}
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
	if err := p.outputCommandRun(command, cwd, false); err != nil {
		return 0, err
	}

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

		if strings.Trim(configValue, php.TrimChars) == "explicit" {
			env = map[string]string{"GIT_DIR": cwd}
		}
	}

	return p.runProcess(command, cwd, env, tty, capture, output, handler)
}

// LogsCommands reports whether Execute logs the commands it runs (an IO
// at debug verbosity).
func (p *ProcessExecutor) LogsCommands() bool {
	return p.io != nil && p.io.IsDebug()
}

// Prefetch starts command in cwd now, for an Execute of it that is likely
// to come (deliberate deviation 3: commands whose turn depends on the
// output of others run at once). The next Execute capturing the output of
// the same command in the same directory, with the environment unchanged,
// takes the started process and waits for it, logging and recording its
// error output as for any run; one that never comes leaves the process to
// finish unobserved. Only commands that change nothing are worth
// prefetching: the process runs whether it is taken or not.
func (p *ProcessExecutor) Prefetch(command Command, cwd string) {
	if command.shell || (cwd != "" && p.RequiresGitDirEnv(command)) {
		return
	}

	key := prefetchKey(command, cwd)

	p.mu.Lock()
	_, ok := p.prefetched[key]
	p.mu.Unlock()

	if ok {
		return
	}

	environ := os.Environ()
	process := p.newProcess(command, cwd, nil)

	if err := process.Start(nil); err != nil {
		// Execute runs it again and reports what failed
		return
	}

	p.mu.Lock()
	if p.prefetched == nil {
		p.prefetched = map[string]*prefetchedProcess{}
	}
	p.prefetched[key] = &prefetchedProcess{process: process, environ: environ}
	p.mu.Unlock()
}

// takePrefetched returns the process Prefetch started for command in cwd,
// once, if the environment is the one it was started in.
func (p *ProcessExecutor) takePrefetched(command Command, cwd string) *Process {
	key := prefetchKey(command, cwd)

	p.mu.Lock()
	pf, ok := p.prefetched[key]
	delete(p.prefetched, key)
	p.mu.Unlock()

	if !ok || !slices.Equal(pf.environ, os.Environ()) {
		return nil
	}

	return pf.process
}

func prefetchKey(command Command, cwd string) string {
	return cwd + "\x00" + strings.Join(command.args, "\x00")
}

// newProcess is the Process running command.
func (p *ProcessExecutor) newProcess(command Command, cwd string, env map[string]string) *Process {
	timeout := time.Duration(GetProcessTimeout()) * time.Second

	if command.shell {
		// On Windows Composer means to resolve the executable of a command
		// line itself, but its possessive {^([^:/\\]++) } also consumes the
		// spaces, so it never matches and command lines run unchanged.
		return NewShellProcess(command.line, cwd, env, timeout)
	}

	args := command.args
	if IsWindows() && len(args) > 0 && !strings.ContainsAny(args[0], `:/\`) {
		args = slices.Clone(args)
		args[0] = getExecutable(args[0])
	}

	return NewProcess(args, cwd, env, timeout)
}

func (p *ProcessExecutor) runProcess(command Command, cwd string, env map[string]string, tty, capture bool, output *string, handler func(typ, buffer string)) (int, error) {
	var prefetched *Process
	if !command.shell && !tty && capture && handler == nil && env == nil {
		prefetched = p.takePrefetched(command, cwd)
	}

	if prefetched != nil {
		stop := p.watchSignals()
		code, err := prefetched.Wait()
		triggered := stop()

		return p.finishProcess(prefetched, code, err, triggered, capture, output, handler)
	}

	process := p.newProcess(command, cwd, env)

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

	return p.finishProcess(process, code, err, triggered, capture, output, handler)
}

// finishProcess is the end of execute() once process ran: its output and
// error output recorded, its exit code returned.
func (p *ProcessExecutor) finishProcess(process *Process, code int, err error, triggered os.Signal, capture bool, output *string, handler func(typ, buffer string)) (int, error) {
	if err != nil {
		if _, signaled := errors.AsType[*ProcessSignaledError](err); !signaled {
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
// resolves with its finished Process. The promise settles on the goroutine
// driving the executor's Scheduler (Loop.Wait, Wait), in the order the
// jobs started; see Scheduler.
func (p *ProcessExecutor) ExecuteAsync(command Command, cwd string) (*Promise[*Process], error) {
	p.mu.Lock()

	if !p.allowAsync {
		p.mu.Unlock()

		return nil, &LogicError{Message: `You must use the ProcessExecutor instance which is part of a Composer\Loop instance to be able to run async processes`, Site: phperr.At("ProcessExecutor.php", 226)}
	}

	job := &asyncJob{status: statusQueued, command: command, cwd: cwd, promise: newPromise[*Process]()}
	job.promise.sched = p.sched
	job.promise.canceller = func() { p.cancelJob(job) }
	p.jobs = append(p.jobs, job)

	if p.runningJobs < p.maxJobs {
		p.startJob(job)
	}

	p.unlock()

	return job.promise, nil
}

// unlock releases p.mu and runs the settlements decided meanwhile.
func (p *ProcessExecutor) unlock() {
	settles := p.settles
	p.settles = nil
	p.mu.Unlock()

	for _, settle := range settles {
		settle()
	}
}

// settleJob settles a job's promise once p.mu is released; p.mu is held.
func (p *ProcessExecutor) settleJob(job *asyncJob, err error) {
	p.settles = append(p.settles, func() {
		if err != nil {
			job.promise.reject(err)
		} else {
			job.promise.resolve(job.process)
		}
	})
}

// errAbortedProcess is the RuntimeException a cancelled job rejects with.
var errAbortedProcess = &RuntimeError{Message: "Aborted process", Site: phperr.At("ProcessExecutor.php", 259)}

// cancelJob ports the promise canceller of executeAsync. A queued job is
// dropped; React would leave its promise pending, here it is rejected so
// waiters do not hang.
func (p *ProcessExecutor) cancelJob(job *asyncJob) {
	p.mu.Lock()

	switch job.status {
	case statusQueued:
		job.status = statusAborted
		p.settleJob(job, errAbortedProcess)
		p.unlock()

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
	p.runningJobs--
	p.settleJob(job, errAbortedProcess)
	p.unlock()

	p.startQueued()
}

// startJob ports ProcessExecutor::startJob; p.mu is held. The process is
// waited for on its own goroutine, which hands its end to the scheduler.
func (p *ProcessExecutor) startJob(job *asyncJob) {
	if job.status != statusQueued {
		return
	}

	job.status = statusStarted
	p.runningJobs++

	if err := p.outputCommandRun(job.command, job.cwd, true); err != nil {
		// PHP throws before starting the process; the job fails with it.
		job.status = statusFailed
		p.runningJobs--
		p.settleJob(job, err)

		return
	}

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
		p.runningJobs--
		p.settleJob(job, err)

		return
	}

	ticket := p.sched.Ticket()

	go func() {
		// Composer resolves with the process once it stopped running,
		// whatever its exit; only a timeout (thrown by checkTimeout in PHP)
		// is an error here.
		_, err := process.Wait()

		if _, signaled := errors.AsType[*ProcessSignaledError](err); signaled {
			err = nil
		}

		ticket.Complete(func() { p.finishJob(job, err) })
	}()
}

// finishJob is the countActiveJobs tick of a job whose process stopped:
// the then() wrapper of executeAsync (status, markJobDone) and the
// promise's callbacks, then the queued jobs that may start.
func (p *ProcessExecutor) finishJob(job *asyncJob, err error) {
	p.mu.Lock()

	if job.status != statusStarted {
		// cancelled meanwhile
		p.mu.Unlock()

		return
	}

	if err == nil && job.process.IsSuccessful() {
		job.status = statusCompleted
	} else {
		job.status = statusFailed
	}

	p.runningJobs--
	p.settleJob(job, err)
	p.unlock()

	p.startQueued()
}

// startQueued starts queued jobs while fewer than the maximum run.
func (p *ProcessExecutor) startQueued() {
	p.mu.Lock()

	for _, job := range p.jobs {
		if p.runningJobs >= p.maxJobs {
			break
		}

		if job.status == statusQueued {
			p.startJob(job)
		}
	}

	p.unlock()
}

// IO returns the IO the executor writes to (its $io); nil for none.
func (p *ProcessExecutor) IO() IO { return p.io }

// Scheduler returns the scheduler running the completions of the async
// jobs.
func (p *ProcessExecutor) Scheduler() *Scheduler {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.sched
}

// SetScheduler makes the executor's async jobs complete through s; a Loop
// gives it its HttpDownloader's so that one goroutine drives both. Call it
// before starting async jobs.
func (p *ProcessExecutor) SetScheduler(s *Scheduler) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.sched = s
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
		if php.IsNumeric(v) {
			maxJobs = int(max(1, min(50, php.ToInt(v))))
		}
	}

	p.SetMaxJobs(maxJobs)
}

// Wait ports ProcessExecutor::wait: it drives the scheduler until no job
// is queued or running. Only one goroutine may drive it at a time.
func (p *ProcessExecutor) Wait() {
	p.Scheduler().Run(func() bool { return p.CountActiveJobs() == 0 })
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

// CountQueuedJobs is the number of asynchronous jobs waiting for a slot
// (they hold no scheduler ticket yet; Loop counts them as active).
func (p *ProcessExecutor) CountQueuedJobs() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := 0
	for _, job := range p.jobs {
		if job.status == statusQueued {
			n++
		}
	}

	return n
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

// SplitLines ports ProcessExecutor::splitLines: the trimmed output split on
// \r?\n.
func (p *ProcessExecutor) SplitLines(output string) []string {
	return SplitLines(output)
}

// SplitLines ports ProcessExecutor::splitLines, which does not depend on
// the executor.
func SplitLines(output string) []string {
	output = strings.Trim(output, php.TrimChars)
	if output == "" {
		return []string{}
	}

	lines := strings.Split(output, "\n")
	for i := range len(lines) - 1 {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}

	return lines
}

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

var passwordArg = php.MustCompile(`{--password (.*[^\\]') }`)

// outputCommandRun ports ProcessExecutor::outputCommandRun: the debug line
// announcing a command, with credentials masked.
func (p *ProcessExecutor) outputCommandRun(command Command, cwd string, async bool) error {
	if p.io == nil || !p.io.IsDebug() {
		return nil
	}

	// Preg::replace throws a PcreException when matching fails.
	safeCommand, _, err := passwordArg.Replace(SanitizeURL(command.String()), `--password '***' `, -1)
	if err != nil {
		return err
	}

	mode := "Executing"
	if async {
		mode += " async"
	}

	if !phpTruthy(cwd) {
		cwd = "CWD"
	}

	p.io.WriteError(mode+" command ("+cwd+"): "+safeCommand, true, VerbosityNormal)

	return nil
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
	"\uff02", `"`,
	"\u02ba", `"`,
	"\u301d", `"`,
	"\u301e", `"`,
	"\u030e", `"`,
	"\uff1a", ":",
	"\u0589", ":",
	"\u2236", ":",
	"\uff0f", "/",
	"\u2044", "/",
	"\u2215", "/",
	"\u00b4", "/",
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
		return quoteSingle(argument)
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
		if intersectIsList(cmd, gitCmd) {
			return true
		}
	}

	return false
}

// intersectIsList reports whether array_intersect($cmd, $gitCmd) ===
// $gitCmd: array_intersect keeps $cmd's keys, so the elements of cmd found
// in gitCmd must be exactly gitCmd, at indexes 0, 1, ...
func intersectIsList(cmd, gitCmd []string) bool {
	n := 0

	for i, part := range cmd {
		if !slices.Contains(gitCmd, part) {
			continue
		}

		if i != n || n >= len(gitCmd) || gitCmd[n] != part {
			return false
		}

		n++
	}

	return n == len(gitCmd)
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
