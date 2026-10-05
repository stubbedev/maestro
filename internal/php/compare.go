// Ports PHP 8 comparison from Zend/zend_operators.c (zend_compare,
// zendi_smart_strcmp, compare_longs_to_string, zend_is_identical),
// Zend/zend_hash.c (zend_hash_compare) and stdClass comparison from
// Zend/zend_object_handlers.c.

package php

import (
	"math"
	"strconv"
	"strings"
)

// uncomparable is ZEND_UNCOMPARABLE.
const uncomparable = 1

// Compare returns $a <=> $b with PHP 8 semantics (-1, 0 or 1). It is the
// comparison sort() uses with SORT_REGULAR.
func Compare(a, b any) int {
	switch a := a.(type) {
	case int64:
		return compareInt(a, b)
	case int:
		return compareInt(int64(a), b)
	case string:
		switch b := b.(type) {
		case string:
			return smartStrcmp(a, b)
		case int64:
			return -compareLongToString(b, a)
		case int:
			return -compareLongToString(int64(b), a)
		case float64:
			if math.IsNaN(b) {
				return 1
			}
			return -compareDoubleToString(b, a)
		case nil:
			if a == "" {
				return 0
			}
			return 1
		}
	case float64:
		switch b := b.(type) {
		case float64:
			return threeway(a, b)
		case int64:
			return threeway(a, float64(b))
		case int:
			return threeway(a, float64(b))
		case string:
			if math.IsNaN(a) {
				return 1
			}
			return compareDoubleToString(a, b)
		}
	case nil:
		switch b := b.(type) {
		case nil:
			return 0
		case bool:
			if b {
				return -1
			}
			return 0
		case string:
			if b == "" {
				return 0
			}
			return -1
		case *Object:
			return -1
		}
	case bool:
		switch b := b.(type) {
		case bool:
			return boolCmp(a, b)
		case nil:
			if a {
				return 1
			}
			return 0
		}
	case *Array:
		if b, ok := b.(*Array); ok {
			return compareArrays(a, b)
		}
	case *Object:
		if b, ok := b.(*Object); ok {
			if a == b {
				return 0
			}
			return compareArrays(&a.props, &b.props)
		}
		if b == nil {
			return 1
		}
		return compareObjectTo(b, true)
	}
	if _, ok := b.(*Object); ok {
		return compareObjectTo(a, false)
	}
	return compareFallback(a, b)
}

func compareInt(a int64, b any) int {
	switch b := b.(type) {
	case int64:
		return cmpInt64(a, b)
	case int:
		return cmpInt64(a, int64(b))
	case float64:
		return threeway(float64(a), b)
	case string:
		return compareLongToString(a, b)
	}
	if _, ok := b.(*Object); ok {
		return compareObjectTo(a, false)
	}
	return compareFallback(a, b)
}

// compareFallback is the default branch of zend_compare: bool/null
// against anything via truthiness, then numbers, then arrays as greater.
func compareFallback(a, b any) int {
	_, aBool := a.(bool)
	_, bBool := b.(bool)
	switch {
	case a == nil || a == false:
		if ToBool(b) {
			return -1
		}
		return 0
	case aBool: // true
		if ToBool(b) {
			return 0
		}
		return 1
	case b == nil || b == false:
		if ToBool(a) {
			return 1
		}
		return 0
	case bBool: // true
		if ToBool(a) {
			return 0
		}
		return -1
	}
	_, aArr := a.(*Array)
	_, bArr := b.(*Array)
	if aArr {
		return 1
	}
	if bArr {
		return -1
	}
	// Two scalars that are not both handled above, e.g. after conversion.
	return Compare(toNumber(a), toNumber(b))
}

// toNumber ports _zendi_convert_scalar_to_number_silent.
func toNumber(v any) any {
	switch v := v.(type) {
	case string:
		typ, l, d := isNumericString(v, true)
		switch typ {
		case numLong:
			return l
		case numDouble:
			return d
		}
		return int64(0)
	case int:
		return int64(v)
	}
	return v
}

// compareObjectTo ports zend_std_compare_objects for a stdClass against a
// non-object: the object is cast to the other operand's type; stdClass
// casts only to bool (true); int and float casts fail with a notice and
// use 1; other casts fail and the object is greater.
func compareObjectTo(v any, objectLHS bool) int {
	var casted any
	switch v.(type) {
	case bool:
		casted = true
	case int64, int:
		casted = int64(1)
	case float64:
		casted = 1.0
	default:
		if objectLHS {
			return 1
		}
		return -1
	}
	if objectLHS {
		return Compare(casted, v)
	}
	return Compare(v, casted)
}

// compareArrays ports zend_hash_compare(..., ordered=0): count first, then
// each element of a looked up by key in b.
func compareArrays(a, b *Array) int {
	if a == b {
		return 0
	}
	if a.live != b.live {
		if a.live > b.live {
			return 1
		}
		return -1
	}
	for k, va := range a.All() {
		vb, ok := b.GetKey(k)
		if !ok {
			return uncomparable
		}
		if r := Compare(va, vb); r != 0 {
			return r
		}
	}
	return 0
}

// LooseEquals reports $a == $b.
func LooseEquals(a, b any) bool { return Compare(a, b) == 0 }

// StrictEquals reports $a === $b: same type and value; arrays need the
// same key/value pairs in the same order (values compared with ===);
// objects must be the same instance.
func StrictEquals(a, b any) bool {
	switch a := a.(type) {
	case nil:
		return b == nil
	case bool:
		bb, ok := b.(bool)
		return ok && a == bb
	case int64:
		return isInt(b, a)
	case int:
		return isInt(b, int64(a))
	case float64:
		bf, ok := b.(float64)
		return ok && a == bf
	case string:
		bs, ok := b.(string)
		return ok && a == bs
	case *Array:
		ba, ok := b.(*Array)
		return ok && identicalArrays(a, ba)
	case *Object:
		bo, ok := b.(*Object)
		return ok && a == bo
	}
	return false
}

func isInt(b any, a int64) bool {
	switch b := b.(type) {
	case int64:
		return a == b
	case int:
		return a == int64(b)
	}
	return false
}

func identicalArrays(a, b *Array) bool {
	if a == b {
		return true
	}
	if a.live != b.live {
		return false
	}
	j := 0
	for i := range a.entries {
		ea := &a.entries[i]
		if ea.k.kind == kindDead {
			continue
		}
		for b.entries[j].k.kind == kindDead {
			j++
		}
		eb := &b.entries[j]
		j++
		if ea.k != eb.k || !StrictEquals(ea.v, eb.v) {
			return false
		}
	}
	return true
}

func cmpInt64(a, b int64) int {
	switch {
	case a > b:
		return 1
	case a < b:
		return -1
	}
	return 0
}

// threeway is ZEND_THREEWAY_COMPARE for doubles: NaN compares as 1.
func threeway(a, b float64) int {
	switch {
	case a == b:
		return 0
	case a < b:
		return -1
	}
	return 1
}

func boolCmp(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

// normalizeSign is ZEND_NORMALIZE_BOOL for a float difference.
func normalizeSign(d float64) int {
	switch {
	case d > 0:
		return 1
	case d < 0:
		return -1
	}
	return 0
}

// binaryStrcmp is zend_binary_strcmp normalized to -1, 0, 1.
func binaryStrcmp(a, b string) int { return strings.Compare(a, b) }

func compareLongToString(l int64, s string) int {
	typ, sl, sd := isNumericString(s, false)
	switch typ {
	case numLong:
		return cmpInt64(l, sl)
	case numDouble:
		return threeway(float64(l), sd)
	}
	return binaryStrcmp(strconv.FormatInt(l, 10), s)
}

func compareDoubleToString(d float64, s string) int {
	typ, sl, sd := isNumericString(s, false)
	switch typ {
	case numLong:
		return threeway(d, float64(sl))
	case numDouble:
		return threeway(d, sd)
	}
	return binaryStrcmp(FloatToString(d), s)
}

// smartStrcmp ports zendi_smart_strcmp: two numeric strings compare as
// numbers, anything else byte-wise.
func smartStrcmp(s1, s2 string) int {
	if s1 == s2 {
		return 0
	}
	ret1, l1, d1, of1 := isNumericStringEx(s1, false)
	if ret1 == 0 {
		return binaryStrcmp(s1, s2)
	}
	ret2, l2, d2, of2 := isNumericStringEx(s2, false)
	if ret2 == 0 {
		return binaryStrcmp(s1, s2)
	}
	if of1 != 0 && of1 == of2 && d1-d2 == 0 {
		return binaryStrcmp(s1, s2)
	}
	if ret1 == numDouble || ret2 == numDouble {
		switch {
		case ret1 != numDouble:
			if of2 != 0 {
				return -of2
			}
			d1 = float64(l1)
		case ret2 != numDouble:
			if of1 != 0 {
				return of1
			}
			d2 = float64(l2)
		case d1 == d2 && (math.IsInf(d1, 0) || math.IsNaN(d1)):
			return binaryStrcmp(s1, s2)
		}
		return normalizeSign(d1 - d2)
	}
	return cmpInt64(l1, l2)
}
