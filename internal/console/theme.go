// maestro's theme for formatter tags: Composer's tags and the roles of
// internal/ui's palette render as the palette says (docs/PORTING.md
// "Presentation of free output"), not with Symfony's colours.

package console

import (
	"io"

	"github.com/stubbedev/maestro/internal/ui"
)

// RoleStyle is the formatter style of a palette role: the role's SGR
// sequences around the text. It is fixed, so its setters fail.
type RoleStyle struct{ role ui.Role }

// NewRoleStyle is the style of role.
func NewRoleStyle(role ui.Role) *RoleStyle { return &RoleStyle{role: role} }

func (s *RoleStyle) fixed() error {
	return newError(KindInvalidArgument, `The style of the "%s" role is fixed by maestro's theme.`, s.role)
}

// SetForeground implements Style.
func (s *RoleStyle) SetForeground(string) error { return s.fixed() }

// SetBackground implements Style.
func (s *RoleStyle) SetBackground(string) error { return s.fixed() }

// SetOption implements Style.
func (s *RoleStyle) SetOption(string) error { return s.fixed() }

// UnsetOption implements Style.
func (s *RoleStyle) UnsetOption(string) error { return s.fixed() }

// SetOptions implements Style.
func (s *RoleStyle) SetOptions([]string) error { return s.fixed() }

// Apply implements Style. Like Symfony's styles it styles an empty text
// too, which the style stack compares styles by.
func (s *RoleStyle) Apply(text string) string {
	set, reset := s.role.SGR()

	return set + text + reset
}

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

// setRoleStyles registers every role's own tag (ui.Role.Tag) on f, so
// maestro's text styled by role (ui.Role.Wrap) renders on any formatter.
func setRoleStyles(f Formatter) {
	for _, r := range ui.Roles() {
		f.SetStyle(r.Tag(), NewRoleStyle(r))
	}
}
