// Ports the type conversions of Zend/zend_operators.c (zval_get_long,
// zval_get_double, zval_get_string, zend_is_true, _is_numeric_string_ex)
// and the type names of Zend/zend_API.c.

package php

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Opaque is a value of another package that an Array or Object holds as
// is: the plugin runtime's objects (docs/PLUGINS.md §6.4), which stand
// for PHP objects other than stdClass. It is copied by reference, as PHP
// copies objects; the functions here do not look into it.
type Opaque interface {
	PHPOpaque()
}

// Classer is implemented by a Go value that stands for a PHP object:
// PHPClass is get_class() of that object.
type Classer interface {
	PHPClass() string
}

// normalize validates a value stored into an Array or Object, converting
// a Go int to int64.
func normalize(v any) any {
	switch v := v.(type) {
	case nil, bool, int64, float64, string, *Array, *Object, Opaque:
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

// ZvalValueName is zend_zval_value_name(), the type TypeErrors name ("X
// given", "X returned"): get_debug_type, except that booleans are "true"
// and "false".
func ZvalValueName(v any) string {
	if b, ok := v.(bool); ok {
		if b {
			return "true"
		}

		return "false"
	}

	return TypeName(v)
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

// Truthy is (bool) $s for a string: neither "" nor "0".
func Truthy(s string) bool { return s != "" && s != "0" }

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

// ToNativeInt is ToInt as a Go int. PHP's int is 64 bits on every platform
// maestro is built for, Go's int only 32 on some (386, arm): values beyond
// int's range saturate there instead of wrapping.
func ToNativeInt(v any) int {
	i := ToInt(v)
	switch {
	case i > math.MaxInt:
		return math.MaxInt
	case i < math.MinInt:
		return math.MinInt
	}

	return int(i)
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

// dvalToLvalCap ports zend_dval_to_lval_cap, used for numeric strings:
// infinities and NaN are 0, other out of range values saturate.
func dvalToLvalCap(d float64) int64 {
	switch {
	case math.IsNaN(d) || math.IsInf(d, 0):
		return 0
	case d >= -9223372036854775808.0 && d < 9223372036854775808.0:
		return int64(d)
	case d > 0:
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
	// at mimics reading the C string, NUL terminated.
	at := func(i int) byte {
		if i < len(s) {
			return s[i]
		}
		return 0
	}
	str := 0
	for isStrtodSpace(at(str)) {
		str++
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
	var tmp uint64
	isDouble := false
	switch {
	case isDigit(at(ptr)):
		for at(ptr) == '0' {
			ptr++
		}
		typ = numLong
		for ; digits < maxLengthOfLong; digits, ptr = digits+1, ptr+1 {
			c := at(ptr)
			if isDigit(c) {
				tmp = tmp*10 + uint64(c-'0')
				continue
			}
			switch c {
			case '.':
				isDouble = true
			case 'e', 'E':
				e := ptr + 1
				if at(e) == '-' || at(e) == '+' {
					// PHP leaves ptr on the sign (ptr = e++), which shifts
					// the overflow check below.
					ptr = e
					e++
				}
				isDouble = isDigit(at(e))
			}
			break
		}
		if digits >= maxLengthOfLong {
			oflow = 1
			if at(str) == '-' {
				oflow = -1
			}
			isDouble = true
		}
	case at(ptr) == '.' && isDigit(at(ptr+1)):
		isDouble = true
	default:
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
		for isStrtodSpace(at(end)) {
			end++
		}
		if end != len(s) && !allowErrors {
			return 0, 0, 0, 0
		}
	}

	if typ == numLong {
		if digits == maxLengthOfLong-1 {
			// strcmp against the rest of the C string, trailing bytes
			// included.
			cmp := strings.Compare(s[ptr-digits:], "9223372036854775808")
			if cmp > 0 || cmp == 0 && at(str) != '-' {
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
		return numLong, int64(tmp), 0, 0
	}
	return numDouble, 0, d, oflow
}
