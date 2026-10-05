// Ports php_formatted_print() of ext/standard/formatted_print.c: PHP 8's
// sprintf().

package php

import (
	"math"
	"strconv"
	"strings"
)

// Sprintf ports sprintf($format, ...$args). A bad format is a
// *EngineError carrying PHP's ValueError or ArgumentCountError.
func Sprintf(format string, args ...any) (string, error) {
	if strings.IndexByte(format, '%') < 0 {
		return format, nil
	}
	var b strings.Builder
	b.Grow(len(format) + 16)
	currarg := 0
	maxMissing := -1
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' {
			b.WriteByte(c)

			continue
		}
		i++
		if i < len(format) && format[i] == '%' {
			b.WriteByte('%')

			continue
		}

		// argnum$
		argnum := -1
		if j := sprintfDigits(format, i); j > i && j < len(format) && format[j] == '$' {
			n, _ := strconv.Atoi(format[i:j])
			if n <= 0 {
				return "", valueError("Argument number specifier must be greater than zero and less than 2147483647")
			}
			argnum = n - 1
			i = j + 1
		}

		// flags
		left, alwaysSign := false, false
		padding := byte(' ')
	flags:
		for i < len(format) {
			switch format[i] {
			case '-':
				left = true
			case '+':
				alwaysSign = true
			case ' ', '0':
				padding = format[i]
			case '\'':
				if i+1 >= len(format) {
					return "", valueError("Missing padding character")
				}
				i++
				padding = format[i]
			default:
				break flags
			}
			i++
		}

		width := 0
		if j := sprintfDigits(format, i); j > i {
			width, _ = strconv.Atoi(format[i:j])
			i = j
		}
		precision, hasPrecision := 6, false
		if i < len(format) && format[i] == '.' {
			i++
			j := sprintfDigits(format, i)
			precision, _ = strconv.Atoi(format[i:j])
			if j == i {
				precision = 0
			}
			hasPrecision = true
			i = j
		}
		if i < len(format) && format[i] == 'l' {
			i++
		}
		if i >= len(format) {
			return "", valueError("Missing format specifier at end of string")
		}

		if argnum < 0 {
			argnum = currarg
			currarg++
		}
		if argnum >= len(args) {
			maxMissing = max(maxMissing, argnum)

			continue
		}
		arg := args[argnum]

		switch conv := format[i]; conv {
		case 's':
			s := ToString(arg)
			sprintfAppendString(&b, s, width, padding, left, false, false, hasPrecision, precision)
		case 'd':
			n := ToInt(arg)
			s := strconv.FormatInt(n, 10)
			if n >= 0 && alwaysSign {
				s = "+" + s
			}
			sprintfAppendString(&b, s, width, padding, left, n < 0, alwaysSign, false, 0)
		case 'u':
			sprintfAppendString(&b, strconv.FormatUint(uint64(ToInt(arg)), 10), width, padding, left, false, false, false, 0) //nolint:gosec // %u reinterprets the bits
		case 'e', 'E', 'f', 'F', 'g', 'G':
			f := ToFloat(arg)
			switch {
			case math.IsNaN(f):
				// PHP writes these without padding or sign.
				b.WriteString("NaN")
			case math.IsInf(f, 1):
				b.WriteString("INF")
			case math.IsInf(f, -1):
				b.WriteString("-INF")
			default:
				s := sprintfDouble(f, conv, precision, alwaysSign)
				sprintfAppendString(&b, s, width, padding, left, f < 0, alwaysSign, false, 0)
			}
		case 'c':
			b.WriteByte(byte(ToInt(arg))) //nolint:gosec // chr() keeps the low byte
		case 'o':
			sprintfAppendString(&b, strconv.FormatUint(uint64(ToInt(arg)), 8), width, padding, left, false, false, false, 0) //nolint:gosec // unsigned view
		case 'x':
			sprintfAppendString(&b, strconv.FormatUint(uint64(ToInt(arg)), 16), width, padding, left, false, false, false, 0) //nolint:gosec // unsigned view
		case 'X':
			sprintfAppendString(&b, strings.ToUpper(strconv.FormatUint(uint64(ToInt(arg)), 16)), width, padding, left, false, false, false, 0) //nolint:gosec // unsigned view
		case 'b':
			sprintfAppendString(&b, strconv.FormatUint(uint64(ToInt(arg)), 2), width, padding, left, false, false, false, 0) //nolint:gosec // unsigned view
		default:
			return "", valueError(`Unknown format specifier "` + string(conv) + `"`)
		}
	}

	if maxMissing >= 0 {
		return "", &EngineError{Class: "ArgumentCountError", Message: strconv.Itoa(maxMissing+2) + " arguments are required, " + strconv.Itoa(len(args)+1) + " given"}
	}

	return b.String(), nil
}

func sprintfDigits(s string, i int) int {
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}

	return i
}

// sprintfAppendString ports php_sprintf_appendstring().
func sprintfAppendString(b *strings.Builder, s string, minWidth int, padding byte, left, neg, alwaysSign, expprec bool, precision int) {
	copyLen := len(s)
	if expprec && precision < copyLen {
		copyLen = precision
	}
	npad := 0
	if minWidth >= copyLen {
		npad = minWidth - copyLen
	}
	if !left {
		if (neg || alwaysSign) && padding == '0' && copyLen > 0 {
			b.WriteByte(s[0])
			s = s[1:]
			copyLen--
		}
		for range npad {
			b.WriteByte(padding)
		}
	}
	b.WriteString(s[:copyLen])
	if left {
		for range npad {
			b.WriteByte(padding)
		}
	}
}

// sprintfDouble ports php_sprintf_appenddouble() for one conversion.
func sprintfDouble(f float64, conv byte, precision int, alwaysSign bool) string {
	precision = min(precision, 53)

	var s string
	switch conv {
	case 'f', 'F':
		s = strconv.FormatFloat(f, 'f', precision, 64)
	case 'e', 'E':
		s = sprintfExponent(strconv.FormatFloat(f, 'e', precision, 64), conv)
	default: // g, G
		if precision == 0 {
			precision = 1
		}
		s = strconv.FormatFloat(f, 'g', precision, 64)
		if strings.ContainsAny(s, "e") {
			e := byte('e')
			if conv == 'G' {
				e = 'E'
			}
			mant, exp, _ := strings.Cut(s, "e")
			if !strings.Contains(mant, ".") {
				mant += ".0"
			}
			s = sprintfExponent(mant+"e"+exp, e)
		}
	}
	if alwaysSign && f >= 0 {
		s = "+" + s
	}

	return s
}

// sprintfExponent rewrites Go's "1.5e+03" exponent as PHP's "1.5e+3".
func sprintfExponent(s string, e byte) string {
	mant, exp, ok := strings.Cut(s, "e")
	if !ok {
		return s
	}
	sign := exp[0]
	digits := strings.TrimLeft(exp[1:], "0")
	if digits == "" {
		digits = "0"
	}

	return mant + string(e) + string(sign) + digits
}
