// Integration tests: scripts run through a real ProcessExecutor with the
// shell, and @php / @composer lines with the php on the PATH (skipped when
// there is none).

package eventdispatcher

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/platform"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// realDispatcher dispatches the root package scripts with a real
// ProcessExecutor writing to a NORMAL BufferIO, in a temporary project
// directory.
func realDispatcher(t *testing.T, scripts *php.Array) (*EventDispatcher, *io.BufferIO, string) {
	t.Helper()
	if util.IsWindows() {
		t.Skip("POSIX shell scripts")
	}

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	keepEnv(t, "PATH", "PHP_BINARY", "COMPOSER_BINARY")

	composer := createComposerInstance()
	composer.root.SetScripts(scripts)
	out := bufferIO(t, console.VerbosityNormal)
	d := New(composer, out, nil)

	return d, out, dir
}

func requirePHP(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php is not on the PATH")
	}

	return p
}

func TestIntegration_ShellScriptsPutenvAndReferences(t *testing.T) {
	keepEnv(t, "FOO")
	d, out, _ := realDispatcher(t, php.ArrayOf(
		"post-install-cmd", php.ListOf("echo hello", "@putenv FOO=bar", "echo $FOO", "@other", "@putenv FOO", `echo "[$FOO]"`),
		"other", php.ListOf("echo other-$FOO"),
	))

	if _, err := d.DispatchScript("post-install-cmd", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, out,
		"> echo hello", "hello",
		"> @putenv FOO=bar",
		"> echo $FOO", "bar",
		"> echo other-$FOO", "other-bar",
		"> @putenv FOO",
		`> echo "[$FOO]"`, "[]",
	)
}

func TestIntegration_AdditionalArgs(t *testing.T) {
	d, out, _ := realDispatcher(t, php.ArrayOf("test", php.ListOf("echo", "echo [@additional_args]")))

	if _, err := d.DispatchScript("test", false, []string{"a b", "it's"}, nil); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, out,
		`> echo 'a b' 'it'\''s'`, "a b it's",
		`> echo ['a b' 'it'\''s']`, "[a b it's]",
	)
}

// Verified against Composer 2.10.3: an @script reference using
// @additional_args gets no arguments at all (array_splice's return value).
func TestIntegration_VerboseReferences(t *testing.T) {
	d, _, _ := realDispatcher(t, php.ArrayOf(
		"test", php.ListOf("@missing", "echo x @no_additional_args", "@sub a @additional_args b"),
		"sub", php.ListOf("echo sub"),
	))
	out := bufferIO(t, console.VerbosityVerbose)
	d.io = out
	d.process = http.NewProcessExecutor(out)

	if _, err := d.DispatchScript("test", false, []string{"u"}, nil); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, out,
		"> test (u): @missing",
		"<warning>You made a reference to a non-existent script @missing</warning>",
		"> test: echo x", "x",
		"> test (u): @sub a @additional_args b",
		"> sub: echo sub", "sub",
	)
}

func TestIntegration_FailingScripts(t *testing.T) {
	d, out, _ := realDispatcher(t, php.ArrayOf(
		"post-install-cmd", php.ListOf("@fail", "echo never"),
		"fail", php.ListOf("echo oops >&2; exit 3"),
	))

	code, err := d.DispatchScript("post-install-cmd", false, nil, nil)
	var e *ScriptExecutionError
	if !errors.As(err, &e) || e.Code != 3 || e.Message != "Error Output: oops\n" || code != 0 {
		t.Fatalf("code, err = %d, %#v", code, err)
	}
	expectOutput(t, out,
		"> echo oops >&2; exit 3",
		"oops",
		"Script echo oops >&2; exit 3 handling the fail event returned with error code 3",
		"Script @fail was called via post-install-cmd",
	)
}

func TestIntegration_BinDirOnPath(t *testing.T) {
	d, out, dir := realDispatcher(t, php.ArrayOf("test", php.ListOf("mytool arg")))
	writeExecutable(t, filepath.Join(dir, "vendor", "bin", "mytool"), "#!/bin/sh\necho \"mytool $1\"\n")

	if _, err := d.DispatchScript("test", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DispatchScript("test", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, out, "> mytool arg", "mytool arg", "> mytool arg", "mytool arg")

	binDir := filepath.Join(dir, "vendor", "bin")
	if p := os.Getenv("PATH"); !strings.HasPrefix(p, binDir+string(os.PathListSeparator)) || strings.Count(p, binDir) != 1 {
		t.Errorf("PATH = %q, want %s prepended once", p, binDir)
	}
}

func TestIntegration_LocalBinaryRunsThroughItsInterpreter(t *testing.T) {
	d, out, dir := realDispatcher(t, php.ArrayOf("test", php.ListOf("tool")))
	// not executable: the dispatcher runs it with the shebang's interpreter
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "tool"), []byte("#!/usr/bin/env sh\necho \"tool ran by sh\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.composer.Package().(*pkg.RootPackage).SetBinaries(php.ListOf("bin/tool"))

	if _, err := d.DispatchScript("test", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, out, "> tool", "tool ran by sh")
}

func TestIntegration_PhpScripts(t *testing.T) {
	phpBin := requirePHP(t)
	d, out, dir := realDispatcher(t, php.ArrayOf("test", php.ListOf(
		`@php -r "echo ini_get('memory_limit'), PHP_EOL;"`,
		"@php script.php one",
		`echo "$PHP_BINARY"`,
	)))
	t.Setenv("COMPOSER_MEMORY_LIMIT", "")
	if err := os.WriteFile(filepath.Join(dir, "script.php"), []byte(`<?php echo 'script ', $argv[1], PHP_EOL;`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PHP_BINARY", phpBin)

	if _, err := d.DispatchScript("test", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	// bin/composer raises memory_limit to 1536M unless it is higher
	// (or -1) already, and scripts inherit it
	want := []string{
		`> @php -r "echo ini_get('memory_limit'), PHP_EOL;"`, phpMemoryLimit(t, phpBin),
		"> @php script.php one", "script one",
		`> echo "$PHP_BINARY"`, phpBin,
	}
	expectOutput(t, out, want...)
}

// phpMemoryLimit is the memory_limit of Composer's process: the probed
// php's, as bin/composer raises it.
func phpMemoryLimit(t *testing.T, phpBin string) string {
	t.Helper()
	s, err := platform.Probe(t.Context(), phpBin)
	if err != nil {
		t.Fatal(err)
	}
	view, _ := s.ComposerView(os.LookupEnv)
	v, _ := view.IniGet("memory_limit")

	return v
}

func TestIntegration_ComposerLauncher(t *testing.T) {
	phpBin := requirePHP(t)
	d, out, dir := realDispatcher(t, php.ArrayOf("test", php.ListOf("@composer show --foo", "composer validate x")))
	t.Setenv("PHP_BINARY", phpBin)

	// a stand-in launcher: the real one proc_opens maestro
	launcher := filepath.Join(dir, "launcher dir", "composer")
	t.Setenv("COMPOSER_BINARY", launcher)
	var ensured int
	d.SetEnsureComposerBinary(func() error {
		ensured++
		if err := os.MkdirAll(filepath.Dir(launcher), 0o755); err != nil {
			return err
		}

		return os.WriteFile(launcher, []byte(`<?php echo 'launcher ', implode(' ', array_slice($argv, 1)), PHP_EOL;`), 0o644)
	})

	if _, err := d.DispatchScript("test", false, []string{"--extra"}, nil); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, out,
		"launcher show --foo --extra",
		"> composer validate x '--extra'",
		"launcher validate x --extra",
	)
	if ensured != 2 {
		t.Errorf("launcher ensured %d times, want 2", ensured)
	}
}

func TestIntegration_OutputThroughIO(t *testing.T) {
	if util.IsWindows() {
		t.Skip("POSIX shell scripts")
	}
	keepEnv(t, "PATH", "PHP_BINARY")
	ioi := newRecordingIO()
	d := New(createComposerInstance(), ioi, http.NewProcessExecutor(ioi))
	d.SetPHP(fakePHP{})
	d.AddListener("ev", Script("printf 'a\\nb\\n'"), 0)

	if _, err := d.Dispatch("ev", nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ioi.raws, ""); got != "a\nb\n" {
		t.Errorf("raw output %q", got)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
