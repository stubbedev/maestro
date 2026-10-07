package command_test

import (
	"os"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
)

// completionCase is a run of `completion`: $SHELL, the arguments, and
// the result.
type completionCase struct {
	name  string
	shell string
	args  []any
	want  commandtest.Streams
}

// runCompletion runs each case as Composer's binary named "composer", as
// testdata/completion.bash was recorded (tools/oracle/command/help.sh).
func runCompletion(t *testing.T, cases []completionCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("SHELL", c.shell)
			commandtest.RunAs(t, "composer")
			commandtest.InitTempDir(t)
			got := commandtest.GetApplicationTester(t).RunStreams(append([]any{"command", "completion"}, c.args...)...)
			if got != c.want {
				t.Errorf("got %+v\nwant %+v", got, c.want)
			}
		})
	}
}

// TestCompletion_Dump: the script is Composer's byte for byte, for the
// shell given or guessed from $SHELL.
func TestCompletion_Dump(t *testing.T) {
	script, err := os.ReadFile("testdata/completion.bash")
	if err != nil {
		t.Fatal(err)
	}
	want := commandtest.Streams{Stdout: string(script)}
	runCompletion(t, []completionCase{
		{"given", "", []any{"shell", "bash"}, want},
		{"guessed from SHELL", "/usr/bin/bash", nil, want},
	})
}

// TestCompletion_DumpUnsupportedShell: an unsupported or undetected
// shell is reported on stderr alone, with exit code 2.
func TestCompletion_DumpUnsupportedShell(t *testing.T) {
	fish := commandtest.Streams{Code: 2, Stderr: `Detected shell "fish", which is not supported by Symfony shell completion (supported shells: "bash").` + "\n"}
	runCompletion(t, []completionCase{
		{"given", "/usr/bin/bash", []any{"shell", "fish"}, fish},
		{"guessed from SHELL", "/bin/fish", nil, fish},
		{"no SHELL", "", nil, commandtest.Streams{Code: 2, Stderr: `Shell not detected, Symfony shell completion only supports "bash").` + "\n"}},
	})
}
