// PHP runtime functions the console port relies on that internal/php does
// not provide (strip_tags, levenshtein, escapeshellarg, stripcslashes,
// basename), and the bridge from the console's input value model (nil,
// bool, string, int, float64, []string, []any) to internal/php.

package console

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// phpValue converts an input value to the internal/php value model.
func phpValue(v any) any {
	switch x := v.(type) {
	case []string:
		return php.StringList(x)
	case []any:
		a := php.NewArrayCap(len(x))
		for _, e := range x {
			a.Append(phpValue(e))
		}

		return a
	}

	return v
}

// phpTruthy is PHP's (bool) cast of an input value.
func phpTruthy(v any) bool {
	switch x := v.(type) {
	case []string:
		return len(x) > 0
	case []any:
		return len(x) > 0
	}

	return php.ToBool(v)
}

// phpToString is PHP's (string) cast of an input value ("Array" for arrays,
// with PHP's "Array to string conversion" warning left out).
func phpToString(v any) string {
	switch v.(type) {
	case []string, []any:
		return "Array"
	}

	return php.ToString(v)
}

// phpLooseEqualsString is PHP 8's $v == $s for a string $s. Arrays never
// equal a string.
func phpLooseEqualsString(v any, s string) bool {
	switch v.(type) {
	case []string, []any:
		return false
	}

	return php.LooseEquals(v, s)
}

// phpIntval is PHP's (int) cast of a string.
func phpIntval(s string) int { return int(php.ToInt(s)) }

// strPadRight is str_pad($s, $length, ' '), which pads by bytes.
func strPadRight(s string, length int) string {
	if len(s) >= length {
		return s
	}

	return s + strings.Repeat(" ", length-len(s))
}

// jsonEncodeValue is json_encode($v, JSON_UNESCAPED_SLASHES |
// JSON_UNESCAPED_UNICODE) of an input value; "" where PHP returns false.
func jsonEncodeValue(v any) string {
	s, err := php.JSONEncode(phpValue(v), php.JSONUnescapedSlashes|php.JSONUnescapedUnicode)
	if err != nil {
		return ""
	}

	return s
}

// StripTags ports php_strip_tags_ex() with no allowed tags: it removes
// HTML/PHP tags, comments and NUL bytes from s.
func StripTags(s string) string {
	if strings.IndexByte(s, '<') < 0 && strings.IndexByte(s, '>') < 0 && strings.IndexByte(s, 0) < 0 {
		return s
	}

	n := len(s)
	at := func(i int) byte {
		if i < n {
			return s[i]
		}

		return 0
	}
	isSpace := func(c byte) bool {
		return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
	}

	out := make([]byte, 0, n)
	var lc, inQ byte
	depth, br := 0, 0
	isXML := false
	state := 0
	p := 0

	for p < n {
		c := s[p]
		switch state {
		case 0:
			switch c {
			case 0:
			case '<':
				if inQ != 0 {
					break
				}
				if isSpace(at(p + 1)) {
					out = append(out, c)

					break
				}
				lc = '<'
				state = 1
			case '>':
				if depth > 0 {
					depth--

					break
				}
				if inQ != 0 {
					break
				}
				out = append(out, c)
			default:
				out = append(out, c)
			}
			p++
		case 1:
			switch c {
			case 0:
			case '<':
				if inQ != 0 {
					break
				}
				if isSpace(at(p + 1)) {
					break
				}
				depth++
			case '>':
				if depth > 0 {
					depth--

					break
				}
				if inQ != 0 {
					break
				}
				lc = '>'
				if isXML && p >= 1 && s[p-1] == '-' {
					break
				}
				inQ, isXML = 0, false
				state = 0
			case '"', '\'':
				if p != 0 && (inQ == 0 || c == inQ) {
					if inQ != 0 {
						inQ = 0
					} else {
						inQ = c
					}
				}
			case '!':
				// JavaScript & Other HTML scripting languages
				if p >= 1 && s[p-1] == '<' {
					state = 3
					lc = c
				}
			case '?':
				if p >= 1 && s[p-1] == '<' {
					br = 0
					state = 2
				}
			}
			p++
		case 2: // PHP
			switch c {
			case '(':
				if lc != '"' && lc != '\'' {
					lc = '('
					br++
				}
			case ')':
				if lc != '"' && lc != '\'' {
					lc = ')'
					br--
				}
			case '>':
				if depth > 0 {
					depth--

					break
				}
				if inQ != 0 {
					break
				}
				if br == 0 && p >= 1 && lc != '"' && s[p-1] == '?' {
					inQ = 0
					state = 0
				}
			case '"', '\'':
				if p >= 1 && s[p-1] != '\\' {
					if lc == c {
						lc = 0
					} else if lc != '\\' {
						lc = c
					}
				}
				if p != 0 && (inQ == 0 || c == inQ) {
					if inQ != 0 {
						inQ = 0
					} else {
						inQ = c
					}
				}
			case 'l', 'L':
				// swm: If we encounter '<?xml' then we shouldn't be in
				// state == 2 (PHP). Switch back to HTML.
				if p > 4 && (s[p-1] == 'm' || s[p-1] == 'M') && (s[p-2] == 'x' || s[p-2] == 'X') &&
					s[p-3] == '?' && s[p-4] == '<' {
					state = 1
					isXML = true
				}
			}
			p++
		case 3: // JavaScript/CSS/etc...
			switch c {
			case '>':
				if depth > 0 {
					depth--

					break
				}
				if inQ != 0 {
					break
				}
				inQ = 0
				state = 0
			case '"', '\'':
				if p != 0 && s[p-1] != '\\' && (inQ == 0 || c == inQ) {
					if inQ != 0 {
						inQ = 0
					} else {
						inQ = c
					}
				}
			case '-':
				if p >= 2 && s[p-1] == '-' && s[p-2] == '!' {
					state = 4
				}
			case 'E', 'e':
				// !DOCTYPE exception
				if p > 6 && (s[p-1]|0x20) == 'p' && (s[p-2]|0x20) == 'y' && (s[p-3]|0x20) == 't' &&
					(s[p-4]|0x20) == 'c' && (s[p-5]|0x20) == 'o' && (s[p-6]|0x20) == 'd' {
					state = 1
				}
			}
			p++
		case 4: // inside <!-- comment -->
			if c == '>' && inQ == 0 && p >= 2 && s[p-1] == '-' && s[p-2] == '-' {
				inQ = 0
				state = 0
			}
			p++
		}
	}

	return string(out)
}

// levenshtein ports PHP's levenshtein() with unit costs (byte based).
func levenshtein(a, b string) int {
	if a == "" {
		return len(b)
	}
	if b == "" {
		return len(a)
	}
	p1 := make([]int, len(b)+1)
	p2 := make([]int, len(b)+1)
	for i := range p1 {
		p1[i] = i
	}
	for i := range len(a) {
		p2[0] = p1[0] + 1
		for j := range len(b) {
			c0 := p1[j]
			if a[i] != b[j] {
				c0++
			}
			c1 := p1[j+1] + 1
			if c1 < c0 {
				c0 = c1
			}
			c2 := p2[j] + 1
			if c2 < c0 {
				c0 = c2
			}
			p2[j+1] = c0
		}
		p1, p2 = p2, p1
	}

	return p1[len(b)]
}

// escapeShellArg ports escapeshellarg() on Unix. Like PHP it rejects NUL
// bytes, by panicking with the ValueError.
func escapeShellArg(s string) string {
	if strings.IndexByte(s, 0) >= 0 {
		panic(newError(KindValueError, "Input.php", 195, "escapeshellarg(): Argument #1 ($arg) must not contain any null bytes"))
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// phpBasename ports basename() for "/"-separated paths.
func phpBasename(path string) string {
	path = strings.TrimRight(path, "/")
	if path == "" {
		return ""
	}

	return filepath.Base(path)
}

// stripCSlashes ports PHP's stripcslashes().
func stripCSlashes(s string) string {
	if strings.IndexByte(s, '\\') < 0 {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			out = append(out, s[i])

			continue
		}
		i++
		switch c := s[i]; c {
		case 'n':
			out = append(out, '\n')
		case 't':
			out = append(out, '\t')
		case 'r':
			out = append(out, '\r')
		case 'a':
			out = append(out, '\a')
		case 'v':
			out = append(out, '\v')
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'x':
			if i+1 < len(s) && isHexDigit(s[i+1]) {
				v := hexVal(s[i+1])
				i++
				if i+1 < len(s) && isHexDigit(s[i+1]) {
					v = v*16 + hexVal(s[i+1])
					i++
				}
				out = append(out, byte(v)) //nolint:gosec // at most two hex digits
			} else {
				out = append(out, 'x')
			}
		default:
			if c >= '0' && c <= '7' {
				v := int(c - '0')
				for k := 0; k < 2 && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '7'; k++ {
					i++
					v = v*8 + int(s[i]-'0')
				}
				out = append(out, byte(v)) // wraps like C's (char) cast
			} else {
				out = append(out, c)
			}
		}
	}

	return string(out)
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c|0x20 >= 'a' && c|0x20 <= 'f')
}

func hexVal(c byte) int {
	if c <= '9' {
		return int(c - '0')
	}

	return int(c|0x20-'a') + 10
}

func isCSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

// typeString approximates get_debug_type() for a Go value: its type name
// without the pointer star.
func typeString(v any) string { return strings.TrimPrefix(fmt.Sprintf("%T", v), "*") }
