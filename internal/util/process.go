// Ports the parts of vendor/symfony/process/Process.php (5.4) that
// Composer\Util\ProcessExecutor relies on: running a shell command line or
// an argument list through /bin/sh -c (cmd.exe on Windows), environment
// merging, placeholders, output callbacks, timeouts, signals and TTY mode.

package util

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Output types passed to Process callbacks (Process::OUT, Process::ERR).
const (
	ProcessOut = "out"
	ProcessErr = "err"
)

// pipeDrainTimeout is how long Symfony's last blocking read waits for output
// once the process exited (Process::TIMEOUT_PRECISION), which bounds the wait
// for grandchildren still holding the pipes.
const pipeDrainTimeout = 200 * time.Millisecond

// ProcessTimedOutError is Symfony's ProcessTimedOutException.
type ProcessTimedOutError struct {
	CommandLine string
	Timeout     time.Duration
}

// PHPClass implements PHPClasser.
func (*ProcessTimedOutError) PHPClass() (string, int) {
	return `Symfony\Component\Process\Exception\ProcessTimedOutException`, 0
}

func (e *ProcessTimedOutError) Error() string {
	return `The process "` + e.CommandLine + `" exceeded the timeout of ` + strconv.FormatFloat(e.Timeout.Seconds(), 'f', -1, 64) + " seconds."
}

// ProcessSignaledError is Symfony's ProcessSignaledException: the process
// was killed by a signal it was not sent by this Process.
type ProcessSignaledError struct {
	Signal int
}

// PHPClass implements PHPClasser.
func (*ProcessSignaledError) PHPClass() (string, int) {
	return `Symfony\Component\Process\Exception\ProcessSignaledException`, 0
}

func (e *ProcessSignaledError) Error() string {
	return `The process has been signaled with signal "` + strconv.Itoa(e.Signal) + `".`
}

// errLaunch is the RuntimeException Process::start throws when proc_open
// fails.
var errLaunch = &RuntimeError{Class: ClassProcessRuntime, Message: "Unable to launch a new process."}

// Process ports Symfony\Component\Process\Process.
type Process struct {
	args    []string // the argument list, when not a shell command line
	line    string   // the shell command line
	isShell bool
	cwd     string
	env     []string // NAME=value overrides, ahead of the inherited environment
	timeout time.Duration
	tty     bool
	input   *string // the standard input (Process::setInput); nil for none

	mu           sync.Mutex // guards the state below
	cmd          *exec.Cmd
	started      bool
	exited       bool
	done         chan struct{} // closed once the process terminated
	exitCode     int
	termSig      int
	latestSignal int
	timedOut     bool
	timer        *time.Timer

	outMu  sync.Mutex // guards the buffers and the chunks
	stdout bytes.Buffer
	stderr bytes.Buffer
	// callback is start()'s; it gets the chunks of output on the goroutine
	// waiting for the process (Wait), as Symfony's wait() calls it from
	// readPipes() on PHP's one thread
	callback func(typ, buffer string)
	chunks   []processChunk // the output the callback has not had yet
	arrived  chan struct{}  // signalled when a chunk arrives
	cbMu     sync.Mutex     // serialises deliveries
}

// processChunk is a chunk of a process's output.
type processChunk struct {
	typ, buffer string
}

// NewProcess ports `new Process($command, $cwd, $env, null, $timeout)`. An
// empty cwd inherits the working directory; a zero timeout disables it.
func NewProcess(args []string, cwd string, env map[string]string, timeout time.Duration) *Process {
	return &Process{args: args, cwd: cwd, env: envPairs(env), timeout: timeout}
}

// NewShellProcess ports Process::fromShellCommandline.
func NewShellProcess(line, cwd string, env map[string]string, timeout time.Duration) *Process {
	return &Process{line: line, isShell: true, cwd: cwd, env: envPairs(env), timeout: timeout}
}

func envPairs(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}

	pairs := make([]string, 0, len(env))
	for k, v := range env {
		pairs = append(pairs, k+"="+v)
	}

	return pairs
}

// SetTty ports Process::setTty.
func (p *Process) SetTty(tty bool) error {
	if tty && IsWindows() {
		return &RuntimeError{Class: ClassProcessRuntime, Message: "TTY mode is not supported on Windows platform."}
	}

	if tty && !isTtySupported() {
		return &RuntimeError{Class: ClassProcessRuntime, Message: "TTY mode requires /dev/tty to be read/writable."}
	}

	p.tty = tty

	return nil
}

// isTtySupported ports Process::isTtySupported.
var isTtySupported = sync.OnceValue(func() bool {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}

	_ = f.Close()

	return true
})

// SetInput ports Process::setInput (and the $input argument of the
// constructor) for a string: the child reads it as its standard input.
func (p *Process) SetInput(input string) {
	p.input = &input
}

// GetCommandLine ports Process::getCommandLine.
func (p *Process) GetCommandLine() string {
	if p.isShell {
		return p.line
	}

	return joinEscaped(p.args, IsWindows())
}

func joinEscaped(args []string, windows bool) string {
	escaped := make([]string, len(args))
	for i, arg := range args {
		escaped[i] = symfonyEscapeArgument(arg, windows)
	}

	return strings.Join(escaped, " ")
}

// Run ports Process::run: starts the process and waits for it. callback
// receives output chunks as they arrive, on the calling goroutine (Wait).
func (p *Process) Run(callback func(typ, buffer string)) (int, error) {
	if err := p.Start(callback); err != nil {
		return 0, err
	}

	return p.Wait()
}

// Start ports Process::start.
func (p *Process) Start(callback func(typ, buffer string)) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started && !p.isTerminated() {
		return &RuntimeError{Class: ClassProcessRuntime, Message: "Process is already running."}
	}

	p.outMu.Lock()
	p.stdout.Reset()
	p.stderr.Reset()
	p.callback, p.chunks, p.arrived = callback, nil, make(chan struct{}, 1)
	p.outMu.Unlock()

	p.exitCode, p.termSig, p.latestSignal, p.timedOut, p.exited = 0, 0, 0, false, false

	env := mergeEnv(p.env, os.Environ(), IsWindows())

	var commandline string

	if p.isShell {
		line, err := replacePlaceholders(p.line, env, IsWindows())
		if err != nil {
			return err
		}

		commandline = line
	} else {
		commandline = joinEscaped(p.args, IsWindows())
		if !IsWindows() {
			// exec is mandatory to deal with sending a signal to the process.
			commandline = "exec " + commandline
		}
	}

	if p.cwd != "" && !isDir(p.cwd) {
		return &RuntimeError{Class: ClassProcessRuntime, Message: `The provided cwd "` + p.cwd + `" does not exist.`}
	}

	// An argument list naming a VCS tool starts it without the shell
	// whose only job is to exec it (deliberate deviation 3); the shell
	// still runs it when it is not plainly found in PATH, so what a
	// missing or odd executable gives (exit code 127, the shell's
	// message) stays as it was.
	var cmd *exec.Cmd
	if !p.isShell {
		cmd = directCommand(p.args, env)
	}

	direct := cmd != nil
	if !direct {
		var err error
		if cmd, err = shellCommand(commandline, &env); err != nil {
			return err
		}
	}

	return p.startCmd(cmd, commandline, env, direct)
}

// startCmd starts cmd (env being its environment) and the goroutine
// waiting for it. A command started without the shell (direct, from
// directCommand) that cannot start is run through the shell instead.
func (p *Process) startCmd(cmd *exec.Cmd, commandline string, env []string, direct bool) error {
	cmd.Dir = p.cwd
	cmd.Env = env

	var closers []*os.File

	if p.tty {
		in, err := os.Open("/dev/tty")
		if err != nil {
			return errLaunch
		}

		out, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			_ = in.Close()

			return errLaunch
		}

		cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, out
		closers = append(closers, in, out)
	} else {
		if p.input != nil {
			cmd.Stdin = strings.NewReader(*p.input)
		} else {
			// Symfony hands the child a stdin pipe it closes at once.
			r, w, err := os.Pipe()
			if err != nil {
				return errLaunch
			}

			_ = w.Close()

			cmd.Stdin = r
			closers = append(closers, r)
		}

		cmd.Stdout = &processWriter{p: p, typ: ProcessOut}
		cmd.Stderr = &processWriter{p: p, typ: ProcessErr}
		cmd.WaitDelay = pipeDrainTimeout
	}

	// On Windows the output goes through files, as Symfony's WindowsPipes
	// have it (redirectToFiles).
	var pipes *filePipes

	stdout, stderr := cmd.Stdout, cmd.Stderr

	if !p.tty && !direct {
		var err error
		if pipes, err = redirectToFiles(cmd); err != nil {
			for _, f := range closers {
				_ = f.Close()
			}

			return err
		}
	}

	err := cmd.Start()

	for _, f := range closers {
		_ = f.Close()
	}

	if err != nil {
		pipes.cleanup()
	}

	if err != nil && direct {
		shell, serr := shellCommand(commandline, &env)
		if serr != nil {
			return serr
		}

		return p.startCmd(shell, commandline, env, false)
	}

	if err != nil {
		return errLaunch
	}

	p.cmd = cmd
	p.started = true
	p.done = make(chan struct{})

	if p.timeout > 0 {
		p.timer = time.AfterFunc(p.timeout, func() {
			p.mu.Lock()
			expired := !p.exited
			p.timedOut = expired
			p.mu.Unlock()

			if expired {
				p.Stop(0)
			}
		})
	}

	pipes.start(stdout, stderr)

	go p.wait(cmd, pipes, p.done)

	return nil
}

// processWriter receives a stream's output, buffering it and queueing it
// for the callback, which the goroutine waiting for the process runs
// (deliver); both streams' chunks queue in the order they arrive.
type processWriter struct {
	p   *Process
	typ string
}

func (w *processWriter) Write(b []byte) (int, error) {
	p := w.p
	p.outMu.Lock()
	if w.typ == ProcessOut {
		p.stdout.Write(b)
	} else {
		p.stderr.Write(b)
	}
	if p.callback != nil {
		p.chunks = append(p.chunks, processChunk{typ: w.typ, buffer: string(b)})
		select {
		case p.arrived <- struct{}{}:
		default:
		}
	}
	p.outMu.Unlock()

	return len(b), nil
}

// deliver runs the callback with the chunks that arrived, in order, on the
// calling goroutine.
func (p *Process) deliver() {
	p.cbMu.Lock()
	defer p.cbMu.Unlock()

	for {
		p.outMu.Lock()
		if len(p.chunks) == 0 {
			p.outMu.Unlock()

			return
		}
		c, callback := p.chunks[0], p.callback
		p.chunks = p.chunks[1:]
		p.outMu.Unlock()

		callback(c.typ, c.buffer)
	}
}

// awaitDelivering blocks until done is closed, running the callback with
// the output as it arrives (Symfony's wait() reading the pipes) and the
// wait hooks (what parallel work posted to the waiting goroutine).
func (p *Process) awaitDelivering(done <-chan struct{}) {
	p.outMu.Lock()
	arrived := p.arrived
	p.outMu.Unlock()

	for {
		p.deliver()
		hook := waitHookWake()
		RunWaitHooks()
		select {
		case <-done:
			// the pipes are drained once the process is done
			p.deliver()

			return
		case <-arrived:
		case <-hook:
		}
	}
}

func (p *Process) wait(cmd *exec.Cmd, pipes *filePipes, done chan struct{}) {
	_ = cmd.Wait()
	pipes.finish()

	p.mu.Lock()
	if p.timer != nil {
		p.timer.Stop()
	}

	p.exited = true
	p.exitCode, p.termSig = exitStatus(cmd.ProcessState)
	p.mu.Unlock()

	close(done)
}

// Wait ports Process::wait: blocks until the process terminated and returns
// its exit code (128+signal when killed by a signal). Meanwhile the
// callback start() was given gets the output as it arrives, on the
// calling goroutine, as Symfony's wait() runs it on PHP's one thread.
func (p *Process) Wait() (int, error) {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()

		return 0, &LogicError{Class: ClassProcessLogic, Message: `Process must be started before calling "wait()".`}
	}

	done := p.done
	p.mu.Unlock()

	p.awaitDelivering(done)

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.timedOut {
		return p.exitCode, &ProcessTimedOutError{CommandLine: p.GetCommandLine(), Timeout: p.timeout}
	}

	if p.termSig > 0 && p.termSig != p.latestSignal {
		return p.exitCode, &ProcessSignaledError{Signal: p.termSig}
	}

	return p.exitCode, nil
}

// IsRunning ports Process::isRunning.
func (p *Process) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.started && !p.isTerminated()
}

func (p *Process) isTerminated() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// Stop ports Process::stop: SIGTERM, then SIGKILL if the process is still
// running after timeout. It returns the exit code.
func (p *Process) Stop(timeout time.Duration) int {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()

		return 0
	}

	done := p.done
	p.mu.Unlock()

	if !p.terminatedOrSignal(termSignal) {
		// Symfony polls every millisecond until the timeout, at least once.
		select {
		case <-done:
		case <-time.After(max(timeout, time.Millisecond)):
			p.terminatedOrSignal(killSignal)
		}
	}

	<-done

	p.mu.Lock()
	defer p.mu.Unlock()

	return p.exitCode
}

// Signal ports Process::signal.
func (p *Process) Signal(sig int) error {
	p.mu.Lock()
	running := p.started && !p.isTerminated()
	p.mu.Unlock()

	if !running {
		return &LogicError{Class: ClassProcessLogic, Message: "Cannot send signal on a non running process."}
	}

	if err := p.sendSignal(sig); err != nil {
		return &RuntimeError{Class: ClassProcessRuntime, Message: `Error while sending signal "` + strconv.Itoa(sig) + `".`}
	}

	return nil
}

// terminatedOrSignal sends sig unless the process already terminated, and
// reports whether it had.
func (p *Process) terminatedOrSignal(sig int) bool {
	p.mu.Lock()
	terminated := p.isTerminated()
	p.mu.Unlock()

	if !terminated {
		_ = p.sendSignal(sig)
	}

	return terminated
}

func (p *Process) sendSignal(sig int) error {
	p.mu.Lock()
	p.latestSignal = sig
	proc := p.cmd.Process
	p.mu.Unlock()

	return signalProcess(proc, sig)
}

// IsSuccessful ports Process::isSuccessful.
func (p *Process) IsSuccessful() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.started && p.isTerminated() && p.exitCode == 0
}

// GetExitCode ports Process::getExitCode.
func (p *Process) GetExitCode() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.exitCode
}

// GetOutput ports Process::getOutput: all standard output so far.
func (p *Process) GetOutput() string {
	p.outMu.Lock()
	defer p.outMu.Unlock()

	return p.stdout.String()
}

// GetErrorOutput ports Process::getErrorOutput: all error output so far.
func (p *Process) GetErrorOutput() string {
	p.outMu.Lock()
	defer p.outMu.Unlock()

	return p.stderr.String()
}

// DirectToolPath is the file a ProcessExecutor runs, in the current
// environment and with no cwd, for an argument list naming the VCS tool
// name (it starts those without a shell, see directCommand); "" when the
// shell would run it.
func DirectToolPath(name string) string {
	if cmd := directCommand([]string{name}, os.Environ()); cmd != nil {
		return cmd.Path
	}

	return ""
}

// mergeEnv ports the environment Process::start builds: the overrides, then
// the inherited variables not overridden (case-insensitively on Windows).
func mergeEnv(overrides, inherited []string, windows bool) []string {
	if len(overrides) == 0 {
		return inherited
	}

	env := make([]string, 0, len(overrides)+len(inherited))
	env = append(env, overrides...)

	for _, kv := range inherited {
		name, _, _ := strings.Cut(kv, "=")
		if lookupEnv(overrides, name, windows) < 0 {
			env = append(env, kv)
		}
	}

	return env
}

// lookupEnv returns the index of name in a NAME=value list, or -1.
func lookupEnv(env []string, name string, windows bool) int {
	for i, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if k == name || (windows && strings.EqualFold(k, name)) {
			return i
		}
	}

	return -1
}

// replacePlaceholders ports Process::replacePlaceholders: each "${:NAME}"
// becomes the escaped value of the NAME environment variable.
func replacePlaceholders(commandline string, env []string, windows bool) (string, error) {
	var b strings.Builder

	rest := commandline

	for {
		i := strings.Index(rest, `"${:`)
		if i < 0 {
			break
		}

		name, ok := placeholderName(rest[i+4:])
		if !ok {
			b.WriteString(rest[:i+1])
			rest = rest[i+1:]

			continue
		}

		idx := lookupEnv(env, name, false)
		if idx < 0 {
			return "", &InvalidArgumentError{Class: ClassProcessInvalidArg, Message: `Command line is missing a value for parameter "` + name + `": ` + commandline}
		}

		_, value, _ := strings.Cut(env[idx], "=")
		b.WriteString(rest[:i])
		b.WriteString(symfonyEscapeArgument(value, windows))
		rest = rest[i+4+len(name)+2:]
	}

	if b.Len() == 0 {
		return commandline, nil
	}

	b.WriteString(rest)

	return b.String(), nil
}

// placeholderName matches ([_a-zA-Z]++[_a-zA-Z0-9]*+)\}" at the start of s.
func placeholderName(s string) (string, bool) {
	n := 0
	for n < len(s) && (isASCIIAlpha(s[n]) || s[n] == '_') {
		n++
	}

	if n == 0 {
		return "", false
	}

	for n < len(s) && (isASCIIAlnum(s[n]) || s[n] == '_') {
		n++
	}

	if !strings.HasPrefix(s[n:], `}"`) {
		return "", false
	}

	return s[:n], true
}

// quoteSingle is the Unix branch of Process::escapeArgument and
// ProcessExecutor::escapeArgument: s in single quotes, each ' written as
// '\' followed by a new quote (str_replace, not escapeshellarg(), so every
// byte is kept).
func quoteSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// symfonyEscapeArgument ports Process::escapeArgument.
func symfonyEscapeArgument(argument string, windows bool) string {
	if argument == "" {
		return `""`
	}

	if !windows {
		return quoteSingle(argument)
	}

	argument = strings.ReplaceAll(argument, "\x00", "?")
	if !strings.ContainsAny(argument, "()%!^\"<>&|\t\n\v\f\r []=;*?'$") {
		return argument
	}

	argument = doubleTrailingBackslashes(argument)

	return `"` + strings.NewReplacer(`"`, `""`, "^", `"^^"`, "%", `"^%"`, "!", `"^!"`, "\n", "!LF!").Replace(argument) + `"`
}

// doubleTrailingBackslashes ports preg_replace('/(\\\\*)$/', '$1$1', $s):
// the backslashes ending s, or ending it before a final newline (where PCRE's
// $ also matches), are doubled.
func doubleTrailingBackslashes(s string) string {
	end := len(s)
	if strings.HasSuffix(s, "\n") {
		end--
	}

	start := end
	for start > 0 && s[start-1] == '\\' {
		start--
	}

	if start == end {
		return s
	}

	return s[:end] + s[start:end] + s[end:]
}
