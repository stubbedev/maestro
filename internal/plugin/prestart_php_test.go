package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// A prestarted child is the one Start boots, and what changed in between
// (the environment, the working directory) reaches PHP; one that nothing
// needs is killed when the run ends, without output.
func TestRuntime_Prestart(t *testing.T) {
	requirePHP(t)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(wd)
	dir := t.TempDir()
	t.Cleanup(func() { _ = os.Chdir(wd) })

	t.Run("used", func(t *testing.T) {
		rt, stdout, stderr := newTestRuntime(t)
		t.Setenv("MAESTRO_T_PRE", "before")
		rt.Prestart()
		rt.Prestart()
		<-rt.pre.done
		pid := rt.Info().PID
		if pid == 0 {
			t.Fatal("the prestart did not reach the handshake")
		}

		t.Setenv("MAESTRO_T_PRE", "after")
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		start(t, rt)
		if got := rt.Info().PID; got != pid {
			t.Errorf("Start ran php %d, the prestart %d", got, pid)
		}

		v, err := rt.Call("test.getenv", php.ListOf("MAESTRO_T_PRE"))
		if err != nil {
			t.Fatal(err)
		}
		if got := get(t, v, "MAESTRO_T_PRE").(*php.Array).Values(); got[0] != "after" || got[1] != "after" {
			t.Errorf("PHP sees MAESTRO_T_PRE = %v", got)
		}
		cwd, err := rt.Call("test.getcwd", nil)
		if err != nil {
			t.Fatal(err)
		}
		if resolved, _ := filepath.EvalSymlinks(dir); cwd != dir && cwd != resolved {
			t.Errorf("PHP's cwd = %v, want %s", cwd, dir)
		}

		if code, err := rt.Shutdown(0); err != nil || code != 0 {
			t.Errorf("Shutdown = %d, %v", code, err)
		}
		if out := readFile(t, stdout) + readFile(t, stderr); out != "" {
			t.Errorf("output %q", out)
		}
	})

	t.Run("unused", func(t *testing.T) {
		rt, stdout, stderr := newTestRuntime(t)
		rt.Prestart()
		<-rt.pre.done
		c := rt.child

		if code := rt.Finish(3); code != 3 {
			t.Errorf("Finish = %d, want 3", code)
		}
		select {
		case <-c.done:
		default:
			t.Error("the prestarted php still runs")
		}
		if out := readFile(t, stdout) + readFile(t, stderr); out != "" {
			t.Errorf("output %q", out)
		}
	})
}

// A prestart that finds no php leaves the error to Start, which names
// what needed PHP.
func TestRuntime_PrestartWithoutPHP(t *testing.T) {
	rt, _, _ := newTestRuntime(t, func(o *Options) {
		o.FindPHP = func() (string, bool) { return "", false }
	})
	rt.Prestart()
	err := rt.Start(`plugin "a/b"`)
	if err == nil || !strings.Contains(err.Error(), `plugin "a/b" requires PHP`) {
		t.Errorf("Start = %v", err)
	}
}
