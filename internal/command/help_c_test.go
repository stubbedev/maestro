package command_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
)

// helpGolden runs args and compares the display with
// testdata/help/<golden>.txt, the reference Composer's output
// (tools/oracle/command/help.sh).
func helpGolden(t *testing.T, golden string, args ...any) {
	t.Helper()
	want, err := os.ReadFile("testdata/help/" + goldenFile(golden))
	if err != nil {
		t.Fatal(err)
	}
	prev := console.ScriptName
	console.ScriptName = "composer"
	t.Cleanup(func() { console.ScriptName = prev })
	commandtest.InitTempComposer(t, nil, nil, nil, true)

	appTester := commandtest.GetApplicationTester(t)
	if code, err := appTester.RunArgs(commandtest.Options{}, args...); err != nil || code != 0 {
		t.Fatalf("run: %d %v", code, err)
	}
	if got := appTester.Display(true); got != string(want) {
		i := 0
		for i < len(got) && i < len(want) && got[i] == want[i] {
			i++
		}
		t.Errorf("%s differs at byte %d:\n got %q\nwant %q", golden, i, tail(got, i), tail(string(want), i))
	}
}

// TestHelp_GroupC compares the help of group C's commands with Composer's.
func TestHelp_GroupC(t *testing.T) {
	for _, name := range []string{"about", "clear-cache", "repository", "policy", "self-update"} {
		t.Run(name, func(t *testing.T) { helpGolden(t, name, "command", "help", "command_name", name) })
	}
}

// TestHelp_List compares `list` with Composer's once every command is
// registered.
func TestHelp_List(t *testing.T) {
	if !command.AllCommandsRegistered() {
		t.Skip("not every command is registered yet")
	}
	helpGolden(t, "list", "command", "list")
}

// TestHelp_ListJSON compares `list --format=json` (every command's
// definition) with Composer's once every command is registered.
func TestHelp_ListJSON(t *testing.T) {
	if !command.AllCommandsRegistered() {
		t.Skip("not every command is registered yet")
	}
	helpGolden(t, "list.json", "command", "list", "--format", "json")
}

func goldenFile(name string) string {
	if strings.Contains(name, ".") {
		return name
	}

	return name + ".txt"
}

func tail(s string, i int) string {
	return s[max(0, i-80):min(len(s), i+200)]
}
