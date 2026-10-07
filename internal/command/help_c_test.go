package command_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
)

// helpGolden runs args and compares the display with
// testdata/help/<golden>.txt, the reference Composer's output
// (tools/oracle/command/help.sh).
func helpGolden(t *testing.T, golden string, args ...any) {
	t.Helper()
	got, want := runHelp(t, golden, args...)
	compareHelp(t, golden, got, want)
}

// runHelp runs args and returns the display and the golden.
func runHelp(t *testing.T, golden string, args ...any) (got, want string) {
	t.Helper()
	data, err := os.ReadFile("testdata/help/" + goldenFile(golden))
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

	return appTester.Display(true), string(data)
}

func compareHelp(t *testing.T, golden, got, want string) {
	t.Helper()
	if got != want {
		i := 0
		for i < len(got) && i < len(want) && got[i] == want[i] {
			i++
		}
		t.Errorf("%s differs at byte %d:\n got %q\nwant %q", golden, i, tail(got, i), tail(want, i))
	}
}

// TestHelp_GroupC compares the help of group C's commands with Composer's.
func TestHelp_GroupC(t *testing.T) {
	for _, name := range []string{"about", "clear-cache", "repository", "policy", "self-update"} {
		t.Run(name, func(t *testing.T) { helpGolden(t, name, "command", "help", "command_name", name) })
	}
}

// TestHelp_List compares `list` with Composer's once every command is
// registered, all but the banner (the logo and version up to the first
// empty line), which is maestro's own (docs/PORTING.md deviation 8).
func TestHelp_List(t *testing.T) {
	if !command.AllCommandsRegistered() {
		t.Skip("not every command is registered yet")
	}
	got, want := runHelp(t, "list", "command", "list")
	gotBanner, gotRest, _ := strings.Cut(got, "\n\n")
	_, wantRest, _ := strings.Cut(want, "\n\n")
	compareHelp(t, "list", gotRest, wantRest)

	// commandtest's runtime is maestro version "test".
	if wantBanner := command.Logo + "maestro version test (Composer " + composer.GetVersion() + " compatible)"; gotBanner != wantBanner {
		t.Errorf("banner:\n%s\nwant:\n%s", gotBanner, wantBanner)
	}
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
