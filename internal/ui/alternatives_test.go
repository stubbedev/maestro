package ui

import (
	"slices"
	"strings"
	"testing"
)

func TestTakeAlternatives(t *testing.T) {
	for _, tc := range []struct {
		message, alternatives []string
		wantMessage           string
		want                  []string
	}{
		// Symfony's block leaves the message
		{[]string{`Command "x" is not defined.` + DidYouMean([]string{"a", "b"})}, []string{"a", "b"}, `Command "x" is not defined.`, []string{"a", "b"}},
		// alternatives maestro adds
		{[]string{`The "--dry-runn" option does not exist.`}, []string{"--dry-run"}, `The "--dry-runn" option does not exist.`, []string{"--dry-run"}},
		// a message offering them its own way keeps them
		{[]string{"The namespace \"f\" is ambiguous.\nDid you mean one of these?\n    foo, fop."}, []string{"foo", "fop"}, "The namespace \"f\" is ambiguous.\nDid you mean one of these?\n    foo, fop.", nil},
		{[]string{"m"}, nil, "m", nil},
	} {
		d := Diagnostic{Message: tc.message[0]}
		d.TakeAlternatives(tc.alternatives)
		if d.Message != tc.wantMessage || !slices.Equal(d.Alternatives, tc.want) {
			t.Errorf("TakeAlternatives(%q) on %q: %q, %q", tc.alternatives, tc.message[0], d.Message, d.Alternatives)
		}
	}
}

func TestLinesAlternatives(t *testing.T) {
	d := Diagnostic{Kind: Error, Message: "m", Alternatives: []string{"a", "b"}, Hints: []string{"h"}}
	want := []string{"Error: m", "  Did you mean one of these?", "      a", "      b", "  Hint: h"}
	if got := d.Lines(Options{}); !slices.Equal(got, want) {
		t.Errorf("Lines() = %q, want %q", got, want)
	}
	d.Alternatives = []string{"a"}
	want = []string{"Error: m", "  Did you mean this? a", "  Hint: h"}
	if got := d.Lines(Options{}); !slices.Equal(got, want) {
		t.Errorf("Lines() = %q, want %q", got, want)
	}
}

// Decorated, a usage is wrapped to the terminal between its items, its
// lines under the first; undecorated it stays one line.
func TestLinesUsageWrapped(t *testing.T) {
	d := Diagnostic{Kind: Error, Message: "m", Usage: "install [--prefer-install PREFER-INSTALL] [--dry-run] [--] [<packages>...]"}
	if got := d.Lines(Options{Width: 40}); got[1] != "  Usage: "+d.Usage {
		t.Errorf("undecorated usage %q", got[1])
	}
	got := d.Lines(Options{Decorated: true, Width: 60})
	want := []string{
		"install [--prefer-install PREFER-INSTALL]",
		"         [--dry-run] [--] [<packages>...]",
	}
	for i, w := range want {
		if !strings.HasSuffix(got[1+i], w) {
			t.Errorf("usage line %d = %q, want it to end in %q", i, got[1+i], w)
		}
	}
}
