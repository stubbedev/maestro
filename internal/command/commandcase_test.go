package command_test

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
)

// commandCase is one run of a command through the ApplicationTester, with
// what it must give: an exception with a message, or a status code and a
// display holding some lines and not others.
type commandCase struct {
	name   string
	params []console.Param
	// env is set for the run (t.Setenv).
	env map[string]string
	// inputs answer the command's questions; the run is interactive when
	// there are any.
	inputs []string
	// err is the message of the exception the run must throw; empty when
	// it must throw none.
	err string
	// code is the status code of a run that throws nothing.
	code int
	// contains are lines (or parts of lines) the display must hold.
	contains []string
	// excludes are ones it must not.
	excludes []string
	// streams, when set, captures stderr apart from stdout: the run's
	// stdout and stderr must equal it, and contains and excludes look at
	// both.
	streams *streams
	// check, when set, checks the run's files afterwards.
	check func(t *testing.T)
}

// streams are what a run writes to stdout and to stderr (PHP_EOL
// normalised).
type streams struct{ stdout, stderr string }

// runCommandCases runs each case on a project setup builds (in the
// test's temporary composer directory, commandtest.InitTempComposer).
func runCommandCases(t *testing.T, setup func(t *testing.T), cases []commandCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setup(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			appTester := commandtest.GetApplicationTester(t)
			o := commandtest.Options{CaptureStderrSeparately: tc.streams != nil}
			if tc.inputs != nil {
				appTester.SetInputs(tc.inputs...)
				interactive := true
				o.Interactive = &interactive
			}
			code, err := appTester.Run(tc.params, o)
			switch {
			case tc.err != "" && err == nil:
				t.Fatalf("no exception, want %q (status %d, display:\n%s)", tc.err, code, appTester.Display(true))
			case tc.err != "" && err.Error() != tc.err:
				t.Fatalf("exception %q, want %q", err.Error(), tc.err)
			case tc.err == "" && err != nil:
				t.Fatalf("unexpected exception: %v (display:\n%s)", err, appTester.Display(true))
			case tc.err == "" && code != tc.code:
				t.Errorf("status %d, want %d (display:\n%s)", code, tc.code, appTester.Display(true))
			}
			display := appTester.Display(true)
			if tc.streams != nil {
				got := streams{stdout: display, stderr: appTester.ErrorOutput(true)}
				if got != *tc.streams {
					t.Errorf("stdout %q, stderr %q; want stdout %q, stderr %q", got.stdout, got.stderr, tc.streams.stdout, tc.streams.stderr)
				}
				display = got.stderr + got.stdout
			}
			for _, s := range tc.contains {
				if !strings.Contains(display, s) {
					t.Errorf("display lacks %q:\n%s", s, display)
				}
			}
			for _, s := range tc.excludes {
				if strings.Contains(display, s) {
					t.Errorf("display holds %q:\n%s", s, display)
				}
			}
			if tc.check != nil {
				tc.check(t)
			}
		})
	}
}

// cmd is the params of a command line: the command name, then option
// names and values (true for a flag) or argument names and values.
func cmd(name string, kv ...any) []console.Param {
	params := []console.Param{console.P("command", name)}
	for i := 0; i+1 < len(kv); i += 2 {
		key, _ := kv[i].(string)
		params = append(params, console.P(key, kv[i+1]))
	}

	return params
}
