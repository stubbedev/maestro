// The bridge from the console's input value model (nil, bool, string, int,
// float64, []string, []any) to internal/php, and thin adapters of
// internal/php functions to the console's error model.

package console

import (
	"fmt"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// phpValue converts an input value to the internal/php value model.
func phpValue(v any) any {
	switch x := v.(type) {
	case []string:
		return php.StringList(x)
	case []any:
		a := php.NewArrayCap(len(x))
		for _, e := range x {
			a.Append(phpValue(e))
		}

		return a
	}

	return v
}

// phpTruthy is PHP's (bool) cast of an input value.
func phpTruthy(v any) bool {
	switch x := v.(type) {
	case []string:
		return len(x) > 0
	case []any:
		return len(x) > 0
	}

	return php.ToBool(v)
}

// phpToString is PHP's (string) cast of an input value ("Array" for arrays,
// with PHP's "Array to string conversion" warning left out).
func phpToString(v any) string {
	switch v.(type) {
	case []string, []any:
		return "Array"
	}

	return php.ToString(v)
}

// phpLooseEqualsString is PHP 8's $v == $s for a string $s. Arrays never
// equal a string.
func phpLooseEqualsString(v any, s string) bool {
	switch v.(type) {
	case []string, []any:
		return false
	}

	return php.LooseEquals(v, s)
}

// phpIntval is PHP's (int) cast of a string.
func phpIntval(s string) int { return int(php.ToInt(s)) }

// strPadRight is str_pad($s, $length, ' '), which pads by bytes.
func strPadRight(s string, length int) string {
	if len(s) >= length {
		return s
	}

	return s + strings.Repeat(" ", length-len(s))
}

// jsonEncodeValue is json_encode($v, JSON_UNESCAPED_SLASHES |
// JSON_UNESCAPED_UNICODE) of an input value; "" where PHP returns false.
func jsonEncodeValue(v any) string {
	s, err := php.JSONEncode(phpValue(v), php.JSONUnescapedSlashes|php.JSONUnescapedUnicode)
	if err != nil {
		return ""
	}

	return s
}

// StripTags ports strip_tags() with no allowed tags.
func StripTags(s string) string { return php.StripTags(s) }

// escapeShellArg ports escapeshellarg() on Unix. Like PHP it rejects NUL
// bytes, by panicking with the ValueError.
func escapeShellArg(s string) string {
	quoted, err := php.Escapeshellarg(s)
	if err != nil {
		panic(newError(KindValueError, "Input.php", 195, "%s", err.Error()))
	}

	return quoted
}

func isCSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

// typeString approximates get_debug_type() for a Go value: its type name
// without the pointer star.
func typeString(v any) string { return strings.TrimPrefix(fmt.Sprintf("%T", v), "*") }
