// Ports the pattern syntax of PCRE2 10.48 (pcre2_compile.c: parse_regex,
// check_escape, read_repeat_counts) and the delimiter and modifier parsing
// of PHP's pcre_get_compiled_regex_cache_ex (ext/pcre/php_pcre.c).

package php

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// reOp is the kind of a parsed pattern node.
type reOp uint8

const (
	reEmpty   reOp = iota
	reLit          // the character r
	reClass        // cls
	reAny          // . (dotall: any character, else any but \n)
	reNewline      // \R
	reAnchor       // the zero-width assertion anchor
	reConcat       // subs in sequence
	reAlt          // subs as alternatives
	reCapture      // capture group n around subs[0]
	reGroup        // non-capturing group around subs[0]
	reAtomic       // (?>subs[0])
	reLook         // lookaround subs[0] (look holds the kind)
	reRepeat       // subs[0]{min,max} (max < 0: unbounded)
	reBackref      // back reference to group n (or name)
	reCall         // subroutine call of group n (or name); n == 0 is (?R)
	reCond         // conditional group: cond, yes = subs[0], no = subs[1] (optional)
	reKeep         // \K
)

// Anchors.
const (
	anchorBOL      uint8 = iota // ^
	anchorMBOL                  // ^ in multiline mode
	anchorEOL                   // $ (also before a final newline)
	anchorMEOL                  // $ in multiline mode
	anchorStart                 // \A
	anchorEndZ                  // \Z
	anchorEnd                   // \z, and $ with the D modifier
	anchorG                     // \G
	anchorWordB                 // \b
	anchorNotWordB              // \B
)

// Lookaround kinds (bit flags).
const (
	lookNeg    uint8 = 1
	lookBehind uint8 = 2
)

// Repeat modes.
const (
	repGreedy uint8 = iota
	repLazy
	repPossessive
)

// Condition kinds of reCond.
const (
	condGroup   uint8 = iota // (?(n)...), (?(<name>)...)
	condRecurse              // (?(R)...), (?(Rn)...), (?(R&name)...); n < 0: any recursion
	condDefine               // (?(DEFINE)...)
	condAssert               // (?(?=...)...)
)

type reNode struct {
	op       reOp
	r        rune
	fold     bool  // reLit, reBackref: caseless
	dotall   bool  // reAny
	anchor   uint8 // reAnchor
	look     uint8 // reLook
	mode     uint8 // reRepeat
	cond     uint8 // reCond
	n        int   // group number
	min, max int   // reRepeat
	name     string
	cls      *charClass
	subs     []*reNode
	assert   *reNode // reCond with condAssert: the reLook condition
	offset   int     // pattern offset, for errors on unresolved references
	relative bool    // reCall: the number was relative ((?+1), (?-1))
	dups     []int   // reBackref, reCond: all groups of a duplicated name
	pf       *pform  // reClass: what PCRE2 compiles it to
	info     pinfo   // single-character items: the PCRE2 opcode data, computed once
	hasInfo  bool
	dollar   bool // reAnchor: written as $
}

// pform records the PCRE2 opcode a reClass node compiles to, which
// auto-possessification (pcre_possess.go) depends on.
type pform struct {
	kind     uint8  // pfEscape, pfProp or pfBracket
	esc      byte   // pfEscape: d D s S w W h H v V
	prop     string // pfProp: the property name
	neg      bool   // pfProp: \P or \p{^...}; pfBracket: [^...]
	lits     []rune // pfBracket: its characters, when it lists only literal characters
	litsOnly bool
	hasProp  bool // pfBracket: contains a Unicode property (UTF mode)
	hasWide  bool // pfBracket: contains characters above 255 (UTF mode)
	fold     bool // compiled caseless
}

const (
	pfEscape uint8 = iota
	pfProp
	pfBracket
)

// reFlags are the options that can change inside a pattern.
type reFlags struct {
	caseless, multiline, dotall, extended, extendedMore, ungreedy, noAutoCapture bool
}

// PatternError is a pattern that PHP refuses to compile; Error returns the
// text of PHP's warning without the "preg_*(): " prefix.
type PatternError struct {
	Pattern string
	Msg     string
}

func (e *PatternError) Error() string { return e.Msg }

// compileError builds a PCRE2 compilation error at offset.
func compileError(msg string, offset int) *PatternError {
	return &PatternError{Msg: "Compilation failed: " + msg + " at offset " + strconv.Itoa(offset)}
}

// regexOptions holds what the delimiters and modifiers of a PHP pattern
// literal select.
type regexOptions struct {
	flags       reFlags
	utf         bool
	anchored    bool // A
	dollarEnd   bool // D
	dupNames    bool // J
	body        string
	bodyOffset  int // offset of body in the literal
	caselessRes bool
}

// parseDelimiters ports the delimiter and modifier handling of
// pcre_get_compiled_regex_cache_ex.
func parseDelimiters(regex string) (*regexOptions, *PatternError) {
	p := 0
	for p < len(regex) && isCSpace(regex[p]) {
		p++
	}
	if p >= len(regex) {
		return nil, &PatternError{Msg: "Empty regular expression"}
	}
	delim := regex[p]
	if delim < 0x80 && (isASCIIWord(rune(delim)) && delim != '_') || delim == '\\' || delim == 0 {
		return nil, &PatternError{Msg: "Delimiter must not be alphanumeric, backslash, or NUL byte"}
	}
	p++
	start := p
	endDelim := delim
	if i := strings.IndexByte("([{< )]}> )]}>", delim); i >= 0 {
		endDelim = ")]}> )]}>"[i]
	}
	pp := p
	if endDelim == delim {
		for pp < len(regex) {
			if regex[pp] == '\\' && pp+1 < len(regex) {
				pp++
			} else if regex[pp] == delim {
				break
			}
			pp++
		}
	} else {
		brackets := 1
		for pp < len(regex) {
			if regex[pp] == '\\' && pp+1 < len(regex) {
				pp++
			} else if regex[pp] == endDelim {
				brackets--
				if brackets <= 0 {
					break
				}
			} else if regex[pp] == delim {
				brackets++
			}
			pp++
		}
	}
	if pp >= len(regex) {
		if endDelim == delim {
			return nil, &PatternError{Msg: "No ending delimiter '" + string(delim) + "' found"}
		}
		return nil, &PatternError{Msg: "No ending matching delimiter '" + string(endDelim) + "' found"}
	}
	o := &regexOptions{body: regex[start:pp], bodyOffset: start}
	for _, c := range []byte(regex[pp+1:]) {
		switch c {
		case 'i':
			o.flags.caseless = true
		case 'm':
			o.flags.multiline = true
		case 'n':
			o.flags.noAutoCapture = true
		case 's':
			o.flags.dotall = true
		case 'x':
			o.flags.extended = true
		case 'A':
			o.anchored = true
		case 'D':
			o.dollarEnd = true
		case 'r':
			o.caselessRes = true
		case 'S', 'X', ' ', '\n', '\r':
		case 'U':
			o.flags.ungreedy = true
		case 'u':
			o.utf = true
		case 'J':
			o.dupNames = true
		case 0:
			return nil, &PatternError{Msg: "NUL byte is not a valid modifier"}
		default:
			return nil, &PatternError{Msg: "Unknown modifier '" + string(c) + "'"}
		}
	}
	return o, nil
}

// groupName records a named group.
type groupName struct {
	name string
	n    int
}

// parser parses a pattern body into a reNode tree.
type parser struct {
	src       string
	pos       int
	utf       bool
	dollarEnd bool
	dupNames  bool
	dupCap    bool // (?| was used: group numbers repeat (PCRE2_DUPCAPUSED)
	slab      *slab[reNode]
	seqs      *sliceSlab[*reNode]
	stack     []*reNode // items of the sequences being parsed
	ncap      int
	names     []groupName
	refs      []*reNode // nodes referring to groups by name or number, checked after parsing
	inQuote   bool      // inside \Q...\E
	err       *PatternError
}

func (p *parser) fail(msg string, offset int) {
	if p.err == nil {
		p.err = compileError(msg, offset)
	}
}

func (p *parser) more() bool { return p.pos < len(p.src) && p.err == nil }

func (p *parser) peek() byte {
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return 0
}

func (p *parser) peekAt(i int) byte {
	if p.pos+i < len(p.src) {
		return p.src[p.pos+i]
	}
	return 0
}

// nextChar consumes one character: a byte, or a code point in UTF mode.
func (p *parser) nextChar() rune {
	c := p.src[p.pos]
	if !p.utf || c < utf8.RuneSelf {
		p.pos++
		return rune(c)
	}
	r, n := utf8.DecodeRuneInString(p.src[p.pos:])
	p.pos += n
	return r
}

// parse parses the whole body.
func (p *parser) parse(flags reFlags) *reNode {
	n := p.parseAlt(&flags)
	if p.err == nil && p.pos < len(p.src) {
		// Only an unmatched ')' stops parseAlt early.
		p.fail("unmatched closing parenthesis", p.pos)
	}
	return n
}

// parseAlt parses alternatives up to an unmatched ')' or the end. Option
// changes made in one alternative carry on into the following ones.
func (p *parser) parseAlt(flags *reFlags) *reNode {
	alts := p.parseBranches(flags, false)
	if len(alts) == 1 {
		return alts[0]
	}
	return p.node(reNode{op: reAlt, subs: alts})
}

// parseBranches parses '|'-separated sequences. With reset, each branch
// numbers its groups from the same number ((?|...)).
func (p *parser) parseBranches(flags *reFlags, reset bool) []*reNode {
	mark := len(p.stack)
	base, maxCap := p.ncap, p.ncap
	for {
		if reset {
			p.ncap = base
		}
		alt := p.parseSeq(flags)
		p.stack = append(p.stack, alt)
		maxCap = max(maxCap, p.ncap)
		if p.err != nil || p.peek() != '|' || p.pos >= len(p.src) {
			break
		}
		p.pos++
	}
	if reset {
		p.ncap = maxCap
	}
	alts := p.seqs.alloc(len(p.stack) - mark)
	copy(alts, p.stack[mark:])
	p.stack = p.stack[:mark]
	return alts
}

// skipExtended skips white space and comments in extended mode.
func (p *parser) skipExtended(flags *reFlags) {
	if !flags.extended || p.inQuote {
		return
	}
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case c == ' ' || c >= '\t' && c <= '\r':
			p.pos++
		case c == '#':
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
		case p.utf && c >= 0x80:
			r, n := utf8.DecodeRuneInString(p.src[p.pos:])
			if r != 0x85 && r != 0x200E && r != 0x200F && r != 0x2028 && r != 0x2029 {
				return
			}
			p.pos += n
		default:
			return
		}
	}
}

// parseSeq parses a sequence of quantified items.
func (p *parser) parseSeq(flags *reFlags) *reNode {
	mark := len(p.stack)
	for {
		p.skipExtended(flags)
		if !p.more() {
			break
		}
		if p.inQuote {
			if p.peek() == '\\' && p.peekAt(1) == 'E' {
				p.pos += 2
				p.inQuote = false
				continue
			}
			p.stack = append(p.stack, p.lit(p.nextChar(), flags))
			if p.peek() == '\\' && p.peekAt(1) == 'E' {
				p.pos += 2
				p.inQuote = false
				p.quantify(p.stack[mark:], flags)
			}
			continue
		}
		c := p.peek()
		if c == '|' || c == ')' {
			break
		}
		start := p.pos
		atom := p.parseAtom(flags)
		if p.err != nil {
			break
		}
		if atom == nil {
			// An option setting, comment, \E or the start of \Q.
			continue
		}
		atom.offset = start
		p.stack = append(p.stack, atom)
		p.quantify(p.stack[mark:], flags)
	}
	items := p.stack[mark:]
	var n *reNode
	switch len(items) {
	case 0:
		n = p.node(reNode{op: reEmpty})
	case 1:
		n = items[0]
	default:
		subs := p.seqs.alloc(len(items))
		copy(subs, items)
		n = p.node(reNode{op: reConcat, subs: subs})
	}
	p.stack = p.stack[:mark]
	return n
}

// quantify applies a quantifier following the last item, if any, in
// place.
func (p *parser) quantify(items []*reNode, flags *reFlags) {
	p.skipExtended(flags)
	if !p.more() {
		return
	}
	start := p.pos
	minN, maxN, ok := p.parseQuantifier()
	if !ok {
		return
	}
	mode := repGreedy
	if flags.ungreedy {
		mode = repLazy
	}
	switch p.peek() {
	case '?':
		p.pos++
		if flags.ungreedy {
			mode = repGreedy
		} else {
			mode = repLazy
		}
	case '+':
		p.pos++
		mode = repPossessive
	}
	last := items[len(items)-1]
	switch last.op {
	case reAnchor, reKeep, reEmpty:
		p.fail("quantifier does not follow a repeatable item", start)
		return
	case reLook:
		// PCRE2 repeats an assertion at most one more time than the
		// minimum.
		if maxN < 0 {
			maxN = minN + 1
		}
	}
	if minN == 1 && maxN == 1 && mode != repPossessive {
		return
	}
	items[len(items)-1] = p.node(reNode{op: reRepeat, subs: []*reNode{last}, min: minN, max: maxN, mode: mode})
	// A quantifier following a quantifier is an error, except for the
	// lazy and possessive suffixes handled above.
	p.skipExtended(flags)
	if p.more() {
		save := p.pos
		if _, _, again := p.parseQuantifier(); again {
			p.fail("quantifier does not follow a repeatable item", save)
		}
		p.pos = save
	}
}

// parseQuantifier reads *, +, ? or a {n,m} quantifier (PCRE2 10.43+ syntax,
// with {,m} and spaces allowed). A '{' that does not start a valid
// quantifier is not consumed.
func (p *parser) parseQuantifier() (minN, maxN int, ok bool) {
	switch p.peek() {
	case '*':
		p.pos++
		return 0, -1, true
	case '+':
		p.pos++
		return 1, -1, true
	case '?':
		p.pos++
		return 0, 1, true
	case '{':
	default:
		return 0, 0, false
	}
	i := p.pos + 1
	spaces := func() {
		for i < len(p.src) && (p.src[i] == ' ' || p.src[i] == '\t') {
			i++
		}
	}
	number := func() (int, bool, bool) {
		s := i
		v := 0
		for i < len(p.src) && isDigit(p.src[i]) {
			if v <= 65535 {
				v = v*10 + int(p.src[i]-'0')
			}
			i++
		}
		return v, i > s, v > 65535
	}
	spaces()
	lo, hasLo, bigLo := number()
	spaces()
	hi, hasHi, bigHi := lo, hasLo, bigLo
	comma := false
	if i < len(p.src) && p.src[i] == ',' {
		comma = true
		i++
		spaces()
		hi, hasHi, bigHi = number()
		spaces()
	}
	if i >= len(p.src) || p.src[i] != '}' || !hasLo && !hasHi || !hasLo && !comma {
		return 0, 0, false
	}
	if bigLo || bigHi {
		p.fail("number too big in {} quantifier", i)
		return 0, 0, false
	}
	switch {
	case !comma:
	case !hasHi:
		hi = -1
	case !hasLo:
		lo = 0
	}
	if hi >= 0 && hi < lo {
		p.fail("numbers out of order in {} quantifier", i)
		return 0, 0, false
	}
	p.pos = i + 1
	return lo, hi, true
}

// lit returns a literal character node.
func (p *parser) lit(r rune, flags *reFlags) *reNode {
	return p.node(reNode{op: reLit, r: r, fold: flags.caseless})
}

// parseAtom parses one item. It returns nil for constructs that match
// nothing (option settings, comments, \E, \Q).
func (p *parser) parseAtom(flags *reFlags) *reNode {
	start := p.pos
	switch c := p.peek(); c {
	case '(':
		p.pos++
		return p.parseGroup(flags)
	case '[':
		p.pos++
		return p.parseClass(flags)
	case '.':
		p.pos++
		return p.node(reNode{op: reAny, dotall: flags.dotall})
	case '^':
		p.pos++
		if flags.multiline {
			return p.node(reNode{op: reAnchor, anchor: anchorMBOL})
		}
		return p.node(reNode{op: reAnchor, anchor: anchorBOL})
	case '$':
		p.pos++
		switch {
		case flags.multiline:
			return p.node(reNode{op: reAnchor, anchor: anchorMEOL, dollar: true})
		case p.dollarEnd:
			return p.node(reNode{op: reAnchor, anchor: anchorEnd, dollar: true})
		}
		return p.node(reNode{op: reAnchor, anchor: anchorEOL, dollar: true})
	case '\\':
		p.pos++
		return p.parseEscape(flags)
	case '*', '+', '?':
		p.fail("quantifier does not follow a repeatable item", start)
		return nil
	case '{':
		if _, _, ok := p.parseQuantifier(); ok {
			p.fail("quantifier does not follow a repeatable item", start)
			return nil
		}
		p.pos = start + 1
		return p.lit('{', flags)
	}
	return p.lit(p.nextChar(), flags)
}

// parseEscape parses an escape sequence outside a class, after the '\'.
func (p *parser) parseEscape(flags *reFlags) *reNode {
	start := p.pos - 1
	if p.pos >= len(p.src) {
		p.fail("\\ at end of pattern", p.pos)
		return nil
	}
	c := p.src[p.pos]
	switch c {
	case 'd', 'D', 's', 'S', 'w', 'W', 'h', 'H', 'v', 'V':
		p.pos++
		return p.node(reNode{op: reClass, cls: typeClass(c, p.utf), pf: &pform{kind: pfEscape, esc: c}})
	case 'p', 'P':
		p.pos++
		cls, name, neg := p.parseProperty(c == 'P', flags.caseless)
		if cls == nil {
			return nil
		}
		return p.node(reNode{op: reClass, cls: cls, pf: &pform{kind: pfProp, prop: name, neg: neg, fold: flags.caseless}})
	case 'b':
		p.pos++
		return p.node(reNode{op: reAnchor, anchor: anchorWordB})
	case 'B':
		p.pos++
		return p.node(reNode{op: reAnchor, anchor: anchorNotWordB})
	case 'A':
		p.pos++
		return p.node(reNode{op: reAnchor, anchor: anchorStart})
	case 'Z':
		p.pos++
		return p.node(reNode{op: reAnchor, anchor: anchorEndZ})
	case 'z':
		p.pos++
		return p.node(reNode{op: reAnchor, anchor: anchorEnd})
	case 'G':
		p.pos++
		return p.node(reNode{op: reAnchor, anchor: anchorG})
	case 'K':
		p.pos++
		return p.node(reNode{op: reKeep})
	case 'R':
		p.pos++
		return p.node(reNode{op: reNewline})
	case 'N':
		if p.peekAt(1) != '{' {
			p.pos++
			return p.node(reNode{op: reAny})
		}
	case 'Q':
		p.pos++
		p.inQuote = true
		return nil
	case 'E':
		p.pos++
		return nil
	case 'g':
		return p.parseGRef(flags)
	case 'k':
		p.pos++
		var term byte
		switch p.peek() {
		case '<':
			term = '>'
		case '\'':
			term = '\''
		case '{':
			term = '}'
		default:
			p.fail("\\k is not followed by a braced, angle-bracketed, or quoted name", p.pos)
			return nil
		}
		p.pos++
		name := p.parseName(term)
		if p.err != nil {
			return nil
		}
		return p.ref(p.node(reNode{op: reBackref, name: name, fold: flags.caseless, offset: start}))
	case 'X', 'C':
		p.fail("escape sequence \\"+string(c)+" is not supported", p.pos)
		return nil
	}
	if c >= '1' && c <= '9' {
		// \1-\9, \8x, \9x and numbers with at least that many previous
		// groups are back references; otherwise octal.
		i := p.pos
		v := 0
		for i < len(p.src) && isDigit(p.src[i]) && v <= 65535 {
			v = v*10 + int(p.src[i]-'0')
			i++
		}
		if v < 10 || c >= '8' || v <= p.ncap {
			p.pos = i
			return p.ref(p.node(reNode{op: reBackref, n: v, fold: flags.caseless, offset: start}))
		}
	}
	r, ok := p.parseCharEscape(false)
	if !ok {
		return nil
	}
	return p.lit(r, flags)
}

// parseGRef parses \g references: \gN, \g{N}, \g{-N}, \g{name} are back
// references; \g<...> and \g'...' are subroutine calls.
func (p *parser) parseGRef(flags *reFlags) *reNode {
	start := p.pos - 1
	p.pos++ // 'g'
	switch p.peek() {
	case '<', '\'':
		term := byte('>')
		if p.peek() == '\'' {
			term = '\''
		}
		p.pos++
		if n, rel, ok := p.parseGroupNumber(term); ok {
			return p.ref(p.node(reNode{op: reCall, n: n, relative: rel, offset: start}))
		}
		name := p.parseName(term)
		return p.ref(p.node(reNode{op: reCall, name: name, offset: start}))
	case '{':
		p.pos++
		if n, rel, ok := p.parseGroupNumber('}'); ok {
			if n == 0 || rel && n > p.ncap {
				p.fail("reference to non-existent subpattern", p.pos)
			}
			return p.ref(p.node(reNode{op: reBackref, n: n, fold: flags.caseless, offset: start}))
		}
		name := p.parseName('}')
		return p.ref(p.node(reNode{op: reBackref, name: name, fold: flags.caseless, offset: start}))
	}
	n, rel, ok := p.parseGroupNumber(0)
	if !ok || n == 0 {
		p.fail("reference to non-existent subpattern", p.pos)
		return nil
	}
	if rel && n > p.ncap {
		p.fail("reference to non-existent subpattern", p.pos)
	}
	return p.ref(p.node(reNode{op: reBackref, n: n, fold: flags.caseless, offset: start}))
}

// parseGroupNumber reads [+-]digits followed by term (or anything when
// term is 0) and resolves a relative number. It consumes nothing when
// there is no number.
func (p *parser) parseGroupNumber(term byte) (n int, relative, ok bool) {
	i := p.pos
	sign := byte(0)
	if i < len(p.src) && (p.src[i] == '+' || p.src[i] == '-') {
		sign = p.src[i]
		i++
	}
	s := i
	v := 0
	for i < len(p.src) && isDigit(p.src[i]) && v <= 65535 {
		v = v*10 + int(p.src[i]-'0')
		i++
	}
	if i == s || term != 0 && (i >= len(p.src) || p.src[i] != term) {
		return 0, false, false
	}
	if term != 0 {
		i++
	}
	switch sign {
	case '-':
		if v == 0 {
			p.fail("a relative value of zero is not allowed", i)
			return 0, false, false
		}
		v = p.ncap - v + 1
		if v <= 0 {
			p.fail("reference to non-existent subpattern", i)
			return 0, false, false
		}
	case '+':
		if v == 0 {
			p.fail("a relative value of zero is not allowed", i)
			return 0, false, false
		}
		v += p.ncap
	}
	p.pos = i
	return v, sign != 0, true
}

// node allocates a node.
func (p *parser) node(n reNode) *reNode { return p.slab.alloc(n) }

// ref records a node that refers to a group, to check after parsing.
func (p *parser) ref(n *reNode) *reNode {
	p.refs = append(p.refs, n)
	return n
}

// isNameChar reports whether r may appear in a group name: ASCII word
// characters, and in UTF mode Unicode letters and digits.
func isNameChar(r rune, utf bool) bool {
	if r < utf8.RuneSelf {
		return isASCIIWord(r)
	}
	return utf && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

// parseName reads a group name terminated by term.
func (p *parser) parseName(term byte) string {
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != term {
		r := p.nextChar()
		if !isNameChar(r, p.utf) {
			p.fail("syntax error in subpattern name (missing terminator?)", p.pos)
			return ""
		}
	}
	name := p.src[start:p.pos]
	switch {
	case p.pos >= len(p.src):
		p.fail("syntax error in subpattern name (missing terminator?)", p.pos)
	case name == "":
		p.fail("subpattern name expected", p.pos)
	case isDigit(name[0]):
		p.fail("subpattern name must start with a non-digit", start)
	case len(name) > 128:
		p.fail("subpattern name is too long (maximum 128 code units)", p.pos)
	}
	p.pos++
	return name
}

// parseGroup parses a group after its '('.
func (p *parser) parseGroup(flags *reFlags) *reNode {
	start := p.pos - 1
	if p.peek() == '*' {
		p.fail("(*VERB) not recognized or malformed", p.pos)
		return nil
	}
	if p.peek() != '?' {
		if flags.noAutoCapture {
			return p.groupBody(flags, p.node(reNode{op: reGroup}))
		}
		p.ncap++
		return p.groupBody(flags, p.node(reNode{op: reCapture, n: p.ncap}))
	}
	p.pos++
	c := p.peek()
	switch c {
	case '#':
		for p.pos < len(p.src) && p.src[p.pos] != ')' {
			p.pos++
		}
		if p.pos >= len(p.src) {
			p.fail("missing ) after (?# comment", p.pos)
			return nil
		}
		p.pos++
		return nil
	case ':':
		p.pos++
		return p.groupBody(flags, p.node(reNode{op: reGroup}))
	case '|':
		p.pos++
		p.dupCap = true
		local := *flags
		alts := p.parseBranches(&local, true)
		if !p.closeParen() {
			return nil
		}
		if len(alts) == 1 {
			return p.node(reNode{op: reGroup, subs: alts})
		}
		return p.node(reNode{op: reGroup, subs: []*reNode{{op: reAlt, subs: alts}}})
	case '>':
		p.pos++
		return p.groupBody(flags, p.node(reNode{op: reAtomic}))
	case '=':
		p.pos++
		return p.groupBody(flags, p.node(reNode{op: reLook}))
	case '!':
		p.pos++
		return p.groupBody(flags, p.node(reNode{op: reLook, look: lookNeg}))
	case '<':
		switch p.peekAt(1) {
		case '=':
			p.pos += 2
			return p.groupBody(flags, p.node(reNode{op: reLook, look: lookBehind}))
		case '!':
			p.pos += 2
			return p.groupBody(flags, p.node(reNode{op: reLook, look: lookBehind | lookNeg}))
		}
		p.pos++
		return p.namedGroup('>', flags)
	case '\'':
		p.pos++
		return p.namedGroup('\'', flags)
	case 'P':
		p.pos++
		switch p.peek() {
		case '<':
			p.pos++
			return p.namedGroup('>', flags)
		case '=':
			p.pos++
			name := p.parseName(')')
			return p.ref(p.node(reNode{op: reBackref, name: name, fold: flags.caseless, offset: start}))
		case '>':
			p.pos++
			name := p.parseName(')')
			return p.ref(p.node(reNode{op: reCall, name: name, offset: start}))
		}
		p.fail("unrecognized character after (?P", p.pos)
		return nil
	case '&':
		p.pos++
		name := p.parseName(')')
		return p.ref(p.node(reNode{op: reCall, name: name, offset: start}))
	case 'R':
		if p.peekAt(1) == ')' {
			p.pos += 2
			return p.ref(p.node(reNode{op: reCall, n: 0, offset: start}))
		}
	case '(':
		p.pos++
		return p.parseCond(flags, start)
	}
	if c == '+' || c == '-' && isDigit(p.peekAt(1)) || isDigit(c) {
		if n, rel, ok := p.parseGroupNumber(')'); ok {
			return p.ref(p.node(reNode{op: reCall, n: n, relative: rel, offset: start}))
		}
		if p.err == nil {
			p.fail("digit expected after (?+ or (?-", p.pos)
		}
		return nil
	}
	return p.parseOptions(flags)
}

// parseOptions parses (?imnsxJU-imnsxJU) and (?^...) settings, either
// alone (changing the rest of the enclosing group) or as (?flags:...).
func (p *parser) parseOptions(flags *reFlags) *reNode {
	local := *flags
	on := true
	if p.peek() == '^' {
		p.pos++
		local.caseless, local.multiline, local.noAutoCapture, local.dotall, local.extended, local.extendedMore = false, false, false, false, false, false
	}
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		p.pos++
		switch c {
		case '-':
			if !on {
				p.fail("unrecognized character after (? or (?-", p.pos-1)
				return nil
			}
			on = false
		case 'i':
			local.caseless = on
		case 'm':
			local.multiline = on
		case 'n':
			local.noAutoCapture = on
		case 's':
			local.dotall = on
		case 'x':
			if on && p.peek() == 'x' {
				p.pos++
				local.extendedMore = true
			}
			local.extended = on
			if !on {
				local.extendedMore = false
			}
		case 'U':
			local.ungreedy = on
		case 'J':
			p.dupNames = on
		case ')':
			*flags = local
			return nil
		case ':':
			return p.groupBody(&local, p.node(reNode{op: reGroup}))
		default:
			p.fail("unrecognized character after (? or (?-", p.pos-1)
			return nil
		}
	}
	p.fail("missing closing parenthesis", p.pos)
	return nil
}

// namedGroup parses a named capture group after "(?<", "(?'" or "(?P<".
func (p *parser) namedGroup(term byte, flags *reFlags) *reNode {
	name := p.parseName(term)
	if p.err != nil {
		return nil
	}
	p.ncap++
	for _, g := range p.names {
		if g.name == name && g.n != p.ncap && !p.dupNames {
			p.fail("two named subpatterns have the same name (PCRE2_DUPNAMES not set)", p.pos)
			return nil
		}
	}
	p.names = append(p.names, groupName{name, p.ncap})
	return p.groupBody(flags, p.node(reNode{op: reCapture, n: p.ncap, name: name}))
}

// groupBody parses the alternatives of a group and its ')'. Option
// changes inside the group do not leak out of it.
func (p *parser) groupBody(flags *reFlags, n *reNode) *reNode {
	local := *flags
	body := p.parseAlt(&local)
	if !p.closeParen() {
		return nil
	}
	n.subs = []*reNode{body}
	return n
}

// closeParen consumes the ')' ending a group.
func (p *parser) closeParen() bool {
	if p.err != nil {
		return false
	}
	if p.pos >= len(p.src) || p.src[p.pos] != ')' {
		p.fail("missing closing parenthesis", len(p.src))
		return false
	}
	p.pos++
	return true
}

// parseCond parses a conditional group after "(?(".
func (p *parser) parseCond(flags *reFlags, start int) *reNode {
	n := p.node(reNode{op: reCond, offset: start})
	switch {
	case p.peek() == '?' && (p.peekAt(1) == '=' || p.peekAt(1) == '!' || p.peekAt(1) == '<' && (p.peekAt(2) == '=' || p.peekAt(2) == '!')):
		// p.pos is at the '?', just after a '(' as parseGroup expects.
		a := p.parseGroup(flags)
		if a == nil || a.op != reLook {
			if p.err == nil {
				p.fail("assertion expected after (?( or (?(?C)", p.pos)
			}
			return nil
		}
		n.cond, n.assert = condAssert, a
	case strings.HasPrefix(p.src[p.pos:], "DEFINE)"):
		p.pos += len("DEFINE)")
		n.cond = condDefine
	case p.peek() == 'R':
		p.pos++
		n.cond, n.n = condRecurse, -1
		switch {
		case p.peek() == ')':
			p.pos++
		case p.peek() == '&':
			p.pos++
			n.name = p.parseName(')')
			p.ref(n)
		default:
			v, _, ok := p.parseGroupNumber(')')
			if !ok {
				// A group named R...
				p.pos--
				n.cond, n.n = condGroup, 0
				n.name = p.parseName(')')
				p.ref(n)
				break
			}
			n.n = v
			p.ref(n)
		}
	case p.peek() == '<' || p.peek() == '\'':
		term := byte('>')
		if p.peek() == '\'' {
			term = '\''
		}
		p.pos++
		n.cond = condGroup
		n.name = p.parseName(term)
		if p.err == nil && p.peek() != ')' {
			p.fail("malformed number or name after (?(", p.pos)
		}
		p.pos++
		p.ref(n)
	default:
		n.cond = condGroup
		if v, _, ok := p.parseGroupNumber(')'); ok {
			n.n = v
		} else if p.err == nil {
			n.name = p.parseName(')')
		}
		p.ref(n)
	}
	if p.err != nil {
		return nil
	}
	local := *flags
	alts := p.parseBranches(&local, false)
	if !p.closeParen() {
		return nil
	}
	switch {
	case len(alts) > 2:
		p.fail("conditional subpattern contains more than two branches", p.pos)
		return nil
	case n.cond == condDefine && len(alts) > 1:
		p.fail("DEFINE subpattern contains more than one branch", p.pos)
		return nil
	}
	n.subs = alts
	return n
}

// parseProperty parses the name after \p or \P; it also returns the name
// and whether the property is negated.
func (p *parser) parseProperty(neg, fold bool) (*charClass, string, bool) {
	name := ""
	if p.peek() == '{' {
		end := strings.IndexByte(p.src[p.pos:], '}')
		if end < 0 {
			p.fail("malformed \\P or \\p sequence", p.pos)
			return nil, "", false
		}
		name = p.src[p.pos+1 : p.pos+end]
		p.pos += end + 1
		if strings.HasPrefix(name, "^") {
			neg = !neg
			name = name[1:]
		}
	} else if p.pos < len(p.src) {
		name = string(p.src[p.pos])
		p.pos++
	}
	prop := unicodeProperty(name)
	if prop == nil {
		p.fail("unknown property name after \\P or \\p", p.pos)
		return nil, "", false
	}
	b := newClassBuilder(p.utf, fold)
	b.addProp(prop, false)
	return b.build(neg), name, neg
}

// parseCharEscape parses an escape that stands for one character, with
// p.pos after the '\'. Inside a class (inClass), \b is a backspace and
// \8 \9 are the digits.
func (p *parser) parseCharEscape(inClass bool) (rune, bool) {
	start := p.pos
	c := p.src[p.pos]
	p.pos++
	var r rune
	switch c {
	case 'a':
		r = 7
	case 'e':
		r = 27
	case 'f':
		r = '\f'
	case 'n':
		r = '\n'
	case 'r':
		r = '\r'
	case 't':
		r = '\t'
	case 'b':
		if !inClass {
			p.fail("unrecognized character follows \\", p.pos)
			return 0, false
		}
		r = '\b'
	case '0':
		// \0 and up to two more octal digits.
		for i := 0; i < 2 && p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '7'; i++ {
			r = r*8 + rune(p.src[p.pos]-'0')
			p.pos++
		}
	case '1', '2', '3', '4', '5', '6', '7':
		r = rune(c - '0')
		for i := 0; i < 2 && p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '7'; i++ {
			r = r*8 + rune(p.src[p.pos]-'0')
			p.pos++
		}
	case '8', '9':
		if !inClass {
			p.fail("reference to non-existent subpattern", p.pos)
			return 0, false
		}
		r = rune(c)
	case 'o':
		if p.peek() != '{' {
			p.fail("missing opening brace after \\o", p.pos)
			return 0, false
		}
		end := strings.IndexByte(p.src[p.pos:], '}')
		if end < 2 {
			p.fail("digits missing after \\x or in \\x{} or \\o{} or \\N{U+}", p.pos)
			return 0, false
		}
		v, err := strconv.ParseUint(strings.Trim(p.src[p.pos+1:p.pos+end], " \t"), 8, 32)
		if err != nil {
			p.fail("non-octal character in \\o{} (closing brace missing?)", p.pos)
			return 0, false
		}
		p.pos += end + 1
		r = codePoint(v)
	case 'x':
		if p.peek() == '{' {
			end := strings.IndexByte(p.src[p.pos:], '}')
			if end < 0 {
				p.fail("missing terminating } after \\x{", p.pos)
				return 0, false
			}
			digits := strings.Trim(p.src[p.pos+1:p.pos+end], " \t")
			if digits == "" {
				p.fail("digits missing after \\x or in \\x{} or \\o{} or \\N{U+}", p.pos)
				return 0, false
			}
			v, err := strconv.ParseUint(digits, 16, 32)
			if err != nil {
				p.fail("non-hex character in \\x{} (closing brace missing?)", p.pos)
				return 0, false
			}
			p.pos += end + 1
			r = codePoint(v)
			break
		}
		n := 0
		for n < 2 && p.pos < len(p.src) && isHexDigit(p.src[p.pos]) {
			r = r<<4 | rune(hexVal(p.src[p.pos]))
			p.pos++
			n++
		}
		if n == 0 {
			p.fail("digits missing after \\x or in \\x{} or \\o{} or \\N{U+}", p.pos)
			return 0, false
		}
	case 'c':
		if p.pos >= len(p.src) {
			p.fail("\\c at end of pattern", p.pos)
			return 0, false
		}
		x := p.src[p.pos]
		if x < 32 || x > 126 {
			p.fail("\\c must be followed by a printable ASCII character", p.pos)
			return 0, false
		}
		p.pos++
		r = rune(upperASCII(x) ^ 0x40)
	case 'N':
		// \N{U+hhh}
		if !p.utf || !strings.HasPrefix(p.src[p.pos:], "{U+") {
			p.fail("PCRE2 does not support \\F, \\L, \\l, \\N{name}, \\U, or \\u", p.pos)
			return 0, false
		}
		end := strings.IndexByte(p.src[p.pos:], '}')
		v, err := strconv.ParseUint(p.src[p.pos+3:p.pos+max(end, 3)], 16, 32)
		if end < 0 || err != nil {
			p.fail("digits missing after \\x or in \\x{} or \\o{} or \\N{U+}", p.pos)
			return 0, false
		}
		p.pos += end + 1
		r = codePoint(v)
	case 'u', 'U', 'l', 'L':
		p.fail("PCRE2 does not support \\F, \\L, \\l, \\N{name}, \\U, or \\u", p.pos)
		return 0, false
	default:
		p.pos = start
		if c < 0x80 && isASCIIWord(rune(c)) && c != '_' {
			p.fail("unrecognized character follows \\", p.pos+1)
			return 0, false
		}
		return p.nextChar(), true
	}
	switch {
	case !p.utf && r > 0xFF:
		if c == 'o' || c == '0' || c >= '1' && c <= '7' {
			p.fail("octal value is greater than \\377 in 8-bit non-UTF mode", p.pos)
		} else {
			p.fail("character code point value in \\x{} or \\o{} is too large", p.pos)
		}
		return 0, false
	case r > unicode.MaxRune:
		p.fail("character code point value in \\x{} or \\o{} is too large", p.pos)
		return 0, false
	case p.utf && r >= 0xD800 && r <= 0xDFFF:
		p.fail("disallowed Unicode code point (>= 0xd800 && <= 0xdfff)", p.pos)
		return 0, false
	}
	return r, true
}

// codePoint is the code point an escape's value v (at most 32 bits) names.
// Values beyond unicode.MaxRune are MaxRune+1 rather than wrapping to a
// negative rune, so the range check rejects them as PCRE2 does.
func codePoint(v uint64) rune {
	if v > unicode.MaxRune {
		return unicode.MaxRune + 1
	}

	return rune(v)
}

func isHexDigit(c byte) bool { return isDigit(c) || c|0x20 >= 'a' && c|0x20 <= 'f' }

func hexVal(c byte) byte {
	if isDigit(c) {
		return c - '0'
	}
	return c | 0x20 - 'a' + 10
}

// checkPosixSyntax ports check_posix_syntax: at "[:" (or "[." "[="),
// whether a POSIX class follows, and the offset of its terminator.
func checkPosixSyntax(s string, i int) (int, bool) {
	term := s[i+1]
	for j := i + 2; len(s)-j >= 2; j++ {
		switch {
		case s[j] == '\\' && (s[j+1] == ']' || s[j+1] == '\\'):
			j++
		case s[j] == '[' && s[j+1] == term || s[j] == ']':
			return 0, false
		case s[j] == term && s[j+1] == ']':
			return j, true
		}
	}
	return 0, false
}

// parseClass parses a bracketed class after its '['.
func (p *parser) parseClass(flags *reFlags) *reNode {
	b := newClassBuilder(p.utf, flags.caseless)
	neg := false
	if p.peek() == '^' {
		neg = true
		p.pos++
	}
	pf := &pform{kind: pfBracket, litsOnly: true, fold: flags.caseless}
	first, inQuote := true, false
	// prev is a character that may still start a range; rangeLo is set
	// once "x-" has been seen.
	var prev, rangeLo rune
	havePrev, inRange := false, false
	flushPrev := func() {
		if havePrev {
			b.addRune(prev)
			pf.lits = append(pf.lits, prev)
			if prev > 0xFF {
				pf.hasWide = true
			}
			havePrev = false
		}
	}
	for {
		if p.pos >= len(p.src) {
			p.fail("missing terminating ] for character class", len(p.src))
			return nil
		}
		c := p.src[p.pos]
		if inQuote {
			if c == '\\' && p.peekAt(1) == 'E' {
				p.pos += 2
				inQuote = false
				continue
			}
		} else {
			if c == ']' && !first {
				p.pos++
				break
			}
			if flags.extendedMore && (c == ' ' || c == '\t') {
				p.pos++
				continue
			}
		}
		first = false
		var (
			r       rune
			literal bool // an escaped or quoted character: never a range operator
			set     func(rune) bool
			setNeg  bool
			setCls  *charClass
		)
		switch {
		case inQuote:
			r, literal = p.nextChar(), true
		case c == '[' && (p.peekAt(1) == ':' || p.peekAt(1) == '.' || p.peekAt(1) == '='):
			end, ok := checkPosixSyntax(p.src, p.pos)
			if !ok {
				r = p.nextChar()
				break
			}
			if p.peekAt(1) != ':' {
				p.fail("POSIX collating elements are not supported", p.pos)
				return nil
			}
			name := p.src[p.pos+2 : end]
			setNeg = strings.HasPrefix(name, "^")
			if set = posixClass(strings.TrimPrefix(name, "^"), p.utf); set == nil {
				p.fail("unknown POSIX class name", p.pos)
				return nil
			}
			if p.utf {
				switch strings.TrimPrefix(name, "^") {
				case "ascii", "xdigit", "cntrl":
				case "blank":
					pf.hasWide = true
				default:
					pf.hasProp = true
				}
			}
			p.pos = end + 2
		case c == '\\':
			p.pos++
			if p.pos >= len(p.src) {
				p.fail("\\ at end of pattern", p.pos)
				return nil
			}
			switch e := p.src[p.pos]; e {
			case 'd', 'D', 's', 'S', 'w', 'W', 'h', 'H', 'v', 'V':
				p.pos++
				setCls = typeClass(e, p.utf)
				if p.utf {
					switch e | 0x20 {
					case 'h', 'v':
						pf.hasWide = true
					default:
						pf.hasProp = true
					}
				}
			case 'p', 'P':
				p.pos++
				if setCls, _, _ = p.parseProperty(e == 'P', false); setCls == nil {
					return nil
				}
				pf.hasProp = true
			case 'Q':
				p.pos++
				inQuote = true
				continue
			case 'E':
				p.pos++
				continue
			case 'N', 'R', 'X', 'B', 'A', 'Z', 'z', 'G', 'K', 'g', 'k':
				p.fail("escape sequence is invalid in character class", p.pos+1)
				return nil
			default:
				var ok bool
				if r, ok = p.parseCharEscape(true); !ok {
					return nil
				}
				literal = true
			}
		default:
			r = p.nextChar()
		}
		if set != nil || setCls != nil {
			// A class item cannot be the end or the start of a range.
			if inRange || !inQuote && p.peek() == '-' && p.peekAt(1) != ']' && p.pos+1 < len(p.src) {
				p.fail("invalid range in character class", p.pos)
				return nil
			}
			flushPrev()
			pf.litsOnly = false
			if set != nil {
				b.addProp(set, setNeg)
			} else {
				b.addClass(setCls)
			}
			continue
		}
		switch {
		case inRange:
			if r < rangeLo {
				p.fail("range out of order in character class", p.pos-1)
				return nil
			}
			b.addRange(rangeLo, r)
			pf.litsOnly = false
			if r > 0xFF {
				pf.hasWide = true
			}
			inRange = false
		case r == '-' && !literal && havePrev && p.peek() != ']':
			rangeLo, inRange, havePrev = prev, true, false
		default:
			flushPrev()
			prev, havePrev = r, true
		}
	}
	if inRange {
		b.addRune(rangeLo)
		b.addRune('-')
		pf.lits = append(pf.lits, rangeLo, '-')
	}
	flushPrev()
	pf.neg = neg
	return p.node(reNode{op: reClass, cls: b.build(neg), pf: pf})
}
