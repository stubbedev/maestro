// PCRE semantics on top of Go's regexp for the handful of patterns the
// Util ports match, which are all within RE2's syntax: PHP's non-UTF
// caseless matching only folds ASCII letters (Go's (?i) also folds K and ſ
// into k and s), and PCRE's $ without the D modifier also matches before a
// final newline.

package util

import (
	"regexp"
	"strings"
)

// pcre is a compiled PHP regex.
type pcre struct {
	re *regexp.Regexp
	// dollar is the index of the group (\n)? that replaced a trailing $,
	// or 0.
	dollar int
}

// mustPCRE compiles a PHP regex body (without delimiters). caseless is the i
// modifier.
func mustPCRE(pattern string, caseless bool) *pcre {
	if caseless {
		pattern = foldASCII(pattern)
	}

	dollar := strings.HasSuffix(pattern, "$") && !strings.HasSuffix(pattern, `\$`)
	if dollar {
		pattern = pattern[:len(pattern)-1] + `(\n)?\z`
	}

	re := regexp.MustCompile(pattern)
	p := &pcre{re: re}

	if dollar {
		p.dollar = re.NumSubexp()
	}

	return p
}

// foldASCII makes every ASCII letter outside escapes and character classes
// match both cases. Classes holding letters are not supported.
func foldASCII(pattern string) string {
	var b strings.Builder

	for i := 0; i < len(pattern); i++ {
		c := pattern[i]

		switch {
		case c == '\\' && i+1 < len(pattern):
			b.WriteString(pattern[i : i+2])
			i++
		case c == '[':
			end := strings.IndexByte(pattern[i+1:], ']')
			class := pattern[i : i+2+end]

			for j := range len(class) {
				if isASCIIAlpha(class[j]) {
					panic("foldASCII: letters in a character class: " + pattern)
				}
			}

			b.WriteString(class)
			i += 1 + end
		case c == '(' && strings.HasPrefix(pattern[i:], "(?:"):
			b.WriteString("(?:")
			i += 2
		case isASCIIAlpha(c):
			b.WriteString("[" + strings.ToLower(string(c)) + strings.ToUpper(string(c)) + "]")
		default:
			b.WriteByte(c)
		}
	}

	return b.String()
}

// findAt returns the submatch indexes of the first match in s at or after
// pos, with the trailing-$ group stripped and the match end before it.
func (p *pcre) findAt(s string, pos int) []int {
	loc := p.re.FindStringSubmatchIndex(s[pos:])
	if loc == nil {
		return nil
	}

	for i := range loc {
		if loc[i] >= 0 {
			loc[i] += pos
		}
	}

	if p.dollar > 0 {
		if loc[2*p.dollar] >= 0 {
			loc[1] = loc[2*p.dollar]
		}

		loc = loc[:2*p.dollar]
	}

	return loc
}

// MatchString is Preg::isMatch.
func (p *pcre) MatchString(s string) bool {
	return p.findAt(s, 0) != nil
}

// FindStringSubmatch is Preg::isMatch with $matches; unmatched groups are
// empty.
func (p *pcre) FindStringSubmatch(s string) []string {
	loc := p.findAt(s, 0)
	if loc == nil {
		return nil
	}

	m := make([]string, len(loc)/2)
	for i := range m {
		if loc[2*i] >= 0 {
			m[i] = s[loc[2*i]:loc[2*i+1]]
		}
	}

	return m
}

// ReplaceFunc is preg_replace_callback over all matches: each match is
// replaced by f of its groups (unmatched groups are empty). After an empty
// match the search resumes one byte further; none of the patterns used here
// can match empty.
func (p *pcre) ReplaceFunc(s string, f func(m []string) string) string {
	var b strings.Builder

	last := 0
	pos := 0

	for pos <= len(s) {
		loc := p.findAt(s, pos)
		if loc == nil {
			break
		}

		m := make([]string, len(loc)/2)
		for i := range m {
			if loc[2*i] >= 0 {
				m[i] = s[loc[2*i]:loc[2*i+1]]
			}
		}

		b.WriteString(s[last:loc[0]])
		b.WriteString(f(m))
		last = loc[1]

		if loc[1] == loc[0] {
			if loc[1] < len(s) {
				b.WriteByte(s[loc[1]])
			}

			last = loc[1] + 1
			pos = loc[1] + 1

			continue
		}

		pos = loc[1]
	}

	if last < len(s) {
		b.WriteString(s[last:])
	}

	return b.String()
}

// Replace is preg_replace with a PHP replacement string.
func (p *pcre) Replace(s, replacement string) string {
	return p.ReplaceFunc(s, func(m []string) string { return expandReplacement(replacement, m) })
}

// expandReplacement ports preg_replace's replacement expansion: \n, $n and
// ${n} (n up to 99) insert group n, empty when it does not exist; a
// backslash before \ or $ makes it literal.
func expandReplacement(replacement string, m []string) string {
	b := make([]byte, 0, len(replacement))

	var walkLast byte

	for i := 0; i < len(replacement); {
		c := replacement[i]
		if c == '\\' || c == '$' {
			if walkLast == '\\' {
				// The escaping backslash becomes this character.
				b[len(b)-1] = c
				i++
				walkLast = 0

				continue
			}

			if n, width, ok := parseBackref(replacement[i:]); ok {
				if n < len(m) {
					b = append(b, m[n]...)
				}

				i += width

				continue
			}
		}

		b = append(b, c)
		walkLast = c
		i++
	}

	return string(b)
}

// parseBackref ports preg_get_backref.
func parseBackref(s string) (n, width int, ok bool) {
	i := 1
	braces := s[0] == '$' && len(s) > 1 && s[1] == '{'

	if braces {
		i++
	}

	if i >= len(s) || !isASCIIDigit(s[i]) {
		return 0, 0, false
	}

	n = int(s[i] - '0')
	i++

	if i < len(s) && isASCIIDigit(s[i]) {
		n = n*10 + int(s[i]-'0')
		i++
	}

	if braces {
		if i >= len(s) || s[i] != '}' {
			return 0, 0, false
		}

		i++
	}

	return n, i, true
}
