//go:build unix

package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuiltinRm checks that `rm -rf <dir>` runs in-process with rm's
// result, sync and async, logged as the command it stands for.
func TestBuiltinRm(t *testing.T) {
	root := t.TempDir()
	tree := func(name string) string {
		dir := filepath.Join(root, name)
		mustMkdir(t, filepath.Join(dir, "a", "b"))
		mustWrite(t, filepath.Join(dir, "a", "b", "f.php"), "x")
		mustWrite(t, filepath.Join(dir, "g"), "x")

		if err := os.Chmod(filepath.Join(dir, "a", "b", "f.php"), 0o444); err != nil {
			t.Fatal(err)
		}

		if err := os.Symlink(root, filepath.Join(dir, "link")); err != nil {
			t.Fatal(err)
		}

		return dir
	}

	buffer := &bufferIO{debug: true}
	executor := NewProcessExecutor(buffer)

	sync := tree("sync")
	if code, err := executor.Execute(Cmd("rm", "-rf", sync), new(string), ""); err != nil || code != 0 {
		t.Fatalf("Execute = %d, %v", code, err)
	}

	executor.EnableAsync()

	async := tree("async")

	promise, err := executor.ExecuteAsync(Cmd("rm", "-rf", "async"), root)
	if err != nil {
		t.Fatal(err)
	}

	executor.Wait()

	if p, err := promise.Wait(); err != nil || !p.IsSuccessful() {
		t.Fatalf("async: %v", err)
	}

	for _, dir := range []string{sync, async} {
		if _, err := os.Lstat(dir); !os.IsNotExist(err) {
			t.Errorf("%s still there: %v", dir, err)
		}
	}

	if _, err := os.Stat(root); err != nil {
		t.Errorf("a symlink was followed: %v", err)
	}

	// rm -rf of nothing succeeds.
	if code, err := executor.Execute(Cmd("rm", "-rf", filepath.Join(root, "none")), new(string), ""); err != nil || code != 0 {
		t.Errorf("missing directory: %d, %v", code, err)
	}

	want := "Executing command (CWD): 'rm' '-rf' '" + sync + "'\n" +
		"Executing async command (" + root + "): 'rm' '-rf' 'async'\n" +
		"Executing command (CWD): 'rm' '-rf' '" + filepath.Join(root, "none") + "'\n"
	if got := buffer.getOutput(); got != want {
		t.Errorf("debug output %q, want %q", got, want)
	}
}

func TestBuiltinRm_Failure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root removes from read-only directories")
	}

	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	mustMkdir(t, filepath.Join(locked, "sub"))
	mustWrite(t, filepath.Join(locked, "sub", "f"), "x")

	if err := os.Chmod(filepath.Join(locked, "sub"), 0o555); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(filepath.Join(locked, "sub"), 0o755) })

	executor := NewProcessExecutor(nil)

	code, err := executor.Execute(Cmd("rm", "-rf", locked), new(string), "")
	if err != nil || code != 1 {
		t.Fatalf("Execute = %d, %v", code, err)
	}

	if got := executor.GetErrorOutput(); !strings.HasPrefix(got, "rm: cannot remove '") || !strings.HasSuffix(got, "': Permission denied\n") {
		t.Errorf("error output %q", got)
	}
}

// TestBuiltinCommand_LeavesToRm checks the operands rm reads differently
// are not run in-process.
func TestBuiltinCommand_LeavesToRm(t *testing.T) {
	for _, args := range [][]string{
		{"rm", "-rf", ""},
		{"rm", "-rf", "-x"},
		{"rm", "-rf", "dir/"},
		{"rm", "-rf", "/"},
		{"rm", "-rf", "."},
		{"rm", "-rf", "a/.."},
		{"rm", "-r", "dir"},
		{"rm", "-rf", "a", "b"},
		{"rmdir", "-rf", "dir"},
	} {
		if builtinCommand(args, "") != nil {
			t.Errorf("%q runs in-process", args)
		}
	}
}
