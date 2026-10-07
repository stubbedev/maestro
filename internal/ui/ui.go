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

import "strings"

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

// headlineRoles are the roles of each kind's label.
var headlineRoles = [len(labels)]Role{Error: RoleDanger, Warning: RoleWarning, Deprecation: RoleWarning, Note: RoleAccent}

// Lines renders d as lines without line endings.
func (d Diagnostic) Lines(opts Options) []string {
	style := func(r Role) func(string) string {
		return func(s string) string { return r.Render(s, opts.Decorated) }
	}
	text := func(s string) string { return s }
	muted := style(RoleMuted)

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

	message := text
	if d.Kind == Error {
		message = style(RoleEmphasis)
	}
	add("", labels[d.Kind], style(headlineRoles[d.Kind]), d.Message, message, text)
	for _, c := range d.Causes {
		add("  ", "Caused by:", style(RoleNotice), c, text, text)
	}
	for _, h := range d.Hints {
		add("  ", "Hint:", style(RoleSuccess), h, text, text)
	}
	if d.Usage != "" {
		add("  ", "Usage:", muted, d.Usage, text, text)
	}
	if opts.Verbose && len(d.Details) > 0 {
		add("  ", "Debug:", muted, strings.Join(d.Details, "\n"), muted, muted)
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
