package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// CheckStatus is the outcome of a check (diagnose's).
type CheckStatus uint8

// The outcomes.
const (
	// CheckOK is a check that passed (or was skipped).
	CheckOK CheckStatus = iota
	// CheckWarning is a check that found a problem the tool works with.
	CheckWarning
	// CheckFailed is a check that failed.
	CheckFailed

	numCheckStatuses
)

// checkStatuses are each outcome's glyph, role and the kind of
// diagnostic its messages are.
var checkStatuses = [numCheckStatuses]struct {
	glyph Glyph
	role  Role
	kind  Kind
}{
	CheckOK:      {GlyphCheckOK, RoleSuccess, Note},
	CheckWarning: {GlyphCheckWarning, RoleWarning, Warning},
	CheckFailed:  {GlyphCheckFailed, RoleDanger, Error},
}

// CheckList is the decorated look of a list of facts and checks
// (diagnose): one line per fact, its label in a column; one line per
// check, its outcome's glyph, its label in the same column and, for a
// check that passed, what it found; a warning or failure's messages
// follow as a diagnostic, indented. Lines are styled text without line
// endings, to write unformatted.
type CheckList struct {
	// LabelWidth is the width of the labels' column.
	LabelWidth int
}

// glyphWidth is the width of the widest outcome glyph.
func glyphWidth() int {
	w := 0
	for _, s := range checkStatuses {
		w = max(w, lipgloss.Width(s.glyph.String()))
	}

	return w
}

// pad is s followed by spaces up to width, and at least one.
func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(1, width-lipgloss.Width(s)))
}

// Fact is a fact's line: its label, muted, and its value.
func (l CheckList) Fact(label, value string) string {
	return strings.Repeat(" ", glyphWidth()+1) + RoleMuted.Styled(label) + strings.Repeat(" ", max(1, l.LabelWidth-lipgloss.Width(label))) + value
}

// Check is a check's lines: its outcome and label, then what it found
// (messages, plain text): beside a passed check (muted), or as a warning
// or error beneath one that did not pass.
func (l CheckList) Check(status CheckStatus, label string, messages []string) []string {
	s := checkStatuses[status]
	glyph := s.role.Styled(s.glyph.String()) + strings.Repeat(" ", glyphWidth()-lipgloss.Width(s.glyph.String())+1)
	indent := strings.Repeat(" ", glyphWidth()+1)
	text := strings.Join(messages, "\n")
	if status == CheckOK || text == "" {
		found := SplitLines(text)
		lines := []string{glyph + pad(label, l.LabelWidth) + RoleMuted.Render(found[0], true)}
		for _, f := range found[1:] {
			lines = append(lines, indent+strings.Repeat(" ", l.LabelWidth)+RoleMuted.Render(f, true))
		}
		lines[0] = strings.TrimRight(lines[0], " ")

		return lines
	}
	lines := []string{glyph + s.role.Styled(label)}
	for _, d := range (Diagnostic{Kind: s.kind, Message: text}).Lines(Options{Decorated: true}) {
		if d == "" {
			lines = append(lines, "")

			continue
		}
		lines = append(lines, indent+"  "+d)
	}

	return lines
}
