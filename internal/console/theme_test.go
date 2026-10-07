package console

import (
	"bytes"
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

// A progress bar in maestro's style (ui.ProgressBar) is as wide as
// Composer's: its cells' markup and escape sequences take no room.
func TestThemedProgressBarWidth(t *testing.T) {
	var buf bytes.Buffer
	out := NewStreamOutput(&buf, VerbosityNormal, new(true), NewOutputFormatter(true, ThemeStyles()...))
	bar := NewProgressBar(out, 10, 0)
	s := ui.ProgressBar()
	bar.SetBarCharacter(s.Done)
	bar.SetProgressCharacter(s.Head)
	bar.SetEmptyBarCharacter(s.Todo)
	bar.SetFormat("[%bar%]")
	bar.Start()
	bar.Advance(4)

	frames := strings.Split(buf.String(), "\x1b[2K")
	last := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(frames[len(frames)-1], "")
	if got := Width(last); got != bar.BarWidth()+2 {
		t.Errorf("bar %q is %d wide, want %d", last, got, bar.BarWidth()+2)
	}
}

// Not on a terminal, even decorated, a question is asked as Composer asks
// it.
func TestPromptNotStyledOffTerminal(t *testing.T) {
	var buf bytes.Buffer
	out := NewStreamOutput(&buf, VerbosityNormal, new(true), NewOutputFormatter(true, ThemeStyles()...))
	if IsStyledTerminal(out) {
		t.Fatal("a buffer is a styled terminal")
	}
	in, err := NewArrayInput(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	in.SetStream(strings.NewReader("x\n"))
	if _, err := NewQuestionHelper().Ask(in, out, NewQuestion("Name? ", nil)); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "Name? " {
		t.Errorf("prompt %q", got)
	}
}
