// Ports the filter_var() validators NoProxyPattern uses, from PHP's
// ext/filter/logical_filters.c: FILTER_VALIDATE_INT with a range and
// FILTER_VALIDATE_IP without flags.

package util

import (
	"math"
	"strings"
)

// filterValidateInt ports filter_var($s, FILTER_VALIDATE_INT,
// ['options' => ['min_range' => min, 'max_range' => max]]).
func filterValidateInt(s string, minRange, maxRange int) (int, bool) {
	// PHP_FILTER_TRIM_DEFAULT
	s = strings.Trim(s, " \t\r\v\n")
	if s == "" {
		return 0, false
	}

	neg := false

	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}

	var value int

	switch {
	case s == "0":
		// +0 and -0 are special cases.
	case s == "" || s[0] < '1' || s[0] > '9':
		return 0, false
	default:
		for i := range len(s) {
			if !isASCIIDigit(s[i]) {
				return 0, false
			}

			digit := int(s[i] - '0')
			if !neg && value > (math.MaxInt-digit)/10 || neg && -value < (math.MinInt+digit)/10 {
				return 0, false
			}

			value = value*10 + digit
		}
	}

	if neg {
		value = -value
	}

	if value < minRange || value > maxRange {
		return 0, false
	}

	return value, true
}

// filterValidateIP ports filter_var($s, FILTER_VALIDATE_IP): an address
// holding a colon is IPv6, else one holding a dot IPv4.
func filterValidateIP(s string) bool {
	switch {
	case strings.IndexByte(s, ':') >= 0:
		return validateIPv6(s)
	case strings.IndexByte(s, '.') >= 0:
		return validateIPv4(s)
	default:
		return false
	}
}

// validateIPv4 ports _php_filter_validate_ipv4: four decimal octets with no
// leading zeros.
func validateIPv4(s string) bool {
	i, n := 0, 0

	for i < len(s) {
		if !isASCIIDigit(s[i]) {
			return false
		}

		leadingZero := s[i] == '0'
		m := 1
		num := int(s[i] - '0')
		i++

		for i < len(s) && isASCIIDigit(s[i]) {
			num = num*10 + int(s[i]-'0')
			m++

			if num > 255 || m > 3 {
				return false
			}

			i++
		}

		// A leading 0 would introduce octal numbers, which are not
		// supported.
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

	compressedPos := -1
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
				if compressedPos >= 0 {
					// Cannot use :: more than once.
					return false
				}

				compressedPos = blocks
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
		for i < len(str) && isHexDigit(str[i]) {
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

	return (compressedPos >= 0 && blocks <= 8) || blocks == 8
}

func isHexDigit(c byte) bool {
	return isASCIIDigit(c) || ((c|0x20) >= 'a' && (c|0x20) <= 'f')
}
