package command_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
)

// helpGolden runs args and compares the display with testdata/<golden>,
// the reference Composer's output (tools/oracle/command/help.sh).
func helpGolden(t *testing.T, golden string, args ...any) {
	t.Helper()
	got, want := runHelp(t, golden, args...)
	compareHelp(t, golden, got, want)
}

// runHelp runs args in an empty project, as help.sh runs Composer, and
// returns the display and the golden.
func runHelp(t *testing.T, golden string, args ...any) (got, want string) {
	t.Helper()
	data, err := os.ReadFile("testdata/" + golden)
	if err != nil {
		t.Fatalf("%v (write it with tools/oracle/command/help.sh)", err)
	}
	prev := console.ScriptName
	console.ScriptName = "composer"
	t.Cleanup(func() { console.ScriptName = prev })
	commandtest.InitTempDir(t)

	appTester := commandtest.GetApplicationTester(t)
	if code, err := appTester.RunArgs(commandtest.Options{ComposerStyles: true}, args...); err != nil || code != 0 {
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

// listedCommands are the names `list` shows: every command but the
// hidden ones, without aliases.
func listedCommands(t *testing.T) []string {
	t.Helper()
	var names []string
	for _, nc := range commandtest.NewApplication().All("") {
		cmd := nc.Command.Base()
		if nc.Name == cmd.Name() && !cmd.IsHidden() {
			names = append(names, nc.Name)
		}
	}
	slices.Sort(names)

	return names
}

// TestHelp_All compares `help <command>` of every listed command with
// Composer's (testdata/help/<command>.txt): help texts are frozen.
func TestHelp_All(t *testing.T) {
	for _, name := range listedCommands(t) {
		t.Run(name, func(t *testing.T) { helpGolden(t, "help/"+name+".txt", "command", "help", "command_name", name) })
	}
}

// TestHelp_Quiet: `help -q` succeeds and writes nothing.
func TestHelp_Quiet(t *testing.T) {
	commandtest.InitTempDir(t)
	got := commandtest.GetApplicationTester(t).RunStreams("command", "help", "command_name", "install", "--quiet", true)
	if got != (commandtest.Streams{}) {
		t.Errorf("help -q: %+v, want exit 0 and no output", got)
	}
}

// TestHelp_List compares `list` with Composer's, all but the banner (the
// logo and version up to the first empty line), which is maestro's own
// (docs/PORTING.md deviation 8).
func TestHelp_List(t *testing.T) {
	got, want := runHelp(t, "list.txt", "command", "list")
	gotBanner, gotRest, _ := strings.Cut(got, "\n\n")
	_, wantRest, _ := strings.Cut(want, "\n\n")
	compareHelp(t, "list.txt", gotRest, wantRest)

	// commandtest's runtime is maestro version "test".
	if wantBanner := command.Logo + "maestro version test (Composer " + composer.GetVersion() + " compatible)"; gotBanner != wantBanner {
		t.Errorf("banner:\n%s\nwant:\n%s", gotBanner, wantBanner)
	}
}

// TestHelp_ListJSON compares `list --format=json` (every command's
// definition) with Composer's.
func TestHelp_ListJSON(t *testing.T) {
	helpGolden(t, "list.json", "command", "list", "--format", "json")
}

func tail(s string, i int) string {
	return s[max(0, i-80):min(len(s), i+200)]
}
