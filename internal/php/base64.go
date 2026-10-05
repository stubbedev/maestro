// Ports base64_decode() (php_base64_decode_ex from ext/standard/base64.c).

package php

// Base64Decode ports base64_decode($string, $strict): ok is false where
// PHP returns false (only in strict mode). Without strict, characters
// outside the alphabet are skipped; with it, only whitespace is, and
// misplaced padding or a truncated last group fail.
func Base64Decode(s string, strict bool) (string, bool) {
	out := make([]byte, 0, len(s)*3/4+1)

	var cur byte

	i, padding := 0, 0

	for k := range len(s) {
		c := s[k]
		if c == '=' {
			padding++

			continue
		}

		b, kind := base64Value(c)

		switch {
		case kind == base64Char && strict && padding > 0:
			// fail if any data follows padding
			return "", false
		case kind == base64Space && strict, kind != base64Char && !strict:
			// skip whitespace (strict) or unknown characters
			continue
		case kind != base64Char:
			return "", false
		}

		switch i % 4 {
		case 0:
			cur = b << 2
		case 1:
			out = append(out, cur|b>>4)
			cur = (b & 0x0f) << 4
		case 2:
			out = append(out, cur|b>>2)
			cur = (b & 0x03) << 6
		case 3:
			out = append(out, cur|b)
		}

		i++
	}

	// fail if the input is truncated (only one char in last group)
	if strict && i%4 == 1 {
		return "", false
	}

	// fail if the padding length is wrong (not VV==, VVV=), but accept zero padding
	if strict && padding > 0 && (padding > 2 || (i+padding)%4 != 0) {
		return "", false
	}

	return string(out), true
}

// The kinds of characters of base64_reverse_table.
const (
	base64Char  = iota // an alphabet character
	base64Space        // whitespace (-1 in the table)
	base64Other        // anything else (-2)
)

// base64Value is base64_reverse_table: the value of c and its kind.
func base64Value(c byte) (byte, int) {
	switch {
	case c >= 'A' && c <= 'Z':
		return c - 'A', base64Char
	case c >= 'a' && c <= 'z':
		return c - 'a' + 26, base64Char
	case c >= '0' && c <= '9':
		return c - '0' + 52, base64Char
	case c == '+':
		return 62, base64Char
	case c == '/':
		return 63, base64Char
	case c == '\t', c == '\n', c == '\r', c == ' ':
		return 0, base64Space
	}

	return 0, base64Other
}
