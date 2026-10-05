// Ports ext/json/json_encoder.c (php_json_encode_zval,
// php_json_encode_array, php_json_escape_string) and json_encode() from
// ext/json/json.c.

package php

import (
	"math"
	"strconv"
)

// JSONEncode ports json_encode($v, $flags) with the default depth of 512.
// On failure it returns PHP's error as *JSONError; with
// JSONPartialOutputOnError it also returns the partial output, as PHP
// does.
func JSONEncode(v any, flags JSONFlag) (string, error) {
	return JSONEncodeDepth(v, flags, JSONDefaultDepth)
}

// JSONEncodeDepth ports json_encode($v, $flags, $depth). Values must be
// acyclic.
func JSONEncodeDepth(v any, flags JSONFlag, depth int) (string, error) {
	e := jsonEncoder{flags: flags, maxDepth: depth}
	e.encode(v)
	if e.err != JSONErrorNone {
		err := &JSONError{Code: e.err}
		if flags&JSONPartialOutputOnError != 0 {
			return string(e.buf), err
		}
		return "", err
	}
	return string(e.buf), nil
}

type jsonEncoder struct {
	buf      []byte
	flags    JSONFlag
	depth    int
	maxDepth int
	err      int // encoder->error_code: the last error set
}

func (e *jsonEncoder) partial() bool { return e.flags&JSONPartialOutputOnError != 0 }

// encode ports php_json_encode_zval; it returns false on FAILURE.
func (e *jsonEncoder) encode(v any) bool {
	switch v := v.(type) {
	case nil:
		e.buf = append(e.buf, "null"...)
	case bool:
		if v {
			e.buf = append(e.buf, "true"...)
		} else {
			e.buf = append(e.buf, "false"...)
		}
	case int64:
		e.buf = strconv.AppendInt(e.buf, v, 10)
	case int:
		e.buf = strconv.AppendInt(e.buf, int64(v), 10)
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) {
			e.err = JSONErrorInfOrNaN
			e.buf = append(e.buf, '0')
		} else {
			e.encodeDouble(v)
		}
	case string:
		return e.escapeString(v, e.flags)
	case *Array:
		return e.encodeArray(v, nil)
	case *Object:
		return e.encodeArray(&v.props, v)
	default:
		e.err = JSONErrorUnsupportedType
		if e.partial() {
			e.buf = append(e.buf, "null"...)
		}
		return false
	}
	return true
}

func (e *jsonEncoder) encodeDouble(d float64) {
	start := len(e.buf)
	e.buf = appendGcvt(e.buf, d, -1, 'e')
	if e.flags&JSONPreserveZeroFraction != 0 && !hasByte(e.buf[start:], '.') {
		e.buf = append(e.buf, '.', '0')
	}
}

func (e *jsonEncoder) newlineIndent() {
	if e.flags&JSONPrettyPrint != 0 {
		e.buf = append(e.buf, '\n')
		for range e.depth {
			e.buf = append(e.buf, "    "...)
		}
	}
}

// encodeArray ports php_json_encode_array for arrays (obj == nil) and
// stdClass objects (props is the object's property table).
func (e *jsonEncoder) encodeArray(a *Array, obj *Object) bool {
	asObject := obj != nil || e.flags&JSONForceObject != 0 || !a.IsList()
	if asObject {
		e.buf = append(e.buf, '{')
	} else {
		e.buf = append(e.buf, '[')
	}
	e.depth++
	needComma := false
	for k, v := range a.All() {
		if asObject && obj != nil && k.kind == kindStr && len(k.s) > 0 && k.s[0] == 0 {
			// Mangled protected/private property name.
			continue
		}
		if needComma {
			e.buf = append(e.buf, ',')
		} else {
			needComma = true
		}
		e.newlineIndent()
		if asObject {
			if k.kind == kindStr {
				if !e.escapeString(k.s, e.flags&^JSONNumericCheck) && e.partial() {
					e.buf = e.buf[:len(e.buf)-4]
					e.buf = append(e.buf, `""`...)
				}
			} else {
				e.buf = append(e.buf, '"')
				e.buf = strconv.AppendInt(e.buf, k.i, 10)
				e.buf = append(e.buf, '"')
			}
			e.buf = append(e.buf, ':')
			if e.flags&JSONPrettyPrint != 0 {
				e.buf = append(e.buf, ' ')
			}
		}
		if !e.encode(v) && !e.partial() {
			return false
		}
	}
	if e.depth > e.maxDepth {
		e.err = JSONErrorDepth
		if !e.partial() {
			return false
		}
	}
	e.depth--
	if needComma {
		e.newlineIndent()
	}
	if asObject {
		e.buf = append(e.buf, '}')
	} else {
		e.buf = append(e.buf, ']')
	}
	return true
}

const hexDigits = "0123456789abcdef"

func (e *jsonEncoder) appendU(u rune) {
	e.buf = append(e.buf, '\\', 'u',
		hexDigits[(u>>12)&0xf], hexDigits[(u>>8)&0xf], hexDigits[(u>>4)&0xf], hexDigits[u&0xf])
}

// jsonNeedsEscape is php_json_escape_string's charmap: control characters,
// '"', '&', '\'', '/', '<', '>', '\\' and every byte >= 0x80.
var jsonNeedsEscape = func() (t [256]bool) {
	for c := range 0x20 {
		t[c] = true
	}
	for _, c := range `"&'/<>\` {
		t[c] = true
	}
	for c := 0x80; c < 0x100; c++ {
		t[c] = true
	}
	return t
}()

// escapeString ports php_json_escape_string; it returns false on FAILURE.
func (e *jsonEncoder) escapeString(s string, flags JSONFlag) bool {
	if s == "" {
		e.buf = append(e.buf, `""`...)
		return true
	}
	if flags&JSONNumericCheck != 0 {
		switch typ, l, d := isNumericString(s, false); typ {
		case numLong:
			e.buf = strconv.AppendInt(e.buf, l, 10)
			return true
		case numDouble:
			if !math.IsInf(d, 0) && !math.IsNaN(d) {
				e.encodeDouble(d)
				return true
			}
		}
	}
	checkpoint := len(e.buf)
	e.buf = append(e.buf, '"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if !jsonNeedsEscape[c] {
			i++
			continue
		}
		e.buf = append(e.buf, s[start:i]...)
		if c >= 0x80 {
			r, n, ok := nextUTF8Char(s, i)
			switch {
			case !ok:
				switch {
				case flags&JSONInvalidUTF8Ignore != 0:
				case flags&JSONInvalidUTF8Substitute != 0:
					if flags&JSONUnescapedUnicode != 0 {
						e.buf = append(e.buf, "\xef\xbf\xbd"...)
					} else {
						e.buf = append(e.buf, `�`...)
					}
				default:
					e.buf = e.buf[:checkpoint]
					e.err = JSONErrorUTF8
					if e.partial() {
						e.buf = append(e.buf, "null"...)
					}
					return false
				}
			case flags&JSONUnescapedUnicode != 0 &&
				(flags&JSONUnescapedLineTerminators != 0 || r < 0x2028 || r > 0x2029):
				e.buf = append(e.buf, s[i:i+n]...)
			default:
				if r >= 0x10000 {
					r -= 0x10000
					e.appendU(0xD800 | r>>10)
					r = 0xDC00 | r&0x3FF
				}
				e.appendU(r)
			}
			i += n
			start = i
			continue
		}
		switch c {
		case '"':
			if flags&JSONHexQuot != 0 {
				e.buf = append(e.buf, `"`...)
			} else {
				e.buf = append(e.buf, `\"`...)
			}
		case '\\':
			e.buf = append(e.buf, `\\`...)
		case '/':
			if flags&JSONUnescapedSlashes != 0 {
				e.buf = append(e.buf, '/')
			} else {
				e.buf = append(e.buf, `\/`...)
			}
		case '\b':
			e.buf = append(e.buf, `\b`...)
		case '\f':
			e.buf = append(e.buf, `\f`...)
		case '\n':
			e.buf = append(e.buf, `\n`...)
		case '\r':
			e.buf = append(e.buf, `\r`...)
		case '\t':
			e.buf = append(e.buf, `\t`...)
		case '<':
			e.appendHexOr(flags&JSONHexTag != 0, `<`, c)
		case '>':
			e.appendHexOr(flags&JSONHexTag != 0, `>`, c)
		case '&':
			e.appendHexOr(flags&JSONHexAmp != 0, `&`, c)
		case '\'':
			e.appendHexOr(flags&JSONHexApos != 0, `'`, c)
		default:
			e.appendU(rune(c))
		}
		i++
		start = i
	}
	e.buf = append(e.buf, s[start:]...)
	e.buf = append(e.buf, '"')
	return true
}

func (e *jsonEncoder) appendHexOr(hex bool, esc string, c byte) {
	if hex {
		e.buf = append(e.buf, esc...)
	} else {
		e.buf = append(e.buf, c)
	}
}
