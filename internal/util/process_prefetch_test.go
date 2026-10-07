package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeTool writes an executable shell script named name into a new
// directory and puts that directory first in PATH.
func fakeTool(t *testing.T, name, script string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, name)

	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	return path
}

func TestProcessExecutor_Prefetch(t *testing.T) {
	skipOnWindows(t)

	fakeTool(t, "git", `echo "out $1 ${MAESTRO_PREFETCH_TEST:-unset}"; echo "err $1" >&2; exit 3`+"\n")

	buffer := &bufferIO{debug: true}
	process := NewProcessExecutor(buffer)
	dir := t.TempDir()

	process.Prefetch(Cmd("git", "a"), dir)

	if got := buffer.getOutput(); got != "" {
		t.Errorf("prefetching logged %q", got)
	}

	var output string

	code, err := process.Execute(Cmd("git", "a"), &output, dir)
	if err != nil {
		t.Fatal(err)
	}

	if code != 3 || output != "out a unset\n" || process.GetErrorOutput() != "err a\n" {
		t.Errorf("code %d, output %q, error output %q", code, output, process.GetErrorOutput())
	}

	if got, want := buffer.getOutput(), "Executing command ("+dir+"): 'git' 'a'\n"; got != want {
		t.Errorf("debug output %q, want %q", got, want)
	}

	// taken once: the next run starts its own process
	t.Setenv("MAESTRO_PREFETCH_TEST", "1")

	if _, err := process.Execute(Cmd("git", "a"), &output, dir); err != nil || output != "out a 1\n" {
		t.Errorf("second run: %q, %v", output, err)
	}

	// a prefetch started in another environment is not taken
	process.Prefetch(Cmd("git", "b"), dir)
	t.Setenv("MAESTRO_PREFETCH_TEST", "2")

	if _, err := process.Execute(Cmd("git", "b"), &output, dir); err != nil || output != "out b 2\n" {
		t.Errorf("after an environment change: %q, %v", output, err)
	}

	// nor one for another directory
	process.Prefetch(Cmd("git", "c"), dir)

	other := t.TempDir()
	if _, err := process.Execute(Cmd("git", "c"), &output, other); err != nil || output != "out c 2\n" {
		t.Errorf("other directory: %q, %v", output, err)
	}
}

func TestProcessExecutor_PrefetchEnv(t *testing.T) {
	skipOnWindows(t)

	runs := filepath.Join(t.TempDir(), "runs")
	fakeTool(t, "git", `echo "$1" >> '`+runs+`'; echo "out $1 ${MAESTRO_PREFETCH_TEST:-unset}"`+"\n")

	dir := t.TempDir()
	execute := func(name string) string {
		t.Helper()

		var output string
		if _, err := NewProcessExecutor(nil).Execute(Cmd("git", name), &output, dir); err != nil {
			t.Fatal(err)
		}

		return output
	}
	// started counts the runs of git name, once count of them started
	started := func(name string, count int) int {
		t.Helper()

		for range 500 {
			data, _ := os.ReadFile(runs)
			if n := strings.Count(string(data), name+"\n"); n >= count {
				return n
			}
			time.Sleep(10 * time.Millisecond)
		}

		return -1
	}

	// started for the environment to come, taken by another executor once
	// the environment is that
	NewProcessExecutor(nil).PrefetchEnv(Cmd("git", "a"), dir, map[string]string{"MAESTRO_PREFETCH_TEST": "set"})
	t.Setenv("MAESTRO_PREFETCH_TEST", "set")

	if got := execute("a"); got != "out a set\n" || started("a", 1) != 1 {
		t.Errorf("taken: %q, %d runs", got, started("a", 1))
	}

	// not taken when the environment came out otherwise
	NewProcessExecutor(nil).PrefetchEnv(Cmd("git", "b"), dir, map[string]string{"MAESTRO_PREFETCH_TEST": "other"})

	if got := execute("b"); got != "out b set\n" || started("b", 2) != 2 {
		t.Errorf("other environment: %q", got)
	}

	// nor when the process timeout changed in between
	NewProcessExecutor(nil).Prefetch(Cmd("git", "c"), dir)

	timeout := GetProcessTimeout()
	SetProcessTimeout(timeout + 1)
	t.Cleanup(func() { SetProcessTimeout(timeout) })

	if got := execute("c"); got != "out c set\n" || started("c", 2) != 2 {
		t.Errorf("other timeout: %q", got)
	}
}

func TestDirectCommand(t *testing.T) {
	skipOnWindows(t)

	git := fakeTool(t, "git", "exit 0\n")
	env := os.Environ()

	cmd := directCommand([]string{"git", "status"}, env)
	if cmd == nil || cmd.Path != git || strings.Join(cmd.Args, " ") != "git status" {
		t.Fatalf("directCommand = %+v", cmd)
	}

	// not a VCS tool, not found, or a relative PATH entry first: the
	// shell runs it
	for _, tc := range []struct {
		args []string
		path string
	}{
		{[]string{"printf", "x"}, os.Getenv("PATH")},
		{[]string{"git"}, t.TempDir()},
		{[]string{"git"}, "bin" + string(os.PathListSeparator) + filepath.Dir(git)},
		{[]string{"git"}, ""},
	} {
		if cmd := directCommand(tc.args, []string{"PATH=" + tc.path}); cmd != nil {
			t.Errorf("directCommand(%q) with PATH %q = %v", tc.args, tc.path, cmd.Path)
		}
	}

	// started without a shell, it runs as through one
	var output string

	fakeTool(t, "hg", `printf '%s|' "$@"`+"\n")

	code, err := NewProcessExecutor(nil).Execute(Cmd("hg", "a b", "it's"), &output, "")
	if err != nil || code != 0 || output != "a b|it's|" {
		t.Errorf("hg: %d, %q, %v", code, output, err)
	}

	// a tool the shell cannot find still gives its answer
	t.Setenv("PATH", t.TempDir())

	code, err = NewProcessExecutor(nil).Execute(Cmd("fossil", "version"), &output, "")
	if err != nil || code != 127 {
		t.Errorf("missing fossil: %d, %v", code, err)
	}
}

func TestExecutableFinder_FindShellOnlyName(t *testing.T) {
	skipOnWindows(t)

	// `command -v` names a builtin by its bare name, found when the working
	// directory has an executable of that name
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile(filepath.Join(dir, "echo"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", t.TempDir())

	finder := NewExecutableFinder()

	if got, ok := finder.Find("echo"); !ok || got != "echo" {
		t.Errorf("Find(echo) = %q, %v", got, ok)
	}

	if _, ok := finder.Find("maestro-surely-missing"); ok {
		t.Error("found a missing command")
	}
}
