package testutil

import (
	"regexp"
	"strings"
	"unicode"
)

// How errors are rendered is free (docs/PORTING.md "The contract"):
// tests that compare maestro with Composer's recorded or live output check
// the messages of Composer's exceptions, not its error boxes, exception
// classes, "In File.php line N:" headings or "Exception trace:" stacks.

// boxTitle is the first line of a Symfony error box at -v and above, or of
// an exception without a message: "  [Class (code)]".
var boxTitle = regexp.MustCompile(`^  \[[^\]]+\] *$`)

// ErrorRendering finds the exceptions Symfony's
// Application::renderThrowable rendered in output (one error box per
// exception of the previous chain, each after an empty line and, unless
// it is a console exception at normal verbosity, an "In File.php line N:"
// heading). It returns their messages, compacted (see CompactMessage), in
// order, and the byte offset where the rendering starts: the empty line
// before the first box (len(output) when there is none).
//
// A box is a line of spaces, the message lines (with a "  [Class]" title
// first at -v) and a line of spaces before an empty line; a line of
// spaces followed by more box lines is an empty line of the message. Lines
// may end in "\r" (PHP_EOL on Windows). The empty line may instead end
// the "  > " prompt of a question whose answer was invalid.
func ErrorRendering(output string) (messages []string, start int) {
	lines := strings.Split(output, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	spaces := func(i int) bool {
		return i < len(lines) && lines[i] != "" && strings.TrimLeft(lines[i], " ") == ""
	}
	start = -1
	for i := 0; i < len(lines); i++ {
		heading := i > 0 && strings.HasPrefix(lines[i-1], "In ")
		// the empty line ends the prompt of a question whose answer
		// failed validation (stdin is not echoed)
		prompt := i > 0 && strings.TrimSpace(lines[i-1]) == ">"
		if !spaces(i) || (i > 0 && lines[i-1] != "" && !heading && !prompt) {
			continue
		}
		end := i + 1
		for end < len(lines) && (!spaces(end) || (end+1 < len(lines) && lines[end+1] != "")) {
			end++
		}
		if end == len(lines) {
			break
		}
		if start < 0 {
			first := i
			if heading {
				first--
			}
			if first > 0 && lines[first-1] == "" {
				first--
			}
			start = lineOffset(output, first)
		}
		body := lines[i+1 : end]
		if len(body) > 0 && boxTitle.MatchString(body[0]) {
			body = body[1:]
		}
		if msg := CompactMessage(strings.Join(body, "\n")); msg != "" {
			messages = append(messages, msg)
		}
		i = end
	}
	if start < 0 {
		start = len(output)
	}

	return messages, start
}

// lineOffset is the byte offset of line n of s.
func lineOffset(s string, n int) int {
	off := 0
	for range n {
		i := strings.IndexByte(s[off:], '\n')
		if i < 0 {
			return len(s)
		}
		off += i + 1
	}

	return off
}

// typeErrorSite is PHP's call site in a TypeError's message, compacted.
var typeErrorSite = regexp.MustCompile(`,calledin\S+?online[0-9]+`)

// connectTime is curl's "after N ms" in a connection error, compacted: it
// depends on the machine's load.
var connectTime = regexp.MustCompile(`after[0-9]+ms:`)

// CompactMessage is the form error messages are compared in, so that how
// they are laid out does not matter: without whitespace (box padding and
// wrapping, indentation, line breaks) and box-drawing characters, without
// PHP's call site in a TypeError (", called in X on line N"), and with
// curl's connection time as 0 ms. A message is reported when its compacted
// form occurs in the compacted output.
func CompactMessage(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || (r >= 0x2500 && r <= 0x257f) {
			return -1
		}

		return r
	}, s)
	s = typeErrorSite.ReplaceAllString(s, "")

	return connectTime.ReplaceAllString(s, "after0ms:")
}
