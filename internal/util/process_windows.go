//go:build windows

package util

import (
	"os"
	"os/exec"
	"sync"
	"syscall"
)

const (
	termSignal = 15
	killSignal = 9
	intSignal  = 2
)

// windowsComSpec finds cmd.exe once, as Symfony caches it statically.
var windowsComSpec = sync.OnceValues(func() (string, bool) {
	return NewExecutableFinder().Find("cmd.exe")
})

// directCommand is nil: on Windows every command line goes through
// cmd.exe, as Symfony runs it.
func directCommand([]string, []string) *exec.Cmd { return nil }

// shellCommand runs commandline through cmd.exe as Symfony does on
// Windows, moving quoted arguments into environment variables.
func shellCommand(commandline string, env *[]string) (*exec.Cmd, error) {
	comSpec, ok := windowsComSpec()
	line, vars := prepareWindowsCommandLine(commandline, quoteComSpec(comSpec, ok), newUniqid())
	*env = append(*env, vars...)

	path := comSpec
	if !ok {
		path = "cmd"
	}

	cmd := exec.Command(path) //nolint:gosec // cmd.exe from COMSPEC, as Symfony Process runs it.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: line}

	return cmd, nil
}

// redirectToFiles makes cmd (cmd.exe running a command line) write its
// output to files as Symfony's WindowsPipes do: cmd.exe gets the null
// device for stdout and stderr, and the command line ends with
// " 1>file 2>file". What cmd.exe itself writes outside the redirections
// (its "... was unexpected at this time." for a line it cannot parse) is
// lost, as it is with Composer.
func redirectToFiles(cmd *exec.Cmd) (*filePipes, error) {
	fp, err := newFilePipes()
	if err != nil {
		return nil, err
	}

	cmd.SysProcAttr.CmdLine += fp.redirections()
	cmd.Stdout, cmd.Stderr = nil, nil
	cmd.WaitDelay = 0

	return fp, nil
}

func exitStatus(state *os.ProcessState) (code, termSig int) {
	if state == nil {
		return -1, 0
	}

	return state.ExitCode(), 0
}

// signalProcess: Windows has no signals; Symfony kills the process tree.
func signalProcess(proc *os.Process, _ int) error {
	return proc.Kill()
}

// handledSignals: Ctrl+C, the only signal Windows delivers.
var handledSignals = []os.Signal{os.Interrupt}

func signalName(os.Signal) string {
	return "SIGINT"
}

// exitWithSignal ports SignalHandler::exitWithLastSignal, which exits with
// 128+SIGINT where it cannot raise the signal.
func exitWithSignal(os.Signal) {
	os.Exit(128 + intSignal)
}
