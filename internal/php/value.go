// Ports the type conversions of Zend/zend_operators.c (zval_get_long,
// zval_get_double, zval_get_string, zend_is_true, _is_numeric_string_ex)
// and the type names of Zend/zend_API.c.

package php

import (
	"fmt"
	"math"
	"strconv"
)

// normalize validates a value stored into an Array or Object, converting
// a Go int to int64.
func normalize(v any) any {
	switch v := v.(type) {
	case nil, bool, int64, float64, string, *Array, *Object:
		return v
	case int:
		return int64(v)
	default:
		panic(fmt.Sprintf("php: unsupported value type %T", v))
	}
}

// TypeName returns get_debug_type($v): "null", "bool", "int", "float",
// "string", "array" or "stdClass".
func TypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case int64, int:
		return "int"
	case float64:
		return "float"
	case string:
		return "string"
	case *Array:
		return "array"
	case *Object:
		return "stdClass"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// GetType returns gettype($v): "NULL", "boolean", "integer", "double",
// "string", "array" or "object".
func GetType(v any) string {
	switch v.(type) {
	case nil:
		return "NULL"
	case bool:
		return "boolean"
	case int64, int:
		return "integer"
	case float64:
		return "double"
	case string:
		return "string"
	case *Array:
		return "array"
	case *Object:
		return "object"
	default:
		return "unknown type"
	}
}

// ToBool converts a value as (bool) does: null, false, 0, 0.0, -0.0, "",
// "0" and the empty array are false; everything else, including NaN and
// every object, is true.
func ToBool(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	case int64:
		return v != 0
	case int:
		return v != 0
	case float64:
		return v != 0
	case string:
		return v != "" && v != "0"
	case *Array:
		return v.Len() > 0
	default:
		return true
	}
}

// ToInt converts a value as (int) does: numeric prefixes of strings
// ("12abc" is 12, "1e3" is 1000), floats truncated (saturating for numeric
// strings, wrapping for floats, as PHP does), arrays to 0 or 1.
func ToInt(v any) int64 {
	switch v := v.(type) {
	case nil:
		return 0
	case bool:
		if v {
			return 1
		}
		return 0
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return dvalToLval(v)
	case string:
		typ, l, d := isNumericString(v, true)
		switch typ {
		case numDouble:
			return dvalToLvalCap(d)
		case numLong:
			return l
		}
		return 0
	case *Array:
		if v.Len() > 0 {
			return 1
		}
		return 0
	default:
		return 1
	}
}

// ToFloat converts a value as (float) does.
func ToFloat(v any) float64 {
	switch v := v.(type) {
	case nil:
		return 0
	case bool:
		if v {
			return 1
		}
		return 0
	case int64:
		return float64(v)
	case int:
		return float64(v)
	case float64:
		return v
	case string:
		return strtod(v)
	case *Array:
		if v.Len() > 0 {
			return 1
		}
		return 0
	default:
		return 1
	}
}

// ToString converts a value as (string) does: null and false are "", true
// is "1", floats use precision 14 ("0.1", "1.0E+25"), arrays are "Array".
// Converting an object panics with PHP's Error message, since stdClass has
// no __toString.
func ToString(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case nil:
		return ""
	case bool:
		if v {
			return "1"
		}
		return ""
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	case float64:
		return FloatToString(v)
	case *Array:
		return "Array"
	default:
		panic("Object of class " + TypeName(v) + " could not be converted to string")
	}
}

// IsNumeric ports is_numeric(): ints and floats, and strings that are
// numeric with optional leading and trailing whitespace.
func IsNumeric(v any) bool {
	switch v := v.(type) {
	case int64, int, float64:
		return true
	case string:
		typ, _, _ := isNumericString(v, false)
		return typ != 0
	}
	return false
}

func dvalToLvalCap(d float64) int64 {
	if math.IsNaN(d) || math.IsInf(d, 0) {
		return 0
	}
	if d >= -9223372036854775808.0 && d < 9223372036854775808.0 {
		return int64(d)
	}
	if d > 0 {
		return math.MaxInt64
	}
	return math.MinInt64
}

const (
	numLong   = 1
	numDouble = 2
)

// isNumericString ports _is_numeric_string_ex with a non-NULL dval. It
// returns 0 when s is not numeric, numLong with the int, or numDouble with
// the float. With allowErrors, trailing garbage is accepted (the numeric
// prefix is used), as for arithmetic and (int) casts. oflow reports an
// integer string that overflowed int64 (1 or -1).
func isNumericString(s string, allowErrors bool) (typ int, l int64, d float64) {
	typ, l, d, _ = isNumericStringEx(s, allowErrors)
	return typ, l, d
}

func isNumericStringEx(s string, allowErrors bool) (typ int, l int64, d float64, oflow int) {
	if s == "" || s[0] > '9' {
		return 0, 0, 0, 0
	}
	str := 0
	for str < len(s) && isStrtodSpace(s[str]) {
		str++
	}
	at := func(i int) byte {
		if i < len(s) {
			return s[i]
		}
		return 0
	}
	ptr := str
	neg := false
	if at(ptr) == '-' {
		neg = true
		ptr++
	} else if at(ptr) == '+' {
		ptr++
	}

	const maxLengthOfLong = 20
	digits := 0
	dpOrE := 0
	var tmp uint64
	isDouble := false
	if isDigit(at(ptr)) {
		for at(ptr) == '0' {
			ptr++
		}
		for typ = numLong; ; digits, ptr = digits+1, ptr+1 {
			if digits >= maxLengthOfLong {
				// Too many digits for an int: re-parse as a double.
				oflow = 1
				if at(str) == '-' {
					oflow = -1
				}
				isDouble = true
				break
			}
			c := at(ptr)
			if isDigit(c) {
				tmp = tmp*10 + uint64(c-'0')
				continue
			} else if c == '.' && dpOrE < 1 {
				isDouble = true
				break
			} else if (c == 'e' || c == 'E') && dpOrE < 2 {
				e := ptr + 1
				if at(e) == '-' || at(e) == '+' {
					e++
				}
				if isDigit(at(e)) {
					isDouble = true
					break
				}
			}
			break
		}
	} else if at(ptr) == '.' && isDigit(at(ptr+1)) {
		isDouble = true
	} else {
		return 0, 0, 0, 0
	}

	if isDouble {
		typ = numDouble
		n := strtodPrefix(s[str:])
		d = strtod(s[str : str+n])
		ptr = str + n
	}

	if ptr != len(s) {
		end := ptr
		for end < len(s) && isStrtodSpace(s[end]) {
			end++
		}
		if end != len(s) && !allowErrors {
			return 0, 0, 0, 0
		}
	}

	if typ == numLong {
		if digits == maxLengthOfLong-1 {
			cmp := compareDigits(s[ptr-digits:], "9223372036854775808")
			if !(cmp < 0 || (cmp == 0 && at(str) == '-')) {
				oflow = 1
				if at(str) == '-' {
					oflow = -1
				}
				return numDouble, 0, strtod(s[str:]), oflow
			}
		}
		if neg {
			tmp = -tmp
		}
		return numLong, int64(tmp), 0, 0 //nolint:gosec // wraps like the C cast
	}
	return numDouble, 0, d, oflow
}

func compareDigits(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
