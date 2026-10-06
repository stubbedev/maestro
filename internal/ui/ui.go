// Package ui is maestro's own presentation of the free surface
// (docs/PORTING.md "The contract" and "Presentation of free output", #13):
// how errors, warnings, deprecation notices and hints look. Ported code
// reports the information (a Diagnostic); this package alone decides the
// layout and the styling, with lipgloss.
//
// It renders lines, not output: callers write them, one per line ending
// in php.EOL, to the stream Composer writes the information to (stderr for
// all of it), unformatted (OutputRaw, WriteErrorRaw), and say whether that
// stream is decorated by Composer's rules (--ansi/--no-ansi, NO_COLOR, a
// tty; internal/console's IsDecorated). Undecorated lines are plain text
// with no escape sequences; decorated ones carry SGR colour codes, which
// Windows consoles show once internal/console has enabled VT processing.
//
// The layout: a labelled headline ("Error: ", "Warning: ",
// "Deprecated: ", "Note: ") followed by the message, its further lines
// indented under its first; then, indented by two spaces, one labelled
// item per previous error ("Caused by: "), hint ("Hint: "), the command's
// usage ("Usage: ") and, in verbose mode, debugging details
// ("Debug: "). Indentation is spaces only, so a message's text and line
// breaks survive as they are; long lines are left for the terminal to
// wrap, keeping URLs and paths whole for copying.
package ui

import (
	"io"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Kind is what a Diagnostic reports.
type Kind int

// The kinds of diagnostics.
const (
	// Error is a failure that ends the command.
	Error Kind = iota
	// Warning is a problem the command goes on after.
	Warning
	// Deprecation is a deprecation notice.
	Deprecation
	// Note is information about other diagnostics (that some were
	// hidden, ...).
	Note
)

// Diagnostic is what maestro reports about an error or other problem.
type Diagnostic struct {
	Kind Kind
	// Message says what happened; it may span several lines.
	Message string
	// Causes are the messages of the errors that led to it, the nearest
	// first (Composer's previous exceptions).
	Causes []string
	// Hints say what may have caused it or what to do.
	Hints []string
	// Usage is the synopsis of the command that failed on its input.
	Usage string
	// Details are debugging information, shown in verbose mode only.
	Details []string
}

// Options says how a Diagnostic is rendered.
type Options struct {
	// Decorated is whether the stream it is written to is decorated.
	Decorated bool
	// Verbose is whether the output is verbose (-v or more).
	Verbose bool
}

var labels = [...]string{Error: "Error:", Warning: "Warning:", Deprecation: "Deprecated:", Note: "Note:"}

// Lines renders d as lines without line endings.
func (d Diagnostic) Lines(opts Options) []string {
	s := plain
	if opts.Decorated {
		s = decorated()
	}

	var lines []string
	add := func(indent, label string, labelStyle func(string) string, text string, first func(string) string, rest func(string) string) {
		pad := indent + strings.Repeat(" ", len(label)+1)
		for i, l := range SplitLines(text) {
			if i == 0 {
				lines = append(lines, indent+labelStyle(label)+" "+first(l))

				continue
			}
			if l == "" {
				lines = append(lines, "")

				continue
			}
			lines = append(lines, pad+rest(l))
		}
	}

	head := s.headline[d.Kind]
	add("", labels[d.Kind], head, d.Message, s.message[d.Kind], s.text)
	for _, c := range d.Causes {
		add("  ", "Caused by:", s.cause, c, s.text, s.text)
	}
	for _, h := range d.Hints {
		add("  ", "Hint:", s.hint, h, s.text, s.text)
	}
	if d.Usage != "" {
		add("  ", "Usage:", s.faint, d.Usage, s.text, s.text)
	}
	if opts.Verbose && len(d.Details) > 0 {
		add("  ", "Debug:", s.faint, strings.Join(d.Details, "\n"), s.faint, s.faint)
	}

	return lines
}

// SplitLines splits a message into its lines ("\n" or "\r\n" ends one),
// without trailing whitespace; an empty message is one empty line.
func SplitLines(s string) []string {
	s = strings.Trim(s, "\r\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}

	return lines
}

// styles are the functions that style each part of a Diagnostic.
type styles struct {
	headline [len(labels)]func(string) string
	message  [len(labels)]func(string) string
	cause    func(string) string
	hint     func(string) string
	faint    func(string) string
	text     func(string) string
}

func identity(s string) string { return s }

// plain styles nothing.
var plain = styles{
	headline: [len(labels)]func(string) string{identity, identity, identity, identity},
	message:  [len(labels)]func(string) string{identity, identity, identity, identity},
	cause:    identity,
	hint:     identity,
	faint:    identity,
	text:     identity,
}

// decorated is the styles of decorated output: the 16 ANSI colours, which
// follow the terminal's theme, and no background colours.
var decorated = sync.OnceValue(func() styles {
	// The renderer never looks at a terminal: whether to decorate is
	// Composer's decision, made by the caller.
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.ANSI)
	base := r.NewStyle().TabWidth(lipgloss.NoTabConversion)
	render := func(st lipgloss.Style) func(string) string {
		return func(s string) string {
			if s == "" {
				return s
			}

			return st.Render(s)
		}
	}
	red, yellow, green, cyan := lipgloss.Color("1"), lipgloss.Color("3"), lipgloss.Color("2"), lipgloss.Color("6")
	bold := render(base.Bold(true))

	return styles{
		headline: [len(labels)]func(string) string{
			Error:       render(base.Bold(true).Foreground(red)),
			Warning:     render(base.Bold(true).Foreground(yellow)),
			Deprecation: render(base.Bold(true).Foreground(yellow)),
			Note:        render(base.Foreground(cyan)),
		},
		message: [len(labels)]func(string) string{Error: bold, Warning: identity, Deprecation: identity, Note: identity},
		cause:   render(base.Foreground(yellow)),
		hint:    render(base.Foreground(green)),
		faint:   render(base.Faint(true)),
		text:    identity,
	}
})
