// maestro's theme for formatter tags: Composer's tags and the roles and
// marks of internal/ui's palette render as the palette says
// (docs/PORTING.md "Presentation of free output"), not with
// Symfony's colours.

package console

import (
	"fmt"
	"io"

	"github.com/stubbedev/maestro/internal/ui"
)

// themed is a role or mark of maestro's theme.
type themed interface {
	fmt.Stringer
	// Styled is text as decorated output shows it (an empty text too).
	Styled(text string) string
}

// ThemeStyle is the formatter style of a role (ui.Role) or mark (ui.Mark)
// of maestro's theme. It is fixed, so its setters fail.
type ThemeStyle struct{ t themed }

// NewRoleStyle is the style of role.
func NewRoleStyle(role ui.Role) *ThemeStyle { return &ThemeStyle{t: role} }

// NewMarkStyle is the style of mark.
func NewMarkStyle(mark ui.Mark) *ThemeStyle { return &ThemeStyle{t: mark} }

func (s *ThemeStyle) fixed() error {
	return newError(KindInvalidArgument, `The style "%s" is fixed by maestro's theme.`, s.t)
}

// SetForeground implements Style.
func (s *ThemeStyle) SetForeground(string) error { return s.fixed() }

// SetBackground implements Style.
func (s *ThemeStyle) SetBackground(string) error { return s.fixed() }

// SetOption implements Style.
func (s *ThemeStyle) SetOption(string) error { return s.fixed() }

// UnsetOption implements Style.
func (s *ThemeStyle) UnsetOption(string) error { return s.fixed() }

// SetOptions implements Style.
func (s *ThemeStyle) SetOptions([]string) error { return s.fixed() }

// Apply implements Style. Like Symfony's styles it styles an empty text
// too, which the style stack compares styles by.
func (s *ThemeStyle) Apply(text string) string { return s.t.Styled(text) }

// ThemeStyles are the styles of Composer's tags in maestro's theme
// (ui.ComposerTags), for Composer's formatters
// (Factory::createAdditionalStyles).
func ThemeStyles() []NamedStyle {
	styles := make([]NamedStyle, len(ui.ComposerTags))
	for i, t := range ui.ComposerTags {
		styles[i] = NamedStyle{Name: t.Name, Style: NewRoleStyle(t.Role)}
	}

	return styles
}

// IsStyledTerminal reports whether out (its error output for a console
// output) is decorated and a terminal: where maestro styles progress and
// prompts (ui.ProgressBar, ui.Prompt) beyond Composer's colours.
func IsStyledTerminal(out Output) bool {
	out = ErrorOutputOf(out)
	s, ok := out.(interface{ Stream() io.Writer })

	return ok && out.IsDecorated() && IsTTY(s.Stream())
}

// setThemeStyles registers the tags of every role (ui.Role.Tag) and mark
// (ui.Mark.Tag) on f, so maestro's text styled with them renders on any
// formatter.
func setThemeStyles(f Formatter) {
	for _, r := range ui.Roles() {
		f.SetStyle(r.Tag(), NewRoleStyle(r))
	}
	for _, m := range ui.Marks() {
		f.SetStyle(m.Tag(), NewMarkStyle(m))
	}
}
