// PHP functions the HTTP layer needs that internal/php does not provide
// yet: rawurlencode, rawurldecode, http_build_query and date.

package http

import (
	"strings"
	"time"
)

const upperHex = "0123456789ABCDEF"

// rawurlencode is PHP's rawurlencode (RFC 3986: all but A-Za-z0-9-_.~
// is percent-encoded).
func rawurlencode(s string) string {
	n := 0
	for i := range len(s) {
		if !isUnreserved(s[i]) {
			n++
		}
	}

	if n == 0 {
		return s
	}

	b := make([]byte, 0, len(s)+2*n)
	for i := range len(s) {
		c := s[i]
		if isUnreserved(c) {
			b = append(b, c)
		} else {
			b = append(b, '%', upperHex[c>>4], upperHex[c&15])
		}
	}

	return string(b)
}

func isUnreserved(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~'
}

// urlencode is PHP's urlencode (application/x-www-form-urlencoded: space
// is +, ~ is encoded).
func urlencode(s string) string {
	var b strings.Builder

	b.Grow(len(s))

	for i := range len(s) {
		c := s[i]

		switch {
		case c == ' ':
			b.WriteByte('+')
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(upperHex[c>>4])
			b.WriteByte(upperHex[c&15])
		}
	}

	return b.String()
}

// rawurldecode is PHP's rawurldecode: %XX sequences are decoded, invalid
// ones kept as is, + stays +.
func rawurldecode(s string) string {
	i := strings.IndexByte(s, '%')
	if i < 0 {
		return s
	}

	b := make([]byte, 0, len(s))
	b = append(b, s[:i]...)

	for ; i < len(s); i++ {
		c := s[i]
		if c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			b = append(b, unhex(s[i+1])<<4|unhex(s[i+2]))
			i += 2

			continue
		}

		b = append(b, c)
	}

	return string(b)
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}

	return c - 'A' + 10
}

// httpBuildQuery is http_build_query($pairs, ”, '&') for string values.
func httpBuildQuery(pairs ...string) string {
	var b strings.Builder

	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			b.WriteByte('&')
		}

		b.WriteString(urlencode(pairs[i]))
		b.WriteByte('=')
		b.WriteString(urlencode(pairs[i+1]))
	}

	return b.String()
}

// phpDate formats a Unix timestamp like date() with the default UTC time
// zone (Composer runs with PHP's default unless php.ini sets
// date.timezone). Only the Y, m, d, H, i and s characters are supported.
func phpDate(format string, ts int64) string {
	t := time.Unix(ts, 0).UTC()

	var b strings.Builder

	for i := range len(format) {
		switch format[i] {
		case 'Y':
			b.WriteString(t.Format("2006"))
		case 'm':
			b.WriteString(t.Format("01"))
		case 'd':
			b.WriteString(t.Format("02"))
		case 'H':
			b.WriteString(t.Format("15"))
		case 'i':
			b.WriteString(t.Format("04"))
		case 's':
			b.WriteString(t.Format("05"))
		default:
			b.WriteByte(format[i])
		}
	}

	return b.String()
}
