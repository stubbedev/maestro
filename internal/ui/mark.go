package ui

import (
	"regexp"
	"strconv"
	"strings"
)

// Mark is the bullet of an item of a list of operations ("  - Installing
// x (1.0.0)"), which says what the operation does. Undecorated it is
// Composer's "-"; decorated, the formatter shows the mark's glyph in its
// role instead ("+" green for an install, "↑" for an upgrade, ...).
type Mark uint8

// The marks.
const (
	// MarkItem is any other item.
	MarkItem Mark = iota
	// MarkInstall is a package installed (or locked).
	MarkInstall
	// MarkUpgrade is a package upgraded.
	MarkUpgrade
	// MarkDowngrade is a package downgraded.
	MarkDowngrade
	// MarkRemove is a package removed.
	MarkRemove

	numMarks
)

// marks are each mark's name, role and glyph.
var marks = [numMarks]struct {
	name  string
	role  Role
	glyph Glyph
}{
	MarkItem:      {"item", RoleMuted, GlyphItem},
	MarkInstall:   {"install", RoleSuccess, GlyphInstall},
	MarkUpgrade:   {"upgrade", RoleAccent, GlyphUpgrade},
	MarkDowngrade: {"downgrade", RoleNotice, GlyphDowngrade},
	MarkRemove:    {"remove", RoleHighlight, GlyphRemove},
}

// Marks are all the marks.
func Marks() []Mark {
	m := make([]Mark, numMarks)
	for i := range m {
		m[i] = Mark(i)
	}

	return m
}

// markText is what a mark's tag encloses: Composer's bullet.
const markText = "-"

// Tag is the name of the formatter tag of the mark.
func (m Mark) Tag() string { return "maestro-mark-" + marks[m].name }

// Item is the start of an item of a list marked m: formatter markup that
// is Composer's "  - " undecorated.
func (m Mark) Item() string { return "  <" + m.Tag() + ">" + markText + "</" + m.Tag() + "> " }

// String is the mark's name.
func (m Mark) String() string { return marks[m].name }

// Styled is what the formatter shows for text in the mark's tag when
// decorated: in its role, Composer's bullet as the mark's glyph.
func (m Mark) Styled(text string) string {
	if text == markText {
		text = marks[m].glyph.String()
	}

	return marks[m].role.Styled(text)
}

// Count is a number of things, as Composer writes it: "1 install",
// "2 installs" (Noun is the singular, "s" makes the plural).
type Count struct {
	N    int
	Noun string
}

// String is the count as text.
func (c Count) String() string {
	s := strconv.Itoa(c.N) + " " + c.Noun
	if c.N != 1 {
		s += "s"
	}

	return s
}

// Tally is "label: count, count, ..." as formatter markup (Composer's
// "<info>Package operations: 2 installs, 1 update, 0 removals</info>"):
// in the success role, but a zero count muted, so what changed stands
// out. Undecorated it is Composer's text.
func Tally(label string, counts ...Count) string {
	var b strings.Builder
	b.WriteString(RoleSuccess.Wrap(label + ": "))
	for i, c := range counts {
		if i > 0 {
			b.WriteString(RoleSuccess.Wrap(", "))
		}
		role := RoleSuccess
		if c.N == 0 {
			role = RoleMuted
		}
		b.WriteString(role.Wrap(c.String()))
	}

	return b.String()
}

// themeTagRe matches the tags of maestro's theme (Role.Tag, Mark.Tag).
var themeTagRe = regexp.MustCompile(`<(/?)(maestro-[a-z-]+)>`)

// Foreign is markup for a formatter that knows only Symfony's and
// Composer's tags (a PHP IO given to maestro by a plugin): each role's tag
// becomes its inline style, and marks and faint roles lose their tags,
// so the text is what maestro's formatter would show.
func Foreign(markup string) string {
	if !strings.Contains(markup, "<maestro-") {
		return markup
	}

	inline := map[string]string{}
	for _, r := range Roles() {
		inline[r.Tag()] = r.Inline()
	}

	return themeTagRe.ReplaceAllStringFunc(markup, func(tag string) string {
		m := themeTagRe.FindStringSubmatch(tag)
		style, ok := inline[m[2]]
		switch {
		case !ok && !strings.HasPrefix(m[2], "maestro-mark-"):
			return tag
		case style == "":
			return ""
		case m[1] == "/":
			return "</>"
		}

		return "<" + style + ">"
	})
}
