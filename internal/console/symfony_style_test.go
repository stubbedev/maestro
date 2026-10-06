// Ports Tests/Style/SymfonyStyleTest.php (symfony/console) with the
// fixture commands of Tests/Fixtures/Style/SymfonyStyle/command as Go
// funcs; their expected outputs are read from testdata.

package console

import (
	"bytes"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

const lorem = "Lorem ipsum dolor sit amet, consectetur adipisicing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum"

type styleFixture func(in Input, out Output)

// titlesFixture is command_4 and command_4_with_iterators (an iterator
// written with write()/writeln() is written element by element).
func titlesFixture(in Input, out Output) {
	s := NewSymfonyStyle(in, out)

	s.Write("Lorem ipsum dolor sit amet", false, OutputNormal)
	s.Title("First title")

	s.Writeln("Lorem ipsum dolor sit amet")
	s.Title("Second title")

	s.Write("Lorem ipsum dolor sit amet", false, OutputNormal)
	s.Write("", false, OutputNormal)
	s.Title("Third title")

	// Ensure edge case by appending empty strings to history:
	s.Write("Lorem ipsum dolor sit amet", false, OutputNormal)
	WriteMessages(s, []string{"", "", ""}, false, OutputNormal)
	s.Title("Fourth title")

	// Ensure have manual control over number of blank lines:
	s.Writeln("Lorem ipsum dolor sit amet")
	WriteMessages(s, []string{"", ""}, true, OutputNormal) // Should append an extra blank line
	s.Title("Fifth title")

	s.Writeln("Lorem ipsum dolor sit amet")
	s.NewLine(2) // Should append an extra blank line
	s.Title("Fifth title")
}

var styleFixtures = map[string]styleFixture{
	"0": func(in Input, out Output) {
		NewSymfonyStyle(in, out).Caution("Lorem ipsum dolor sit amet")
	},
	"1": func(in Input, out Output) {
		s := NewSymfonyStyle(in, out)
		s.Title("Title")
		s.Warning("Lorem ipsum dolor sit amet")
		s.Title("Title")
	},
	"2": func(in Input, out Output) {
		s := NewSymfonyStyle(in, out)
		s.Warning("Warning")
		s.Caution("Caution")
		s.Error("Error")
		s.Success("Success")
		s.Note("Note")
		s.Info("Info")
		s.Block([]string{"Custom block"}, "CUSTOM", "fg=white;bg=green", "X ", true, true)
	},
	"3": func(in Input, out Output) {
		s := NewSymfonyStyle(in, out)
		s.Title("First title")
		s.Title("Second title")
	},
	"4":                titlesFixture,
	"4_with_iterators": titlesFixture,
	"5": func(in Input, out Output) {
		s := NewSymfonyStyle(in, out)

		s.Writeln("Lorem ipsum dolor sit amet")
		s.Listing([]string{"Lorem ipsum dolor sit amet", "consectetur adipiscing elit"})

		// Even using write:
		s.Write("Lorem ipsum dolor sit amet", false, OutputNormal)
		s.Listing([]string{"Lorem ipsum dolor sit amet", "consectetur adipiscing elit"})

		s.Write("Lorem ipsum dolor sit amet", false, OutputNormal)
		s.Text("Lorem ipsum dolor sit amet", "consectetur adipiscing elit")

		s.NewLine(1)

		s.Write("Lorem ipsum dolor sit amet", false, OutputNormal)
		s.Comment("Lorem ipsum dolor sit amet", "consectetur adipiscing elit")
	},
	"6": func(in Input, out Output) {
		s := NewSymfonyStyle(in, out)
		s.Listing([]string{"Lorem ipsum dolor sit amet", "consectetur adipiscing elit"})
		s.Success("Lorem ipsum dolor sit amet")
	},
	"7": func(in Input, out Output) {
		s := NewSymfonyStyle(in, out)
		s.Title("Title")
		_, _ = s.AskHidden("Hidden question", nil)
		_, _ = s.Choice("Choice question with default", php.ListOf("choice1", "choice2"), "choice1")
		_, _ = s.Confirm("Confirmation with yes default", true)
		s.Text("Duis aute irure dolor in reprehenderit in voluptate velit esse")
	},
	"8": func(in Input, out Output) {
		headers := []any{
			[]any{NewTableCell("Main table title", TableCellOptions{Colspan: 3})},
			[]any{"ISBN", "Title", "Author"},
		}
		rows := []any{
			[]any{"978-0521567817", "De Monarchia", NewTableCell("Dante Alighieri\nspans multiple rows", TableCellOptions{Rowspan: 2})},
			[]any{"978-0804169127", "Divine Comedy"},
		}
		_ = NewSymfonyStyle(in, out).Table(headers, rows)
	},
	"9": func(in Input, out Output) {
		NewSymfonyStyle(in, out).Block([]string{"Custom block", "Second custom block line"}, "CUSTOM", "fg=white;bg=green", "X ", true, true)
	},
	"10": func(in Input, out Output) {
		NewSymfonyStyle(in, out).Block([]string{lorem}, "CUSTOM", "fg=white;bg=green", "X ", true, true)
	},
	"11": func(in Input, out Output) {
		word := "Lopadotemachoselachogaleokranioleipsanodrimhypotrimmatosilphioparaomelitokatakechymenokichlepikossyphophattoperisteralektryonoptekephalliokigklopeleiolagoiosiraiobaphetraganopterygon"
		NewSymfonyStyle(in, out).Block([]string{word}, "CUSTOM", "fg=white;bg=blue", " § ", false, true)
	},
	"12": func(in Input, out Output) {
		NewSymfonyStyle(in, out).Comment(lorem)
	},
	"13": func(in Input, out Output) {
		out.SetDecorated(true)
		NewSymfonyStyle(in, out).Comment("Lorem ipsum dolor sit <comment>amet, consectetur adipisicing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur.</comment> Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum")
	},
	"14": func(in Input, out Output) {
		NewSymfonyStyle(in, out).Block([]string{lorem}, "", "", "$ ", true, true)
	},
	"15": func(in Input, out Output) {
		NewSymfonyStyle(in, out).Block([]string{lorem}, "TEST", "", " ", false, true)
	},
	"16": func(in Input, out Output) {
		out.SetDecorated(true)
		NewSymfonyStyle(in, out).Success(lorem) // PHP ignores the extra "TEST" argument
	},
	"17": func(in Input, out Output) {
		s := NewSymfonyStyle(in, out)
		s.Title(`Title ending with \`)
		s.Section(`Section ending with \`)
	},
	"18": func(in Input, out Output) {
		_ = NewSymfonyStyle(in, out).DefinitionList(
			php.ArrayOf("foo", "bar"),
			NewTableSeparator(),
			"this is a title",
			NewTableSeparator(),
			php.ArrayOf("foo2", "bar2"),
		)
	},
	"19": func(in Input, out Output) {
		_ = NewSymfonyStyle(in, out).HorizontalTable([]any{"a", "b", "c", "d"}, []any{[]any{1, 2, 3}, []any{4, 5}, []any{7, 8, 9}})
	},
	"20": func(in Input, out Output) {
		out.SetDecorated(true)
		s := NewSymfonyStyle(in, out)
		s.Write("<question>do you want <comment>something</>", false, OutputNormal)
		s.Writeln("?</>")
	},
	"21": func(in Input, out Output) {
		s := NewSymfonyStyle(in, out)
		s.Success("Lorem ipsum dolor sit amet")
		s.Success("Lorem ipsum dolor sit amet with one emoji 🎉")
		s.Success("Lorem ipsum dolor sit amet with so many of them 👩‍🌾👩‍🌾👩‍🌾👩‍🌾👩‍🌾")
	},
}

func runStyleFixture(t *testing.T, fixture styleFixture, interactive bool) string {
	t.Helper()
	t.Setenv("COLUMNS", "121")

	command := NewCommand("sfstyle")
	command.SetCode(func(in Input, out Output) (int, error) {
		fixture(in, out)

		return 0, nil
	})
	tester := newCommandTester(command)
	if _, err := tester.Execute(nil, testerOptions{interactive: new(interactive), decorated: new(false)}); err != nil {
		t.Fatal(err)
	}

	return tester.Display()
}

func TestSymfonyStyle_Outputs(t *testing.T) {
	for name, fixture := range styleFixtures {
		t.Run("command_"+name, func(t *testing.T) {
			want, err := os.ReadFile("testdata/Fixtures/Style/SymfonyStyle/output/output_" + name + ".txt")
			if err != nil {
				t.Fatal(err)
			}
			if got := runStyleFixture(t, fixture, false); got != string(want) {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestSymfonyStyle_InteractiveOutputs(t *testing.T) {
	want, err := os.ReadFile("testdata/Fixtures/Style/SymfonyStyle/output/interactive_output_1.txt")
	if err != nil {
		t.Fatal(err)
	}
	got := runStyleFixture(t, func(in Input, out Output) {
		s := NewSymfonyStyle(in, out)
		in.SetStream(strings.NewReader("Foo\nBar\nBaz"))

		_, _ = s.Ask("What's your name?", nil, nil)
		_, _ = s.Ask("How are you?", nil, nil)
		_, _ = s.Ask("Where do you come from?", nil, nil)
	}, true)
	if got != string(want) {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestSymfonyStyle_OutputProgressIterate(t *testing.T) {
	// The shade characters are the bar's outside Windows, and in Hyper.
	if runtime.GOOS == "windows" {
		t.Setenv("TERM_PROGRAM", "Hyper")
	}
	want, err := os.ReadFile("testdata/Fixtures/Style/SymfonyStyle/progress/output_progress_iterate_shade.txt")
	if err != nil {
		t.Fatal(err)
	}
	got := runStyleFixture(t, func(in Input, out Output) {
		s := NewSymfonyStyle(in, out)
		steps := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
		for range SymfonyStyleProgressIterate(s, slices.All(steps), len(steps)) { // noop
		}
		s.Writeln("end of progressbar")
	}, false)
	if got != string(want) {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestSymfonyStyle_GetErrorStyle(t *testing.T) {
	var stdout, stderr bytes.Buffer
	out := NewConsoleOutputStreams(&stdout, &stderr, VerbosityNormal, new(false), nil)
	in, _ := NewArrayInput(nil, nil)

	NewSymfonyStyle(in, out).ErrorStyle().Write("x", false, OutputNormal)
	if stdout.String() != "" || stderr.String() != "x" {
		t.Fatalf("stdout %q, stderr %q", stdout.String(), stderr.String())
	}
}

func TestSymfonyStyle_GetErrorStyleUsesTheCurrentOutputIfNoErrorOutputIsAvailable(t *testing.T) {
	out := NewBufferedOutput(VerbosityNormal, false, nil)
	in, _ := NewArrayInput(nil, nil)

	NewSymfonyStyle(in, out).ErrorStyle().Write("x", false, OutputNormal)
	if got := out.Fetch(); got != "x" {
		t.Fatalf("got %q", got)
	}
}

// TestSymfonyStyle_MemoryConsumption checks that the history buffer stays
// trimmed however much is written.
func TestSymfonyStyle_MemoryConsumption(t *testing.T) {
	in, _ := NewArrayInput(nil, nil)
	io := NewSymfonyStyle(in, NewNullOutput())
	for range 102 {
		io.Write("teststr", true, VerbosityQuiet)
	}
	if n := len(io.bufferedOutput.buffer); n > 2 {
		t.Fatalf("buffer holds %d bytes", n)
	}
}
