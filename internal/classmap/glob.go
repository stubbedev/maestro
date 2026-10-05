// Ports the glob() call of Symfony Finder::in():
//
//	glob($dir, GLOB_BRACE | GLOB_ONLYDIR | GLOB_NOSORT) followed by sort()
//
// that is PHP's glob() (ext/standard/dir.c) over glibc's glob(3) and
// fnmatch(3) in the C locale.

package classmap

import (
	"os"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// phpGlobDirs returns the directories pattern matches, sorted as sort()
// sorts strings.
func phpGlobDirs(pattern string) []string {
	var matches []string
	for _, p := range braceExpand(pattern) {
		matches = globPattern(matches, p)
	}
	// PHP checks GLOB_ONLYDIR itself, as glibc only takes it as a hint.
	dirs := matches[:0]
	for _, m := range matches {
		if isDirectory(m) {
			dirs = append(dirs, m)
		}
	}
	php.SortSlice(dirs, func(a, b string) int { return php.Compare(a, b) })

	return dirs
}

// braceExpand is GLOB_BRACE: the patterns "{a,b}" stands for, in order. An
// unbalanced brace disables the expansion.
func braceExpand(pattern string) []string {
	open := -1
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '\\' {
			i++
		} else if pattern[i] == '{' {
			open = i

			break
		}
	}
	if open < 0 {
		return []string{pattern}
	}
	// Find the alternatives and the closing brace.
	var alts []string
	depth, start := 0, open+1
	closing := -1
	for i := open + 1; i < len(pattern) && closing < 0; i++ {
		switch pattern[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			} else {
				alts = append(alts, pattern[start:i])
				closing = i
			}
		case ',':
			if depth == 0 {
				alts = append(alts, pattern[start:i])
				start = i + 1
			}
		}
	}
	if closing < 0 {
		return []string{pattern}
	}
	var out []string
	for _, alt := range alts {
		out = append(out, braceExpand(pattern[:open]+alt+pattern[closing+1:])...)
	}

	return out
}

// globPattern appends the paths a brace-free pattern matches.
func globPattern(dst []string, pattern string) []string {
	if pattern == "" {
		return dst
	}
	slash := strings.LastIndexByte(pattern, '/')
	if slash < 0 {
		return globInDir(dst, ".", "", pattern)
	}
	dirPattern, filePattern := pattern[:slash], pattern[slash+1:]
	if dirPattern == "" {
		dirPattern = "/"
	}
	if filePattern == "" {
		// "dir/" matches the directory itself, with the slash.
		if !hasGlobMagic(dirPattern) {
			if isDirectory(unescapeGlob(dirPattern)) {
				return append(dst, unescapeGlob(dirPattern)+"/")
			}

			return dst
		}
		for _, d := range globPattern(nil, dirPattern) {
			if isDirectory(d) {
				dst = append(dst, d+"/")
			}
		}

		return dst
	}
	if !hasGlobMagic(dirPattern) {
		dir := unescapeGlob(dirPattern)

		return globInDir(dst, dir, dir, filePattern)
	}
	for _, dir := range globPattern(nil, dirPattern) {
		if isDirectory(dir) {
			dst = globInDir(dst, dir, dir, filePattern)
		}
	}

	return dst
}

// globInDir appends the entries of dir matching the file name pattern,
// prefixed with prefix and a slash (none when prefix is "").
func globInDir(dst []string, dir, prefix, pattern string) []string {
	join := func(name string) string {
		switch prefix {
		case "":
			return name
		case "/":
			return "/" + name
		}

		return prefix + "/" + name
	}
	if !hasGlobMagic(pattern) {
		name := unescapeGlob(pattern)
		if _, err := os.Lstat(join(name)); err == nil {
			dst = append(dst, join(name))
		}

		return dst
	}
	f, err := os.Open(dir)
	if err != nil {
		return dst
	}
	names, _ := f.Readdirnames(-1)
	_ = f.Close()
	// readdir() also returns the dot entries Go leaves out.
	for _, name := range append([]string{".", ".."}, names...) {
		if fnmatch(pattern, name) {
			dst = append(dst, join(name))
		}
	}

	return dst
}

// hasGlobMagic is glibc's __glob_pattern_p: whether the pattern has a
// wildcard, a bracket expression counting only when it is closed.
func hasGlobMagic(pattern string) bool {
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '?', '*':
			return true
		case '\\':
			i++
		case '[':
			if strings.IndexByte(pattern[i+1:], ']') >= 0 {
				return true
			}
		}
	}

	return false
}

// unescapeGlob removes the backslashes quoting characters of a pattern
// without wildcards.
func unescapeGlob(pattern string) string {
	if !strings.Contains(pattern, `\`) {
		return pattern
	}
	b := make([]byte, 0, len(pattern))
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '\\' && i+1 < len(pattern) {
			i++
		}
		b = append(b, pattern[i])
	}

	return string(b)
}

// fnmatch is fnmatch(pattern, name, FNM_PERIOD) in the C locale: a leading
// dot must be matched literally.
func fnmatch(pattern, name string) bool {
	if name != "" && name[0] == '.' && (pattern == "" || pattern[0] != '.') {
		if pattern == "" || pattern[0] != '\\' || len(pattern) < 2 || pattern[1] != '.' {
			return false
		}
	}

	return fnmatchAt(pattern, name)
}

func fnmatchAt(p, s string) bool {
	for len(p) > 0 {
		switch p[0] {
		case '*':
			for len(p) > 0 && p[0] == '*' {
				p = p[1:]
			}
			if p == "" {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if fnmatchAt(p, s[i:]) {
					return true
				}
			}

			return false
		case '?':
			if s == "" {
				return false
			}
			p, s = p[1:], s[1:]
		case '[':
			if s == "" {
				return false
			}
			ok, n := matchBracket(p, s[0])
			if n == 0 {
				// Not a bracket expression: a literal '['.
				if s[0] != '[' {
					return false
				}
				p, s = p[1:], s[1:]

				continue
			}
			if !ok {
				return false
			}
			p, s = p[n:], s[1:]
		case '\\':
			if len(p) == 1 {
				return false // a trailing backslash never matches
			}
			p = p[1:]

			fallthrough
		default:
			if s == "" || s[0] != p[0] {
				return false
			}
			p, s = p[1:], s[1:]
		}
	}

	return s == ""
}

// matchBracket matches c against the bracket expression at the start of p
// and returns the result and the expression's length, 0 if it is not
// closed.
func matchBracket(p string, c byte) (bool, int) {
	i := 1
	negate := false
	if i < len(p) && (p[i] == '!' || p[i] == '^') {
		negate = true
		i++
	}
	matched := false
	first := true
	for {
		if i >= len(p) {
			return false, 0
		}
		ch := p[i]
		if ch == ']' && !first {
			return matched != negate, i + 1
		}
		first = false
		if ch == '[' && i+1 < len(p) && p[i+1] == ':' {
			if end := strings.Index(p[i+2:], ":]"); end >= 0 {
				if charClass(p[i+2:i+2+end], c) {
					matched = true
				}
				i += 2 + end + 2

				continue
			}
		}
		if ch == '\\' && i+1 < len(p) {
			i++
			ch = p[i]
		}
		lo := ch
		i++
		if i+1 < len(p) && p[i] == '-' && p[i+1] != ']' {
			hi := p[i+1]
			if hi == '\\' && i+2 < len(p) {
				hi = p[i+2]
				i++
			}
			i += 2
			if lo <= c && c <= hi {
				matched = true
			}

			continue
		}
		if c == lo {
			matched = true
		}
	}
}

// charClass matches c against a POSIX character class in the C locale.
func charClass(name string, c byte) bool {
	lower := c >= 'a' && c <= 'z'
	upper := c >= 'A' && c <= 'Z'
	digit := c >= '0' && c <= '9'
	switch name {
	case "alpha":
		return lower || upper
	case "digit":
		return digit
	case "alnum":
		return lower || upper || digit
	case "lower":
		return lower
	case "upper":
		return upper
	case "space":
		return c == ' ' || c >= '\t' && c <= '\r'
	case "blank":
		return c == ' ' || c == '\t'
	case "punct":
		return c > ' ' && c < 0x7f && !lower && !upper && !digit
	case "print":
		return c >= ' ' && c < 0x7f
	case "graph":
		return c > ' ' && c < 0x7f
	case "cntrl":
		return c < ' ' || c == 0x7f
	case "xdigit":
		return digit || c|0x20 >= 'a' && c|0x20 <= 'f'
	}

	return false
}
