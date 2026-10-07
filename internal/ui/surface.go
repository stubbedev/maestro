package ui

import "strings"

// Surface is the side of the contract (docs/PORTING.md "The contract")
// an output is on: frozen output is Composer's, byte for byte; free
// output is maestro's to present.
type Surface uint8

// The surfaces.
const (
	// Frozen output is written as Composer writes it; maestro's theme
	// does not restyle it.
	Frozen Surface = iota
	// Free output takes its look from the theme when decorated, with the
	// same characters in the same places as undecorated.
	Free
)

// String is the surface's name.
func (s Surface) String() string {
	if s == Free {
		return "free"
	}

	return "frozen"
}

// Style is text, formatter markup, in role on the free surface
// (Role.Wrap) and text as it is on the frozen one. An empty text stays
// empty, so trimming around it works as on Composer's text, and a text
// closing a tag of its own ("</") is left as it is: inside the role's tag
// it could close a style the formatter has not opened, which Composer's
// text alone never does. Undecorated, the formatter drops the role's tag,
// so the free surface's text is the frozen one's.
func (s Surface) Style(r Role, text string) string {
	if s != Free || text == "" || strings.Contains(text, "</") {
		return text
	}

	return r.Wrap(text)
}

// Format is a value of a command's --format option and the surface the
// output it selects is on.
type Format struct {
	Name    string
	Surface Surface
}

// Formats are the values of a --format option, in the order of its
// suggestions; the option's suggestions are their names, so no format is
// offered without saying which surface it is on.
type Formats []Format

// Names are the formats' names.
func (fs Formats) Names() []string {
	names := make([]string, len(fs))
	for i, f := range fs {
		names[i] = f.Name
	}

	return names
}

// Surface is the surface of the format named name; an unknown format is
// frozen, so nothing unclassified is restyled.
func (fs Formats) Surface(name any) Surface {
	for _, f := range fs {
		if f.Name == name {
			return f.Surface
		}
	}

	return Frozen
}
