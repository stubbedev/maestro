// The byte-level PHP/PCRE semantics semver's hand-written matchers rely on,
// from php-src 8.4 and PCRE2: ctype classes in the C locale, PCRE2's
// default \s and $ (non-UTF mode, LF newlines), ASCII-only /i, PHP's
// string truthiness, and $numericString + $int via internal/php.

package semver

import (
	"strconv"

	"github.com/stubbedev/maestro/internal/php"
)

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// isAlnum is isalnum() in the C locale.
func isAlnum(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isSpace is PCRE2's \s without UTF/UCP: space, \t, \n, \v, \f and \r.
func isSpace(c byte) bool { return c == ' ' || (c >= '\t' && c <= '\r') }

func toLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}

	return c
}

// hasPrefixLit reports whether s starts with the pattern literal lit, ASCII
// case-insensitively when ci is set (PCRE's /i without UTF).
func hasPrefixLit(s, lit string, ci bool) bool {
	if len(s) < len(lit) {
		return false
	}
	if !ci {
		return s[:len(lit)] == lit
	}
	for i := range len(lit) {
		if toLower(s[i]) != toLower(lit[i]) {
			return false
		}
	}

	return true
}

// atDollar reports whether PCRE's $ (no /m, no /D) matches at offset i: at
// the end of s or before a newline that ends it.
func atDollar(s string, i int) bool {
	return i == len(s) || (i == len(s)-1 && s[i] == '\n')
}

// dollarEnd returns where a match anchored with $ must end when nothing in
// the pattern can consume a newline: before a final "\n", or at the end.
func dollarEnd(s string) int {
	if s != "" && s[len(s)-1] == '\n' {
		return len(s) - 1
	}

	return len(s)
}

// phpTruthy reports whether a string is truthy in PHP: not "" and not "0".
func phpTruthy(s string) bool { return s != "" && s != "0" }

// phpAddInt ports `$numericString + $increment` for the numeric strings
// manipulateVersionString() works on, returning the result as PHP would
// convert it back to a string, and whether it is negative. Integer overflow
// turns the result into a float, as in PHP.
func phpAddInt(s string, increment int) (string, bool) {
	// Fast path, without boxing: an integer string in range whose sum does
	// not overflow is an int addition in PHP too.
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if sum := n + int64(increment); (sum > n) == (increment > 0) {
			return strconv.FormatInt(sum, 10), sum < 0
		}
	}

	sum, err := php.Add(s, increment)
	if err != nil {
		// Unreachable: the version parts are digit strings.
		panic(err)
	}

	return php.ToString(sum), php.ToFloat(sum) < 0
}
