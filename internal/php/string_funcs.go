// Ports string functions of ext/standard: strip_tags() (php_strip_tags_ex
// in string.c), levenshtein() (levenshtein.c), stripcslashes()
// (php_stripcslashes in string.c), basename() (php_basename in string.c,
// Unix build) and escapeshellarg() (php_escape_shell_arg in exec.c, Unix
// build).

package php

import (
	"runtime"
	"strings"
)

// StripTags ports strip_tags($s) with no allowed tags: it removes HTML/PHP
// tags, comments and NUL bytes.
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
				if isCSpace(at(p + 1)) {
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
				if isCSpace(at(p + 1)) {
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

// Levenshtein ports levenshtein($a, $b) with unit costs (byte based).
func Levenshtein(a, b string) int {
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

// Stripcslashes ports stripcslashes(): C-style escapes (\n, \t, \r, \a,
// \v, \b, \f, \xHH, octal \OOO) are decoded, any other escaped character
// stands for itself.
func Stripcslashes(s string) string {
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
			if v, ok := hexDigitAt(s, i+1); ok {
				i++
				if w, ok := hexDigitAt(s, i+1); ok {
					v = v<<4 | w
					i++
				}
				out = append(out, v)
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

// hexDigitAt returns the value of the hex digit s[i], if it is one.
func hexDigitAt(s string, i int) (byte, bool) {
	if i >= len(s) {
		return 0, false
	}
	switch c := s[i]; {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c|0x20 >= 'a' && c|0x20 <= 'f':
		return c | 0x20 - 'a' + 10, true
	}
	return 0, false
}

// Basename ports basename($path, $suffix): the last component without
// trailing separators, minus suffix when the component ends with it and
// is longer than it. The separator is "/", and on Windows "\" too.
func Basename(path, suffix string) string {
	isSep := func(c byte) bool { return c == '/' || c == '\\' && runtime.GOOS == "windows" }
	end := len(path)
	for end > 0 && isSep(path[end-1]) {
		end--
	}
	if end == 0 {
		return ""
	}
	start := end
	for start > 0 && !isSep(path[start-1]) {
		start--
	}
	if len(suffix) < end-start && path[end-len(suffix):end] == suffix {
		end -= len(suffix)
	}
	return path[start:end]
}

// Escapeshellarg ports escapeshellarg() on Unix: arg in single quotes,
// with each ' written as '\”. Bytes that mblen() rejects in PHP 8's
// default LC_CTYPE (C.UTF-8) are dropped, as PHP does. Like PHP it rejects
// NUL bytes, with a *EngineError carrying the ValueError.
func Escapeshellarg(arg string) (string, error) {
	if strings.IndexByte(arg, 0) >= 0 {
		return "", valueError("escapeshellarg(): Argument #1 ($arg) must not contain any null bytes")
	}
	b := make([]byte, 0, len(arg)+2)
	b = append(b, '\'')
	for x := 0; x < len(arg); x++ {
		c := arg[x]
		if c < 0x80 {
			if c == '\'' {
				b = append(b, '\'', '\\', '\'', '\'')
			} else {
				b = append(b, c)
			}
			continue
		}
		n := mblenUTF8(arg[x:])
		if n < 0 {
			continue // skip non-valid multibyte characters
		}
		b = append(b, arg[x:x+n]...)
		x += n - 1
	}
	b = append(b, '\'')
	return string(b), nil
}

// mblenUTF8 is glibc's mblen() in a UTF-8 locale for s starting with a
// byte >= 0x80: the length of the sequence (up to 6 bytes, values up to
// 0x7FFFFFFF), or -1 when it is invalid, overlong, a surrogate or
// truncated.
func mblenUTF8(s string) int {
	c := s[0]
	var n int
	var v, minV uint32
	switch {
	case c < 0xC2:
		return -1
	case c < 0xE0:
		n, v, minV = 2, uint32(c&0x1F), 0x80
	case c < 0xF0:
		n, v, minV = 3, uint32(c&0x0F), 0x800
	case c < 0xF8:
		n, v, minV = 4, uint32(c&0x07), 0x10000
	case c < 0xFC:
		n, v, minV = 5, uint32(c&0x03), 0x200000
	case c < 0xFE:
		n, v, minV = 6, uint32(c&0x01), 0x4000000
	default:
		return -1
	}
	if len(s) < n {
		return -1
	}
	for i := 1; i < n; i++ {
		if s[i]&0xC0 != 0x80 {
			return -1
		}
		v = v<<6 | uint32(s[i]&0x3F)
	}
	if v < minV || v >= 0xD800 && v <= 0xDFFF {
		return -1
	}
	return n
}
