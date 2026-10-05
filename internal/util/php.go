// PHP runtime functions the Composer\Util ports depend on, with PHP's exact
// semantics: dirname, basename, var_export of a string, strerror messages.

package util

import (
	"errors"
	"strconv"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
)

// isPathSep reports whether c separates path segments for PHP's dirname and
// basename: only '/' on Unix, '/' and '\' on Windows.
func isPathSep(c byte, windows bool) bool {
	return c == '/' || (windows && c == '\\')
}

// phpDirname ports zend_dirname (PHP's dirname with one level).
func phpDirname(path string, windows bool) string {
	if path == "" {
		return ""
	}

	drive := ""
	if windows && len(path) >= 2 && isASCIIAlpha(path[0]) && path[1] == ':' {
		// The drive spec is kept as is; dirname("c:") is "c:".
		drive, path = path[:2], path[2:]
		if path == "" {
			return drive
		}
	}

	sep := "/"
	if windows {
		sep = `\`
	}

	end := len(path) - 1
	for end >= 0 && isPathSep(path[end], windows) {
		end--
	}

	if end < 0 {
		return drive + sep
	}

	for end >= 0 && !isPathSep(path[end], windows) {
		end--
	}

	if end < 0 {
		return drive + "."
	}

	for end >= 0 && isPathSep(path[end], windows) {
		end--
	}

	if end < 0 {
		return drive + sep
	}

	return drive + path[:end+1]
}

// phpBasename ports php_basename without a suffix.
func phpBasename(path string, windows bool) string {
	end := len(path)
	for end > 0 && isPathSep(path[end-1], windows) {
		end--
	}

	start := end
	for start > 0 && !isPathSep(path[start-1], windows) {
		start--
	}

	return path[start:end]
}

// varExportString ports var_export($s, true) for a string.
func varExportString(s string) string {
	var b strings.Builder

	b.Grow(len(s) + 2)
	b.WriteByte('\'')

	for i := range len(s) {
		switch c := s[i]; c {
		case '\'', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case 0:
			b.WriteString(`' . "\0" . '`)
		default:
			b.WriteByte(c)
		}
	}

	b.WriteByte('\'')

	return b.String()
}

func isASCIIAlpha(c byte) bool {
	return (c|0x20) >= 'a' && (c|0x20) <= 'z'
}

func isASCIIDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isASCIIAlnum(c byte) bool {
	return isASCIIDigit(c) || isASCIIAlpha(c)
}

// hasPrefixFold reports whether s starts with the lowercase ASCII prefix,
// ignoring ASCII case.
func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

// isPCRESpace reports whether c matches PCRE2's \s without UCP: space, \t,
// \n, \v, \f and \r.
func isPCRESpace(c byte) bool {
	return c == ' ' || (c >= '\t' && c <= '\r')
}

// pcreEndsAt reports whether a PCRE "$" (without the D modifier) matches at
// position i of s: at the very end, or before a final newline.
func pcreEndsAt(s string, i int) bool {
	return i == len(s) || (i == len(s)-1 && s[i] == '\n')
}

// strerror renders an OS error the way PHP's warnings do, from the C
// library's strerror: Go's errno texts are the glibc ones, lowercased.
func strerror(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		err = errno
	}

	msg := err.Error()
	r, size := utf8.DecodeRuneInString(msg)

	return string(unicode.ToUpper(r)) + msg[size:]
}

// phpWarning formats the message error_get_last() holds after a filesystem
// function failed: "func(arg): Message".
func phpWarning(fn, arg string, err error) string {
	return fn + "(" + arg + "): " + strerror(err)
}

// phpTrimChars are the characters PHP's trim() strips by default.
const phpTrimChars = " \t\n\r\x00\x0B"

// phpNumeric ports is_numeric for strings and returns the number: optional
// leading and trailing whitespace, a sign, digits with an optional fraction
// and exponent.
func phpNumeric(s string) (float64, bool) {
	s = strings.TrimLeft(s, " \t\n\r\v\f")
	s = strings.TrimRight(s, " \t\n\r\v\f")

	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}

	digits := 0
	for i < len(s) && isASCIIDigit(s[i]) {
		i++
		digits++
	}

	if i < len(s) && s[i] == '.' {
		i++

		for i < len(s) && isASCIIDigit(s[i]) {
			i++
			digits++
		}
	}

	if digits == 0 {
		return 0, false
	}

	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}

		if j < len(s) && isASCIIDigit(s[j]) {
			for j < len(s) && isASCIIDigit(s[j]) {
				j++
			}

			i = j
		}
	}

	if i != len(s) {
		return 0, false
	}

	f, err := strconv.ParseFloat(s, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, false
	}

	return f, true
}
