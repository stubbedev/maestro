// Ports zend_gcvt (Zend/zend_strtod.c), smart_str_append_double
// (Zend/zend_smart_str.c) and the prefix parsing of zend_strtod.

package php

import (
	"bytes"
	"errors"
	"math"
	"strconv"
)

// precision is PHP's default `precision` ini setting, used when a float is
// converted to a string.
const precision = 14

// appendGcvt ports zend_gcvt. ndigit < 0 means the shortest repr that
// round-trips (serialize_precision = -1); otherwise ndigit significant
// digits. expChar is 'e' (json_encode) or 'E' (var_export, string casts).
func appendGcvt(dst []byte, v float64, ndigit int, expChar byte) []byte {
	if math.IsNaN(v) {
		return append(dst, "NAN"...)
	}
	if math.IsInf(v, 0) {
		if v < 0 {
			dst = append(dst, '-')
		}
		return append(dst, "INF"...)
	}
	prec := -1
	if ndigit >= 0 {
		if ndigit == 0 {
			ndigit = 1
		}
		prec = ndigit - 1
	} else {
		ndigit = 17
	}

	var buf [40]byte
	b := strconv.AppendFloat(buf[:0], math.Abs(v), 'e', prec, 64)
	// b is d[.ddd]e±XX; split it into the digit string and the exponent.
	e := len(b) - 1
	for b[e] != 'e' {
		e--
	}
	exp := 0
	for _, c := range b[e+2:] {
		exp = exp*10 + int(c-'0')
	}
	if b[e+1] == '-' {
		exp = -exp
	}
	digits := b[:e]
	if len(digits) > 1 {
		// Drop the '.' after the first digit.
		copy(digits[1:], digits[2:])
		digits = digits[:len(digits)-1]
	}
	for len(digits) > 1 && digits[len(digits)-1] == '0' {
		digits = digits[:len(digits)-1]
	}
	decpt := exp + 1
	if digits[0] == '0' {
		decpt = 1
	}

	if math.Signbit(v) {
		dst = append(dst, '-')
	}
	switch {
	case decpt > ndigit || decpt < -3:
		// Exponential format, e.g. 1.2345e+13.
		dst = append(dst, digits[0], '.')
		if len(digits) == 1 {
			dst = append(dst, '0')
		} else {
			dst = append(dst, digits[1:]...)
		}
		dst = append(dst, expChar)
		decpt--
		if decpt < 0 {
			dst = append(dst, '-')
			decpt = -decpt
		} else {
			dst = append(dst, '+')
		}
		dst = strconv.AppendInt(dst, int64(decpt), 10)
	case decpt < 0:
		dst = append(dst, '0', '.')
		for ; decpt < 0; decpt++ {
			dst = append(dst, '0')
		}
		dst = append(dst, digits...)
	default:
		for i := range decpt {
			if i < len(digits) {
				dst = append(dst, digits[i])
			} else {
				dst = append(dst, '0')
			}
		}
		if decpt < len(digits) {
			if decpt == 0 {
				dst = append(dst, '0')
			}
			dst = append(dst, '.')
			dst = append(dst, digits[decpt:]...)
		}
	}
	return dst
}

// appendDouble ports smart_str_append_double: zend_gcvt with 'E', plus
// ".0" when zeroFrac is set, the value is finite and has no '.'.
func appendDouble(dst []byte, v float64, precision int, zeroFrac bool) []byte {
	start := len(dst)
	dst = appendGcvt(dst, v, precision, 'E')
	if zeroFrac && !math.IsInf(v, 0) && !math.IsNaN(v) && bytes.IndexByte(dst[start:], '.') < 0 {
		dst = append(dst, '.', '0')
	}
	return dst
}

// FloatToString converts a float to a string as PHP's (string) cast does
// (precision 14, e.g. "0.1", "1.0E+25", "-0").
func FloatToString(v float64) string {
	var buf [32]byte
	return string(appendGcvt(buf[:0], v, precision, 'E'))
}

// strtodPrefix returns the length of the longest prefix of s that
// zend_strtod consumes after optional leading whitespace and sign:
// digits, an optional fraction and an optional exponent. It returns 0 when
// there is no mantissa digit. Hex, "inf" and "nan" are not recognised,
// matching PHP's build of zend_strtod.
func strtodPrefix(s string) int {
	i := 0
	for i < len(s) && isStrtodSpace(s[i]) {
		i++
	}
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		i++
	}
	mant := 0
	for i < len(s) && isDigit(s[i]) {
		i++
		mant++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && isDigit(s[i]) {
			i++
			mant++
		}
	}
	if mant == 0 {
		return 0
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '-' || s[j] == '+') {
			j++
		}
		if j < len(s) && isDigit(s[j]) {
			for j < len(s) && isDigit(s[j]) {
				j++
			}
			i = j
		}
	}
	return i
}

// strtod ports zend_strtod(s, NULL): the value of the longest numeric
// prefix, 0 when there is none, ±Inf on overflow.
func strtod(s string) float64 {
	n := strtodPrefix(s)
	if n == 0 {
		return 0
	}
	t := s[:n]
	for t[0] != '-' && t[0] != '+' && !isDigit(t[0]) && t[0] != '.' {
		t = t[1:]
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0
	}
	return f
}

func isStrtodSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
