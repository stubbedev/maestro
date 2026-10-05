// Ports src/JsonSchema/Constraints/FormatConstraint.php,
// Tool/Validator/UriValidator.php, Tool/Validator/RelativeReferenceValidator.php
// and Rfc3339.php of justinrainbow/json-schema 6.10.0, with the parts of
// PHP's ext/filter (FILTER_VALIDATE_EMAIL, FILTER_VALIDATE_IP) and
// DateTime::createFromFormat they rely on.

package jsonschema

import (
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/maestro/internal/json/res"
	"github.com/stubbedev/maestro/internal/php"
)

// checkFormat ports Constraint::checkFormat and FormatConstraint::check.
// element is a string or a number.
func (v *Validator) checkFormat(errs []Error, element any, schema *php.Object, p depth) []Error {
	format, ok := sget(schema, "format")
	if !ok {
		return errs
	}
	f, _ := format.(string)
	s, isString := element.(string)

	switch f {
	case "date":
		if isString && !validateDateTime(s, "Y-m-d") {
			errs = append(errs, v.newError(errFormatDate, p, php.ArrayOf("date", element, "format", format)))
		}
	case "time":
		if isString && !validateDateTime(s, "H:i:s") {
			errs = append(errs, v.newError(errFormatTime, p, php.ArrayOf("time", jsonEncode(element), "format", format)))
		}
	case "date-time":
		if isString && !isRfc3339(s) {
			errs = append(errs, v.newError(errFormatDateTime, p, php.ArrayOf("dateTime", jsonEncode(element), "format", format)))
		}
	case "utc-millisec":
		// the original value is compared with the formatted date, so only strings pass
		if !isString || !validateDateTime(s, "U") {
			errs = append(errs, v.newError(errFormatDateUTC, p, php.ArrayOf("value", element, "format", format)))
		}
	case "regex":
		if isString && compileJSONPattern(s) == nil {
			errs = append(errs, v.newError(errFormatRegex, p, php.ArrayOf("value", element, "format", format)))
		}
	case "color":
		if isString && !validateColor(s) {
			errs = append(errs, v.newError(errFormatColor, p, php.ArrayOf("format", format)))
		}
	case "style":
		if !isString {
			fail("rtrim(): Argument #1 ($string) must be of type string, " + php.TypeName(element) + " given")
		}
		if !validateStyle(s) {
			errs = append(errs, v.newError(errFormatStyle, p, php.ArrayOf("format", format)))
		}
	case "phone":
		if !isString {
			fail("preg_match(): Argument #2 ($subject) must be of type string, " + php.TypeName(element) + " given")
		}
		if ok, _ := phoneRegexp.IsMatch(s); !ok {
			errs = append(errs, v.newError(errFormatPhone, p, php.ArrayOf("format", format)))
		}
	case "uri":
		if isString && !isValidURI(s) {
			errs = append(errs, v.newError(errFormatURL, p, php.ArrayOf("format", format)))
		}
	case "uriref", "uri-reference":
		if isString && !isValidURI(s) && !isValidRelativeReference(s) {
			errs = append(errs, v.newError(errFormatURL, p, php.ArrayOf("format", format)))
		}
	case "email":
		if isString && !isValidEmail(s) {
			errs = append(errs, v.newError(errFormatEmail, p, php.ArrayOf("format", format)))
		}
	case "ip-address", "ipv4":
		if isString && !isValidIP(s, false) {
			errs = append(errs, v.newError(errFormatIP, p, php.ArrayOf("format", format)))
		}
	case "ipv6":
		if isString && !isValidIP(s, true) {
			errs = append(errs, v.newError(errFormatIP, p, php.ArrayOf("format", format)))
		}
	case "host-name", "hostname":
		if isString {
			if ok, _ := hostnameRegexp.IsMatch(s); !ok {
				errs = append(errs, v.newError(errFormatHostname, p, php.ArrayOf("format", format)))
			}
		}
	}

	return errs
}

func jsonEncode(v any) any {
	s, err := php.JSONEncode(v, 0)
	if err != nil {
		return false
	}

	return s
}

// validateDateTime ports FormatConstraint::validateDateTime for the formats
// it is used with: DateTime::createFromFormat($format, $s) succeeds and
// formats back to $s.
func validateDateTime(s, format string) bool {
	switch format {
	case "Y-m-d":
		if len(s) != 10 || s[4] != '-' || s[7] != '-' || !digits(s[:4]) || !digits(s[5:7]) || !digits(s[8:]) {
			return false
		}
		y, _ := strconv.Atoi(s[:4])
		m, _ := strconv.Atoi(s[5:7])
		d, _ := strconv.Atoi(s[8:])
		return validDate(y, m, d)
	case "H:i:s":
		if len(s) != 8 || s[2] != ':' || s[5] != ':' || !digits(s[:2]) || !digits(s[3:5]) || !digits(s[6:]) {
			return false
		}
		return s[:2] < "24" && s[3:5] < "60" && s[6:] < "60"
	case "U":
		n, err := strconv.ParseInt(s, 10, 64)
		return err == nil && strconv.FormatInt(n, 10) == s
	}

	return false
}

func digits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	return s != ""
}

func validDate(y, m, d int) bool {
	if m < 1 || m > 12 || d < 1 {
		return false
	}

	return d <= time.Date(y, time.Month(m)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

var rfc3339Regexp = php.MustCompile(`/^(\d{4}-\d{2}-\d{2}[T ](0[0-9]|1[0-9]|2[0-3]):([0-5][0-9]):((?:[0-5][0-9]|60)))(\.\d+)?(Z|([+-](0[0-9]|1[0-9]|2[0-3]))(:)?([0-5][0-9]))$/`)

// isRfc3339 ports Rfc3339::createFromString($s) !== null.
func isRfc3339(input string) bool {
	input = php.Strtoupper(input)
	m, err := rfc3339Regexp.Match(input)
	if err != nil || m == nil {
		return false
	}
	date := m.Get(1)
	y, _ := strconv.Atoi(date[:4])
	mo, _ := strconv.Atoi(date[5:7])
	d, _ := strconv.Atoi(date[8:10])
	if !validDate(y, mo, d) {
		return false
	}
	// DateTime's "u" takes at most 6 digits
	if frac := m.Get(5); len(frac) > 7 {
		return false
	}
	if m.Get(4) != "60" {
		return true
	}

	// handle leap seconds: only 23:59:60 UTC is one
	hh, _ := strconv.Atoi(m.Get(2))
	mm, _ := strconv.Atoi(m.Get(3))
	offset := 0
	if tz := m.Get(6); tz != "Z" {
		oh, _ := strconv.Atoi(m.Get(8))
		om, _ := strconv.Atoi(m.Get(10))
		offset = oh*60 + om
		if tz[0] == '-' {
			offset = -offset
		}
	}
	utc := ((hh*60+mm-offset)%1440 + 1440) % 1440

	return utc == 23*60+59
}

var (
	colorRegexp    = php.MustCompile(`/^#([a-f0-9]{3}|[a-f0-9]{6})$/i`)
	styleRegexp    = php.MustCompile(`/^\s*[-a-z]+\s*:\s*.+$/i`)
	phoneRegexp    = php.MustCompile(`/^\+?(\(\d{3}\)|\d{3}) \d{3} \d{4}$/`)
	hostnameRegexp = php.MustCompile(`/^(?!-)(?!.*?[^A-Za-z0-9\-\.])(?:(?!-)[A-Za-z0-9](?:[A-Za-z0-9\-]{0,61}[A-Za-z0-9])?\.)*(?!-)[A-Za-z0-9](?:[A-Za-z0-9\-]{0,61}[A-Za-z0-9])?$/`)
)

// validateColor ports FormatConstraint::validateColor for strings.
func validateColor(color string) bool {
	switch php.Strtolower(color) {
	case "aqua", "black", "blue", "fuchsia", "gray", "green", "lime", "maroon", "navy", "olive", "orange", "purple",
		"red", "silver", "teal", "white", "yellow":
		return true
	}
	ok, _ := colorRegexp.IsMatch(color)

	return ok
}

// validateStyle ports FormatConstraint::validateStyle.
func validateStyle(style string) bool {
	for prop := range strings.SplitSeq(php.RtrimSet(style, ";"), ";") {
		if ok, _ := styleRegexp.IsMatch(prop); !ok {
			return false
		}
	}

	return true
}

var (
	uriHierarchical = php.MustCompile(`/^
            ([a-z][a-z0-9+\-.]*):\/\/                # Scheme (http, https, ftp, etc.)
            (?:([^:@\/?#]+)(?::([^@\/?#]*))?@)?      # Optional userinfo (user:pass@)
            ([a-z0-9._~-]+|\[[a-f0-9:.]+\])          # Hostname or IPv6 in brackets
            (?::(\d{1,5}))?                          # Optional port
            (\/[a-zA-Z0-9._~!$&'()*+,;=:@\/%-]*)*   # Path (valid characters only)
            (\?([^#]*))?                             # Optional query
            (\#(.*))?                                # Optional fragment
        $/ix`)
	uriNonHierarchical = php.MustCompile(`/^
                (mailto|data|urn|news|tel|file):     # Only allow known non-hierarchical schemes
                (.+)                                 # Must contain at least one character after scheme
        $/ix`)
	uriNewsGroup    = php.MustCompile(`/^[a-z0-9]+(\.[a-z0-9]+)*$/i`)
	uriTel          = php.MustCompile(`/^\+?[0-9.\-() ]+$/`)
	uriEmail        = php.MustCompile(`/^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/`)
	uriDoubleDot    = php.MustCompile(`/\.\./`)
	uriIllegalChars = php.MustCompile("/[<>{}|\\\\^`]/")
)

// isValidURI ports UriValidator::isValid.
func isValidURI(uri string) bool {
	// First, check if it's a valid hierarchical URI
	if m, err := uriHierarchical.Match(uri); err == nil && m != nil {
		// Validate domain name (no double dots like example..com)
		if host := m.Get(4); php.ToBool(host) {
			if ok, _ := uriDoubleDot.IsMatch(host); ok {
				return false
			}
		}
		// Validate port (should be between 1 and 65535 if specified)
		if port := m.Get(5); php.ToBool(port) && (php.Compare(port, int64(1)) < 0 || php.Compare(int64(65535), port) < 0) {
			return false
		}
		// Validate the path (reject illegal characters: < > { } | \ ^ `)
		if p := m.Get(6); php.ToBool(p) {
			if ok, _ := uriIllegalChars.IsMatch(p); ok {
				return false
			}
		}

		return true
	}

	// If not hierarchical, check non-hierarchical URIs
	if m, err := uriNonHierarchical.Match(uri); err == nil && m != nil {
		var re *php.Regexp
		switch php.Strtolower(m.Get(1)) {
		case "mailto":
			re = uriEmail
		case "news":
			re = uriNewsGroup
		case "tel":
			re = uriTel
		default:
			return true
		}
		ok, _ := re.IsMatch(m.Get(2))

		return ok
	}

	return false
}

var (
	relativeRef       = php.MustCompile(`/^(([^\/?#]+):)?(\/\/([^\/?#]*))?([^?#]*)(\?([^#]*))?(#(.*))?$/`)
	relativeAbsolute  = php.MustCompile(`/^(http|https):\/\//`)
	relativeNoScheme  = php.MustCompile(`/^:\/\//`)
	relativeBadSep    = php.MustCompile(`/^:\//`)
	relativeEmptyAuth = php.MustCompile(`/^\/\/$/`)
	relativeThree     = php.MustCompile(`/^\/\/\/[^\/]/`)
	relativeSpace     = php.MustCompile(`/\s/`)
)

// isValidRelativeReference ports RelativeReferenceValidator::isValid.
func isValidRelativeReference(ref string) bool {
	if ok, _ := relativeRef.IsMatch(ref); !ok {
		return false
	}
	if strings.Contains(ref, `\`) {
		return false
	}
	for _, re := range []*php.Regexp{relativeAbsolute, relativeNoScheme, relativeBadSep, relativeEmptyAuth, relativeThree, relativeSpace} {
		if ok, _ := re.IsMatch(ref); ok {
			return false
		}
	}

	return true
}

// emailRegexp is the regular expression of PHP 8.4's
// php_filter_validate_email with FILTER_FLAG_EMAIL_UNICODE.
var emailRegexp = php.MustCompile(`/^(?!(?:(?:\x22?\x5C[\x00-\x7E]\x22?)|(?:\x22?[^\x5C\x22]\x22?)){255,})(?!(?:(?:\x22?\x5C[\x00-\x7E]\x22?)|(?:\x22?[^\x5C\x22]\x22?)){65,}@)(?:(?:[\x21\x23-\x27\x2A\x2B\x2D\x2F-\x39\x3D\x3F\x5E-\x7E\pL\pN]+)|(?:\x22(?:[\x01-\x08\x0B\x0C\x0E-\x1F\x21\x23-\x5B\x5D-\x7F\pL\pN]|(?:\x5C[\x00-\x7F]))*\x22))(?:\.(?:(?:[\x21\x23-\x27\x2A\x2B\x2D\x2F-\x39\x3D\x3F\x5E-\x7E\pL\pN]+)|(?:\x22(?:[\x01-\x08\x0B\x0C\x0E-\x1F\x21\x23-\x5B\x5D-\x7F\pL\pN]|(?:\x5C[\x00-\x7F]))*\x22)))*@(?:(?:(?!.*[^.]{64,})(?:(?:(?:xn--)?[a-z0-9]+(?:-+[a-z0-9]+)*\.){1,126}){1,}(?:(?:[a-z][a-z0-9]*)|(?:(?:xn--)[a-z0-9]+))(?:-+[a-z0-9]+)*)|(?:\[(?:(?:IPv6:(?:(?:[a-f0-9]{1,4}(?::[a-f0-9]{1,4}){7})|(?:(?!(?:.*[a-f0-9][:\]]){7,})(?:[a-f0-9]{1,4}(?::[a-f0-9]{1,4}){0,5})?::(?:[a-f0-9]{1,4}(?::[a-f0-9]{1,4}){0,5})?)))|(?:(?:IPv6:(?:(?:[a-f0-9]{1,4}(?::[a-f0-9]{1,4}){5}:)|(?:(?!(?:.*[a-f0-9]:){5,})(?:[a-f0-9]{1,4}(?::[a-f0-9]{1,4}){0,3})?::(?:[a-f0-9]{1,4}(?::[a-f0-9]{1,4}){0,3}:)?)))?(?:(?:25[0-5])|(?:2[0-4][0-9])|(?:1[0-9]{2})|(?:[1-9]?[0-9]))(?:\.(?:(?:25[0-5])|(?:2[0-4][0-9])|(?:1[0-9]{2})|(?:[1-9]?[0-9]))){3}))\]))$/iDu`)

// isValidEmail ports filter_var($s, FILTER_VALIDATE_EMAIL,
// FILTER_FLAG_EMAIL_UNICODE) !== false.
func isValidEmail(s string) bool {
	// The maximum length of an e-mail address is 320 octets, per RFC 2821.
	if s == "" || len(s) > 320 {
		return false
	}
	ok, err := emailRegexp.IsMatch(s)

	return ok && err == nil
}

// isValidIP ports filter_var($s, FILTER_VALIDATE_IP, FILTER_FLAG_IPV4 or
// FILTER_FLAG_IPV6) !== false.
func isValidIP(s string, v6 bool) bool {
	if !v6 {
		// dotted decimal, no leading zeros
		parts := strings.Split(s, ".")
		if len(parts) != 4 {
			return false
		}
		for _, part := range parts {
			if !digits(part) || len(part) > 3 || len(part) > 1 && part[0] == '0' {
				return false
			}
			if n, _ := strconv.Atoi(part); n > 255 {
				return false
			}
		}
		return true
	}
	if !strings.Contains(s, ":") || strings.Contains(s, "%") {
		return false
	}
	addr, err := netip.ParseAddr(s)

	return err == nil && addr.Is6()
}

// compiledPatterns holds the regular expressions of every "pattern" and
// "patternProperties" key in Composer's schemas, compiled once.
var compiledPatterns = sync.OnceValue(func() map[string]*php.Regexp {
	m := map[string]*php.Regexp{}
	var walk func(v any, parent string)
	walk = func(v any, parent string) {
		switch x := v.(type) {
		case *php.Object:
			for k, val := range x.All() {
				if parent == "patternProperties" {
					m[k] = compileJSONPattern(k)
				}
				if s, ok := val.(string); ok && k == "pattern" {
					m[s] = compileJSONPattern(s)
				}
				walk(val, k)
			}
		case *php.Array:
			for _, val := range x.All() {
				walk(val, "")
			}
		}
	}
	for _, text := range []string{res.ComposerSchema(), res.LockSchema(), res.RepositorySchema()} {
		decoded, _ := php.JSONDecode(text, false)
		walk(decoded, "")
	}

	return m
})
