package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/archive/archivetest"
	"github.com/stubbedev/maestro/internal/switches"
)

// budgetFiles and budgetDirs shape the package TestInstallSyscallBudget
// installs: enough files that the process's fixed cost (its start, the
// archive, the index) is a small part of the count.
const (
	budgetFiles = 2000
	budgetDirs  = 40
)

// installBudget is how many system calls, the Go scheduler's left out,
// installing a package into an empty store may make per file. A new file
// takes five on Linux: its object is opened (O_EXCL), written and stamped
// (futimens) and closed, and the package file hard-linked to it; each
// directory takes a mkdir, and the rest is the process's fixed cost.
const installBudget = 5.5

// schedulerCalls are the system calls the Go runtime makes on its own
// (scheduling, signals, memory): how many depends on the machine's load,
// not on the store.
var schedulerCalls = map[string]bool{
	"futex": true, "nanosleep": true, "sched_yield": true, "epoll_pwait": true, "epoll_wait": true,
	"tgkill": true, "getpid": true, "gettid": true, "rt_sigreturn": true, "rt_sigaction": true,
	"rt_sigprocmask": true, "sigaltstack": true, "madvise": true, "mmap": true, "munmap": true,
	"clone": true, "clone3": true, "restart_syscall": true,
}

// TestInstallSyscallBudget installs a package of budgetFiles files into an
// empty store in a process of its own under strace, and holds the system
// calls it makes per file to installBudget.
func TestInstallSyscallBudget(t *testing.T) {
	if !switches.On(switches.SyscallBudgets) {
		t.Skip("set " + switches.SyscallBudgets + "=1 to count the system calls of an install under strace")
	}

	strace, err := exec.LookPath("strace")
	if err != nil {
		t.Skip("strace not found")
	}

	work := tempDir(t)

	entries := []archivetest.ZipEntry{archivetest.UnixDir("pkg/", 0o755)}
	for d := range budgetDirs {
		dir := fmt.Sprintf("pkg/d%02d/", d)
		entries = append(entries, archivetest.UnixDir(dir, 0o755))

		for f := range budgetFiles / budgetDirs {
			entries = append(entries, archivetest.UnixFile(fmt.Sprintf("%sf%03d.php", dir, f), 0o644, fmt.Sprintf("<?php // %d %d\n", d, f)))
		}
	}

	writeFile(t, work, "real.zip", archivetest.Zip("", entries...))

	if err := os.Symlink("real.zip", filepath.Join(work, "dist.zip")); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(work, "strace.txt")
	cmd := exec.Command(strace, "-f", "-c", "-o", out, os.Args[0])
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "MAESTRO_STORE_HELPER=1")

	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install under strace: %v\n%s", err, msg)
	}

	if st, err := lstat(filepath.Join(work, "pkg", "d00", "f000.php")); err != nil || st.nlink != 2 {
		t.Fatalf("the package file is not linked to its object: %+v %v", st, err)
	}

	calls, scheduler := countCalls(t, out)
	perFile := float64(calls) / budgetFiles
	t.Logf("%d files in %d directories: %d system calls (%.2f per file), %d more by the Go scheduler", budgetFiles, budgetDirs, calls, perFile, scheduler)

	if perFile > installBudget {
		summary, _ := os.ReadFile(out)
		t.Errorf("%.2f system calls per file, budget %.1f:\n%s", perFile, installBudget, summary)
	}
}

// countCalls reads strace -c's summary: the calls the store made, and those
// of the Go scheduler.
func countCalls(t *testing.T, path string) (calls, scheduler int) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	for line := range strings.Lines(string(data)) {
		f := strings.Fields(line)
		if len(f) < 5 || f[len(f)-1] == "total" {
			continue
		}

		n, err := strconv.Atoi(f[3])
		if err != nil {
			continue
		}

		if schedulerCalls[f[len(f)-1]] {
			scheduler += n
		} else {
			calls += n
		}
	}

	if calls == 0 {
		t.Fatalf("no system calls in strace's summary:\n%s", data)
	}

	return calls, scheduler
}
