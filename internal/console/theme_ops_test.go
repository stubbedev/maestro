package console

import (
	"regexp"
	"testing"

	"github.com/stubbedev/maestro/internal/ui"
)

// The summary of an install or update: undecorated it is Composer's
// text; decorated, it is that text with SGR sequences and with the
// bullets of operations as their marks' glyphs.
func TestOperationSummaryRendering(t *testing.T) {
	sgr := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	cases := []struct{ markup, plain string }{
		{
			ui.Tally("Package operations", ui.Count{N: 2, Noun: "install"}, ui.Count{N: 1, Noun: "update"}, ui.Count{N: 0, Noun: "removal"}),
			"Package operations: 2 installs, 1 update, 0 removals",
		},
		{ui.MarkInstall.Item() + "Installing <info>a/b</info> (<comment>1.0.0</comment>)", "  - Installing a/b (1.0.0)"},
		{ui.MarkUpgrade.Item() + "Upgrading <info>a/b</info>", "  - Upgrading a/b"},
		{ui.MarkDowngrade.Item() + "Downgrading <info>a/b</info>", "  - Downgrading a/b"},
		{ui.MarkRemove.Item() + "Removing <info>a/b</info>", "  - Removing a/b"},
		{ui.MarkItem.Item() + "Marking <info>a/b</info>", "  - Marking a/b"},
		{ui.RoleMuted.Wrap("Generating autoload files"), "Generating autoload files"},
	}
	for _, c := range cases {
		if got := NewOutputFormatter(false, ThemeStyles()...).Format(c.markup); got != c.plain {
			t.Errorf("undecorated %q, want %q", got, c.plain)
		}
		decorated := NewOutputFormatter(true, ThemeStyles()...).Format(c.markup)
		stripped := sgr.ReplaceAllString(decorated, "")
		for _, m := range ui.Marks() {
			glyph := sgr.ReplaceAllString(m.Styled("-"), "")
			if len(stripped) > 2 && stripped[2:2+len(glyph)] == glyph {
				stripped = "  -" + stripped[2+len(glyph):]

				break
			}
		}
		if stripped != c.plain || decorated == c.plain {
			t.Errorf("decorated %q is not %q styled", decorated, c.plain)
		}
	}
}
