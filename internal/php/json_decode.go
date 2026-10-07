// Ports ext/json/json_scanner.re, ext/json/json_parser.y and json_decode()
// from ext/json/json.c.

package php

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// JSONDecode ports json_decode($data, $assoc) with the default depth of
// 512: JSON objects become *Array when assoc is set, else *Object.
func JSONDecode(data string, assoc bool) (any, error) {
	flags := JSONFlag(0)
	if assoc {
		flags = JSONObjectAsArray
	}
	return JSONDecodeFlags(data, flags, JSONDefaultDepth)
}

// JSONDecodeFlags ports json_decode($data, null, $depth, $flags); objects
// decode to *Array when flags has JSONObjectAsArray. depth must be > 0.
func JSONDecodeFlags(data string, flags JSONFlag, depth int) (any, error) {
	if data == "" {
		return nil, &JSONError{Code: JSONErrorSyntax}
	}
	if depth <= 0 {
		panic("json_decode(): Argument #3 ($depth) must be greater than 0")
	}
	d := jsonDecoder{s: data, flags: flags, depth: 1, maxDepth: depth}
	return d.decode()
}

// JSONSpan is the bytes [Start, End) of a value in a JSON document.
type JSONSpan struct{ Start, End int }

// JSONDecodeSpans is JSONDecodeFlags(data, JSONObjectAsArray, depth),
// which also returns where the values of the arrays and objects level
// levels below the top (the top is level 0) are in data: their spans, in
// the order of the values in the *Array the array or object decodes to.
// Decoding a value's span gives what decoding data gives of the value.
// An object with a key twice has no spans.
func JSONDecodeSpans(data string, depth, level int) (any, map[*Array][]JSONSpan, error) {
	if data == "" {
		return nil, nil, &JSONError{Code: JSONErrorSyntax}
	}
	if depth <= 0 {
		panic("json_decode(): Argument #3 ($depth) must be greater than 0")
	}
	d := jsonDecoder{s: data, flags: JSONObjectAsArray, depth: 1, maxDepth: depth, spanDepth: level + 2, spans: map[*Array][]JSONSpan{}}
	v, err := d.decode()
	if err != nil {
		return nil, nil, err
	}
	return v, d.spans, nil
}

func (d *jsonDecoder) decode() (any, error) {
	v, ok := d.parse()
	if !ok {
		code := d.err
		if code == JSONErrorNone {
			code = JSONErrorSyntax
		}
		return nil, &JSONError{Code: code}
	}
	return v, nil
}

type jsonToken uint8

const (
	tokError jsonToken = iota
	tokEOI
	tokLBrace
	tokRBrace
	tokLBracket
	tokRBracket
	tokColon
	tokComma
	tokValue  // null, true, false, int, double
	tokString // a string, also a big int with JSON_BIGINT_AS_STRING
)

type jsonDecoder struct {
	s        string
	pos      int
	flags    JSONFlag
	depth    int
	maxDepth int
	err      int
	val      any // the scanned scalar
	// tok is where the last token scanned starts
	tok int
	// spans, when set, are the spans of the values of the arrays and
	// objects at depth spanDepth (JSONDecodeSpans)
	spans     map[*Array][]JSONSpan
	spanDepth int
}

// parse ports the grammar's start rule: value EOI.
func (d *jsonDecoder) parse() (any, bool) {
	v, ok := d.value(d.scan())
	if !ok {
		return nil, false
	}
	if d.scan() != tokEOI {
		return nil, false
	}
	return v, true
}

func (d *jsonDecoder) value(t jsonToken) (any, bool) {
	switch t {
	case tokValue, tokString:
		return d.val, true
	case tokLBrace:
		return d.object()
	case tokLBracket:
		return d.array()
	}
	return nil, false
}

func (d *jsonDecoder) enter() bool {
	if d.maxDepth != 0 && d.depth >= d.maxDepth {
		d.err = JSONErrorDepth
		return false
	}
	d.depth++
	return true
}

func (d *jsonDecoder) array() (any, bool) {
	if !d.enter() {
		return nil, false
	}
	a := NewArray()
	var spans []JSONSpan
	keep := d.spans != nil && d.depth == d.spanDepth
	t := d.scan()
	if t != tokRBracket && t != tokRBrace {
		for {
			start := d.tok
			v, ok := d.value(t)
			if !ok {
				return nil, false
			}
			a.Append(v)
			if keep {
				spans = append(spans, JSONSpan{start, d.pos})
			}
			if t = d.scan(); t != tokComma {
				break
			}
			t = d.scan()
		}
	}
	if keep {
		d.spans[a] = spans
	}
	return a, d.close(t, tokRBracket, tokRBrace)
}

// close checks the token ending an array or object: the wrong bracket is
// a state mismatch.
func (d *jsonDecoder) close(t, want, mismatch jsonToken) bool {
	switch t {
	case want:
		d.depth--
		return true
	case mismatch:
		d.err = JSONErrorStateMismatch
	}
	return false
}

func (d *jsonDecoder) object() (any, bool) {
	if !d.enter() {
		return nil, false
	}
	assoc := d.flags&JSONObjectAsArray != 0
	var (
		arr   *Array
		obj   *Object
		spans []JSONSpan
	)
	keep := assoc && d.spans != nil && d.depth == d.spanDepth
	if assoc {
		arr = NewArray()
	} else {
		obj = NewObject()
	}
	t := d.scan()
	if t != tokRBrace && t != tokRBracket {
		for {
			if t != tokString {
				return nil, false
			}
			// With JSON_BIGINT_AS_STRING a big int is a string token too,
			// and PHP's grammar accepts it as a key.
			key := d.val.(string) //nolint:errcheck,forcetypeassert // tokString always carries a string
			if d.scan() != tokColon {
				return nil, false
			}
			t = d.scan()
			start := d.tok
			v, ok := d.value(t)
			if !ok {
				return nil, false
			}
			if assoc {
				n := arr.Len()
				arr.Set(key, v)
				if keep {
					// a key set again keeps its place: no spans
					spans = append(spans, JSONSpan{start, d.pos})
					keep = arr.Len() > n
				}
			} else {
				if key != "" && key[0] == 0 {
					d.err = JSONErrorInvalidPropertyName
					return nil, false
				}
				obj.Set(key, v)
			}
			if t = d.scan(); t != tokComma {
				break
			}
			t = d.scan()
		}
	}
	if !d.close(t, tokRBrace, tokRBracket) {
		return nil, false
	}
	if keep {
		d.spans[arr] = spans
	}
	if assoc {
		return arr, true
	}
	return obj, true
}

// scan ports php_json_scan in its JS state.
func (d *jsonDecoder) scan() jsonToken {
	for d.pos < len(d.s) {
		c := d.s[d.pos]
		d.tok = d.pos
		switch c {
		case ' ', '\t', '\n', '\r':
			d.pos++
			continue
		case '{':
			d.pos++
			return tokLBrace
		case '}':
			d.pos++
			return tokRBrace
		case '[':
			d.pos++
			return tokLBracket
		case ']':
			d.pos++
			return tokRBracket
		case ':':
			d.pos++
			return tokColon
		case ',':
			d.pos++
			return tokComma
		case '"':
			d.pos++
			return d.scanString()
		case 'n', 't', 'f':
			for _, lit := range [...]struct {
				s string
				v any
			}{{"null", nil}, {"true", true}, {"false", false}} {
				if strings.HasPrefix(d.s[d.pos:], lit.s) {
					d.pos += len(lit.s)
					d.val = lit.v
					return tokValue
				}
			}
			d.err = JSONErrorSyntax
			return tokError
		}
		if c == '-' || isDigit(c) {
			if t, ok := d.scanNumber(); ok {
				return t
			}
		}
		switch {
		case c < 0x20:
			d.err = JSONErrorCtrlChar
		case c < 0x80:
			d.err = JSONErrorSyntax
		default:
			if r, _ := utf8.DecodeRuneInString(d.s[d.pos:]); r == utf8.RuneError {
				d.err = JSONErrorUTF8
			} else {
				d.err = JSONErrorSyntax
			}
		}
		return tokError
	}
	return tokEOI
}

// scanNumber matches INT, FLOAT or EXP at d.pos (re2c longest match).
func (d *jsonDecoder) scanNumber() (jsonToken, bool) {
	s := d.s
	i := d.pos
	neg := s[i] == '-'
	if neg {
		i++
	}
	switch {
	case i < len(s) && s[i] == '0':
		i++
	case i < len(s) && s[i] >= '1' && s[i] <= '9':
		for i < len(s) && isDigit(s[i]) {
			i++
		}
	default:
		return 0, false
	}
	intEnd := i
	isFloat := false
	if i+1 < len(s) && s[i] == '.' && isDigit(s[i+1]) {
		i += 2
		for i < len(s) && isDigit(s[i]) {
			i++
		}
		isFloat = true
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j < len(s) && isDigit(s[j]) {
			for j < len(s) && isDigit(s[j]) {
				j++
			}
			i = j
			isFloat = true
		}
	}
	tok := s[d.pos:i]
	digits := intEnd - d.pos
	if neg {
		digits--
	}
	d.pos = i
	if isFloat {
		d.val = strtod(tok)
		return tokValue, true
	}
	bigint := false
	if digits >= 19 {
		if digits == 19 {
			start := 0
			if neg {
				start = 1
			}
			cmp := strings.Compare(tok[start:], "9223372036854775808")
			bigint = cmp > 0 || cmp == 0 && !neg
		} else {
			bigint = true
		}
	}
	switch {
	case !bigint:
		n, _ := strconv.ParseInt(tok, 10, 64)
		d.val = n
	case d.flags&JSONBigintAsString != 0:
		d.val = tok
		return tokString, true
	default:
		d.val = strtod(tok)
	}
	return tokValue, true
}

// scanString scans a string after its opening quote (the STR_P1 and
// STR_P2 states).
func (d *jsonDecoder) scanString() jsonToken {
	s := d.s
	start := d.pos
	var b []byte // nil until an escape or invalid byte needs rewriting
	i := start
	for {
		if i >= len(s) {
			// The NUL terminator is a control character.
			d.err = JSONErrorCtrlChar
			return tokError
		}
		c := s[i]
		switch {
		case c == '"':
			if b == nil {
				d.val = s[start:i]
			} else {
				d.val = string(append(b, s[start:i]...))
			}
			d.pos = i + 1
			return tokString
		case c < 0x20:
			d.err = JSONErrorCtrlChar
			return tokError
		case c == '\\':
			esc, n, code := jsonUnescape(s, i)
			if code != JSONErrorNone {
				d.err = code
				return tokError
			}
			b = append(append(b, s[start:i]...), esc...)
			i += n
			start = i
		case c < 0x80:
			i++
		default:
			r, n := utf8.DecodeRuneInString(s[i:])
			if r != utf8.RuneError || n > 1 {
				i += n
				continue
			}
			if d.flags&(JSONInvalidUTF8Ignore|JSONInvalidUTF8Substitute) == 0 {
				d.err = JSONErrorUTF8
				return tokError
			}
			b = append(b, s[start:i]...)
			if d.flags&JSONInvalidUTF8Substitute != 0 {
				b = append(b, "\xef\xbf\xbd"...)
			}
			i++
			start = i
		}
	}
}

// jsonUnescape decodes the escape sequence at s[i] (a backslash),
// returning the bytes it stands for, its length, or an error code.
func jsonUnescape(s string, i int) (string, int, int) {
	if i+1 >= len(s) {
		return "", 0, JSONErrorSyntax
	}
	switch s[i+1] {
	case '"':
		return `"`, 2, JSONErrorNone
	case '\\':
		return `\`, 2, JSONErrorNone
	case '/':
		return "/", 2, JSONErrorNone
	case 'b':
		return "\b", 2, JSONErrorNone
	case 'f':
		return "\f", 2, JSONErrorNone
	case 'n':
		return "\n", 2, JSONErrorNone
	case 'r':
		return "\r", 2, JSONErrorNone
	case 't':
		return "\t", 2, JSONErrorNone
	case 'u':
		hi, ok := hex4(s, i+2)
		if !ok {
			return "", 0, JSONErrorSyntax
		}
		if hi >= 0xD800 && hi <= 0xDBFF {
			if lo, ok := hex4(s, i+8); ok && s[i+6] == '\\' && s[i+7] == 'u' && lo >= 0xDC00 && lo <= 0xDFFF {
				return string(((hi&0x3FF)<<10 | lo&0x3FF) + 0x10000), 12, JSONErrorNone
			}
			return "", 0, JSONErrorUTF16
		}
		if hi >= 0xDC00 && hi <= 0xDFFF {
			return "", 0, JSONErrorUTF16
		}
		return string(hi), 6, JSONErrorNone
	}
	return "", 0, JSONErrorSyntax
}

// hex4 parses 4 hex digits at s[i:].
func hex4(s string, i int) (rune, bool) {
	if i+4 > len(s) {
		return 0, false
	}
	var v rune
	for _, c := range []byte(s[i : i+4]) {
		if !isHexDigit(c) {
			return 0, false
		}
		v = v<<4 | rune(hexVal(c))
	}
	return v, true
}
