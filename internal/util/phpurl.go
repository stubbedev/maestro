// Ports php_url_parse_ex2 from PHP's ext/standard/url.c: parse_url().

package util

import "strings"

// phpURL is the result of parse_url(); hasX reports whether a component was
// present.
type phpURL struct {
	scheme, user, pass, host, path, query, fragment string
	port                                            int
	hasScheme, hasHost, hasPort                     bool
}

// parseURL ports parse_url(); ok is false where PHP returns false.
func parseURL(str string) (phpURL, bool) {
	var ret phpURL

	s := 0
	ue := len(str)

	hostStart := -1 // where parse_host continues

	e := strings.IndexByte(str, ':')

	switch {
	case e > 0:
		valid := true

		for p := range e {
			c := str[p]
			if isASCIIAlnum(c) || c == '+' || c == '.' || c == '-' {
				continue
			}

			valid = false

			q := strings.IndexByte(str, '?')
			switch {
			case e+1 < ue && q >= 0 && e < q:
				return parseURLPort(str, s, e, ret)
			case s+1 < ue && str[s] == '/' && str[s+1] == '/':
				hostStart = s + 2
			default:
				return parseURLPath(str, s, ret)
			}

			break
		}

		if valid {
			if e+1 == ue {
				// Only the scheme is available.
				ret.scheme, ret.hasScheme = replaceControlChars(str[:e]), true

				return ret, true
			}

			if str[e+1] != '/' {
				// Certain schemes like mailto: and zlib: may not have any /
				// after them; a run of digits is a port though, as in
				// a.com:80.
				p := e + 1
				for p < ue && isASCIIDigit(str[p]) {
					p++
				}

				if (p == ue || str[p] == '/') && p-e < 7 {
					return parseURLPort(str, s, e, ret)
				}

				ret.scheme, ret.hasScheme = replaceControlChars(str[:e]), true

				return parseURLPath(str, e+1, ret)
			}

			ret.scheme, ret.hasScheme = replaceControlChars(str[:e]), true

			if e+2 < ue && str[e+2] == '/' {
				hostStart = e + 3

				if strings.EqualFold(ret.scheme, "file") && e+3 < ue && str[e+3] == '/' {
					// Support Windows drive letters as in
					// file:///c:/somedir/file.txt
					if e+5 < ue && str[e+5] == ':' {
						return parseURLPath(str, e+4, ret)
					}

					return parseURLPath(str, e+3, ret)
				}
			} else {
				return parseURLPath(str, e+1, ret)
			}
		}
	case e == 0:
		// No scheme; starts with a colon: look for a port.
		return parseURLPort(str, s, e, ret)
	case s+1 < ue && str[s] == '/' && str[s+1] == '/':
		// Relative-scheme URL.
		hostStart = s + 2
	default:
		return parseURLPath(str, s, ret)
	}

	return parseURLHost(str, hostStart, ret)
}

// parseURLPort is the parse_port label: a port after the colon at e.
func parseURLPort(str string, s, e int, ret phpURL) (phpURL, bool) {
	ue := len(str)
	p := e + 1
	pp := p

	for pp < ue && pp-p < 6 && isASCIIDigit(str[pp]) {
		pp++
	}

	switch {
	case pp-p > 0 && pp-p < 6 && (pp == ue || str[pp] == '/'):
		port, ok := strtolPort(str[p:pp])
		if !ok {
			return phpURL{}, false
		}

		ret.port, ret.hasPort = port, true

		if s+1 < ue && str[s] == '/' && str[s+1] == '/' {
			s += 2
		}
	case p == pp && pp == ue:
		return phpURL{}, false
	case s+1 < ue && str[s] == '/' && str[s+1] == '/':
		s += 2
	default:
		return parseURLPath(str, s, ret)
	}

	return parseURLHost(str, s, ret)
}

// parseURLHost is the parse_host label.
func parseURLHost(str string, s int, ret phpURL) (phpURL, bool) {
	ue := len(str)

	// Binary-safe strcspn(s, "/?#").
	e := ue
	if i := strings.IndexAny(str[s:], "/?#"); i >= 0 {
		e = s + i
	}

	// Check for login and password.
	if p := strings.LastIndexByte(str[s:e], '@'); p >= 0 {
		p += s
		if pp := strings.IndexByte(str[s:p], ':'); pp >= 0 {
			pp += s
			ret.user = replaceControlChars(str[s:pp])
			ret.pass = replaceControlChars(str[pp+1 : p])
		} else {
			ret.user = replaceControlChars(str[s:p])
		}

		s = p + 1
	}

	// Check for a port; an IPv6 address in brackets short-circuits it.
	p := -1
	if !(s < ue && str[s] == '[' && e > 0 && str[e-1] == ']') {
		if i := strings.LastIndexByte(str[s:e], ':'); i >= 0 {
			p = s + i
		}
	}

	if p >= 0 {
		if ret.port == 0 {
			portStr := str[p+1 : e]

			if len(portStr) > 5 {
				// A port cannot be longer than 5 characters.
				return phpURL{}, false
			}

			if portStr != "" {
				port, ok := strtolPort(portStr)
				if !ok {
					return phpURL{}, false
				}

				ret.port, ret.hasPort = port, true
			}
		}
	} else {
		p = e
	}

	// Check if we have a valid host, if we don't reject the string as url.
	if p-s < 1 {
		return phpURL{}, false
	}

	ret.host, ret.hasHost = replaceControlChars(str[s:p]), true

	if e == ue {
		return ret, true
	}

	return parseURLPath(str, e, ret)
}

// parseURLPath is the just_path label: path, query and fragment from s.
func parseURLPath(str string, s int, ret phpURL) (phpURL, bool) {
	ue := len(str)
	e := ue

	if i := strings.IndexByte(str[s:e], '#'); i >= 0 {
		p := s + i + 1
		ret.fragment = replaceControlChars(str[p:e])
		e = p - 1
	}

	if i := strings.IndexByte(str[s:e], '?'); i >= 0 {
		p := s + i + 1
		ret.query = replaceControlChars(str[p:e])
		e = p - 1
	}

	if s < e || s == ue {
		ret.path = replaceControlChars(str[s:e])
	}

	return ret, true
}

// strtolPort ports the port check: ZEND_STRTOL of the digits must consume
// something and land in 0..65535.
func strtolPort(s string) (int, bool) {
	i := 0
	for i < len(s) && (isPCRESpace(s[i])) {
		i++
	}

	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}

	start := i
	n := 0

	for i < len(s) && isASCIIDigit(s[i]) {
		n = n*10 + int(s[i]-'0')
		i++
	}

	if i == start {
		return 0, false
	}

	if neg {
		n = -n
	}

	if n < 0 || n > 65535 {
		return 0, false
	}

	return n, true
}

// replaceControlChars ports php_replace_controlchars_ex: control characters
// become underscores.
func replaceControlChars(s string) string {
	for i := range len(s) {
		if s[i] < 0x20 || s[i] == 0x7f {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] < 0x20 || b[j] == 0x7f {
					b[j] = '_'
				}
			}

			return string(b)
		}
	}

	return s
}
