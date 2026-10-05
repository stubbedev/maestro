// Ports src/Composer/Json/JsonFormatter.php.

package json

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/maestro/internal/php"
)

// The pattern is '/(\\\\+)u([0-9a-f]{4})/i' in PHP source: one or more
// backslashes, a "u" and four hex digits.
var escapedCodeUnit = php.MustCompile(`/(\\+)u([0-9a-f]{4})/i`)

// Format ports JsonFormatter::format: pretty prints json (with four space
// indentation), optionally unescaping unicode escapes and slashes in its
// strings. Composer deprecates it in favour of json_encode flags. The error
// is str_repeat()'s ValueError, for input closing more brackets than it
// opens.
func Format(json string, unescapeUnicode, unescapeSlashes bool) (string, error) {
	var result strings.Builder
	result.Grow(len(json) + len(json)/2)
	pos := 0
	outOfQuotes := true
	var buffer strings.Builder
	noescape := true

	for i := range len(json) {
		// Grab the next character in the string
		char := json[i]

		// Are we inside a quoted string?
		if char == '"' && noescape {
			outOfQuotes = !outOfQuotes
		}

		if !outOfQuotes {
			buffer.WriteByte(char)
			if char == '\\' {
				noescape = !noescape
			} else {
				noescape = true
			}

			continue
		}
		if buffer.Len() > 0 {
			result.WriteString(unescapeBuffer(buffer.String(), unescapeUnicode, unescapeSlashes))
			result.WriteByte(char)
			buffer.Reset()

			continue
		}

		switch char {
		case ':':
			// Add a space after the : character
			result.WriteString(": ")

			continue
		case '}', ']':
			pos--
			// substr($json, $i - 1, 1): the last character when $i is 0
			prevChar := json[(i+len(json)-1)%len(json)]

			if prevChar != '{' && prevChar != '[' {
				// If this character is the end of an element,
				// output a new line and indent the next line
				result.WriteByte('\n')
				if pos < 0 {
					return "", errNegativeRepeat
				}
				writeIndent(&result, pos)
			} else {
				// Collapse empty {} and []
				trimmed := php.Rtrim(result.String())
				result.Reset()
				result.WriteString(trimmed)
			}
		}

		result.WriteByte(char)

		// If the last character was the beginning of an element,
		// output a new line and indent the next line
		if char == ',' || char == '{' || char == '[' {
			result.WriteByte('\n')

			if char == '{' || char == '[' {
				pos++
			}

			if pos < 0 {
				return "", errNegativeRepeat
			}
			writeIndent(&result, pos)
		}
	}

	return result.String(), nil
}

var errNegativeRepeat = &php.EngineError{Class: "ValueError", Message: "str_repeat(): Argument #2 ($times) must be greater than or equal to 0"}

// writeIndent is str_repeat('    ', $pos) for $pos >= 0.
func writeIndent(b *strings.Builder, pos int) {
	for range pos {
		b.WriteString("    ")
	}
}

func unescapeBuffer(buffer string, unescapeUnicode, unescapeSlashes bool) string {
	if unescapeSlashes {
		buffer = strings.ReplaceAll(buffer, `\/`, "/")
	}

	if !unescapeUnicode {
		return buffer
	}

	// https://stackoverflow.com/questions/2934563/how-to-decode-unicode-escape-sequences-like-u00ed-to-proper-utf-8-encoded-cha
	out, _, err := escapedCodeUnit.ReplaceCallback(buffer, func(m *php.Match) string {
		l := len(m.Get(1))

		if l%2 == 1 {
			code, _ := strconv.ParseUint(m.Get(2), 16, 16)
			// 0xD800..0xDFFF denotes UTF-16 surrogate pair which won't be unescaped
			// see https://github.com/composer/composer/issues/7510
			if code >= 0xD800 && code <= 0xDFFF {
				return m.Get(0)
			}

			return strings.Repeat(`\`, l-1) + string(utf8.AppendRune(nil, rune(code)))
		}

		return m.Get(0)
	}, -1)
	if err != nil {
		return buffer
	}

	return out
}
