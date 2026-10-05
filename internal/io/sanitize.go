// Ports ConsoleIO::sanitize() and ConsoleIO::ensureValidUtf8() of
// src/Composer/IO/ConsoleIO.php (Composer).

package io

import (
	"strings"
	"unicode/utf8"
)

// Sanitize removes ANSI escape sequences and control characters from a
// message (ConsoleIO::sanitize). With allowNewlines, "\n" and "\r\n" are
// kept; otherwise every character in \x01-\x1A is removed. Invalid UTF-8 is
// first replaced like mb_convert_encoding($s, 'UTF-8', 'UTF-8').
//
// It is a hand-written matcher for the PCRE pattern (in /u mode)
//
//	\x1B\[[\x30-\x3F]*[\x20-\x2F]*[\x40-\x7E]|\x1B\].*?(?:\x1B\\|\x07)|\x1B.|[\x01-\x09\x0B\x0C\x0E-\x1A]|\r(?!\n)
//
// (the last two branches being [\x01-\x1A] without allowNewlines), whose
// alternatives are tried in order at each position.
func Sanitize(message string, allowNewlines bool) string {
	message = ensureValidUTF8(message)

	var b strings.Builder
	last := 0
	for i := 0; i < len(message); {
		end := sanitizeMatch(message, i, allowNewlines)
		if end < 0 {
			i++

			continue
		}
		if b.Cap() == 0 {
			b.Grow(len(message))
		}
		b.WriteString(message[last:i])
		i, last = end, end
	}
	if last == 0 {
		return message
	}
	b.WriteString(message[last:])

	return b.String()
}

// SanitizeMessages sanitizes each message (sanitize() of an array).
func SanitizeMessages(messages []string, allowNewlines bool) []string {
	out := make([]string, len(messages))
	for i, m := range messages {
		out[i] = Sanitize(m, allowNewlines)
	}

	return out
}

// sanitizeMatch returns the end of the match starting at s[i], or -1.
func sanitizeMatch(s string, i int, allowNewlines bool) int {
	c := s[i]
	switch {
	case c == 0x1B:
		if i+1 >= len(s) {
			return -1
		}
		switch s[i+1] {
		case '[':
			// CSI: parameter bytes, intermediate bytes, final byte. The
			// classes are disjoint, so greedy matching never backtracks.
			j := i + 2
			for j < len(s) && s[j] >= 0x30 && s[j] <= 0x3F {
				j++
			}
			for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2F {
				j++
			}
			if j < len(s) && s[j] >= 0x40 && s[j] <= 0x7E {
				return j + 1
			}
		case ']':
			// OSC: the shortest run without "\n" up to ESC \ or BEL.
			for j := i + 2; j < len(s) && s[j] != '\n'; j++ {
				if s[j] == 0x07 {
					return j + 1
				}
				if s[j] == 0x1B && j+1 < len(s) && s[j+1] == '\\' {
					return j + 2
				}
			}
		}
		// ESC followed by any character but a newline.
		if s[i+1] == '\n' {
			return -1
		}
		_, size := utf8.DecodeRuneInString(s[i+1:])

		return i + 1 + size
	case c >= 0x01 && c <= 0x1A:
		if !allowNewlines {
			return i + 1
		}
		switch c {
		case '\n':
			return -1
		case '\r':
			if i+1 < len(s) && s[i+1] == '\n' {
				return -1
			}
		}

		return i + 1
	}

	return -1
}

// ensureValidUTF8 replaces every maximal invalid subsequence with "?", as
// mbstring's UTF-8 to UTF-8 conversion does.
func ensureValidUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] < utf8.RuneSelf {
			b.WriteByte(s[i])
			i++

			continue
		}
		if r, size := utf8.DecodeRuneInString(s[i:]); r != utf8.RuneError || size > 1 {
			b.WriteString(s[i : i+size])
			i += size

			continue
		}
		b.WriteByte('?')
		i += invalidSubpartLen(s[i:])
	}

	return b.String()
}

// invalidSubpartLen returns the length of the maximal subpart of an
// ill-formed UTF-8 sequence starting at s[0] (at least 1).
func invalidSubpartLen(s string) int {
	c := s[0]
	var need int
	lo, hi := byte(0x80), byte(0xBF)
	switch {
	case c >= 0xC2 && c <= 0xDF:
		need = 1
	case c == 0xE0:
		need, lo = 2, 0xA0
	case c == 0xED:
		need, hi = 2, 0x9F
	case c >= 0xE1 && c <= 0xEF:
		need = 2
	case c == 0xF0:
		need, lo = 3, 0x90
	case c == 0xF4:
		need, hi = 3, 0x8F
	case c >= 0xF1 && c <= 0xF3:
		need = 3
	default:
		return 1
	}
	n := 1
	for k := range need {
		if n >= len(s) {
			break
		}
		x := s[n]
		if k == 0 && (x < lo || x > hi) || k > 0 && (x < 0x80 || x > 0xBF) {
			break
		}
		n++
	}

	return n
}
