// Ports Symfony\Component\Finder\Glob::toRegex (symfony/finder 5.4, as
// vendored by Composer 2.10.3), which BaseExcludeFilter uses.

package archiver

import "strings"

// globToRegex is Glob::toRegex($glob) with its defaults: strict leading
// dot, strict wildcard slash and '#' as the delimiter.
func globToRegex(glob string) string {
	const delimiter = "#"

	firstByte := true
	escaping := false
	inCurlies := 0

	var regex strings.Builder

	for i := 0; i < len(glob); i++ {
		car := glob[i : i+1]

		if firstByte && car != "." {
			regex.WriteString(`(?=[^\.])`)
		}

		firstByte = car == "/"

		if firstByte && i+2 < len(glob) && glob[i+1:i+3] == "**" && (i+3 == len(glob) || glob[i+3] == '/') {
			car = "[^/]++/"
			if i+3 == len(glob) {
				car += "?"
			}

			car = "/(?:" + `(?=[^\.])` + car + ")*"

			// $i += 2 + isset($glob[$i + 3])
			i += 2
			if i+1 < len(glob) {
				i++
			}
		}

		switch {
		case car == delimiter || car == "." || car == "(" || car == ")" || car == "|" || car == "+" || car == "^" || car == "$":
			regex.WriteString(`\` + car)
		case car == "*":
			regex.WriteString(pick(escaping, `\*`, "[^/]*"))
		case car == "?":
			regex.WriteString(pick(escaping, `\?`, "[^/]"))
		case car == "{":
			regex.WriteString(pick(escaping, `\{`, "("))

			if !escaping {
				inCurlies++
			}
		case car == "}" && inCurlies > 0:
			regex.WriteString(pick(escaping, "}", ")"))

			if !escaping {
				inCurlies--
			}
		case car == "," && inCurlies > 0:
			regex.WriteString(pick(escaping, ",", "|"))
		case car == `\`:
			if escaping {
				regex.WriteString(`\\`)
				escaping = false
			} else {
				escaping = true
			}

			continue
		default:
			regex.WriteString(car)
		}

		escaping = false
	}

	return delimiter + "^" + regex.String() + "$" + delimiter
}

// pick is PHP's `$cond ? $a : $b`.
func pick(cond bool, a, b string) string {
	if cond {
		return a
	}

	return b
}
