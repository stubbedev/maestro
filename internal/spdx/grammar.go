// Ports the license expression regex of SpdxLicenses::isValidLicenseString
// (src/SpdxLicenses.php) as an exact recogniser.
//
// The PHP regex, with the x and i modifiers and without u, is:
//
//	idstring            [\pL\pN.-]{1,}
//	licenseid           any license identifier (lowercased, preg_quoted)
//	licenseexceptionid  any exception identifier
//	licenseref          (?:DocumentRef-(?&idstring):)?LicenseRef-(?&idstring)
//	simple_expression   (?&licenseid)\+? | (?&licenseid) | (?&licenseref)
//	compound_head       (?&simple_expression) ( \s+ WITH \s+ (?&licenseexceptionid))?
//	                    | \( \s* (?&compound_expression) \s* \)
//	compound_expression (?&compound_head) (?: \s+ (?:AND|OR) \s+ (?&compound_expression))?
//	license_expression  (?&compound_expression) | (?&simple_expression)
//	                    ^(NONE | NOASSERTION | (?&license_expression))$
//
// PCRE2 (since 10.30) backtracks into subroutine calls, so the regex
// matches exactly when some derivation of the grammar spans the subject;
// alternative order is irrelevant. This recogniser computes, for each
// nonterminal and start offset, the set of offsets where a derivation can
// end. Byte-mode PCRE semantics apply: \pL and \pN classify each byte as
// the Latin-1 code point of that value, \s is [\t\n\v\f\r ], caseless
// matching folds ASCII letters only, and $ (no D modifier) also matches
// before a final "\n".

package spdx

import (
	"slices"
	"unicode"
)

// byteClass flags for the bytes the grammar distinguishes.
const (
	classID    = 1 << iota // [\pL\pN.-]
	classSpace             // \s
)

var byteClasses = func() (t [256]uint8) {
	for b := range 256 {
		r := rune(b)
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '.' || r == '-' {
			t[b] |= classID
		}
	}

	for _, b := range []byte{'\t', '\n', '\v', '\f', '\r', ' '} {
		t[b] |= classSpace
	}

	return t
}()

// lower folds an ASCII letter.
func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}

	return c
}

// hasPrefixFold reports whether s[i:] starts with the lowercase ASCII word,
// ignoring ASCII case.
func hasPrefixFold(s string, i int, word string) bool {
	if len(s)-i < len(word) {
		return false
	}

	for j := range len(word) {
		if lower(s[i+j]) != word[j] {
			return false
		}
	}

	return true
}

// trie holds lowercase identifiers for caseless prefix matching.
type trie struct {
	nodes []trieNode
}

type trieNode struct {
	labels   []byte  // sorted edge bytes
	children []int32 // node index per label
	terminal bool
}

func (t *trie) add(key string) {
	if len(t.nodes) == 0 {
		t.nodes = append(t.nodes, trieNode{})
	}

	n := 0

	for i := range len(key) {
		c := key[i]
		node := &t.nodes[n]

		j := 0
		for j < len(node.labels) && node.labels[j] < c {
			j++
		}

		if j < len(node.labels) && node.labels[j] == c {
			n = int(node.children[j])

			continue
		}

		child := int32(len(t.nodes)) //nolint:gosec // a few thousand nodes
		node.labels = append(node.labels[:j], append([]byte{c}, node.labels[j:]...)...)
		node.children = append(node.children[:j], append([]int32{child}, node.children[j:]...)...)
		t.nodes = append(t.nodes, trieNode{})
		n = int(child)
	}

	t.nodes[n].terminal = true
}

// match calls fn with every end offset e such that s[i:e] is a key,
// ignoring ASCII case, shortest first.
func (t *trie) match(s string, i int, fn func(end int)) {
	if len(t.nodes) == 0 {
		return
	}

	n := 0

	for ; i < len(s); i++ {
		node := &t.nodes[n]
		c := lower(s[i])

		j := 0
		for j < len(node.labels) && node.labels[j] < c {
			j++
		}

		if j == len(node.labels) || node.labels[j] != c {
			return
		}

		n = int(node.children[j])
		if t.nodes[n].terminal {
			fn(i + 1)
		}
	}
}

// matcher recognises one subject.
type matcher struct {
	s        *SpdxLicenses
	subject  string
	head     []memo // by start offset
	compound []memo
}

type memo struct {
	ends []int
	done bool
}

// addEnd appends e to ends unless present; end sets are tiny.
func addEnd(ends []int, e int) []int {
	if slices.Contains(ends, e) {
		return ends
	}

	return append(ends, e)
}

// skipSpace returns the offset after the run of \s at i.
func (m *matcher) skipSpace(i int) int {
	for i < len(m.subject) && byteClasses[m.subject[i]]&classSpace != 0 {
		i++
	}

	return i
}

// idRun returns the offset after the run of idstring bytes at i.
func (m *matcher) idRun(i int) int {
	for i < len(m.subject) && byteClasses[m.subject[i]]&classID != 0 {
		i++
	}

	return i
}

// keyword matches `\s+ WORD \s+` at i, returning the offset after it or
// -1. A following token never starts with \s, so taking whole runs of
// whitespace loses no derivation.
func (m *matcher) keyword(i int, word string) int {
	j := m.skipSpace(i)
	if j == i || !hasPrefixFold(m.subject, j, word) {
		return -1
	}

	j += len(word)
	k := m.skipSpace(j)

	if k == j {
		return -1
	}

	return k
}

// simple appends the ends of simple_expression at i.
func (m *matcher) simple(i int, ends []int) []int {
	s := m.subject

	m.s.licenseIDs.match(s, i, func(e int) {
		ends = addEnd(ends, e)
		if e < len(s) && s[e] == '+' {
			ends = addEnd(ends, e+1)
		}
	})

	// licenseref, with and without the DocumentRef- prefix. idstring
	// before ':' must be the whole run, ':' not being an id byte.
	ends = m.licenseRef(i, ends)

	if hasPrefixFold(s, i, "documentref-") {
		start := i + len("documentref-")
		if r := m.idRun(start); r > start && r < len(s) && s[r] == ':' {
			ends = m.licenseRef(r+1, ends)
		}
	}

	return ends
}

// licenseRef appends the ends of LicenseRef-(?&idstring) at i: every
// non-empty prefix of the id run.
func (m *matcher) licenseRef(i int, ends []int) []int {
	if !hasPrefixFold(m.subject, i, "licenseref-") {
		return ends
	}

	start := i + len("licenseref-")
	for e, run := start+1, m.idRun(start); e <= run; e++ {
		ends = addEnd(ends, e)
	}

	return ends
}

// compoundHead returns the ends of compound_head at i.
func (m *matcher) compoundHead(i int) []int {
	if m.head[i].done {
		return m.head[i].ends
	}

	m.head[i].done = true
	s := m.subject

	var ends []int

	if i < len(s) && s[i] == '(' {
		for _, c := range m.compoundExpression(m.skipSpace(i + 1)) {
			if r := m.skipSpace(c); r < len(s) && s[r] == ')' {
				ends = addEnd(ends, r+1)
			}
		}
	} else {
		ends = m.simple(i, ends)
		for _, e := range ends {
			if k := m.keyword(e, "with"); k >= 0 {
				m.s.exceptIDs.match(s, k, func(x int) { ends = addEnd(ends, x) })
			}
		}
	}

	m.head[i].ends = ends

	return ends
}

// compoundExpression returns the ends of compound_expression at i:
// head ((AND|OR) head)*.
func (m *matcher) compoundExpression(i int) []int {
	if m.compound[i].done {
		return m.compound[i].ends
	}

	m.compound[i].done = true

	var ends []int

	starts := []int{i}
	for n := 0; n < len(starts); n++ {
		for _, e := range m.compoundHead(starts[n]) {
			if slices.Contains(ends, e) {
				continue
			}

			ends = append(ends, e)

			for _, op := range [...]string{"and", "or"} {
				if k := m.keyword(e, op); k >= 0 {
					starts = addEnd(starts, k)
				}
			}
		}
	}

	m.compound[i].ends = ends

	return ends
}

// matchExpression reports whether the regex of isValidLicenseString
// matches license.
func (s *SpdxLicenses) matchExpression(license string) bool {
	n := len(license)
	memos := make([]memo, 2*(n+1))
	m := matcher{s: s, subject: license, head: memos[:n+1], compound: memos[n+1:]}

	spans := func(end int) bool {
		if (end == len("none") && hasPrefixFold(license, 0, "none")) ||
			(end == len("noassertion") && hasPrefixFold(license, 0, "noassertion")) {
			return true
		}

		return slices.Contains(m.compoundExpression(0), end)
	}

	return spans(n) || (n > 0 && license[n-1] == '\n' && spans(n-1))
}
