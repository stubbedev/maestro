package ui

import (
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Role is what a piece of decorated free output is: a success, a warning,
// a package name... The theme (palette) says how each role looks; every
// styled surface of maestro takes its look from a Role, never from a
// colour of its own, so one edit here restyles all of it.
//
// The palette keeps to the 16 ANSI colours, which follow the terminal's
// theme (light or dark), and to background colours only where Composer
// uses them.
type Role uint8

// The roles of the palette.
const (
	// RoleSuccess is what went well (Composer's <info>): green.
	RoleSuccess Role = iota
	// RoleNotice is a remark worth a look (Composer's <comment>): yellow.
	RoleNotice
	// RoleWarning is a problem the run goes on after: bold yellow.
	RoleWarning
	// RoleDanger is a failure: bold red.
	RoleDanger
	// RoleHighlight is a value that needs attention (Composer's
	// <highlight>): red.
	RoleHighlight
	// RoleAccent sets information apart: cyan.
	RoleAccent
	// RoleSecondary is a second accent: magenta.
	RoleSecondary
	// RoleTertiary is a third accent: blue.
	RoleTertiary
	// RoleMuted is information in the background: faint.
	RoleMuted
	// RoleEmphasis is text that stands out: bold.
	RoleEmphasis
	// RolePackage is a package name: bold green.
	RolePackage
	// RoleVersion is a version: yellow.
	RoleVersion
	// RoleLink is a link: underlined.
	RoleLink
	// RoleQuestion is a question asked (Composer's <question>): black on
	// cyan.
	RoleQuestion
	// RoleBanner is a heading block (Composer's init welcome): white on
	// blue.
	RoleBanner

	numRoles
)

// Roles are all the roles of the palette.
func Roles() []Role {
	roles := make([]Role, numRoles)
	for i := range roles {
		roles[i] = Role(i)
	}

	return roles
}

// TreeRoles are the roles of the levels of a dependency tree (show
// --tree, depends/prohibits --tree), cycled through by depth: Composer's
// green, yellow, cyan, magenta and blue.
var TreeRoles = [...]Role{RoleSuccess, RoleNotice, RoleAccent, RoleSecondary, RoleTertiary}

// TreeTags are the tags of TreeRoles.
func TreeTags() []string {
	tags := make([]string, len(TreeRoles))
	for i, r := range TreeRoles {
		tags[i] = r.Tag()
	}

	return tags
}

// FormatterTag is a formatter tag of Composer's text and the role it is
// rendered with.
type FormatterTag struct {
	Name string
	Role Role
}

// ComposerTags are the tags Composer's text carries (Symfony's defaults
// and Factory::createAdditionalStyles) and the roles maestro renders them
// with: the one table from tag to role. Composer's <error> (white on red)
// and <warning> (black on yellow) boxes become the Danger and Warning
// roles, the look of maestro's diagnostics, so one run shows one look.
var ComposerTags = [...]FormatterTag{
	{"info", RoleSuccess},
	{"comment", RoleNotice},
	{"question", RoleQuestion},
	{"error", RoleDanger},
	{"warning", RoleWarning},
	{"highlight", RoleHighlight},
}

// color is one of the 8 basic ANSI colours (their normal, theme-following
// variants), or none.
type color uint8

const (
	noColor color = iota
	black
	red
	green
	yellow
	blue
	magenta
	cyan
	white
)

// colorNames are Symfony's names of the colours.
var colorNames = [...]string{"", "black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}

// attrs are text attributes.
type attrs uint8

const (
	bold attrs = 1 << iota
	faint
	underline
)

// spec is how a role looks.
type spec struct {
	name   string
	fg, bg color
	attrs  attrs
}

// palette is the theme: how each role looks.
var palette = [numRoles]spec{
	RoleSuccess:   {name: "success", fg: green},
	RoleNotice:    {name: "notice", fg: yellow},
	RoleWarning:   {name: "warning", fg: yellow, attrs: bold},
	RoleDanger:    {name: "danger", fg: red, attrs: bold},
	RoleHighlight: {name: "highlight", fg: red},
	RoleAccent:    {name: "accent", fg: cyan},
	RoleSecondary: {name: "secondary", fg: magenta},
	RoleTertiary:  {name: "tertiary", fg: blue},
	RoleMuted:     {name: "muted", attrs: faint},
	RoleEmphasis:  {name: "emphasis", attrs: bold},
	RolePackage:   {name: "package", fg: green, attrs: bold},
	RoleVersion:   {name: "version", fg: yellow},
	RoleLink:      {name: "link", attrs: underline},
	RoleQuestion:  {name: "question", fg: black, bg: cyan},
	RoleBanner:    {name: "banner", fg: white, bg: blue},
}

// String is the role's name.
func (r Role) String() string { return palette[r].name }

// Tag is the name of the formatter tag that renders text in the role
// ("<maestro-success>...</>"); prefixed so it cannot take over a tag-like
// word of Composer's text.
func (r Role) Tag() string { return "maestro-" + palette[r].name }

// Wrap encloses text, formatter markup, in the role's tag.
func (r Role) Wrap(text string) string { return "<" + r.Tag() + ">" + text + "</" + r.Tag() + ">" }

// Inline is the role's look as an inline formatter style of Symfony's
// ("fg=green;options=bold"), to combine with "href=..." in one tag.
// Symfony has no faint option, so a faint role's inline style leaves the
// faintness out.
func (r Role) Inline() string {
	s := palette[r]
	var parts, options []string
	if s.fg != noColor {
		parts = append(parts, "fg="+colorNames[s.fg])
	}
	if s.bg != noColor {
		parts = append(parts, "bg="+colorNames[s.bg])
	}
	if s.attrs&bold != 0 {
		options = append(options, "bold")
	}
	if s.attrs&underline != 0 {
		options = append(options, "underscore")
	}
	if len(options) > 0 {
		parts = append(parts, "options="+strings.Join(options, ","))
	}

	return strings.Join(parts, ";")
}

// Styled is text between the escape sequence that starts text in the
// role and the one that ends it (an empty text too). Render writes it,
// and internal/console renders the role's formatter tags with it, so tags
// and diagnostics look the same.
func (r Role) Styled(text string) string {
	seq := sgr()[r]

	return seq[0] + text + seq[1]
}

// Render styles text in the role when decorated; undecorated, or for an
// empty text, it is text unchanged.
func (r Role) Render(text string, decorated bool) string {
	if !decorated || text == "" {
		return text
	}

	return r.Styled(text)
}

// sgr is the SGR() of every role: lipgloss's rendering of the role, in
// the ANSI profile, of a sentinel, split around it.
var sgr = sync.OnceValue(func() (seqs [numRoles][2]string) {
	// The renderer never looks at a terminal: whether to decorate is
	// Composer's decision, made by the caller.
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.ANSI)
	const sentinel = "\x00"
	for i, s := range palette {
		st := r.NewStyle()
		if s.fg != noColor {
			st = st.Foreground(lipgloss.Color(strconv.Itoa(int(s.fg - black))))
		}
		if s.bg != noColor {
			st = st.Background(lipgloss.Color(strconv.Itoa(int(s.bg - black))))
		}
		st = st.Bold(s.attrs&bold != 0).Faint(s.attrs&faint != 0).Underline(s.attrs&underline != 0)
		set, reset, _ := strings.Cut(st.Render(sentinel), sentinel)
		seqs[i] = [2]string{set, reset}
	}

	return seqs
})
