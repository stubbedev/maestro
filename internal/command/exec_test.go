// Ports tests/Composer/Test/Command/ExecCommandTest.php.

package command_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/util"
)

func TestExecCommand_ListThrowsIfNoBinariesExist(t *testing.T) {
	composerDir := commandtest.InitTempComposer(t, nil, nil, nil, true)

	composerBinDir := composerDir + "/vendor/bin"

	tester := commandtest.GetApplicationTester(t)
	_, err := tester.RunArgs(commandtest.Options{}, "command", "exec", "--list", true)
	want := "No binaries found in composer.json or in bin-dir (" + composerBinDir + ")"
	if err == nil || err.Error() != want {
		t.Fatalf("err %v, want %q", err, want)
	}
}

func TestExecCommand_List(t *testing.T) {
	composerDir := commandtest.InitTempComposer(t, `{"bin": ["a"]}`, nil, nil, true)

	composerBinDir := composerDir + "/vendor/bin"
	if err := os.MkdirAll(composerBinDir, 0o777); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"b", "b.bat", "c"} {
		if err := os.WriteFile(composerBinDir+"/"+f, nil, 0o666); err != nil {
			t.Fatal(err)
		}
	}

	tester := commandtest.GetApplicationTester(t)
	if _, err := tester.RunArgs(commandtest.Options{}, "command", "exec", "--list", true); err != nil {
		t.Fatal(err)
	}

	output := tester.Display(true)

	want := "Available binaries:\n- b\n- c\n- a (local)"
	if got := strings.TrimSpace(output); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// execBins writes the bin-dir of the project in dir: one executable
// script per name, printing its name, arguments and working directory and
// exiting with the code its name ends in ("fail3" exits 3), plus a .bat
// copy of the first, which listings skip.
func execBins(t *testing.T, dir string, names ...string) {
	t.Helper()
	binDir := filepath.Join(dir, "vendor", "bin")
	if err := os.MkdirAll(binDir, 0o777); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		code := strings.TrimLeft(name, "abcdefghijklmnopqrstuvwxyz")
		if code == "" {
			code = "0"
		}
		script := "#!/bin/sh\necho \"" + name + " ran with [$*] in $PWD\"\nexit " + code + "\n"
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o777); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(binDir, names[0]+".bat"), nil, 0o666); err != nil {
		t.Fatal(err)
	}
}

// skipShellScripts skips a test that runs execBins' scripts where there
// is no sh to run them.
func skipShellScripts(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the binaries are shell scripts")
	}
}

func TestExecCommand_Run(t *testing.T) {
	skipShellScripts(t)
	setup := func(t *testing.T) {
		dir := commandtest.InitTempComposer(t, `{"bin": ["local"]}`, nil, nil, true)
		execBins(t, dir, "b", "c", "fail3")
	}
	exec := func(kv ...any) []console.Param {
		params := []console.Param{console.P("command", "exec")}
		for i := 0; i+1 < len(kv); i += 2 {
			params = append(params, console.P(kv[i].(string), kv[i+1]))
		}

		return params
	}
	const list = "Available binaries:\n- b\n- c\n- fail3\n- local (local)\n"
	runCommandCases(t, setup, []commandCase{
		{name: "no binary without interaction lists", params: exec("--no-interaction", true), contains: []string{list}},
		{name: "-l lists", params: exec("-l", true, "binary", "b"), contains: []string{list}, excludes: []string{"b ran"}},
		{
			name:     "arguments pass through",
			params:   exec("binary", "b", "args", []string{"--flag", "a b"}),
			contains: []string{"b ran with [--flag a b]"},
		},
		{
			name:     "interactive select runs the chosen binary",
			inputs:   []string{"1"},
			params:   exec(),
			contains: []string{"Binary to run: ", "[0] b\n", "[1] c\n", "[2] fail3\n", "[3] local\n", "c ran with []"},
		},
		{
			// Composer allows one attempt
			name:     "an invalid answer fails",
			inputs:   []string{"zzz", "0"},
			params:   exec(),
			err:      `Invalid binary name "zzz"`,
			excludes: []string{"ran with"},
		},
		{
			name:     "the exit code passes through",
			params:   exec("binary", "fail3"),
			code:     3,
			contains: []string{"fail3 ran with []", "Script fail3 handling the __exec_command event returned with error code 3"},
		},
	})
}

// TestExecCommand_GlobalRunsInInitialDirectory checks that `global exec`
// runs the global binary in the directory maestro started in, not in
// COMPOSER_HOME.
func TestExecCommand_GlobalRunsInInitialDirectory(t *testing.T) {
	skipShellScripts(t)
	globalTearDown(t)
	home := commandtest.InitTempComposer(t, nil, nil, nil, true)
	execBins(t, home, "where")
	util.PutEnv("COMPOSER_HOME", home)
	project := commandtest.UniqueTmpDirectory(t)
	chdir(t, project)

	tester := commandtest.GetApplicationTester(t)
	if _, err := tester.RunArgs(commandtest.Options{}, "command", "global", "command-name", "exec", "args", []string{"where"}); err != nil {
		t.Fatal(err)
	}
	display := tester.Display(true)
	for _, want := range []string{"Changed current directory to " + home + "\n", "where ran with [] in " + project + "\n"} {
		if !strings.Contains(display, want) {
			t.Errorf("display lacks %q:\n%s", want, display)
		}
	}
}
