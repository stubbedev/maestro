// Ports filter_var($url, FILTER_VALIDATE_URL) from PHP's
// ext/filter/logical_filters.c (php_filter_validate_url, with the
// FILTER_SANITIZE_URL character check it starts with), which
// Config::prohibitUrlByConfig uses.

package config

import (
	"strings"

	"github.com/stubbedev/maestro/internal/util"
)

// filterValidateURL reports whether filter_var($url, FILTER_VALIDATE_URL)
// accepts url.
func filterValidateURL(url string) bool {
	// php_filter_url removes every character outside its URL set; a
	// changed length fails validation.
	for i := range len(url) {
		if !isURLChar(url[i]) {
			return false
		}
	}

	u, ok := util.ParseURL(url)
	if !ok {
		return false
	}

	if u.HasScheme && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) {
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
	return isAlnum(c) || strings.IndexByte("$-_.+!*'(),{}|\\^~[]`<>#%\";/?:@&=", c) >= 0
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// isUserinfoValid ports is_userinfo_valid: unreserved and sub-delim
// characters, ':' and %XX escapes.
func isUserinfoValid(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case isAlnum(c) || strings.IndexByte("-._~!$&'()*+,;=:", c) >= 0:
		case c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
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
	if len(s) == 0 || s[0] == '.' || !isAlnum(s[0]) {
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
			if at(i+1) == '.' || !isAlnum(at(i-1)) || !isAlnum(at(i+1)) {
				return false
			}
			// Reset label length counter
			label = 1
		} else {
			if label > 63 || (s[i] != '-' && !isAlnum(s[i])) {
				return false
			}
			label++
		}
	}

	return true
}

// validateIPv4 ports _php_filter_validate_ipv4: four decimal octets with no
// leading zeros.
func validateIPv4(s string) bool {
	i, n := 0, 0
	for i < len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
		leadingZero := s[i] == '0'
		m := 1
		num := int(s[i] - '0')
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			num = num*10 + int(s[i]-'0')
			m++
			if num > 255 || m > 3 {
				return false
			}
			i++
		}
		// A leading 0 would introduce octal numbers, which are not supported.
		if leadingZero && (num != 0 || m > 1) {
			return false
		}
		n++
		if n == 4 {
			return i == len(s)
		}
		if i >= len(s) || s[i] != '.' {
			return false
		}
		i++
	}

	return false
}

// validateIPv6 ports _php_filter_validate_ipv6: up to 8 blocks of 1-4 hex
// digits, one :: and an optional trailing IPv4 address.
func validateIPv6(s string) bool {
	if strings.IndexByte(s, ':') < 0 {
		return false
	}

	compressed := false
	blocks := 0
	str := s

	// Check for a bundled IPv4 address.
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		ipv4 := dot
		for ipv4 > 0 && s[ipv4-1] != ':' {
			ipv4--
		}
		if !validateIPv4(s[ipv4:]) {
			return false
		}
		length := ipv4 // length excluding the IPv4 address
		if length < 2 {
			return false
		}
		if s[ipv4-2] != ':' {
			// Don't include the : before the IPv4 address unless it's a ::
			length--
		}
		str = s[:length]
		blocks = 2
	}

	i := 0
	for i < len(str) {
		if str[i] == ':' {
			i++
			if i >= len(str) {
				// Cannot end in : without a previous :
				return false
			}
			if str[i] == ':' {
				if compressed {
					// Cannot use :: more than once.
					return false
				}
				compressed = true
				blocks++ // :: means 1 or more 16-bit 0 blocks
				i++
				if i == len(str) {
					return blocks <= 8
				}
			} else if i-1 == 0 {
				// Don't allow a leading : without another : following.
				return false
			}
		}
		n := 0
		for i < len(str) && isHex(str[i]) {
			n++
			i++
		}
		if n < 1 || n > 4 {
			return false
		}
		blocks++
		if blocks > 8 {
			return false
		}
	}

	return compressed && blocks <= 8 || blocks == 8
}
