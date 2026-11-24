package main

import (
	"slices"
	"strings"
)

// call is a call a line of PHP makes: the function or method name and its
// first string literal argument as written, quotes included ("" when it
// has none).
type call struct {
	name string
	arg  string
}

func (c call) String() string {
	return c.name + "(" + c.arg + ")"
}

// site is a call found on a line: the call and the depth of parentheses
// it is made at.
type site struct {
	call

	depth int
}

// notCalls are the keywords that take parentheses without calling
// anything.
var notCalls = []string{"if", "elseif", "while", "for", "foreach", "switch", "match", "catch", "function", "fn", "use", "array", "list"}

// annotatedCall returns the call the code of a line stands for: the last
// one made outside the parentheses of any other.
func annotatedCall(code string) (call, bool) {
	sites := calls(code)
	if len(sites) == 0 {
		return call{}, false
	}
	best := sites[0]
	for _, s := range sites[1:] {
		if s.depth <= best.depth {
			best = s
		}
	}

	return best.call, true
}

// makes reports whether the line of PHP makes call c.
func makes(line string, c call) bool {
	return slices.ContainsFunc(calls(line), func(s site) bool { return s.call == c })
}

// calls lexes one line of PHP and returns the calls it makes, in order:
// a name followed by an opening parenthesis, not a variable or a keyword
// of notCalls, with the first string literal directly in its parentheses.
// A comment ends the line; an unterminated string runs to its end.
func calls(line string) []site {
	var sites []site
	var open []int
	name := ""
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\'' || c == '"':
			j := endOfString(line, i)
			if len(open) > 0 && open[len(open)-1] >= 0 && sites[open[len(open)-1]].arg == "" {
				sites[open[len(open)-1]].arg = line[i:j]
			}
			i = j - 1
			name = ""
		case c == '#' || c == '/' && i+1 < len(line) && line[i+1] == '/':
			return sites
		case c == '/' && i+1 < len(line) && line[i+1] == '*':
			j := strings.Index(line[i+2:], "*/")
			if j < 0 {
				return sites
			}
			i += j + 3
			name = ""
		case isNameStart(c):
			j := i + 1
			for j < len(line) && isNameByte(line[j]) {
				j++
			}
			name = ""
			if i == 0 || line[i-1] != '$' {
				name = line[i:j]
			}
			i = j - 1
		case c == ' ' || c == '\t':
		case c == '(':
			if name == "" || slices.Contains(notCalls, name) {
				open = append(open, -1)
			} else {
				open = append(open, len(sites))
				sites = append(sites, site{name: name, depth: len(open) - 1})
			}
			name = ""
		case c == ')':
			if len(open) > 0 {
				open = open[:len(open)-1]
			}
			name = ""
		default:
			name = ""
		}
	}

	return sites
}

// endOfString returns the index just past the string literal opening at
// i, or the end of line when it is not closed there.
func endOfString(line string, i int) int {
	quote := line[i]
	for j := i + 1; j < len(line); j++ {
		switch line[j] {
		case '\\':
			j++
		case quote:
			return j + 1
		}
	}

	return len(line)
}

func isNameStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isNameByte(c byte) bool {
	return isNameStart(c) || c >= '0' && c <= '9'
}
