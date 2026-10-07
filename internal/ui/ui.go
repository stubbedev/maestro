// Package ui is maestro's own presentation of the free surface
// (docs/PORTING.md "The contract" and "Presentation of free output"):
// how errors, warnings, deprecation notices and hints look. Ported code
// reports the information (a Diagnostic); this package alone decides the
// layout and the styling, with the theme's roles.
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
// item per previous error ("Caused by: "), the alternatives to what was
// typed ("Did you mean this? "), hint ("Hint: "), the command's usage
// ("Usage: ", wrapped to the terminal when decorated) and, in verbose
// mode, debugging details ("Debug: "). Indentation is spaces only, so a
// message's text and line breaks survive as they are; other long lines
// are left for the terminal to wrap, keeping URLs and paths whole for
// copying.
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
	// Alternatives are what the input may have meant ("Did you mean
	// this?"), shown after the causes.
	Alternatives []string
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
	// Width is the terminal's width, which a decorated usage is wrapped
	// to (0: not wrapped).
	Width int
}

// didYouMean starts every question offering alternatives.
const didYouMean = "Did you mean"

// didYouMeanQuestion is Symfony's question before n alternatives.
func didYouMeanQuestion(n int) string {
	if n == 1 {
		return didYouMean + " this?"
	}

	return didYouMean + " one of these?"
}

// DidYouMean is the block Symfony and Composer append to a message to
// offer alternatives ("\n\nDid you mean this?\n    install"): the one
// definition of that text, so a Diagnostic can take it back out
// (TakeAlternatives).
func DidYouMean(alternatives []string) string {
	return "\n\n" + didYouMeanQuestion(len(alternatives)) + "\n    " + strings.Join(alternatives, "\n    ")
}

// TakeAlternatives shows alternatives as the diagnostic's own item: out
// of the message when it ends with their DidYouMean block; as they are
// when the message asks no question of its own (alternatives maestro
// adds); not again when the message offers them its own way (Symfony's
// "is ambiguous.\nDid you mean one of these?" lists).
func (d *Diagnostic) TakeAlternatives(alternatives []string) {
	if len(alternatives) == 0 {
		return
	}
	if message, ok := strings.CutSuffix(d.Message, DidYouMean(alternatives)); ok {
		d.Message, d.Alternatives = message, alternatives

		return
	}
	if !strings.Contains(d.Message, didYouMean) {
		d.Alternatives = alternatives
	}
}

// wrapUsage breaks a command's synopsis into lines of at most width
// columns between its items (an option and its value, a bracketed group,
// stay whole).
func wrapUsage(usage string, width int) string {
	var items []string
	depth, start := 0, 0
	for i, r := range usage {
		switch r {
		case '[', '<':
			depth++
		case ']', '>':
			depth--
		case ' ':
			if depth == 0 {
				items = append(items, usage[start:i])
				start = i + 1
			}
		}
	}
	items = append(items, usage[start:])

	var lines []string
	line := ""
	for _, item := range items {
		switch {
		case line == "":
			line = item
		case len(line)+1+len(item) > width:
			lines = append(lines, line)
			line = item
		default:
			line += " " + item
		}
	}

	return strings.Join(append(lines, line), "\n")
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
	if len(d.Alternatives) > 0 {
		question := didYouMeanQuestion(len(d.Alternatives))
		if len(d.Alternatives) == 1 {
			lines = append(lines, "  "+style(RoleSuccess)(question)+" "+style(RoleAccent)(d.Alternatives[0]))
		} else {
			lines = append(lines, "  "+style(RoleSuccess)(question))
			for _, a := range d.Alternatives {
				lines = append(lines, "      "+style(RoleAccent)(a))
			}
		}
	}
	for _, h := range d.Hints {
		add("  ", "Hint:", style(RoleSuccess), h, text, text)
	}
	if d.Usage != "" {
		usage := d.Usage
		if opts.Decorated && opts.Width > 0 {
			usage = wrapUsage(usage, opts.Width-len("  Usage: "))
		}
		add("  ", "Usage:", muted, usage, text, text)
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
