// Ports the array key handling of Zend/zend_hash.c (_zend_handle_numeric_str_ex)
// and Zend/zend_execute.c (array offset coercion).

package php

import (
	"math"
	"strconv"
)

const (
	kindInt  uint8 = iota
	kindStr        // string key
	kindDead       // deleted entry; never visible outside Array
)

// Key is a PHP array key: an int64 or a string that is not a canonical
// decimal integer. The zero Key is the int key 0. Keys are comparable, so
// they work as Go map keys.
type Key struct {
	s    string
	i    int64
	kind uint8
}

// IntKey returns the int key i.
func IntKey(i int64) Key { return Key{i: i} }

// StrKey returns the key PHP uses for the string s: an int key when s is a
// canonical decimal integer in int64 range ("123", "-5"), else a string key
// ("05", "1.5", "-0", " 1", "9223372036854775808").
func StrKey(s string) Key {
	if i, ok := numericKey(s); ok {
		return Key{i: i}
	}
	return Key{s: s, kind: kindStr}
}

// rawStrKey is a string key without integer coercion, used for object
// property names.
func rawStrKey(s string) Key { return Key{s: s, kind: kindStr} }

// ToKey coerces a value used as an array offset, as PHP does: int and
// numeric-string as above, nil to "", bool to 0 or 1, float truncated
// toward zero (0 when it is NaN or out of int64 range). A Key is returned
// unchanged. Arrays and objects are illegal offsets and panic.
func ToKey(v any) Key {
	switch v := v.(type) {
	case string:
		return StrKey(v)
	case int64:
		return Key{i: v}
	case int:
		return Key{i: int64(v)}
	case Key:
		return v
	case nil:
		return Key{kind: kindStr}
	case bool:
		if v {
			return Key{i: 1}
		}
		return Key{}
	case float64:
		return Key{i: dvalToLval(v)}
	default:
		panic("php: illegal offset type " + TypeName(v))
	}
}

// IsInt reports whether k is an int key.
func (k Key) IsInt() bool { return k.kind == kindInt }

// IsString reports whether k is a string key.
func (k Key) IsString() bool { return k.kind == kindStr }

// Int returns the int key, or 0 for a string key.
func (k Key) Int() int64 { return k.i }

// String returns the key as PHP converts it to a string ("5" for int 5).
func (k Key) String() string {
	if k.kind == kindInt {
		return strconv.FormatInt(k.i, 10)
	}
	return k.s
}

// Value returns the key as a PHP value (int64 or string), as foreach
// yields it.
func (k Key) Value() any {
	if k.kind == kindInt {
		return k.i
	}
	return k.s
}

// numericKey ports _zend_handle_numeric_str: a string is used as an int
// key only when it is the canonical decimal form of an int64.
func numericKey(s string) (int64, bool) {
	n := len(s)
	if n == 0 || n > 20 {
		return 0, false
	}
	p := 0
	if s[0] == '-' {
		p = 1
		if n == 1 {
			return 0, false
		}
	}
	if s[p] < '0' || s[p] > '9' || (s[p] == '0' && n > 1) || n-p > 19 {
		return 0, false
	}
	var u uint64
	for ; p < n; p++ {
		c := s[p]
		if c < '0' || c > '9' {
			return 0, false
		}
		u = u*10 + uint64(c-'0')
	}
	if s[0] == '-' {
		if u-1 > math.MaxInt64 {
			return 0, false
		}
		return int64(-u), true //nolint:gosec // two's complement negation, range checked above
	}
	if u > math.MaxInt64 {
		return 0, false
	}
	return int64(u), true
}

// dvalToLval ports zend_dval_to_lval: NaN and infinities become 0, other
// out of range values wrap modulo 2^64.
func dvalToLval(d float64) int64 {
	if math.IsNaN(d) || math.IsInf(d, 0) {
		return 0
	}
	if d >= -9223372036854775808.0 && d < 9223372036854775808.0 {
		return int64(d)
	}
	m := math.Mod(d, 18446744073709551616.0)
	if m < 0 {
		m += 18446744073709551616.0
	}
	return int64(uint64(m)) //nolint:gosec // wraps like the C cast
}
