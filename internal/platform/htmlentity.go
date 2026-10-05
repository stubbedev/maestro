// Ports html_entity_decode() of ext/standard/html.c with PHP 8.1's default
// flags (ENT_QUOTES | ENT_SUBSTITUTE | ENT_HTML401) and the UTF-8 charset.

package platform

import (
	"strings"
	"unicode/utf8"
)

// HTMLEntityDecode ports html_entity_decode($s): the HTML 4.01 named
// entities, decimal and hexadecimal character references of characters
// HTML 4.01 allows, and &quot; and &#39; (ENT_QUOTES). Anything else is
// kept as written.
func HTMLEntityDecode(s string) string {
	first := strings.IndexByte(s, '&')
	if first < 0 {
		return s
	}

	b := make([]byte, 0, len(s))
	b = append(b, s[:first]...)

	for p := first; p < len(s); {
		// assumes there are no single-char entities
		if s[p] != '&' || p+3 >= len(s) {
			b = append(b, s[p])
			p++

			continue
		}

		var code rune

		next := p + 1
		valid := false

		if s[p+1] == '#' {
			next = p + 2
			code, next, valid = numericEntity(s, next)
			valid = valid && html401Allowed(code)
		} else {
			start := next
			for next < len(s) && isASCIIAlnum(s[next]) {
				next++
			}

			if next < len(s) && s[next] == ';' && next > start {
				code, valid = html401Entities[s[start:next]]
			}
		}

		if !valid {
			b = append(b, s[p:next]...)
			p = next

			continue
		}

		b = utf8.AppendRune(b, code)
		p = next + 1
	}

	return string(b)
}

// numericEntity ports process_numeric_entity: the code point of the
// reference starting at s[i] (after "&#"), the index of the terminating
// ';' or where parsing stopped, and whether it is well formed.
func numericEntity(s string, i int) (rune, int, bool) {
	hex := i < len(s) && (s[i] == 'x' || s[i] == 'X')
	if hex {
		i++
	}

	base := uint64(10)
	if hex {
		base = 16
	}

	// The first character must be a digit, but strtol then also skips the
	// "0x" prefix of base 16.
	if hex && i+2 < len(s) && s[i] == '0' && (s[i+1] == 'x' || s[i+1] == 'X') {
		if _, ok := digitValue(s[i+2], true); ok {
			i += 2
		}
	}

	var code uint64

	if _, ok := digitValue(byteAt(s, i), hex); !ok {
		return 0, i, false
	}

	for ; i < len(s); i++ {
		d, ok := digitValue(s[i], hex)
		if !ok {
			break
		}

		if code <= 0x10FFFF { // past that strtol's exact value no longer matters
			code = code*base + d
		}
	}

	if byteAt(s, i) != ';' || code > 0x10FFFF {
		return 0, i, false
	}

	return rune(code), i, true
}

func digitValue(c byte, hex bool) (uint64, bool) {
	switch {
	case c >= '0' && c <= '9':
		return uint64(c - '0'), true
	case hex && c >= 'a' && c <= 'f':
		return uint64(c-'a') + 10, true
	case hex && c >= 'A' && c <= 'F':
		return uint64(c-'A') + 10, true
	}

	return 0, false
}

func isASCIIAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// html401Allowed ports unicode_cp_is_allowed for ENT_HTML401.
func html401Allowed(cp rune) bool {
	return cp >= 0x20 && cp <= 0x7E ||
		cp == 0x0A || cp == 0x09 || cp == 0x0D ||
		cp >= 0xA0 && cp <= 0xD7FF ||
		cp >= 0xE000 && cp <= 0x10FFFF
}

// html401Entities is the HTML 4.01 entity table (ent_ht_html4), as
// get_html_translation_table(HTML_ENTITIES, ENT_QUOTES | ENT_HTML401)
// lists it.
var html401Entities = map[string]rune{
	"AElig": 0xC6, "Aacute": 0xC1, "Acirc": 0xC2, "Agrave": 0xC0, "Alpha": 0x391,
	"Aring": 0xC5, "Atilde": 0xC3, "Auml": 0xC4, "Beta": 0x392, "Ccedil": 0xC7,
	"Chi": 0x3A7, "Dagger": 0x2021, "Delta": 0x394, "ETH": 0xD0, "Eacute": 0xC9,
	"Ecirc": 0xCA, "Egrave": 0xC8, "Epsilon": 0x395, "Eta": 0x397, "Euml": 0xCB,
	"Gamma": 0x393, "Iacute": 0xCD, "Icirc": 0xCE, "Igrave": 0xCC, "Iota": 0x399,
	"Iuml": 0xCF, "Kappa": 0x39A, "Lambda": 0x39B, "Mu": 0x39C, "Ntilde": 0xD1, "Nu": 0x39D,
	"OElig": 0x152, "Oacute": 0xD3, "Ocirc": 0xD4, "Ograve": 0xD2, "Omega": 0x3A9,
	"Omicron": 0x39F, "Oslash": 0xD8, "Otilde": 0xD5, "Ouml": 0xD6, "Phi": 0x3A6,
	"Pi": 0x3A0, "Prime": 0x2033, "Psi": 0x3A8, "Rho": 0x3A1, "Scaron": 0x160,
	"Sigma": 0x3A3, "THORN": 0xDE, "Tau": 0x3A4, "Theta": 0x398, "Uacute": 0xDA,
	"Ucirc": 0xDB, "Ugrave": 0xD9, "Upsilon": 0x3A5, "Uuml": 0xDC, "Xi": 0x39E,
	"Yacute": 0xDD, "Yuml": 0x178, "Zeta": 0x396, "aacute": 0xE1, "acirc": 0xE2,
	"acute": 0xB4, "aelig": 0xE6, "agrave": 0xE0, "alefsym": 0x2135, "alpha": 0x3B1,
	"amp": 0x26, "and": 0x2227, "ang": 0x2220, "aring": 0xE5, "asymp": 0x2248,
	"atilde": 0xE3, "auml": 0xE4, "bdquo": 0x201E, "beta": 0x3B2, "brvbar": 0xA6,
	"bull": 0x2022, "cap": 0x2229, "ccedil": 0xE7, "cedil": 0xB8, "cent": 0xA2,
	"chi": 0x3C7, "circ": 0x2C6, "clubs": 0x2663, "cong": 0x2245, "copy": 0xA9,
	"crarr": 0x21B5, "cup": 0x222A, "curren": 0xA4, "dArr": 0x21D3, "dagger": 0x2020,
	"darr": 0x2193, "deg": 0xB0, "delta": 0x3B4, "diams": 0x2666, "divide": 0xF7,
	"eacute": 0xE9, "ecirc": 0xEA, "egrave": 0xE8, "empty": 0x2205, "emsp": 0x2003,
	"ensp": 0x2002, "epsilon": 0x3B5, "equiv": 0x2261, "eta": 0x3B7, "eth": 0xF0,
	"euml": 0xEB, "euro": 0x20AC, "exist": 0x2203, "fnof": 0x192, "forall": 0x2200,
	"frac12": 0xBD, "frac14": 0xBC, "frac34": 0xBE, "frasl": 0x2044, "gamma": 0x3B3,
	"ge": 0x2265, "gt": 0x3E, "hArr": 0x21D4, "harr": 0x2194, "hearts": 0x2665,
	"hellip": 0x2026, "iacute": 0xED, "icirc": 0xEE, "iexcl": 0xA1, "igrave": 0xEC,
	"image": 0x2111, "infin": 0x221E, "int": 0x222B, "iota": 0x3B9, "iquest": 0xBF,
	"isin": 0x2208, "iuml": 0xEF, "kappa": 0x3BA, "lArr": 0x21D0, "lambda": 0x3BB,
	"lang": 0x2329, "laquo": 0xAB, "larr": 0x2190, "lceil": 0x2308, "ldquo": 0x201C,
	"le": 0x2264, "lfloor": 0x230A, "lowast": 0x2217, "loz": 0x25CA, "lrm": 0x200E,
	"lsaquo": 0x2039, "lsquo": 0x2018, "lt": 0x3C, "macr": 0xAF, "mdash": 0x2014,
	"micro": 0xB5, "middot": 0xB7, "minus": 0x2212, "mu": 0x3BC, "nabla": 0x2207,
	"nbsp": 0xA0, "ndash": 0x2013, "ne": 0x2260, "ni": 0x220B, "not": 0xAC, "notin": 0x2209,
	"nsub": 0x2284, "ntilde": 0xF1, "nu": 0x3BD, "oacute": 0xF3, "ocirc": 0xF4,
	"oelig": 0x153, "ograve": 0xF2, "oline": 0x203E, "omega": 0x3C9, "omicron": 0x3BF,
	"oplus": 0x2295, "or": 0x2228, "ordf": 0xAA, "ordm": 0xBA, "oslash": 0xF8,
	"otilde": 0xF5, "otimes": 0x2297, "ouml": 0xF6, "para": 0xB6, "part": 0x2202,
	"permil": 0x2030, "perp": 0x22A5, "phi": 0x3C6, "pi": 0x3C0, "piv": 0x3D6,
	"plusmn": 0xB1, "pound": 0xA3, "prime": 0x2032, "prod": 0x220F, "prop": 0x221D,
	"psi": 0x3C8, "quot": 0x22, "rArr": 0x21D2, "radic": 0x221A, "rang": 0x232A,
	"raquo": 0xBB, "rarr": 0x2192, "rceil": 0x2309, "rdquo": 0x201D, "real": 0x211C,
	"reg": 0xAE, "rfloor": 0x230B, "rho": 0x3C1, "rlm": 0x200F, "rsaquo": 0x203A,
	"rsquo": 0x2019, "sbquo": 0x201A, "scaron": 0x161, "sdot": 0x22C5, "sect": 0xA7,
	"shy": 0xAD, "sigma": 0x3C3, "sigmaf": 0x3C2, "sim": 0x223C, "spades": 0x2660,
	"sub": 0x2282, "sube": 0x2286, "sum": 0x2211, "sup": 0x2283, "sup1": 0xB9, "sup2": 0xB2,
	"sup3": 0xB3, "supe": 0x2287, "szlig": 0xDF, "tau": 0x3C4, "there4": 0x2234,
	"theta": 0x3B8, "thetasym": 0x3D1, "thinsp": 0x2009, "thorn": 0xFE, "tilde": 0x2DC,
	"times": 0xD7, "trade": 0x2122, "uArr": 0x21D1, "uacute": 0xFA, "uarr": 0x2191,
	"ucirc": 0xFB, "ugrave": 0xF9, "uml": 0xA8, "upsih": 0x3D2, "upsilon": 0x3C5,
	"uuml": 0xFC, "weierp": 0x2118, "xi": 0x3BE, "yacute": 0xFD, "yen": 0xA5, "yuml": 0xFF,
	"zeta": 0x3B6, "zwj": 0x200D, "zwnj": 0x200C,
}

// byteAt is s[i], or NUL past the end (C's terminator).
func byteAt(s string, i int) byte {
	if i < len(s) {
		return s[i]
	}

	return 0
}
