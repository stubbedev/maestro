package ui

import "strings"

// What decorated output on a terminal adds to progress and prompts (#35):
// formatter markup that only the caller's decorated, terminal output
// receives. Undecorated, or not on a terminal, progress and prompts are
// Composer's, so nothing here is used.
//
// They stay line-based: Composer's questions read lines (with
// autocompletion, hidden answers and repeated attempts) and its progress
// bar redraws one line, which internal/console already does on every
// console Composer supports, Windows included. So the look comes from the
// theme, not from a full-screen program taking over the terminal.

// BarStyle is how a progress bar's cells look: formatter markup for each
// done cell, the leading cell and each cell still to do.
type BarStyle struct {
	Done, Head, Todo string
}

// ProgressBar is the style of a progress bar on a decorated terminal: a
// solid line, green as far as it got and faint after.
func ProgressBar() BarStyle {
	return BarStyle{
		Done: RoleSuccess.Wrap(GlyphBarDone.String()),
		Head: RoleSuccess.Wrap(GlyphBarHead.String()),
		Todo: RoleMuted.Wrap(GlyphBarTodo.String()),
	}
}

// Prompt is a question's message, formatter markup, as asked on a
// decorated terminal: its first line is marked with the prompt glyph.
// The message's text is kept as it is.
func Prompt(message string) string {
	text := strings.TrimLeft(message, "\r\n")
	lead := message[:len(message)-len(text)]

	return lead + RoleAccent.Wrap(GlyphPrompt.String()) + " " + text
}
