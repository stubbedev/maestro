// Ports from Zend/zend_operators.c: add_function (with
// zendi_try_convert_scalar_to_number and fast_long_add_function) and
// zendi_smart_streq, the == of two strings.

package php

// StringsLooseEqual is PHP 8's $a == $b for two strings
// (zendi_smart_streq): numeric strings compare as numbers, anything else
// byte-wise. It equals LooseEquals(a, b) without boxing the strings.
func StringsLooseEqual(s1, s2 string) bool {
	if s1 == s2 {
		return true
	}
	ret1, l1, d1, of1 := isNumericStringEx(s1, false)
	if ret1 == 0 {
		return false
	}
	ret2, l2, d2, of2 := isNumericStringEx(s2, false)
	if ret2 == 0 {
		return false
	}
	if of1 != 0 && of1 == of2 && d1-d2 == 0 {
		// Both overflowed to the same side: PHP falls back to comparing
		// the strings, which differ.
		return false
	}
	if ret1 == numDouble || ret2 == numDouble {
		switch {
		case ret1 != numDouble:
			if of2 != 0 {
				return false
			}
			d1 = float64(l1)
		case ret2 != numDouble:
			if of1 != 0 {
				return false
			}
			d2 = float64(l2)
		}
		// Equal infinities (and NaN, which cannot occur) fall back to the
		// strings in PHP, which differ.
		return d1 == d2 && d1-d2 == 0
	}
	return l1 == l2
}

// Add ports $a + $b: int + int is an int unless it overflows, then a
// float; any float operand makes a float; null and bools count as 0 and
// 0/1; numeric strings (including leading-numeric ones, for which PHP only
// warns) are converted; two arrays are their union. Anything else is a
// *EngineError carrying PHP's TypeError.
func Add(a, b any) (any, error) {
	if x, ok := a.(*Array); ok {
		if y, ok := b.(*Array); ok {
			r := x.Clone()
			for k, v := range y.All() {
				if _, ok := r.GetKey(k); !ok {
					r.SetKey(k, v)
				}
			}
			return r, nil
		}
	}
	x, okA := addOperand(a)
	y, okB := addOperand(b)
	if !okA || !okB {
		return nil, &EngineError{Class: "TypeError", Message: "Unsupported operand types: " + TypeName(a) + " + " + TypeName(b)}
	}
	if xi, ok := x.(int64); ok {
		if yi, ok := y.(int64); ok {
			sum := xi + yi
			if (sum > xi) == (yi > 0) {
				return sum, nil
			}
			return float64(xi) + float64(yi), nil
		}
	}
	return addFloat(x) + addFloat(y), nil
}

// addOperand converts an operand of + to an int64 or a float64, as
// zendi_try_convert_scalar_to_number does.
func addOperand(v any) (any, bool) {
	switch v := v.(type) {
	case int64, float64:
		return v, true
	case int:
		return int64(v), true
	case nil:
		return int64(0), true
	case bool:
		if v {
			return int64(1), true
		}
		return int64(0), true
	case string:
		switch typ, l, d := isNumericString(v, true); typ {
		case numLong:
			return l, true
		case numDouble:
			return d, true
		}
	}
	return nil, false
}

func addFloat(v any) float64 {
	switch v := v.(type) {
	case int64:
		return float64(v)
	case float64:
		return v
	}
	panic("unreachable")
}
