package testutil

import (
	"regexp"
	"strings"
)

// How deprecation notices and warnings of Composer's ErrorHandler look is
// free (docs/PORTING.md "The contract", #13): tests that compare maestro
// with Composer's output check that maestro reports each notice's text,
// not Composer's "Deprecation Notice: ... in <file>:<line>" lines or the
// "Stack trace:" under them at -v.

// composerNotice matches the line of a notice ErrorHandler::outputWarning
// printed: the message, then where PHP raised it.
var composerNotice = regexp.MustCompile(`^(?:Deprecation Notice: |Ignored new PHP warning but it should be reported and fixed: )(.*) in \S.*:\d+$`)

// composerHidden is the line ErrorHandler prints once instead of further
// notices below -v.
const composerHidden = "More deprecation notices were hidden, run again with `-v` to show them."

// stackFrame is a line of the "Stack trace:" ErrorHandler prints under a
// notice at -v: " <file>:<line>".
var stackFrame = regexp.MustCompile(`^ \S.*:\d+$`)

// ComposerNotices finds the deprecation notices (and the ignored PHP
// warnings) Composer's ErrorHandler printed in output. It returns their
// texts (the message without its location, or the note that more were
// hidden), in order, and output without those lines and their stack
// traces. Lines may end in "\r" (PHP_EOL on Windows).
func ComposerNotices(output string) (notices []string, rest string) {
	lines := strings.SplitAfter(output, "\n")
	var kept strings.Builder
	for i := 0; i < len(lines); i++ {
		l := strings.TrimRight(lines[i], "\r\n")
		if l == composerHidden {
			notices = append(notices, l)

			continue
		}
		m := composerNotice.FindStringSubmatch(l)
		if m == nil {
			kept.WriteString(lines[i])

			continue
		}
		notices = append(notices, m[1])
		if i+1 < len(lines) && strings.TrimRight(lines[i+1], "\r\n") == "Stack trace:" {
			i++
			for i+1 < len(lines) && stackFrame.MatchString(strings.TrimRight(lines[i+1], "\r\n")) {
				i++
			}
		}
	}

	return notices, kept.String()
}

// noticeLabels are the labels internal/ui gives a deprecation notice, the
// note that more were hidden and a warning.
var noticeLabels = []string{"Deprecated: ", "Note: ", "Warning: "}

// RemoveNotices finds in maestro's output a line reporting each of
// notices (ComposerNotices' texts), in order: a diagnostic's headline
// (internal/ui's "Deprecated: ", "Note: " or "Warning: ") whose message
// contains it, compared as CompactMessage compares messages. It returns
// output without those lines and the notices it did not find.
func RemoveNotices(output string, notices []string) (rest string, missing []string) {
	lines := strings.SplitAfter(output, "\n")
	removed := make([]bool, len(lines))
	from := 0
	for _, n := range notices {
		want := CompactMessage(n)
		found := false
		for i := from; i < len(lines); i++ {
			l := strings.TrimRight(lines[i], "\r\n")
			if removed[i] || !hasNoticeLabel(l) || !strings.Contains(CompactMessage(l), want) {
				continue
			}
			removed[i], found, from = true, true, i+1

			break
		}
		if !found {
			missing = append(missing, n)
		}
	}

	var kept strings.Builder
	for i, l := range lines {
		if !removed[i] {
			kept.WriteString(l)
		}
	}

	return kept.String(), missing
}

func hasNoticeLabel(line string) bool {
	for _, label := range noticeLabels {
		if strings.HasPrefix(line, label) {
			return true
		}
	}

	return false
}
