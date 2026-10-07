// Ports the shared parts of ext/json (json.c, php_json.h): flags, error
// codes and messages.

package php

// JSONFlag holds json_encode/json_decode option bits. Some values are
// shared between encode and decode flags, as in PHP.
type JSONFlag int

// json_encode flags.
const (
	JSONHexTag                   JSONFlag = 1
	JSONHexAmp                   JSONFlag = 2
	JSONHexApos                  JSONFlag = 4
	JSONHexQuot                  JSONFlag = 8
	JSONForceObject              JSONFlag = 16
	JSONNumericCheck             JSONFlag = 32
	JSONUnescapedSlashes         JSONFlag = 64
	JSONPrettyPrint              JSONFlag = 128
	JSONUnescapedUnicode         JSONFlag = 256
	JSONPartialOutputOnError     JSONFlag = 512
	JSONPreserveZeroFraction     JSONFlag = 1024
	JSONUnescapedLineTerminators JSONFlag = 2048
)

// json_decode flags.
const (
	JSONObjectAsArray  JSONFlag = 1
	JSONBigintAsString JSONFlag = 2
)

// Flags for both directions.
const (
	JSONInvalidUTF8Ignore     JSONFlag = 1048576
	JSONInvalidUTF8Substitute JSONFlag = 2097152
	// JSONThrowOnError changes nothing in Go: failures are always
	// returned as *JSONError.
	JSONThrowOnError JSONFlag = 4194304
)

// JSONDefaultDepth is the default $depth of json_encode and json_decode.
const JSONDefaultDepth = 512

// JSON error codes (json_last_error()).
const (
	JSONErrorNone                = 0
	JSONErrorDepth               = 1
	JSONErrorStateMismatch       = 2
	JSONErrorCtrlChar            = 3
	JSONErrorSyntax              = 4
	JSONErrorUTF8                = 5
	JSONErrorRecursion           = 6
	JSONErrorInfOrNaN            = 7
	JSONErrorUnsupportedType     = 8
	JSONErrorInvalidPropertyName = 9
	JSONErrorUTF16               = 10
)

// JSONError is a json_encode/json_decode failure; it stands for both
// json_last_error() and JsonException. Error returns
// json_last_error_msg().
type JSONError struct {
	Code int
}

func (e *JSONError) Error() string { return jsonErrorMsg(e.Code) }

// PHPClass implements phperr.Exception.
func (*JSONError) PHPClass() string { return "JsonException" }

// PHPCode implements phperr.Coded: json_last_error().
func (e *JSONError) PHPCode() int { return e.Code }

func jsonErrorMsg(code int) string {
	switch code {
	case JSONErrorNone:
		return "No error"
	case JSONErrorDepth:
		return "Maximum stack depth exceeded"
	case JSONErrorStateMismatch:
		return "State mismatch (invalid or malformed JSON)"
	case JSONErrorCtrlChar:
		return "Control character error, possibly incorrectly encoded"
	case JSONErrorSyntax:
		return "Syntax error"
	case JSONErrorUTF8:
		return "Malformed UTF-8 characters, possibly incorrectly encoded"
	case JSONErrorRecursion:
		return "Recursion detected"
	case JSONErrorInfOrNaN:
		return "Inf and NaN cannot be JSON encoded"
	case JSONErrorUnsupportedType:
		return "Type is not supported"
	case JSONErrorInvalidPropertyName:
		return "The decoded property name is invalid"
	case JSONErrorUTF16:
		return "Single unpaired UTF-16 surrogate in unicode escape"
	}
	return "Unknown error"
}

// nextUTF8Char ports php_next_utf8_char (ext/standard/html.c): it decodes
// the character at s[pos:], returning it and its length, or ok=false and
// the number of bytes PHP skips for the ill-formed sequence.
func nextUTF8Char(s string, pos int) (r rune, n int, ok bool) {
	avail := len(s) - pos
	c := s[pos]
	lead := func(i int) bool { b := s[pos+i]; return b < 0x80 || (b >= 0xC2 && b <= 0xF4) }
	trail := func(i int) bool { b := s[pos+i]; return b >= 0x80 && b <= 0xBF }
	switch {
	case c < 0x80:
		return rune(c), 1, true
	case c < 0xC2:
		return 0, 1, false
	case c < 0xE0:
		if avail < 2 {
			return 0, 1, false
		}
		if !trail(1) {
			if lead(1) {
				return 0, 1, false
			}
			return 0, 2, false
		}
		return rune(c&0x1F)<<6 | rune(s[pos+1]&0x3F), 2, true
	case c < 0xF0:
		if avail < 3 || !trail(1) || !trail(2) {
			switch {
			case avail < 2 || lead(1):
				return 0, 1, false
			case avail < 3 || lead(2):
				return 0, 2, false
			}
			return 0, 3, false
		}
		r = rune(c&0x0F)<<12 | rune(s[pos+1]&0x3F)<<6 | rune(s[pos+2]&0x3F)
		if r < 0x800 || (r >= 0xD800 && r <= 0xDFFF) {
			return 0, 3, false
		}
		return r, 3, true
	case c < 0xF5:
		if avail < 4 || !trail(1) || !trail(2) || !trail(3) {
			switch {
			case avail < 2 || lead(1):
				return 0, 1, false
			case avail < 3 || lead(2):
				return 0, 2, false
			case avail < 4 || lead(3):
				return 0, 3, false
			}
			return 0, 4, false
		}
		r = rune(c&0x07)<<18 | rune(s[pos+1]&0x3F)<<12 | rune(s[pos+2]&0x3F)<<6 | rune(s[pos+3]&0x3F)
		if r < 0x10000 || r > 0x10FFFF {
			return 0, 4, false
		}
		return r, 4, true
	}
	return 0, 1, false
}
