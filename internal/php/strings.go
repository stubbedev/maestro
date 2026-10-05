// Ports string functions of ext/standard/string.c that have no exact Go
// equivalent: case mapping is ASCII-only since PHP 8.2, trim takes
// character ranges, strtr with pairs prefers the longest match, and
// substr/str_pad/wordwrap have PHP's edge cases.

package php

import (
	"sort"
	"strings"
)

// TrimChars is the default character list of trim(): " \n\r\t\v\0".
const TrimChars = " \n\r\t\v\x00"

// Strtolower ports strtolower(): ASCII letters only.
func Strtolower(s string) string { return mapASCII(s, lowerASCII, 'A', 'Z') }

// Strtoupper ports strtoupper(): ASCII letters only.
func Strtoupper(s string) string { return mapASCII(s, upperASCII, 'a', 'z') }

// mapASCII applies f to every byte, allocating only when a byte in
// [lo, hi] is present.
func mapASCII(s string, f func(byte) byte, lo, hi byte) string {
	for i := range len(s) {
		if c := s[i]; c >= lo && c <= hi {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				b[j] = f(b[j])
			}
			return string(b)
		}
	}
	return s
}

// Ucfirst ports ucfirst().
func Ucfirst(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return string(s[0]-('a'-'A')) + s[1:]
}

// Lcfirst ports lcfirst().
func Lcfirst(s string) string {
	if s == "" || s[0] < 'A' || s[0] > 'Z' {
		return s
	}
	return string(s[0]+('a'-'A')) + s[1:]
}

// UcwordsDelimiters is the default $delimiters of ucwords().
const UcwordsDelimiters = " \t\r\n\f\v"

// Ucwords ports ucwords($s, $delimiters): the first character and every
// character after one of delimiters is upper-cased (ASCII only).
func Ucwords(s, delimiters string) string {
	if s == "" {
		return s
	}
	mask := charMask(delimiters)
	b := []byte(s)
	b[0] = upperASCII(b[0])
	for i := range len(b) - 1 {
		if mask[b[i]] {
			b[i+1] = upperASCII(b[i+1])
		}
	}
	return string(b)
}

// Trim ports trim($s) with the default character list.
func Trim(s string) string { return trimMask(s, &defaultTrimMask, 3) }

// Ltrim ports ltrim($s).
func Ltrim(s string) string { return trimMask(s, &defaultTrimMask, 1) }

// Rtrim ports rtrim($s).
func Rtrim(s string) string { return trimMask(s, &defaultTrimMask, 2) }

// TrimSet ports trim($s, $characters); characters may contain ranges
// like "a..z".
func TrimSet(s, characters string) string {
	m := charMask(characters)
	return trimMask(s, &m, 3)
}

// LtrimSet ports ltrim($s, $characters).
func LtrimSet(s, characters string) string {
	m := charMask(characters)
	return trimMask(s, &m, 1)
}

// RtrimSet ports rtrim($s, $characters).
func RtrimSet(s, characters string) string {
	m := charMask(characters)
	return trimMask(s, &m, 2)
}

var defaultTrimMask = charMask(TrimChars)

// trimMask ports php_trim_int; mode 1 trims left, 2 right, 3 both.
func trimMask(s string, mask *[256]bool, mode int) string {
	start, end := 0, len(s)
	if mode&1 != 0 {
		for start < end && mask[s[start]] {
			start++
		}
	}
	if mode&2 != 0 {
		for end > start && mask[s[end-1]] {
			end--
		}
	}
	return s[start:end]
}

// charMask ports php_charmask: "a..z" is a range; a malformed ".." is
// skipped (PHP warns).
func charMask(chars string) [256]bool {
	var mask [256]bool
	for i := 0; i < len(chars); i++ {
		c := chars[i]
		switch {
		case i+3 < len(chars) && chars[i+1] == '.' && chars[i+2] == '.' && chars[i+3] >= c:
			for x := int(c); x <= int(chars[i+3]); x++ {
				mask[x] = true
			}
			i += 3
		case i+1 < len(chars) && c == '.' && chars[i+1] == '.':
			// Invalid range: PHP warns and ignores this '.'.
		default:
			mask[c] = true
		}
	}
	return mask
}

// Substr ports substr($s, $offset) (to the end of the string).
func Substr(s string, offset int) string { return substr(s, offset, 0, true) }

// SubstrLen ports substr($s, $offset, $length).
func SubstrLen(s string, offset, length int) string { return substr(s, offset, length, false) }

func substr(s string, f, l int, lenNull bool) string {
	n := len(s)
	if f > n {
		return ""
	}
	if f < 0 {
		if -f > n {
			f = 0
		} else {
			f += n
		}
	}
	switch {
	case lenNull:
		l = n - f
	case l < 0:
		if -l > n-f {
			l = 0
		} else {
			l = n - f + l
		}
	case l > n-f:
		l = n - f
	}
	return s[f : f+l]
}

// Pad types of str_pad.
const (
	StrPadLeft  = 0
	StrPadRight = 1
	StrPadBoth  = 2
)

// StrPad ports str_pad($s, $length, $pad, $type). It panics with PHP's
// ValueError message when pad is empty.
func StrPad(s string, length int, pad string, typ int) string {
	if length < 0 || length <= len(s) {
		return s
	}
	if pad == "" {
		panic("str_pad(): Argument #3 ($pad_string) must not be empty")
	}
	num := length - len(s)
	left, right := 0, 0
	switch typ {
	case StrPadLeft:
		left = num
	case StrPadBoth:
		left = num / 2
		right = num - left
	default:
		right = num
	}
	var b strings.Builder
	b.Grow(length)
	for i := range left {
		b.WriteByte(pad[i%len(pad)])
	}
	b.WriteString(s)
	for i := range right {
		b.WriteByte(pad[i%len(pad)])
	}
	return b.String()
}

// Strcmp ports strcmp() (-1, 0 or 1).
func Strcmp(a, b string) int { return binaryStrcmp(a, b) }

// Strcasecmp ports strcasecmp(): ASCII case-insensitive.
func Strcasecmp(a, b string) int { return strcasecmpASCII(a, b) }

// Strncasecmp ports strncasecmp(): strcasecmp() of at most the first n
// bytes of a and b (n >= 0).
func Strncasecmp(a, b string, n int) int {
	return strcasecmpASCII(a[:min(n, len(a))], b[:min(n, len(b))])
}

// Stripos ports stripos($haystack, $needle): the byte offset of the first
// ASCII case-insensitive occurrence of needle, or -1 for false.
func Stripos(haystack, needle string) int {
	n := len(needle)
	for i := 0; i+n <= len(haystack); i++ {
		if strcasecmpASCII(haystack[i:i+n], needle) == 0 {
			return i
		}
	}
	return -1
}

// Strtr ports strtr($s, $from, $to): byte-wise translation of the first
// min(len(from), len(to)) bytes.
func Strtr(s, from, to string) string {
	n := min(len(from), len(to))
	if n == 0 {
		return s
	}
	var table [256]byte
	var set [256]bool
	for i := range n {
		table[from[i]] = to[i]
		set[from[i]] = true
	}
	var b []byte
	for i := range len(s) {
		if set[s[i]] {
			if b == nil {
				b = []byte(s)
			}
			b[i] = table[s[i]]
		}
	}
	if b == nil {
		return s
	}
	return string(b)
}

// StrtrPairs ports strtr($s, $pairs): at each position the longest
// matching key is replaced and the scan continues after it; replaced text
// is not searched again. Empty keys are ignored.
func StrtrPairs(s string, pairs map[string]string) string {
	keys := make([]string, 0, len(pairs))
	minLen, maxLen := len(s)+1, 0
	for k := range pairs {
		if k == "" {
			continue
		}
		keys = append(keys, k)
		minLen = min(minLen, len(k))
		maxLen = max(maxLen, len(k))
	}
	if len(keys) == 0 || minLen > len(s) {
		return s
	}
	// Longest keys first, so the first hit at a position is the longest.
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	var b strings.Builder
	last := 0
	for i := 0; i+minLen <= len(s); {
		found := false
		for _, k := range keys {
			if strings.HasPrefix(s[i:], k) {
				b.WriteString(s[last:i])
				b.WriteString(pairs[k])
				i += len(k)
				last = i
				found = true
				break
			}
		}
		if !found {
			i++
		}
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

// Wordwrap ports wordwrap($s, $width, $break, $cut). It panics with PHP's
// ValueError messages for an empty break or cut with width 0.
func Wordwrap(text string, width int, brk string, cut bool) string {
	if text == "" {
		return ""
	}
	if brk == "" {
		panic("wordwrap(): Argument #3 ($break) must not be empty")
	}
	if width == 0 && cut {
		panic("wordwrap(): Argument #4 ($cut_long_words) cannot be true when argument #2 ($width) is 0")
	}
	n := len(text)
	if len(brk) == 1 && !cut {
		b := []byte(text)
		laststart, lastspace := 0, 0
		for current := range n {
			switch {
			case text[current] == brk[0]:
				laststart, lastspace = current+1, current+1
			case text[current] == ' ':
				if current-laststart >= width {
					b[current] = brk[0]
					laststart = current + 1
				}
				lastspace = current
			case current-laststart >= width && laststart != lastspace:
				b[lastspace] = brk[0]
				laststart = lastspace + 1
			}
		}
		return string(b)
	}

	out := make([]byte, 0, n+n/max(width, 1)*len(brk)+len(brk))
	laststart, lastspace := 0, 0
	current := 0
	for ; current < n; current++ {
		switch {
		case text[current] == brk[0] && current+len(brk) < n && strings.HasPrefix(text[current:], brk):
			out = append(out, text[laststart:current+len(brk)]...)
			current += len(brk) - 1
			laststart, lastspace = current+1, current+1
		case text[current] == ' ':
			if current-laststart >= width {
				out = append(out, text[laststart:current]...)
				out = append(out, brk...)
				laststart = current + 1
			}
			lastspace = current
		case current-laststart >= width && cut && laststart >= lastspace:
			out = append(out, text[laststart:current]...)
			out = append(out, brk...)
			laststart, lastspace = current, current
		case current-laststart >= width && laststart < lastspace:
			out = append(out, text[laststart:lastspace]...)
			out = append(out, brk...)
			laststart = lastspace + 1
			lastspace = laststart
		}
	}
	if laststart != current {
		out = append(out, text[laststart:current]...)
	}
	return string(out)
}
