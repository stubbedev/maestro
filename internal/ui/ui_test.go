package ui

import (
	"slices"
	"strings"
	"testing"
)

func TestLinesPlain(t *testing.T) {
	d := Diagnostic{
		Kind:    Error,
		Message: "\"./composer.json\" does not contain valid JSON\r\nParse error on line 1:\n{\n^\n\nExpected one of: 'STRING'  \n",
		Causes:  []string{"first cause", "second\ncause"},
		Hints:   []string{"Try again."},
		Usage:   "install [--dry-run] [--] [<packages>...]",
		Details: []string{"*util.RuntimeError [RuntimeException]", "exit code 1"},
	}
	want := []string{
		`Error: "./composer.json" does not contain valid JSON`,
		"       Parse error on line 1:",
		"       {",
		"       ^",
		"",
		"       Expected one of: 'STRING'",
		"  Caused by: first cause",
		"  Caused by: second",
		"             cause",
		"  Hint: Try again.",
		"  Usage: install [--dry-run] [--] [<packages>...]",
	}
	if got := d.Lines(Options{}); !slices.Equal(got, want) {
		t.Errorf("Lines() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	want = append(want,
		"  Debug: *util.RuntimeError [RuntimeException]",
		"         exit code 1",
	)
	if got := d.Lines(Options{Verbose: true}); !slices.Equal(got, want) {
		t.Errorf("verbose Lines() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestLinesKinds(t *testing.T) {
	for kind, want := range map[Kind]string{
		Error:       "Error: m",
		Warning:     "Warning: m",
		Deprecation: "Deprecated: m",
		Note:        "Note: m",
	} {
		if got := (Diagnostic{Kind: kind, Message: "m"}).Lines(Options{Verbose: true}); !slices.Equal(got, []string{want}) {
			t.Errorf("kind %d: %q, want %q", kind, got, want)
		}
	}
}

// Decorated, the labels and the error's first line are styled with SGR
// sequences of the 16 ANSI colours; the text is kept as it is (tabs too).
func TestLinesDecorated(t *testing.T) {
	d := Diagnostic{Kind: Error, Message: "a\tb\nc", Causes: []string{"d"}, Hints: []string{"e"}, Details: []string{"f"}}
	want := []string{
		"\x1b[1;31mError:\x1b[0m \x1b[1ma\tb\x1b[0m",
		"       c",
		"  \x1b[33mCaused by:\x1b[0m d",
		"  \x1b[32mHint:\x1b[0m e",
		"  \x1b[2mDebug:\x1b[0m \x1b[2mf\x1b[0m",
	}
	if got := d.Lines(Options{Decorated: true, Verbose: true}); !slices.Equal(got, want) {
		t.Errorf("Lines() = %q, want %q", got, want)
	}

	for _, l := range d.Lines(Options{Verbose: true}) {
		if strings.Contains(l, "\x1b") {
			t.Errorf("undecorated line %q has an escape sequence", l)
		}
	}
}

func TestSplitLines(t *testing.T) {
	if got := SplitLines(""); !slices.Equal(got, []string{""}) {
		t.Errorf("SplitLines(\"\") = %q", got)
	}
	if got := SplitLines("\n a \r\nb\t\n\n"); !slices.Equal(got, []string{" a", "b"}) {
		t.Errorf("SplitLines = %q", got)
	}
}
