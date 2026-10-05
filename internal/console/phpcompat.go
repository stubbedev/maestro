// PHP runtime functions the console port relies on (strip_tags, levenshtein,
// escapeshellarg, json_encode of option defaults, float to string). They are
// local because internal/php is ported separately.

package console

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// phpFloatString converts a float like PHP's (string) cast (precision -1).
func phpFloatString(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "INF"
	case math.IsInf(f, -1):
		return "-INF"
	case math.IsNaN(f):
		return "NAN"
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		if f == 0 {
			if math.Signbit(f) {
				return "-0"
			}

			return "0"
		}

		return strconv.FormatFloat(f, 'f', -1, 64)
	}

	return phpFloatRepr(f, false)
}

// phpFloatRepr formats with the shortest round-trip digits in PHP's %.17G
// style (precision -1): exponent form below 1e-4 or from 1e15 on. In
// exponent form PHP writes "1.0E+25"; json_encode writes "1.0e+25".
func phpFloatRepr(f float64, json bool) string {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	mant, exp, hasExp := strings.Cut(s, "e")
	if !hasExp {
		// 'g' switches to exponent at exp < -4 || exp >= 21; PHP at >= 15.
		e := 0
		if a := math.Abs(f); a != 0 {
			e = int(math.Floor(math.Log10(a)))
		}
		if e < 15 {
			return s
		}
		s = strconv.FormatFloat(f, 'e', -1, 64)
		mant, exp, _ = strings.Cut(s, "e")
	}
	if !strings.Contains(mant, ".") {
		mant += ".0"
	}
	sign := exp[0]
	digits := strings.TrimLeft(exp[1:], "0")
	if digits == "" {
		digits = "0"
	}
	e := "E"
	if json {
		e = "e"
	}

	return mant + e + string(sign) + digits
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

// escapeShellArg ports escapeshellarg() on Unix.
func escapeShellArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// phpTrim is trim() with the default character list.
func phpTrim(s string) string {
	return strings.Trim(s, " \t\n\r\x00\x0B")
}

// jsonEncodeValue ports json_encode($v, JSON_UNESCAPED_SLASHES |
// JSON_UNESCAPED_UNICODE) for the value types inputs hold.
func jsonEncodeValue(v any) string {
	var b strings.Builder
	writeJSONValue(&b, v)

	return b.String()
}

func writeJSONValue(b *strings.Builder, v any) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case int:
		b.WriteString(strconv.Itoa(x))
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			b.WriteString(strconv.FormatFloat(x, 'f', 1, 64))
		} else {
			b.WriteString(phpFloatRepr(x, true))
		}
	case string:
		writeJSONString(b, x)
	case []string:
		b.WriteByte('[')
		for i, s := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, s)
		}
		b.WriteByte(']')
	case []any:
		b.WriteByte('[')
		for i, s := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONValue(b, s)
		}
		b.WriteByte(']')
	default:
		b.WriteString("null")
	}
}

func writeJSONString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"':
			b.WriteString(`\"`)
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\b':
			b.WriteString(`\b`)
		case c == '\f':
			b.WriteString(`\f`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\t':
			b.WriteString(`\t`)
		case c < 0x20:
			b.WriteString(`\u00`)
			b.WriteByte("0123456789abcdef"[c>>4])
			b.WriteByte("0123456789abcdef"[c&0xF])
		case c < utf8.RuneSelf:
			b.WriteByte(c)
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == 0x2028 || r == 0x2029 {
				// JSON_UNESCAPED_LINE_TERMINATORS is not set.
				b.WriteString(`\u` + strconv.FormatInt(int64(r), 16))
			} else {
				b.WriteString(s[i : i+size])
			}
			i += size

			continue
		}
		i++
	}
	b.WriteByte('"')
}

// strnatcasecmp ports strnatcmp_ex(..., is_case_insensitive=true).
func strnatcasecmp(a, b string) int { return strnatcmpEx(a, b, true) }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isCSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

func strnatcmpEx(a, b string, caseInsensitive bool) int {
	if a == "" || b == "" {
		switch {
		case len(a) == len(b):
			return 0
		case len(a) > len(b):
			return 1
		}

		return -1
	}

	// at mimics reading the NUL terminator past the end.
	at := func(s string, i int) byte {
		if i < len(s) {
			return s[i]
		}

		return 0
	}

	ap, bp := 0, 0
	leading := true
	for {
		ca, cb := at(a, ap), at(b, bp)

		// skip over leading zeros
		for leading && ca == '0' && ap+1 < len(a) && isDigit(a[ap+1]) {
			ap++
			ca = a[ap]
		}
		for leading && cb == '0' && bp+1 < len(b) && isDigit(b[bp+1]) {
			bp++
			cb = b[bp]
		}
		leading = false

		// Skip consecutive whitespace
		for isCSpace(ca) {
			ap++
			ca = at(a, ap)
		}
		for isCSpace(cb) {
			bp++
			cb = at(b, bp)
		}

		// process run of digits
		if isDigit(ca) && isDigit(cb) {
			var result int
			if ca == '0' || cb == '0' {
				result, ap, bp = natCompareLeft(a, ap, b, bp)
			} else {
				result, ap, bp = natCompareRight(a, ap, b, bp)
			}

			switch {
			case result != 0:
				return result
			case ap == len(a) && bp == len(b):
				// End of the strings. Let caller sort them out.
				return 0
			case ap == len(a):
				return -1
			case bp == len(b):
				return 1
			}
			// Keep on comparing from the current point.
			ca, cb = a[ap], b[bp]
		}

		if caseInsensitive {
			ca, cb = asciiUpper(ca), asciiUpper(cb)
		}

		if ca < cb {
			return -1
		} else if ca > cb {
			return 1
		}

		ap++
		bp++
		switch {
		case ap >= len(a) && bp >= len(b):
			return 0
		case ap >= len(a):
			return -1
		case bp >= len(b):
			return 1
		}
	}
}

func asciiUpper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - 32
	}

	return c
}

func natCompareRight(a string, ai int, b string, bi int) (int, int, int) {
	bias := 0
	// The longest run of digits wins. That aside, the greatest value wins,
	// but we can't know that it will until we've scanned both numbers to
	// know that they have the same magnitude, so we remember it in BIAS.
	for ; ; ai, bi = ai+1, bi+1 {
		aEnd := ai >= len(a) || !isDigit(a[ai])
		bEnd := bi >= len(b) || !isDigit(b[bi])
		switch {
		case aEnd && bEnd:
			return bias, ai, bi
		case aEnd:
			return -1, ai, bi
		case bEnd:
			return 1, ai, bi
		case a[ai] < b[bi]:
			if bias == 0 {
				bias = -1
			}
		case a[ai] > b[bi]:
			if bias == 0 {
				bias = 1
			}
		}
	}
}

func natCompareLeft(a string, ai int, b string, bi int) (int, int, int) {
	// Compare two left-aligned numbers: the first to have a different
	// value wins.
	for ; ; ai, bi = ai+1, bi+1 {
		aEnd := ai >= len(a) || !isDigit(a[ai])
		bEnd := bi >= len(b) || !isDigit(b[bi])
		switch {
		case aEnd && bEnd:
			return 0, ai, bi
		case aEnd:
			return -1, ai, bi
		case bEnd:
			return 1, ai, bi
		case a[ai] < b[bi]:
			return -1, ai, bi
		case a[ai] > b[bi]:
			return 1, ai, bi
		}
	}
}
