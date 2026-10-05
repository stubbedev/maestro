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

func (e *ProcessTimedOutError) Error() string {
	return `The process "` + e.CommandLine + `" exceeded the timeout of ` + strconv.FormatFloat(e.Timeout.Seconds(), 'f', -1, 64) + " seconds."
}

// ProcessSignaledError is Symfony's ProcessSignaledException: the process
// was killed by a signal it was not sent by this Process.
type ProcessSignaledError struct {
	Signal int
}

func (e *ProcessSignaledError) Error() string {
	return `The process has been signaled with signal "` + strconv.Itoa(e.Signal) + `".`
}

// errLaunch is the RuntimeException Process::start throws when proc_open
// fails.
var errLaunch = &RuntimeError{Message: "Unable to launch a new process."}

// Process ports Symfony\Component\Process\Process.
type Process struct {
	args    []string // the argument list, when not a shell command line
	line    string   // the shell command line
	isShell bool
	cwd     string
	env     []string // NAME=value overrides, ahead of the inherited environment
	timeout time.Duration
	tty     bool

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

	outMu  sync.Mutex // guards the buffers
	stdout bytes.Buffer
	stderr bytes.Buffer
	cbMu   sync.Mutex // serialises callbacks
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
		return &RuntimeError{Message: "TTY mode is not supported on Windows platform."}
	}

	if tty && !isTtySupported() {
		return &RuntimeError{Message: "TTY mode requires /dev/tty to be read/writable."}
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
// receives output chunks as they arrive.
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
		return &RuntimeError{Message: "Process is already running."}
	}

	p.outMu.Lock()
	p.stdout.Reset()
	p.stderr.Reset()
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
		return &RuntimeError{Message: `The provided cwd "` + p.cwd + `" does not exist.`}
	}

	cmd, err := shellCommand(commandline, &env)
	if err != nil {
		return err
	}

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
		// Symfony hands the child a stdin pipe it closes at once.
		r, w, err := os.Pipe()
		if err != nil {
			return errLaunch
		}

		_ = w.Close()

		cmd.Stdin = r
		cmd.Stdout = &processWriter{p: p, typ: ProcessOut, callback: callback}
		cmd.Stderr = &processWriter{p: p, typ: ProcessErr, callback: callback}
		cmd.WaitDelay = pipeDrainTimeout
		closers = append(closers, r)
	}

	err = cmd.Start()

	for _, f := range closers {
		_ = f.Close()
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

	go p.wait(cmd, p.done)

	return nil
}

// processWriter receives a stream's output, buffering it and handing it to
// the callback; callbacks for both streams are serialised.
type processWriter struct {
	p        *Process
	typ      string
	callback func(typ, buffer string)
}

func (w *processWriter) Write(b []byte) (int, error) {
	w.p.outMu.Lock()
	if w.typ == ProcessOut {
		w.p.stdout.Write(b)
	} else {
		w.p.stderr.Write(b)
	}
	w.p.outMu.Unlock()

	if w.callback != nil {
		w.p.cbMu.Lock()
		w.callback(w.typ, string(b))
		w.p.cbMu.Unlock()
	}

	return len(b), nil
}

func (p *Process) wait(cmd *exec.Cmd, done chan struct{}) {
	_ = cmd.Wait()

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
// its exit code (128+signal when killed by a signal).
func (p *Process) Wait() (int, error) {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()

		return 0, &LogicError{Message: `Process must be started before calling "wait()".`}
	}

	done := p.done
	p.mu.Unlock()

	<-done

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
		return &LogicError{Message: "Cannot send signal on a non running process."}
	}

	if err := p.sendSignal(sig); err != nil {
		return &RuntimeError{Message: `Error while sending signal "` + strconv.Itoa(sig) + `".`}
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
			return "", &InvalidArgumentError{Message: `Command line is missing a value for parameter "` + name + `": ` + commandline}
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

// symfonyEscapeArgument ports Process::escapeArgument.
func symfonyEscapeArgument(argument string, windows bool) string {
	if argument == "" {
		return `""`
	}

	if !windows {
		return escapeShellArg(argument)
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
