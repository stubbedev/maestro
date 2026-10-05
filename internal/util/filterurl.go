// Ports filter_var($url, FILTER_VALIDATE_URL) from PHP's
// ext/filter/logical_filters.c (php_filter_validate_url, with the
// FILTER_SANITIZE_URL character check it starts with), which
// Config::prohibitUrlByConfig uses (internal/config).

package util

import (
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// FilterValidateURL reports whether filter_var($url, FILTER_VALIDATE_URL)
// accepts url.
func FilterValidateURL(url string) bool {
	// php_filter_url removes every character outside its URL set; a
	// changed length fails validation.
	for i := range len(url) {
		if !isURLChar(url[i]) {
			return false
		}
	}

	u, ok := ParseURL(url)
	if !ok {
		return false
	}

	if u.HasScheme && (php.Strcasecmp(u.Scheme, "http") == 0 || php.Strcasecmp(u.Scheme, "https") == 0) {
		if !u.HasHost {
			return false
		}
		host := u.Host
		// An IPv6 enclosed by square brackets is a valid hostname
		ipv6 := len(host) >= 2 && host[0] == '[' && host[len(host)-1] == ']' && validateIPv6(host[1:len(host)-1])
		if !ipv6 && !validateHostname(host) {
			return false
		}
	}

	if !u.HasScheme || (!u.HasHost && u.Scheme != "mailto" && u.Scheme != "news" && u.Scheme != "file") {
		return false
	}

	return (!u.HasUser || isUserinfoValid(u.User)) && (!u.HasPass || isUserinfoValid(u.Pass))
}

// isURLChar is the character set of FILTER_SANITIZE_URL: letters, digits
// and $-_.+!*'(),{}|\^~[]`<>#%";/?:@&=.
func isURLChar(c byte) bool {
	return isASCIIAlnum(c) || strings.IndexByte("$-_.+!*'(),{}|\\^~[]`<>#%\";/?:@&=", c) >= 0
}

// isUserinfoValid ports is_userinfo_valid: unreserved and sub-delim
// characters, ':' and %XX escapes.
func isUserinfoValid(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case isASCIIAlnum(c) || strings.IndexByte("-._~!$&'()*+,;=:", c) >= 0:
		case c == '%' && i+2 < len(s) && isHexDigit(s[i+1]) && isHexDigit(s[i+2]):
			i += 2
		default:
			return false
		}
	}

	return true
}

// validateHostname ports _php_filter_validate_domain with
// FILTER_FLAG_HOSTNAME.
func validateHostname(s string) bool {
	l := len(s)
	// Ignore trailing dot
	if l > 0 && s[l-1] == '.' {
		l--
	}
	// The total length cannot exceed 253 characters (final dot not included)
	if l > 253 {
		return false
	}
	// First char must be alphanumeric (PHP reads the terminating NUL of an
	// empty host, which is not alphanumeric either)
	if len(s) == 0 || s[0] == '.' || !isASCIIAlnum(s[0]) {
		return false
	}

	at := func(i int) byte { // s[i], with PHP's NUL terminator past the end
		if i < len(s) {
			return s[i]
		}

		return 0
	}

	label := 1
	for i := range l {
		if s[i] == '.' {
			// The first and the last character of a label must be alphanumeric
			if at(i+1) == '.' || !isASCIIAlnum(at(i-1)) || !isASCIIAlnum(at(i+1)) {
				return false
			}
			// Reset label length counter
			label = 1
		} else {
			if label > 63 || (s[i] != '-' && !isASCIIAlnum(s[i])) {
				return false
			}
			label++
		}
	}

	return true
}
