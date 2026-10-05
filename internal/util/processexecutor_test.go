// Ports tests/Composer/Test/Util/ProcessExecutorTest.php, plus tests of the
// symfony/process parts behind it.

package util

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// bufferIO stands in for Composer's BufferIO at debug verbosity and records
// the raw writes the IOInterface mock of the PHP tests expects.
type bufferIO struct {
	mu       sync.Mutex
	debug    bool
	output   strings.Builder
	raw      []string
	errorRaw []string
}

func (b *bufferIO) IsDebug() bool { return b.debug }

func (b *bufferIO) WriteError(message string, newline bool, _ int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.output.WriteString(message)
	if newline {
		b.output.WriteByte('\n')
	}
}

func (b *bufferIO) WriteRaw(message string, _ bool, _ int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.raw = append(b.raw, message)
}

func (b *bufferIO) WriteErrorRaw(message string, _ bool, _ int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.errorRaw = append(b.errorRaw, message)
}

func (b *bufferIO) getOutput() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.output.String()
}

func skipOnWindows(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell test")
	}
}

func TestProcessExecutor_ExecuteCapturesOutput(t *testing.T) {
	skipOnWindows(t)

	var output string
	if _, err := NewProcessExecutor(nil).Execute(ShellCmd("echo foo"), &output, ""); err != nil {
		t.Fatal(err)
	}

	if output != "foo\n" {
		t.Errorf("output = %q", output)
	}
}

func TestProcessExecutor_ExecuteOutputsIfNotCaptured(t *testing.T) {
	skipOnWindows(t)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	stdout := os.Stdout
	os.Stdout = w

	_, err = NewProcessExecutor(nil).Execute(ShellCmd("echo foo"), nil, "")

	os.Stdout = stdout
	w.Close()

	if err != nil {
		t.Fatal(err)
	}

	got, _ := io.ReadAll(r)
	if string(got) != "foo\n" {
		t.Errorf("stdout = %q", got)
	}
}

func TestProcessExecutor_UseIOIsNotNullAndIfNotCaptured(t *testing.T) {
	skipOnWindows(t)

	buffer := &bufferIO{}
	if _, err := NewProcessExecutor(buffer).Execute(ShellCmd("echo foo"), nil, ""); err != nil {
		t.Fatal(err)
	}

	if len(buffer.raw) != 1 || buffer.raw[0] != "foo\n" {
		t.Errorf("writeRaw calls = %q", buffer.raw)
	}
}

func TestProcessExecutor_ExecuteCapturesStderr(t *testing.T) {
	skipOnWindows(t)

	process := NewProcessExecutor(nil)

	var output string
	if _, err := process.Execute(ShellCmd("cat foo"), &output, t.TempDir()); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(process.GetErrorOutput(), "foo: No such file or directory") {
		t.Errorf("error output = %q", process.GetErrorOutput())
	}
}

func TestProcessExecutor_Timeout(t *testing.T) {
	previous := GetProcessTimeout()
	defer SetProcessTimeout(previous)

	SetProcessTimeout(1)

	if got := GetProcessTimeout(); got != 1 {
		t.Errorf("timeout = %d", got)
	}
}

func TestProcessExecutor_HidePasswords(t *testing.T) {
	skipOnWindows(t)

	for _, c := range []struct{ command, expected string }{
		{"echo https://foo:bar@example.org/", "echo https://foo:***@example.org/"},
		{"echo http://foo@example.org", "echo http://foo@example.org"},
		{"echo http://abcdef1234567890234578:x-oauth-token@github.com/", "echo http://abc***:***@github.com/"},
		{"echo http://github_pat_1234567890abcdefghijkl_1234567890abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVW:x-oauth-token@github.com/", "echo http://git***:***@github.com/"},
		{"echo http://ghp_1234567890abcdefghijklmnopqrstuvwxyzAB@github.com/", "echo http://ghp***@github.com/"},
		{"echo http://abcdef1234567890234578@github.com/", "echo http://abc***@github.com/"},
		{"svn ls --verbose --non-interactive  --username 'foo' --password 'bar'  'https://foo.example.org/svn/'", "svn ls --verbose --non-interactive  --username 'foo' --password '***'  'https://foo.example.org/svn/'"},
		{"svn ls --verbose --non-interactive  --username 'foo' --password 'bar \\'bar'  'https://foo.example.org/svn/'", "svn ls --verbose --non-interactive  --username 'foo' --password '***'  'https://foo.example.org/svn/'"},
	} {
		buffer := &bufferIO{debug: true}

		var output string
		if _, err := NewProcessExecutor(buffer).Execute(ShellCmd(c.command), &output, ""); err != nil {
			t.Fatal(err)
		}

		if got := strings.TrimSpace(buffer.getOutput()); got != "Executing command (CWD): "+c.expected {
			t.Errorf("%s:\n got %q\nwant %q", c.command, got, "Executing command (CWD): "+c.expected)
		}
	}
}

func TestProcessExecutor_DoesntHidePorts(t *testing.T) {
	skipOnWindows(t)

	buffer := &bufferIO{debug: true}

	var output string
	if _, err := NewProcessExecutor(buffer).Execute(ShellCmd("echo https://localhost:1234/"), &output, ""); err != nil {
		t.Fatal(err)
	}

	if got := strings.TrimSpace(buffer.getOutput()); got != "Executing command (CWD): echo https://localhost:1234/" {
		t.Errorf("got %q", got)
	}
}

func TestProcessExecutor_SplitLines(t *testing.T) {
	process := NewProcessExecutor(nil)

	for _, c := range []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"foo", []string{"foo"}},
		{"foo\nbar", []string{"foo", "bar"}},
		{"foo\r\nbar", []string{"foo", "bar"}},
		{"foo\r\nbar\n", []string{"foo", "bar"}},
		// Beyond the PHP test: only one \r before \n goes, inner blank
		// lines stay, trim() strips the ends.
		{"foo\r\r\nbar", []string{"foo\r", "bar"}},
		{"a\n\nb\r\n\r\nc", []string{"a", "", "b", "", "c"}},
		{"\x00 a\rb \x0b", []string{"a\rb"}},
	} {
		got := process.SplitLines(c.in)
		if got == nil || strings.Join(got, "|") != strings.Join(c.want, "|") || len(got) != len(c.want) {
			t.Errorf("SplitLines(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The PHP test runs php printing a console tag through a ConsoleIO; the
// point is that process output goes to writeRaw, unformatted.
func TestProcessExecutor_ConsoleIODoesNotFormatSymfonyConsoleStyle(t *testing.T) {
	skipOnWindows(t)

	buffer := &bufferIO{}
	if _, err := NewProcessExecutor(buffer).Execute(ShellCmd(`echo '<error>foo</error>'`), nil, ""); err != nil {
		t.Fatal(err)
	}

	if strings.Join(buffer.raw, "") != "<error>foo</error>\n" {
		t.Errorf("raw output = %q", buffer.raw)
	}
}

func TestProcessExecutor_ExecuteAsyncCancel(t *testing.T) {
	skipOnWindows(t)

	process := NewProcessExecutor(&bufferIO{debug: true})
	process.EnableAsync()

	start := time.Now()

	promise, err := process.ExecuteAsync(ShellCmd("sleep 2"), "")
	if err != nil {
		t.Fatal(err)
	}

	if n := process.CountActiveJobs(); n != 1 {
		t.Errorf("active jobs = %d, want 1", n)
	}

	promise.Cancel()

	if n := process.CountActiveJobs(); n != 0 {
		t.Errorf("active jobs = %d, want 0", n)
	}

	process.Wait()

	if elapsed := time.Since(start); elapsed >= 2*time.Second {
		t.Errorf("Canceling took longer than it should, lasted %s", elapsed)
	}

	if _, err := promise.Wait(); err == nil || err.Error() != "Aborted process" {
		t.Errorf("promise error = %v", err)
	}
}

func TestProcessExecutor_EscapeArgument(t *testing.T) {
	for name, c := range map[string]struct{ argument, win, unix string }{
		"empty":       {"", `""`, "''"},
		"empty null":  {"", `""`, "''"},
		"empty false": {"", `""`, "''"},
		"unix-sq":     {"a'bc", "a'bc", `'a'\''bc'`},
		"new lines":   {"a\nb\nc", `"a b c"`, "'a\nb\nc'"},
		"ws space":    {"a b c", `"a b c"`, "'a b c'"},
		"ws tab":      {"a\tb\tc", "\"a\tb\tc\"", "'a\tb\tc'"},
		"no-ws":       {"abc", "abc", "'abc'"},
		"comma":       {"a,bc", `"a,bc"`, "'a,bc'"},
		"dq":          {`a"bc`, `a\^"bc`, `'a"bc'`},
		"dq-bslash":   {`a\"bc`, `a\\\^"bc`, `'a\"bc'`},
		"bslash":      {`ab\\c\`, `ab\\c\`, `'ab\\c\'`},
		"bslash dq":   {`a b c\\`, `"a b c\\\\"`, `'a b c\\'`},
		"meta dq":     {`a "b" c`, `^"a \^"b\^" c^"`, `'a "b" c'`},
		"meta-pc1":    {"%path%", "^%path^%", "'%path%'"},
		"meta-pc2":    {"%path", "%path", "'%path'"},
		"meta-pc3":    {"%%path", "%%path", "'%%path'"},
		"meta-bang1":  {"!path!", "^^!path^^!", "'!path!'"},
		"meta-bang2":  {"!path", "!path", "'!path'"},
		"meta-bang3":  {"!!path", "!!path", "'!!path'"},
		"meta-all-dq": {`<>"&|()^`, `^<^>\^"^&^|^(^)^^`, `'<>"&|()^'`},
		"other meta":  {"<> &| ()^", `"<> &| ()^"`, "'<> &| ()^'"},
		"other-meta":  {"<>&|()^", `"<>&|()^"`, "'<>&|()^'"},
	} {
		if got := escapeArgument(c.argument, true); got != c.win {
			t.Errorf("%s: windows escape(%q) = %q, want %q", name, c.argument, got, c.win)
		}

		if got := escapeArgument(c.argument, false); got != c.unix {
			t.Errorf("%s: unix escape(%q) = %q, want %q", name, c.argument, got, c.unix)
		}
	}

	want := escapeArgument("a b", IsWindows())
	if got := Escape("a b"); got != want {
		t.Errorf("Escape = %q, want %q", got, want)
	}
}

func TestProcessExecutor_ExecuteFunc(t *testing.T) {
	skipOnWindows(t)

	var mu sync.Mutex

	got := map[string]string{}

	code, err := NewProcessExecutor(nil).ExecuteFunc(ShellCmd("echo out; echo err >&2; exit 3"), func(typ, buffer string) {
		mu.Lock()
		got[typ] += buffer
		mu.Unlock()
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	if code != 3 || got[ProcessOut] != "out\n" || got[ProcessErr] != "err\n" {
		t.Errorf("code %d, output %q", code, got)
	}
}

func TestProcessExecutor_ArgumentList(t *testing.T) {
	skipOnWindows(t)

	buffer := &bufferIO{debug: true}
	process := NewProcessExecutor(buffer)
	dir := t.TempDir()

	var output string

	code, err := process.Execute(Cmd("printf", "%s|", "a b", "it's", ""), &output, dir)
	if err != nil {
		t.Fatal(err)
	}

	if code != 0 || output != "a b|it's||" {
		t.Errorf("code %d, output %q", code, output)
	}

	if got, want := buffer.getOutput(), "Executing command ("+dir+`): 'printf' '%s|' 'a b' 'it'\''s' ''`+"\n"; got != want {
		t.Errorf("debug output %q, want %q", got, want)
	}
}

func TestProcessExecutor_MissingCwd(t *testing.T) {
	skipOnWindows(t)

	dir := filepath.Join(t.TempDir(), "missing")

	_, err := NewProcessExecutor(nil).Execute(ShellCmd("true"), nil, dir)

	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) || err.Error() != `The provided cwd "`+dir+`" does not exist.` {
		t.Errorf("err = %v", err)
	}
}

func TestProcessExecutor_Signaled(t *testing.T) {
	skipOnWindows(t)

	// A child killed by a signal it was not sent by Composer: PHP catches
	// the ProcessSignaledException and returns the exit code, 128+signal.
	code, err := NewProcessExecutor(nil).Execute(ShellCmd("kill -9 $$"), new(string), "")
	if err != nil || code != 137 {
		t.Errorf("code %d, err %v", code, err)
	}

	_, err = NewShellProcess("kill -9 $$", "", nil, 0).Run(nil)
	if err == nil || err.Error() != `The process has been signaled with signal "9".` {
		t.Errorf("err = %v", err)
	}
}

func TestProcess_TimedOut(t *testing.T) {
	skipOnWindows(t)

	start := time.Now()

	code, err := NewShellProcess("sleep 5", "", nil, 100*time.Millisecond).Run(nil)

	var timedOut *ProcessTimedOutError
	if !errors.As(err, &timedOut) || err.Error() != `The process "sleep 5" exceeded the timeout of 0.1 seconds.` {
		t.Errorf("err = %v", err)
	}

	if code != 128+15 || time.Since(start) > 3*time.Second {
		t.Errorf("code %d after %s", code, time.Since(start))
	}

	_, err = NewProcess([]string{"sleep", "5"}, "", nil, time.Second/2).Run(nil)
	if err == nil || err.Error() != `The process "'sleep' '5'" exceeded the timeout of 0.5 seconds.` {
		t.Errorf("err = %v", err)
	}
}

func TestProcess_Env(t *testing.T) {
	skipOnWindows(t)

	t.Setenv("MAESTRO_INHERITED", "inherited")

	p := NewShellProcess(`printf %s "$MAESTRO_SET-$MAESTRO_INHERITED" "${:MAESTRO_SET}"`, "", map[string]string{"MAESTRO_SET": "a'b"}, 0)
	if _, err := p.Run(nil); err != nil {
		t.Fatal(err)
	}

	if got := p.GetOutput(); got != "a'b-inheriteda'b" {
		t.Errorf("output %q", got)
	}

	_, err := NewShellProcess(`echo "${:MAESTRO_NOPE}"`, "", nil, 0).Run(nil)
	if err == nil || err.Error() != `Command line is missing a value for parameter "MAESTRO_NOPE": echo "${:MAESTRO_NOPE}"` {
		t.Errorf("err = %v", err)
	}
}

func TestReplacePlaceholders(t *testing.T) {
	env := []string{"A=x y", "B_1=", "C=it's"}

	for _, c := range []struct{ in, posix, windows string }{
		{`echo "${:A}"`, `echo 'x y'`, `echo "x y"`},
		{`"${:A}""${:B_1}" "${:C}"`, `'x y'"" 'it'\''s'`, `"x y""" "it's"`},
		{`${:A} "${:1A}" "${:A }"`, `${:A} "${:1A}" "${:A }"`, `${:A} "${:1A}" "${:A }"`},
		{`""${:A}"`, `"'x y'`, `""x y"`},
	} {
		for _, windows := range []bool{false, true} {
			want := c.posix
			if windows {
				want = c.windows
			}

			got, err := replacePlaceholders(c.in, env, windows)
			if err != nil || got != want {
				t.Errorf("replacePlaceholders(%q, windows=%v) = %q, %v; want %q", c.in, windows, got, err, want)
			}
		}
	}
}

func TestMergeEnv(t *testing.T) {
	inherited := []string{"PATH=/bin", "Foo=1", "BAR=2"}

	got := mergeEnv([]string{"FOO=x"}, inherited, false)
	if strings.Join(got, ",") != "FOO=x,PATH=/bin,Foo=1,BAR=2" {
		t.Errorf("posix: %q", got)
	}

	got = mergeEnv([]string{"FOO=x"}, inherited, true)
	if strings.Join(got, ",") != "FOO=x,PATH=/bin,BAR=2" {
		t.Errorf("windows: %q", got)
	}

	if got := mergeEnv(nil, inherited, false); len(got) != 3 {
		t.Errorf("no overrides: %q", got)
	}
}

func TestProcessExecutor_Async(t *testing.T) {
	skipOnWindows(t)

	buffer := &bufferIO{debug: true}
	process := NewProcessExecutor(buffer)

	if _, err := process.ExecuteAsync(ShellCmd("true"), ""); err == nil || !strings.HasPrefix(err.Error(), "You must use the ProcessExecutor instance") {
		t.Fatalf("err = %v", err)
	}

	process.EnableAsync()
	process.SetMaxJobs(1)

	ok, err := process.ExecuteAsync(ShellCmd("echo hi"), "")
	if err != nil {
		t.Fatal(err)
	}

	failed, err := process.ExecuteAsync(Cmd("sh", "-c", "exit 3"), "")
	if err != nil {
		t.Fatal(err)
	}

	process.Wait()

	if n := process.CountActiveJobs(); n != 0 {
		t.Errorf("active jobs = %d", n)
	}

	p, err := ok.Wait()
	if err != nil || p.GetOutput() != "hi\n" || !p.IsSuccessful() {
		t.Errorf("first job: %v", err)
	}

	// A failing command still resolves, with the process.
	p, err = failed.Wait()
	if err != nil || p.IsSuccessful() || p.GetExitCode() != 3 {
		t.Errorf("second job: %v", err)
	}

	want := "Executing async command (CWD): echo hi\nExecuting async command (CWD): 'sh' '-c' 'exit 3'\n"
	if got := buffer.getOutput(); got != want {
		t.Errorf("debug output %q, want %q", got, want)
	}

	removed, err := Then(failed, func(p *Process) (int, error) { return p.GetExitCode(), nil }).Wait()
	if err != nil || removed != 3 {
		t.Errorf("Then = %d, %v", removed, err)
	}
}

func TestProcessExecutor_ResetMaxJobs(t *testing.T) {
	for value, want := range map[string]int{
		"":                     10,
		"abc":                  10,
		"0x1A":                 10,
		"5":                    5,
		" 7 ":                  7,
		"7.9":                  7,
		"0.5":                  1,
		"-3":                   1,
		"1e3":                  50,
		"99999999999999999999": 50,
		"1e400":                1,
		"-1e400":               1,
	} {
		t.Setenv("COMPOSER_MAX_PARALLEL_PROCESSES", value)

		p := NewProcessExecutor(nil)
		if p.maxJobs != want {
			t.Errorf("COMPOSER_MAX_PARALLEL_PROCESSES=%q: maxJobs %d, want %d", value, p.maxJobs, want)
		}
	}
}

func TestProcessExecutor_RequiresGitDirEnv(t *testing.T) {
	p := NewProcessExecutor(nil)

	// array_intersect keeps $cmd's keys, and $cmd[0] is "git", so Composer
	// never matches.
	for _, c := range []Command{
		Cmd("git", "show"), Cmd("git", "log"), Cmd("git", "remote", "set-url", "origin", "x"),
		ShellCmd("git show HEAD"), Cmd("show"), Cmd("hg", "log"), ShellCmd(""),
	} {
		if p.RequiresGitDirEnv(c) {
			t.Errorf("RequiresGitDirEnv(%v) = true", c)
		}
	}

	for _, c := range []struct {
		cmd, gitCmd []string
		want        bool
	}{
		{[]string{"show"}, []string{"show"}, true},
		{[]string{"remote", "set-url", "x"}, []string{"remote", "set-url"}, true},
		{[]string{"set-url", "remote"}, []string{"remote", "set-url"}, false},
		{[]string{"remote", "x", "set-url"}, []string{"remote", "set-url"}, false},
		{[]string{"show", "show"}, []string{"show"}, false},
		{[]string{"x", "show"}, []string{"show"}, false},
	} {
		if got := intersectIsList(c.cmd, c.gitCmd); got != c.want {
			t.Errorf("intersectIsList(%q, %q) = %v", c.cmd, c.gitCmd, got)
		}
	}
}

func TestPhpExecLastLine(t *testing.T) {
	for in, want := range map[string]string{
		"":                 "",
		"/usr/bin/git\n":   "/usr/bin/git",
		"a\nb\n":           "b",
		"a\nb":             "b",
		"a\n\n":            "",
		"a\r\n":            "a",
		"/x \t\x0b\x0c\n":  "/x",
		"a\n  \n":          "",
		"/bin/sh\x00\n":    "/bin/sh\x00",
		"one\ntwo  three ": "two  three",
	} {
		if got := phpExecLastLine(in); got != want {
			t.Errorf("phpExecLastLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExecutableFinder_Find(t *testing.T) {
	skipOnWindows(t)

	dir := t.TempDir()
	tool := filepath.Join(dir, "maestro-tool")

	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "not-executable"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	finder := NewExecutableFinder()

	t.Setenv("PATH", "/nonexistent:"+dir)

	if got, ok := finder.Find("maestro-tool"); !ok || got != tool {
		t.Errorf("Find in PATH = %q, %v", got, ok)
	}

	if _, ok := finder.Find("not-executable"); ok {
		t.Error("found a non-executable file")
	}

	// An extra dir naming the executable itself.
	t.Setenv("PATH", "/nonexistent")

	if got, ok := finder.Find("maestro-tool", tool); !ok || got != tool {
		t.Errorf("Find via extra dir = %q, %v", got, ok)
	}

	if got, ok := finder.Find("maestro-tool", dir); !ok || got != tool {
		t.Errorf("Find in extra dir = %q, %v", got, ok)
	}

	// An empty PATH entry is the current directory.
	t.Chdir(dir)
	t.Setenv("PATH", "")

	if got, ok := finder.Find("maestro-tool"); !ok || got != "./maestro-tool" {
		t.Errorf("Find in . = %q, %v", got, ok)
	}

	// The command -v fallback, through the shell's own PATH lookup.
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip(err)
	}

	t.Setenv("PATH", filepath.Dir(sh))

	if got, ok := finder.Find("sh"); !ok || got != sh {
		t.Errorf("Find(sh) = %q, %v", got, ok)
	}

	if _, ok := finder.Find("maestro-surely-missing"); ok {
		t.Error("found a missing command")
	}

	if _, ok := finder.Find("dir/maestro-surely-missing"); ok {
		t.Error("found a missing command with a slash")
	}
}

func TestPathinfoExtension(t *testing.T) {
	for in, want := range map[string]string{"php": "", "php.exe": "exe", "a.b/php": "", "php.": "", ".bashrc": "bashrc", "/a/b.tar.gz": "gz"} {
		if got := pathinfoExtension(in, false); got != want {
			t.Errorf("pathinfoExtension(%q) = %q, want %q", in, got, want)
		}
	}

	if got := pathinfoExtension(`a.b\php`, true); got != "" {
		t.Errorf("windows pathinfoExtension = %q", got)
	}
}

func TestPhpExecutableFinder_Find(t *testing.T) {
	skipOnWindows(t)

	dir := t.TempDir()
	php := filepath.Join(dir, "php")

	if err := os.WriteFile(php, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PHP_PATH", "")
	t.Setenv("PHP_PEAR_PHP_BIN", "")
	t.Setenv("PHP_BINARY", php)

	finder := NewPhpExecutableFinder()

	if got, ok := finder.Find(); !ok || got != php {
		t.Errorf("PHP_BINARY: %q, %v", got, ok)
	}

	t.Setenv("PHP_BINARY", dir)

	if _, ok := finder.Find(); ok {
		t.Error("PHP_BINARY naming a directory was accepted")
	}

	t.Setenv("PHP_BINARY", "")
	t.Setenv("PHP_PATH", php)

	if got, ok := finder.Find(); !ok || got != php {
		t.Errorf("PHP_PATH: %q, %v", got, ok)
	}

	t.Setenv("PHP_PATH", "")
	t.Setenv("PATH", dir)

	if got, ok := finder.Find(); !ok || got != php {
		t.Errorf("PATH: %q, %v", got, ok)
	}
}

func TestEscapeQuotesBackslashes(t *testing.T) {
	for in, want := range map[string]string{
		"":          "",
		`abc`:       `abc`,
		`a"b`:       `a\"b`,
		`a\"b`:      `a\\\"b`,
		`a\\"b\c"`:  `a\\\\\"b\c\"`,
		`\`:         `\`,
		`"`:         `\"`,
		`C:\a b\"x`: `C:\a b\\\"x`,
	} {
		if got := escapeQuotesBackslashes(in); got != want {
			t.Errorf("escapeQuotesBackslashes(%q) = %q, want %q", in, got, want)
		}
	}

	if got := quoteComSpec(`C:\Windows\system32\cmd.exe`, true); got != `"C:\Windows\system32\cmd.exe"` {
		t.Errorf("quoteComSpec = %q", got)
	}

	if got := quoteComSpec("", false); got != "cmd" {
		t.Errorf("quoteComSpec not found = %q", got)
	}
}

func TestNewUniqid(t *testing.T) {
	// uniqid('', true): 13 hex digits, then a float in [0, 10) with 8
	// decimals.
	if id := newUniqid(); !regexp.MustCompile(`^[0-9a-f]{13}[0-9]\.[0-9]{8}$`).MatchString(id) {
		t.Errorf("uniqid %q", id)
	}
}

func TestCommand_String(t *testing.T) {
	if got := ShellCmd("echo 'a b'").String(); got != "echo 'a b'" {
		t.Errorf("shell command %q", got)
	}

	want := strings.Join([]string{Escape("git"), Escape("a b")}, " ")
	if got := Cmd("git", "a b").String(); got != want {
		t.Errorf("argument list %q, want %q", got, want)
	}
}
