// Ports php_formatted_print() of ext/standard/formatted_print.c: PHP 8's
// sprintf(), used for ProgressBar placeholders ("%percent:3s%"), Table
// formats, ChoiceQuestion error messages and the synopsis rendered after an
// exception.

package console

import (
	"math"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// phpFormatError is the ValueError or ArgumentCountError sprintf() throws
// for a bad format.
type phpFormatError struct {
	class   string
	message string
}

func (e *phpFormatError) Error() string { return e.message }

// ThrowableClass implements Throwable.
func (e *phpFormatError) ThrowableClass() string { return e.class }

// ThrowableFile implements Throwable (the PHP caller's file is unknown).
func (e *phpFormatError) ThrowableFile() string { return "" }

// ThrowableLine implements Throwable.
func (e *phpFormatError) ThrowableLine() int { return 0 }

// ThrowableCode implements Throwable.
func (e *phpFormatError) ThrowableCode() int { return 0 }

// ThrowablePrevious implements Throwable.
func (e *phpFormatError) ThrowablePrevious() error { return nil }

func formatPanic(class, message string) { panic(&phpFormatError{class, message}) }

// phpSprintf formats args like PHP 8's sprintf(). A bad format panics with
// a *phpFormatError carrying PHP's ValueError or ArgumentCountError.
func phpSprintf(format string, args ...any) string {
	if strings.IndexByte(format, '%') < 0 {
		return format
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
		if j := scanDigits(format, i); j > i && j < len(format) && format[j] == '$' {
			n, _ := strconv.Atoi(format[i:j])
			if n <= 0 {
				formatPanic("ValueError", "Argument number specifier must be greater than zero and less than 2147483647")
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
					formatPanic("ValueError", "Missing padding character")
				}
				i++
				padding = format[i]
			default:
				break flags
			}
			i++
		}

		width := 0
		if j := scanDigits(format, i); j > i {
			width, _ = strconv.Atoi(format[i:j])
			i = j
		}
		precision, hasPrecision := 6, false
		if i < len(format) && format[i] == '.' {
			i++
			j := scanDigits(format, i)
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
			formatPanic("ValueError", "Missing format specifier at end of string")
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
			s := php.ToString(arg)
			appendPadded(&b, s, width, padding, left, false, false, hasPrecision, precision)
		case 'd':
			n := php.ToInt(arg)
			s := strconv.FormatInt(n, 10)
			if n >= 0 && alwaysSign {
				s = "+" + s
			}
			appendPadded(&b, s, width, padding, left, n < 0, alwaysSign, false, 0)
		case 'u':
			appendPadded(&b, strconv.FormatUint(uint64(php.ToInt(arg)), 10), width, padding, left, false, false, false, 0) //nolint:gosec // %u reinterprets the bits
		case 'e', 'E', 'f', 'F', 'g', 'G':
			f := php.ToFloat(arg)
			s := formatDouble(f, conv, precision, alwaysSign)
			appendPadded(&b, s, width, padding, left, f < 0, alwaysSign, false, 0)
		case 'c':
			b.WriteByte(byte(php.ToInt(arg))) //nolint:gosec // chr() keeps the low byte
		case 'o':
			appendPadded(&b, strconv.FormatUint(uint64(php.ToInt(arg)), 8), width, padding, left, false, false, false, 0) //nolint:gosec // unsigned view
		case 'x':
			appendPadded(&b, strconv.FormatUint(uint64(php.ToInt(arg)), 16), width, padding, left, false, false, false, 0) //nolint:gosec // unsigned view
		case 'X':
			appendPadded(&b, strings.ToUpper(strconv.FormatUint(uint64(php.ToInt(arg)), 16)), width, padding, left, false, false, false, 0) //nolint:gosec // unsigned view
		case 'b':
			appendPadded(&b, strconv.FormatUint(uint64(php.ToInt(arg)), 2), width, padding, left, false, false, false, 0) //nolint:gosec // unsigned view
		default:
			formatPanic("ValueError", `Unknown format specifier "`+string(conv)+`"`)
		}
	}

	if maxMissing >= 0 {
		formatPanic("ArgumentCountError", strconv.Itoa(maxMissing+2)+" arguments are required, "+strconv.Itoa(len(args)+1)+" given")
	}

	return b.String()
}

func scanDigits(s string, i int) int {
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}

	return i
}

// appendPadded ports php_sprintf_appendstring().
func appendPadded(b *strings.Builder, s string, minWidth int, padding byte, left, neg, alwaysSign, expprec bool, precision int) {
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

// formatDouble ports php_sprintf_appenddouble() for one conversion.
func formatDouble(f float64, conv byte, precision int, alwaysSign bool) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 0):
		s := "Inf"
		if f < 0 {
			s = "-Inf"
		} else if alwaysSign {
			s = "+Inf"
		}

		return s
	}
	precision = min(precision, 53)

	var s string
	switch conv {
	case 'f', 'F':
		s = strconv.FormatFloat(f, 'f', precision, 64)
	case 'e', 'E':
		s = phpExponent(strconv.FormatFloat(f, 'e', precision, 64), conv)
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
			s = phpExponent(mant+"e"+exp, e)
		}
	}
	if alwaysSign && f >= 0 {
		s = "+" + s
	}

	return s
}

// phpExponent rewrites Go's "1.5e+03" exponent as PHP's "1.5e+3".
func phpExponent(s string, e byte) string {
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
