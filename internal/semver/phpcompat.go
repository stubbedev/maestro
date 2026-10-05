// Ports the PHP runtime semantics composer/semver relies on, from php-src
// 8.4: trim()/strtolower() (ext/standard/string.c), PCRE2's default \s, \d
// and $ (non-UTF mode, LF newlines), numeric strings and loose string
// equality (Zend/zend_operators.c: _is_numeric_string_ex,
// zendi_smart_streq), integer/float addition and float-to-string conversion
// (zend_gcvt with precision=14), and usort() (Zend/zend_sort.c with
// ext/standard/array.c's stable fallback).

package semver

import (
	"math"
	"strconv"
	"strings"
)

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// isAlnum is isalnum() in the C locale.
func isAlnum(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isSpace is PCRE2's \s without UTF/UCP: space, \t, \n, \v, \f and \r.
func isSpace(c byte) bool { return c == ' ' || (c >= '\t' && c <= '\r') }

// phpTrim is trim() with its default characters " \t\n\r\0\x0B".
func phpTrim(s string) string {
	return strings.Trim(s, " \t\n\r\x00\x0b")
}

func toLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}

	return c
}

// asciiLower is strtolower(), which is ASCII-only since PHP 8.2.
func asciiLower(s string) string {
	for i := range len(s) {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				b[j] = toLower(b[j])
			}

			return string(b)
		}
	}

	return s
}

// hasPrefixLit reports whether s starts with the pattern literal lit, ASCII
// case-insensitively when ci is set (PCRE's /i without UTF).
func hasPrefixLit(s, lit string, ci bool) bool {
	if len(s) < len(lit) {
		return false
	}
	if !ci {
		return s[:len(lit)] == lit
	}
	for i := range len(lit) {
		if toLower(s[i]) != toLower(lit[i]) {
			return false
		}
	}

	return true
}

// atDollar reports whether PCRE's $ (no /m, no /D) matches at offset i: at
// the end of s or before a newline that ends it.
func atDollar(s string, i int) bool {
	return i == len(s) || (i == len(s)-1 && s[i] == '\n')
}

// dollarEnd returns where a match anchored with $ must end when nothing in
// the pattern can consume a newline: before a final "\n", or at the end.
func dollarEnd(s string) int {
	if s != "" && s[len(s)-1] == '\n' {
		return len(s) - 1
	}

	return len(s)
}

// phpTruthy reports whether a string is truthy in PHP: not "" and not "0".
func phpTruthy(s string) bool { return s != "" && s != "0" }

// phpSubstr is substr($s, $start) for a non-negative start.
func phpSubstr(s string, start int) string {
	if start >= len(s) {
		return ""
	}

	return s[start:]
}

// Numeric string types, as returned by _is_numeric_string_ex.
const (
	notNumeric = iota
	numericLong
	numericDouble
)

// maxLengthOfLong is MAX_LENGTH_OF_LONG on 64-bit platforms.
const maxLengthOfLong = 20

// isNumericNoErrors ports _is_numeric_string_ex(str, length, &lval, &dval,
// allow_errors=false, &oflow, NULL): leading and trailing whitespace are
// allowed, anything else makes the string non-numeric.
func isNumericNoErrors(s string) (typ int, lval int64, dval float64, oflow int) {
	if s == "" {
		return notNumeric, 0, 0, 0
	}
	// The C code reads past the length up to the NUL terminator.
	at := func(i int) byte {
		if i < len(s) {
			return s[i]
		}

		return 0
	}
	isWS := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f' }

	length := len(s)
	str := 0
	for isWS(at(str)) {
		str++
		length--
	}
	ptr := str
	neg := false
	if at(ptr) == '-' {
		neg = true
		ptr++
	} else if at(ptr) == '+' {
		ptr++
	}

	digits := 0
	var tmp uint64
	double := false
	switch {
	case isDigit(at(ptr)):
		for at(ptr) == '0' {
			ptr++
		}
		typ = numericLong
		for ; digits < maxLengthOfLong; digits, ptr = digits+1, ptr+1 {
			c := at(ptr)
			if isDigit(c) {
				tmp = tmp*10 + uint64(c-'0')

				continue
			}
			if c == '.' {
				double = true

				break
			}
			if c == 'e' || c == 'E' {
				e := ptr + 1
				if at(e) == '-' || at(e) == '+' {
					ptr = e
					e++
				}
				if isDigit(at(e)) {
					double = true
				}
			}

			break
		}
		if !double && digits >= maxLengthOfLong {
			if at(str) == '-' {
				oflow = -1
			} else {
				oflow = 1
			}
			double = true
		}
	case at(ptr) == '.' && isDigit(at(ptr+1)):
		double = true
	default:
		return notNumeric, 0, 0, 0
	}
	if double {
		typ = numericDouble
		dval, ptr = zendStrtod(s, str)
	}

	if ptr != str+length {
		for endptr := ptr; isWS(at(endptr)); endptr++ {
			length--
		}
		if ptr != str+length {
			return notNumeric, 0, 0, 0
		}
	}

	if typ == numericLong {
		if digits == maxLengthOfLong-1 {
			cmp := cStrcmp(s[ptr-digits:], "9223372036854775808")
			if cmp > 0 || (cmp == 0 && at(str) != '-') {
				dval, _ = zendStrtod(s, str)
				if at(str) == '-' {
					oflow = -1
				} else {
					oflow = 1
				}

				return numericDouble, 0, dval, oflow
			}
		}
		lval = int64(tmp) // two's complement wrap-around, as in C
		if neg {
			lval = -lval
		}

		return numericLong, lval, 0, oflow
	}

	return numericDouble, 0, dval, oflow
}

// cStrcmp is strcmp() on the C strings starting at a and b.
func cStrcmp(a, b string) int {
	a, b = cString(a), cString(b)

	return strings.Compare(a, b)
}

// zendStrtod parses the decimal number at s[from:] like zend_strtod(), and
// returns it with the offset just past it.
func zendStrtod(s string, from int) (float64, int) {
	i := from
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		i++
	}
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && isDigit(s[i]) {
			i++
		}
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
	text := s[from:i]
	text, _ = strings.CutSuffix(text, ".")
	f, _ := strconv.ParseFloat(text, 64) // out of range yields ±Inf like zend_strtod

	return f, i
}

// phpLooseEquals is PHP 8's $a == $b for two strings (zendi_smart_streq):
// numeric strings compare as numbers, anything else byte-wise.
func phpLooseEquals(s1, s2 string) bool {
	if s1 == s2 {
		return true
	}
	ret1, lval1, dval1, oflow1 := isNumericNoErrors(s1)
	if ret1 == notNumeric {
		return false
	}
	ret2, lval2, dval2, oflow2 := isNumericNoErrors(s2)
	if ret2 == notNumeric {
		return false
	}
	if oflow1 != 0 && oflow1 == oflow2 && dval1-dval2 == 0 {
		// Both overflowed to the same side: PHP falls back to comparing
		// the strings, which differ.
		return false
	}
	if ret1 == numericDouble || ret2 == numericDouble {
		switch {
		case ret1 != numericDouble:
			if oflow2 != 0 {
				return false
			}
			dval1 = float64(lval1)
		case ret2 != numericDouble:
			if oflow1 != 0 {
				return false
			}
			dval2 = float64(lval2)
		case dval1 == dval2 && (math.IsInf(dval1, 0) || math.IsNaN(dval1)):
			return false
		}

		return dval1 == dval2
	}

	return lval1 == lval2
}

// phpAddInt ports `$numericString + $increment` for the numeric strings
// manipulateVersionString() works on, returning the result as PHP would
// convert it back to a string, and whether it is negative. Integer overflow
// turns the result into a float, as in PHP.
func phpAddInt(s string, increment int) (string, bool) {
	typ, lval, dval, _ := isNumericNoErrors(s)
	if typ == numericLong {
		sum := lval + int64(increment)
		overflow := (increment > 0 && sum < lval) || (increment < 0 && sum > lval)
		if !overflow {
			return strconv.FormatInt(sum, 10), sum < 0
		}
		dval = float64(lval)
	}
	dval += float64(increment)

	return phpFloatToString(dval), dval < 0
}

// phpPrecision is PHP's default `precision` ini setting, used when a float
// is converted to a string.
const phpPrecision = 14

// phpFloatToString ports zend_double_to_str(): zend_gcvt(value, 14, '.',
// 'E').
func phpFloatToString(value float64) string {
	switch {
	case math.IsNaN(value):
		return "NAN"
	case math.IsInf(value, 1):
		return "INF"
	case math.IsInf(value, -1):
		return "-INF"
	}

	// zend_dtoa mode 2: the shortest digits that round correctly to
	// phpPrecision significant digits, without trailing zeros.
	e := strconv.FormatFloat(math.Abs(value), 'e', phpPrecision-1, 64)
	mantissa, exp, _ := strings.Cut(e, "e")
	digits := strings.TrimRight(strings.Replace(mantissa, ".", "", 1), "0")
	if digits == "" {
		digits = "0"
	}
	decpt, _ := strconv.Atoi(exp)
	decpt++
	if value == 0 {
		decpt = 1
	}

	var b strings.Builder
	if math.Signbit(value) {
		b.WriteByte('-')
	}
	switch {
	case (decpt >= 0 && decpt > phpPrecision) || decpt < -3:
		// Exponential format, e.g. 1.0E+25.
		decpt--
		expSign := byte('+')
		if decpt < 0 {
			expSign = '-'
			decpt = -decpt
		}
		b.WriteByte(digits[0])
		b.WriteByte('.')
		if len(digits) == 1 {
			b.WriteByte('0')
		} else {
			b.WriteString(digits[1:])
		}
		b.WriteByte('E')
		b.WriteByte(expSign)
		b.WriteString(strconv.Itoa(decpt))
	case decpt < 0:
		b.WriteString("0.")
		b.WriteString(strings.Repeat("0", -decpt))
		b.WriteString(digits)
	default:
		for i := range decpt {
			if i < len(digits) {
				b.WriteByte(digits[i])
			} else {
				b.WriteByte('0')
			}
		}
		if len(digits) > decpt {
			if decpt == 0 {
				b.WriteByte('0')
			}
			b.WriteByte('.')
			b.WriteString(digits[decpt:])
		}
	}

	return b.String()
}

// phpUsort sorts s in place exactly like PHP 8's usort(): zend_sort() with
// the comparison result normalised to -1/0/1 and ties broken by the
// original position. Using PHP's algorithm keeps the result identical even
// for comparators that are not a consistent ordering.
func phpUsort[T any](s []T, cmp func(a, b T) int) {
	if len(s) < 2 {
		return
	}
	order := make([]int, len(s))
	for i := range order {
		order[i] = i
	}
	zendSort(0, len(s), func(i, j int) int {
		if r := sign(cmp(s[i], s[j])); r != 0 {
			return r
		}

		return sign(order[i] - order[j])
	}, func(i, j int) {
		s[i], s[j] = s[j], s[i]
		order[i], order[j] = order[j], order[i]
	})
}

type (
	sortCmp  func(i, j int) int
	sortSwap func(i, j int)
)

func zendSort2(a, b int, cmp sortCmp, swp sortSwap) {
	if cmp(a, b) > 0 {
		swp(a, b)
	}
}

func zendSort3(a, b, c int, cmp sortCmp, swp sortSwap) {
	if cmp(a, b) <= 0 {
		if cmp(b, c) <= 0 {
			return
		}
		swp(b, c)
		if cmp(a, b) > 0 {
			swp(a, b)
		}

		return
	}
	if cmp(c, b) <= 0 {
		swp(a, c)

		return
	}
	swp(a, b)
	if cmp(b, c) > 0 {
		swp(b, c)
	}
}

func zendSort4(a, b, c, d int, cmp sortCmp, swp sortSwap) {
	zendSort3(a, b, c, cmp, swp)
	if cmp(c, d) > 0 {
		swp(c, d)
		if cmp(b, c) > 0 {
			swp(b, c)
			if cmp(a, b) > 0 {
				swp(a, b)
			}
		}
	}
}

func zendSort5(a, b, c, d, e int, cmp sortCmp, swp sortSwap) {
	zendSort4(a, b, c, d, cmp, swp)
	if cmp(d, e) > 0 {
		swp(d, e)
		if cmp(c, d) > 0 {
			swp(c, d)
			if cmp(b, c) > 0 {
				swp(b, c)
				if cmp(a, b) > 0 {
					swp(a, b)
				}
			}
		}
	}
}

// zendInsertSort ports zend_insert_sort() on the elements [start,
// start+nmemb).
func zendInsertSort(start, nmemb int, cmp sortCmp, swp sortSwap) {
	switch nmemb {
	case 0, 1:
		return
	case 2:
		zendSort2(start, start+1, cmp, swp)
	case 3:
		zendSort3(start, start+1, start+2, cmp, swp)
	case 4:
		zendSort4(start, start+1, start+2, start+3, cmp, swp)
	case 5:
		zendSort5(start, start+1, start+2, start+3, start+4, cmp, swp)
	default:
		end := start + nmemb
		sentry := start + 6
		for i := start + 1; i < sentry; i++ {
			j := i - 1
			if cmp(j, i) <= 0 {
				continue
			}
			for j != start {
				j--
				if cmp(j, i) <= 0 {
					j++

					break
				}
			}
			for k := i; k > j; k-- {
				swp(k, k-1)
			}
		}
		for i := sentry; i < end; i++ {
			j := i - 1
			if cmp(j, i) <= 0 {
				continue
			}
			for {
				j -= 2
				if cmp(j, i) <= 0 {
					j++
					if cmp(j, i) <= 0 {
						j++
					}

					break
				}
				if j == start {
					break
				}
				if j == start+1 {
					j--
					if cmp(i, j) > 0 {
						j++
					}

					break
				}
			}
			for k := i; k > j; k-- {
				swp(k, k-1)
			}
		}
	}
}

// zendSort ports zend_sort(), the hybrid insertion sort/quick sort behind
// PHP's sort functions, on the elements [base, base+nmemb).
func zendSort(base, nmemb int, cmp sortCmp, swp sortSwap) {
	for {
		if nmemb <= 16 {
			zendInsertSort(base, nmemb, cmp, swp)

			return
		}
		start := base
		end := start + nmemb
		offset := nmemb >> 1
		pivot := start + offset
		if nmemb>>10 != 0 {
			delta := offset >> 1
			zendSort5(start, start+delta, pivot, pivot+delta, end-1, cmp, swp)
		} else {
			zendSort3(start, pivot, end-1, cmp, swp)
		}
		swp(start+1, pivot)
		pivot = start + 1
		i := pivot + 1
		j := end - 1
	partition:
		for {
			for cmp(pivot, i) > 0 {
				i++
				if i == j {
					break partition
				}
			}
			j--
			if j == i {
				break partition
			}
			for cmp(j, pivot) > 0 {
				j--
				if j == i {
					break partition
				}
			}
			swp(i, j)
			i++
			if i == j {
				break partition
			}
		}
		swp(pivot, i-1)
		if (i-1)-start < end-i {
			zendSort(start, (i-start)-1, cmp, swp)
			base = i
			nmemb = end - i
		} else {
			zendSort(i, end-i, cmp, swp)
			nmemb = (i - start) - 1
		}
	}
}
