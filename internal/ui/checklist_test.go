package ui

import (
	"slices"
	"strings"
	"testing"
)

func TestCheckList(t *testing.T) {
	l := CheckList{LabelWidth: 10}
	gap := strings.Repeat(" ", glyphWidth()+1)
	glyph := func(s CheckStatus) string {
		g := checkStatuses[s].glyph.String()

		return checkStatuses[s].role.Styled(g) + strings.Repeat(" ", glyphWidth()-len([]rune(g))+1)
	}

	if got, want := l.Fact("PHP", "8.4"), gap+RoleMuted.Styled("PHP")+strings.Repeat(" ", 7)+"8.4"; got != want {
		t.Errorf("Fact = %q, want %q", got, want)
	}
	for _, tc := range []struct {
		status   CheckStatus
		messages []string
		want     []string
	}{
		{CheckOK, nil, []string{glyph(CheckOK) + "git"}},
		{CheckOK, []string{"OK git 2\nmore"}, []string{
			glyph(CheckOK) + "git" + strings.Repeat(" ", 7) + RoleMuted.Styled("OK git 2"),
			gap + strings.Repeat(" ", 10) + RoleMuted.Styled("more"),
		}},
		{CheckFailed, nil, []string{glyph(CheckFailed) + "git"}},
		{CheckWarning, []string{"too old"}, []string{
			glyph(CheckWarning) + RoleWarning.Styled("git"),
			gap + "  " + RoleWarning.Styled("Warning:") + " too old",
		}},
		{CheckFailed, []string{"broken"}, []string{
			glyph(CheckFailed) + RoleDanger.Styled("git"),
			gap + "  " + RoleDanger.Styled("Error:") + " " + RoleEmphasis.Styled("broken"),
		}},
	} {
		if got := l.Check(tc.status, "git", tc.messages); !slices.Equal(got, tc.want) {
			t.Errorf("Check(%d, %q) =\n%q\nwant\n%q", tc.status, tc.messages, got, tc.want)
		}
	}
}
