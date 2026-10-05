// Ports src/Helper/FormatterHelper.php (symfony/console).

package console

import "strings"

// FormatterHelper formats sections and blocks.
type FormatterHelper struct {
	HelperBase
}

// Name implements Helper.
func (*FormatterHelper) Name() string { return "formatter" }

// FormatSection formats a message within a section.
func (*FormatterHelper) FormatSection(section, message, style string) string {
	return "<" + style + ">[" + section + "]</" + style + "> " + message
}

// FormatBlock formats messages as a block of text.
func (*FormatterHelper) FormatBlock(messages []string, style string, large bool) string {
	return FormatBlock(messages, style, large)
}

// FormatBlock is FormatterHelper::formatBlock().
func FormatBlock(messages []string, style string, large bool) string {
	l := 0
	lines := make([]string, len(messages))
	for i, message := range messages {
		message = Escape(message)
		if large {
			lines[i] = "  " + message + "  "
			l = max(Width(message)+4, l)
		} else {
			lines[i] = " " + message + " "
			l = max(Width(message)+2, l)
		}
	}

	out := make([]string, 0, len(lines)+2)
	if large {
		out = append(out, strings.Repeat(" ", l))
	}
	for _, line := range lines {
		out = append(out, line+strings.Repeat(" ", max(0, l-Width(line))))
	}
	if large {
		out = append(out, strings.Repeat(" ", l))
	}

	for i, m := range out {
		out[i] = "<" + style + ">" + m + "</" + style + ">"
	}

	return strings.Join(out, "\n")
}

// Truncate truncates message to length characters, appending suffix.
func (*FormatterHelper) Truncate(message string, length int, suffix string) string {
	computedLength := length - Width(suffix)

	if computedLength > Width(message) {
		return message
	}

	return Substr(message, 0, length, false) + suffix
}
