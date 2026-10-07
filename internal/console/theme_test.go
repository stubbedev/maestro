package console

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/ui"
)

// Every tag of maestro's theme (Composer's and the roles' own) renders as
// its role: decorated, the role's SGR sequences around the text, which is
// all that sets it apart from the undecorated text, which has no escape
// sequence.
func TestThemeTags(t *testing.T) {
	sgr := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	tags := map[string]ui.Role{}
	for _, ft := range ui.ComposerTags {
		tags[ft.Name] = ft.Role
	}
	for _, r := range ui.Roles() {
		tags[r.Tag()] = r
	}

	for tag, role := range tags {
		text := "<" + tag + ">a \\<b</" + tag + ">"
		plain := NewOutputFormatter(false, ThemeStyles()...).Format(text)
		decorated := NewOutputFormatter(true, ThemeStyles()...).Format(text)
		if plain != "a <b" || strings.Contains(plain, "\x1b") {
			t.Errorf("<%s>: undecorated %q", tag, plain)
		}
		if sgr.ReplaceAllString(decorated, "") != plain {
			t.Errorf("<%s>: decorated %q is not %q with SGR sequences", tag, decorated, plain)
		}
		if want := role.Render("a <b", true); decorated != want {
			t.Errorf("<%s>: decorated %q, want %q", tag, decorated, want)
		}
	}

	// The roles' tags render on a formatter without Composer's styles.
	if got := NewOutputFormatter(false).Format(ui.RoleMuted.Wrap("x")); got != "x" {
		t.Errorf("role tag on a bare formatter: %q", got)
	}
}
