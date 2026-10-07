package testutil

import (
	"regexp"
	"strings"
)

// How deprecation notices and warnings of Composer's ErrorHandler look is
// free (docs/PORTING.md "The contract"): tests that compare maestro
// with Composer's output check that maestro reports each notice's text,
// not Composer's "Deprecation Notice: ... in <file>:<line>" lines or the
// "Stack trace:" under them at -v.

// composerNotice matches a notice ErrorHandler::outputWarning printed:
// the message, then where PHP raised it. The message may span lines: with
// xdebug.scream on, ErrorHandler::handle appends a blank line and a
// two-line warning to it, and the location follows the last line.
var composerNotice = regexp.MustCompile(`(?s)^(?:Deprecation Notice: |Ignored new PHP warning but it should be reported and fixed: )(.*) in \S[^\n]*:\d+$`)

// noticeStart is the start of the first line of a notice.
var noticeStart = regexp.MustCompile(`^(?:Deprecation Notice: |Ignored new PHP warning but it should be reported and fixed: )`)

// maxNoticeLines is the most lines a notice is looked for in: the
// xdebug.scream suffix makes a one-line message four.
const maxNoticeLines = 4

// composerHidden is the line ErrorHandler prints once instead of further
// notices below -v.
const composerHidden = "More deprecation notices were hidden, run again with `-v` to show them."

// stackFrame is a line of the "Stack trace:" ErrorHandler prints under a
// notice at -v: " <file>:<line>".
var stackFrame = regexp.MustCompile(`^ \S.*:\d+$`)

// ComposerNotices finds the deprecation notices (and the ignored PHP
// warnings) Composer's ErrorHandler printed in output. It returns their
// texts (the message without its location, or the note that more were
// hidden; a message of several lines joined with "\n"), in order, and
// output without those lines and their stack traces. Lines may end in
// "\r" (PHP_EOL on Windows).
func ComposerNotices(output string) (notices []string, rest string) {
	lines := strings.SplitAfter(output, "\n")
	var kept strings.Builder
	for i := 0; i < len(lines); i++ {
		l := strings.TrimRight(lines[i], "\r\n")
		if l == composerHidden {
			notices = append(notices, l)

			continue
		}
		m, last := matchNotice(lines, i)
		if m == nil {
			kept.WriteString(lines[i])

			continue
		}
		notices = append(notices, m[1])
		i = last
		if i+1 < len(lines) && strings.TrimRight(lines[i+1], "\r\n") == "Stack trace:" {
			i++
			for i+1 < len(lines) && stackFrame.MatchString(strings.TrimRight(lines[i+1], "\r\n")) {
				i++
			}
		}
	}

	return notices, kept.String()
}

// matchNotice matches the notice starting at lines[i], which ends at
// lines[last]: the shortest run of lines that is one.
func matchNotice(lines []string, i int) (m []string, last int) {
	if !noticeStart.MatchString(lines[i]) {
		return nil, i
	}
	var run []string
	for j := i; j < len(lines) && j < i+maxNoticeLines; j++ {
		run = append(run, strings.TrimRight(lines[j], "\r\n"))
		if m := composerNotice.FindStringSubmatch(strings.Join(run, "\n")); m != nil {
			return m, j
		}
	}

	return nil, i
}

// noticeLabels are the labels internal/ui gives a deprecation notice, the
// note that more were hidden and a warning.
var noticeLabels = []string{"Deprecated: ", "Note: ", "Warning: "}

// RemoveNotices finds in maestro's output the lines reporting each of
// notices (ComposerNotices' texts), in order: a diagnostic's headline
// (internal/ui's "Deprecated: ", "Note: " or "Warning: ") whose message
// contains the notice's first line, compared as CompactMessage compares
// messages, followed by the notice's further lines, if any, as
// internal/ui indents them (blank lines blank). It returns output without
// those lines and the notices it did not find.
func RemoveNotices(output string, notices []string) (rest string, missing []string) {
	lines := strings.SplitAfter(output, "\n")
	removed := make([]bool, len(lines))
	from := 0
	for _, n := range notices {
		want := strings.Split(strings.ReplaceAll(n, "\r", ""), "\n")
		found := false
		for i := from; i < len(lines); i++ {
			if !reportsNotice(lines, removed, i, want) {
				continue
			}
			for k := range want {
				removed[i+k] = true
			}
			found, from = true, i+len(want)

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

// reportsNotice is whether lines from i on report the notice whose lines
// are want.
func reportsNotice(lines []string, removed []bool, i int, want []string) bool {
	if i+len(want) > len(lines) {
		return false
	}
	for k, w := range want {
		l := strings.TrimRight(lines[i+k], "\r\n")
		switch {
		case removed[i+k]:
			return false
		case k == 0:
			if !hasNoticeLabel(l) || !strings.Contains(CompactMessage(l), CompactMessage(w)) {
				return false
			}
		case strings.TrimSpace(w) == "":
			if strings.TrimSpace(l) != "" {
				return false
			}
		case !strings.HasPrefix(l, " ") || CompactMessage(l) != CompactMessage(w):
			return false
		}
	}

	return true
}

func hasNoticeLabel(line string) bool {
	for _, label := range noticeLabels {
		if strings.HasPrefix(line, label) {
			return true
		}
	}

	return false
}
