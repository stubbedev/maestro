// Ports the character class and character type semantics of PCRE2 10.48
// (pcre2_compile.c, pcre2_ucd.c, pcre2_chartables.c in the C locale) as
// PHP configures them: without /u, characters are bytes and \d \s \w,
// POSIX classes and caseless matching are ASCII-only; with /u, PHP adds
// PCRE2_UCP, so they follow Unicode properties.

package php

import (
	"slices"
	"strings"
	"unicode"
)

// charClass is a compiled set of characters: a bracketed class, an escape
// such as \d, a property such as \p{L}, or a caseless literal.
type charClass struct {
	// bits is the final membership (negation, case folding and properties
	// applied) of the characters 0-255, which covers every byte in byte
	// mode.
	bits [4]uint64
	// The remaining fields describe membership of characters >= 256 in
	// UTF mode, before negation.
	raw    [4]uint64 // members < 256, without negation or folding
	ranges []runeRange
	props  []func(rune) bool
	foldU  bool // Unicode caseless: a character matches if any of its case variants is a member
	neg    bool
}

type runeRange struct{ lo, hi rune }

// has reports whether r is in the class.
func (c *charClass) has(r rune) bool {
	if r < 256 {
		return c.bits[r>>6]&(1<<(r&63)) != 0
	}
	return c.member(r) != c.neg
}

// member is membership before negation.
func (c *charClass) member(r rune) bool {
	if c.in(r) {
		return true
	}
	if c.foldU {
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if c.in(f) {
				return true
			}
		}
	}
	return false
}

// in is membership without folding or negation.
func (c *charClass) in(r rune) bool {
	if r < 256 {
		return c.raw[r>>6]&(1<<(r&63)) != 0
	}
	if _, found := slices.BinarySearchFunc(c.ranges, r, func(x runeRange, r rune) int {
		switch {
		case x.hi < r:
			return -1
		case x.lo > r:
			return 1
		}
		return 0
	}); found {
		return true
	}
	for _, p := range c.props {
		if p(r) {
			return true
		}
	}
	return false
}

// classBuilder accumulates the items of a class.
type classBuilder struct {
	c    charClass
	utf  bool // UTF mode (with UCP)
	fold bool // caseless
}

func newClassBuilder(utf, fold bool) *classBuilder {
	return &classBuilder{utf: utf, fold: fold}
}

func (b *classBuilder) setRaw(r rune) { b.c.raw[r>>6] |= 1 << (r & 63) }

// addRange adds lo-hi; in byte mode both are < 256.
func (b *classBuilder) addRange(lo, hi rune) {
	for r := lo; r <= hi && r < 256; r++ {
		b.setRaw(r)
	}
	if hi >= 256 {
		b.c.ranges = append(b.c.ranges, runeRange{max(lo, 256), hi})
	}
}

func (b *classBuilder) addRune(r rune) { b.addRange(r, r) }

// addProp adds the characters satisfying p (negated when neg is set).
func (b *classBuilder) addProp(p func(rune) bool, neg bool) {
	f := p
	if neg {
		f = func(r rune) bool { return !p(r) }
	}
	for r := range rune(256) {
		if f(r) {
			b.setRaw(r)
		}
	}
	if b.utf {
		b.c.props = append(b.c.props, f)
	}
}

// addClass adds every member of another compiled class (used for escapes
// like \d inside brackets).
func (b *classBuilder) addClass(o *charClass) {
	for r := range rune(256) {
		if o.has(r) {
			b.setRaw(r)
		}
	}
	if b.utf {
		b.c.props = append(b.c.props, o.has)
	}
}

// build finishes the class.
func (b *classBuilder) build(neg bool) *charClass {
	c := &b.c
	c.neg = neg
	if len(c.ranges) > 1 {
		slices.SortFunc(c.ranges, func(x, y runeRange) int { return int(x.lo - y.lo) })
		merged := c.ranges[:1]
		for _, r := range c.ranges[1:] {
			if last := &merged[len(merged)-1]; r.lo <= last.hi+1 {
				last.hi = max(last.hi, r.hi)
			} else {
				merged = append(merged, r)
			}
		}
		c.ranges = merged
	}
	switch {
	case b.fold && b.utf:
		c.foldU = true
	case b.fold:
		// The C locale tables fold ASCII letters only.
		for r := 'a'; r <= 'z'; r++ {
			u := r - ('a' - 'A')
			if c.in(r) || c.in(u) {
				b.setRaw(r)
				b.setRaw(u)
			}
		}
	}
	for r := range rune(256) {
		if c.member(r) != neg {
			c.bits[r>>6] |= 1 << (r & 63)
		}
	}
	return c
}

// Character type predicates. The byte-mode ones are the C locale ctype
// tables PCRE2 is built with; the UTF ones are PCRE2's UCP definitions.

func isASCIIDigit(r rune) bool { return r >= '0' && r <= '9' }

func isASCIIWord(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_'
}

func isASCIISpace(r rune) bool { return r == ' ' || r >= '\t' && r <= '\r' }

// isHSpace is \h: the horizontal white space characters of PCRE2.
func isHSpace(r rune) bool {
	switch r {
	case '\t', ' ', 0xA0, 0x1680, 0x180E, 0x202F, 0x205F, 0x3000:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// isVSpace is \v: the vertical white space characters of PCRE2.
func isVSpace(r rune) bool {
	return r >= '\n' && r <= '\r' || r == 0x85 || r == 0x2028 || r == 0x2029
}

func isUCPDigit(r rune) bool { return unicode.Is(unicode.Nd, r) }

func isUCPSpace(r rune) bool { return unicode.Is(unicode.Z, r) || isHSpace(r) || isVSpace(r) }

// isUCPWord is \w with PCRE2_UCP since 10.43: L, N, Mn and Pc.
func isUCPWord(r rune) bool {
	return unicode.In(r, unicode.L, unicode.N, unicode.Mn, unicode.Pc)
}

// isWordChar is the \w used by \b and \B.
func isWordChar(r rune, utf bool) bool {
	if utf {
		return r < 128 && isASCIIWord(r) || r >= 128 && isUCPWord(r)
	}
	return isASCIIWord(r)
}

// typeClass returns the class of a character type escape (d, s, w, h, v
// and their negations as upper case letters).
func typeClass(c byte, utf bool) *charClass {
	var p func(rune) bool
	switch c | 0x20 {
	case 'd':
		p = isASCIIDigit
		if utf {
			p = isUCPDigit
		}
	case 's':
		p = isASCIISpace
		if utf {
			p = isUCPSpace
		}
	case 'w':
		p = isASCIIWord
		if utf {
			p = isUCPWord
		}
	case 'h':
		p = isHSpace
	case 'v':
		p = isVSpace
	}
	b := newClassBuilder(utf, false)
	b.addProp(p, false)
	return b.build(c < 'a')
}

// posixClass returns the predicate of a POSIX class name ("alpha", ...).
func posixClass(name string, utf bool) func(rune) bool {
	if utf {
		switch name {
		case "alpha":
			return func(r rune) bool { return unicode.Is(unicode.L, r) }
		case "alnum":
			return func(r rune) bool { return unicode.In(r, unicode.L, unicode.N) }
		case "digit":
			return isUCPDigit
		case "lower":
			return func(r rune) bool { return unicode.Is(unicode.Ll, r) }
		case "upper":
			return func(r rune) bool { return unicode.Is(unicode.Lu, r) }
		case "space":
			return isUCPSpace
		case "word":
			return isUCPWord
		case "blank":
			return isHSpace
		case "cntrl":
			return func(r rune) bool { return unicode.Is(unicode.Cc, r) }
		case "graph":
			return isUCPGraph
		case "print":
			return func(r rune) bool { return isUCPGraph(r) || unicode.Is(unicode.Zs, r) && r != 0x180E }
		case "punct":
			return func(r rune) bool { return unicode.Is(unicode.P, r) || r < 128 && unicode.Is(unicode.S, r) }
		}
	}
	switch name {
	case "alpha":
		return func(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }
	case "alnum":
		return func(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || isASCIIDigit(r) }
	case "digit":
		return isASCIIDigit
	case "lower":
		return func(r rune) bool { return r >= 'a' && r <= 'z' }
	case "upper":
		return func(r rune) bool { return r >= 'A' && r <= 'Z' }
	case "space":
		return isASCIISpace
	case "word":
		return isASCIIWord
	case "blank":
		return func(r rune) bool { return r == ' ' || r == '\t' }
	case "cntrl":
		return func(r rune) bool { return r < 32 || r == 127 }
	case "graph":
		return func(r rune) bool { return r > 32 && r < 127 }
	case "print":
		return func(r rune) bool { return r >= 32 && r < 127 }
	case "punct":
		return func(r rune) bool { return r > 32 && r < 127 && !isASCIIWord(r) || r == '_' }
	case "xdigit":
		return func(r rune) bool { return isASCIIDigit(r) || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' }
	case "ascii":
		return func(r rune) bool { return r < 128 }
	}
	return nil
}

// isUCPGraph is [:graph:] with PCRE2_UCP.
func isUCPGraph(r rune) bool {
	switch {
	case r == 0x061C || r == 0x180E || r >= 0x2066 && r <= 0x2069:
		return false
	case unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S, unicode.Cf):
		return true
	}
	return false
}

// isAssigned reports whether r has a general category other than Cn.
func isAssigned(r rune) bool {
	return unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S, unicode.Z, unicode.Cc, unicode.Cf, unicode.Co, unicode.Cs)
}

// unicodeProperty returns the predicate of a \p{...} name: general
// categories, scripts and PCRE2's special properties. Names match loosely
// as in PCRE2: case, spaces, hyphens and underscores are ignored.
func unicodeProperty(name string) func(rune) bool {
	key := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '_':
			return -1
		}
		return unicode.ToLower(r)
	}, name)
	switch key {
	case "any":
		return func(rune) bool { return true }
	case "l&", "lc":
		return func(r rune) bool { return unicode.In(r, unicode.Lu, unicode.Ll, unicode.Lt) }
	case "xan":
		return func(r rune) bool { return unicode.In(r, unicode.L, unicode.N) }
	case "xps", "xsp":
		return isUCPSpace
	case "xwd":
		return isUCPWord
	case "xuc":
		return func(r rune) bool {
			return r == '$' || r == '@' || r == '`' || r >= 0xA0 && r <= 0xD7FF || r >= 0xE000
		}
	case "cn":
		return func(r rune) bool { return !isAssigned(r) }
	case "c":
		return func(r rune) bool {
			return !isAssigned(r) || unicode.In(r, unicode.Cc, unicode.Cf, unicode.Co, unicode.Cs)
		}
	}
	for n, t := range unicode.Categories {
		if strings.ToLower(n) == key {
			return func(r rune) bool { return unicode.Is(t, r) }
		}
	}
	key = strings.TrimPrefix(strings.TrimPrefix(key, "sc:"), "scx:")
	for n, t := range unicode.Scripts {
		if strings.ToLower(strings.ReplaceAll(n, "_", "")) == key {
			return func(r rune) bool { return unicode.Is(t, r) }
		}
	}
	return nil
}
