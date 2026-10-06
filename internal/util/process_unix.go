//go:build unix

package util

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	termSignal = int(syscall.SIGTERM)
	killSignal = int(syscall.SIGKILL)
	intSignal  = int(syscall.SIGINT)
)

// shellCommand runs commandline the way proc_open does: execl("/bin/sh",
// "sh", "-c", commandline).
func shellCommand(commandline string, _ *[]string) (*exec.Cmd, error) {
	cmd := exec.Command("/bin/sh", "-c", commandline) //nolint:gosec // running command lines is the point.
	cmd.Args[0] = "sh"

	return cmd, nil
}

// directTools are the programs directCommand starts without a shell:
// version control tools, which read neither the variables a shell adds to
// the environment (SHLVL, _, PWD) nor anything else it could change.
var directTools = map[string]bool{"git": true, "hg": true, "svn": true, "fossil": true, "p4": true}

// directCommand is the command `sh -c "exec <args>"` ends up running, for
// an argument list naming one of directTools found in an absolute
// directory of env's PATH as an executable regular file; nil otherwise
// (the shell then runs it). argv[0] stays the name, as the shell's exec
// gives it.
func directCommand(args []string, env []string) *exec.Cmd {
	if len(args) == 0 || !directTools[args[0]] {
		return nil
	}

	// the last PATH, as the child gets it
	path := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			path = v
		}
	}

	for _, dir := range filepath.SplitList(path) {
		// the shell resolves a relative entry against the child's
		// directory: leave those to it
		if !filepath.IsAbs(dir) {
			return nil
		}

		file := dir + "/" + args[0]
		if fi, err := os.Stat(file); err == nil && fi.Mode().IsRegular() && unix.Access(file, unix.X_OK) == nil {
			cmd := exec.Command(file, args[1:]...) //nolint:gosec // running VCS commands is the point.
			cmd.Args[0] = args[0]

			return cmd
		}
	}

	return nil
}

// exitStatus returns the exit code, 128+signal for a process killed by a
// signal (Symfony's convention), and the terminating signal.
func exitStatus(state *os.ProcessState) (code, termSig int) {
	if state == nil {
		return -1, 0
	}

	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal()), int(ws.Signal())
	}

	return state.ExitCode(), 0
}

func signalProcess(proc *os.Process, sig int) error {
	return proc.Signal(syscall.Signal(sig))
}

// handledSignals are the signals Composer's SignalHandler intercepts while
// a child process runs.
var handledSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}

// signalName is SignalHandler's name for a handled signal.
func signalName(sig os.Signal) string {
	switch sig {
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGHUP:
		return "SIGHUP"
	default:
		return "SIGINT"
	}
}

// exitWithSignal ports SignalHandler::exitWithLastSignal: restores the
// default action and re-raises the signal, exiting with 128+signal should
// that not end the process.
func exitWithSignal(sig os.Signal) {
	s, _ := sig.(syscall.Signal)
	if s == 0 {
		s = syscall.SIGINT
	}

	signal.Reset(s)
	_ = syscall.Kill(os.Getpid(), s)

	os.Exit(128 + int(s))
}
