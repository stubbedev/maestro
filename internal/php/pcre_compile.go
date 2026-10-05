// Ports the compilation side of PCRE2 10.48 (pcre2_compile.c): group
// numbering and name resolution, lookbehind length checks, and the
// "first code unit" and anchoring analysis pcre2_match uses to skip start
// positions. The program it produces runs on the backtracking machine of
// pcre_vm.go.

package php

import (
	"slices"
	"sync"
	"sync/atomic"
	"unicode"
	"unicode/utf8"
)

type opcode uint8

const (
	opMatch       opcode = iota
	opByte               // the byte c
	opByteFold           // the byte c or c2 (ASCII caseless)
	opRune               // UTF mode: the character r
	opStr                // the bytes s
	opClass              // one character in cls
	opAny                // one character other than \n
	opAnyNL              // one character
	opNewline            // \R
	opRepeat             // item (op, c, c2, r, cls) repeated min..max times in mode arg
	opSplit              // try x, then y
	opJmp                // continue at x
	opCapOpen            // regs[n] = start of group n
	opCapClose           // set group n, or return from a call of group n
	opAssert             // anchor arg
	opBackref            // the text of the first set group of groups (arg: caseless)
	opAtomicOpen         // atomic group start, mark index in regs[n]
	opAtomicClose        // atomic group end
	opLookOpen           // assertion start (arg: mark kind), mark index in regs[n]; x: where failure of the body continues
	opLookClose          // assertion end (arg 1: lookbehind); x: no branch of a negative condition
	opReverse            // lookbehind branch start: move back min..max characters
	opLoopMark           // regs[n] = start of an iteration that may be empty
	opLoopCheck          // after such an iteration: to x if it was empty, else to y
	opCall               // subroutine call of group n at x
	opCondRef            // continue if one of groups is set, else at x
	opCondRecurse        // continue if in a call (of group n when n >= 0), else at x
	opKeep               // \K
)

// Assertion mark kinds.
const (
	markAtomic uint8 = iota
	markLookPos
	markLookNeg
	markCondPos
	markCondNeg
)

type inst struct {
	op       opcode
	item     opcode // opRepeat: the repeated single-character op
	arg      uint8
	c, c2    byte
	r        rune
	n        int
	x, y     int
	min, max int
	s        string
	cls      *charClass
	groups   []int
}

// Regexp is a compiled PHP regular expression literal. It is safe for
// concurrent use.
type Regexp struct {
	pattern  string
	prog     []inst
	ncap     int      // number of capture groups
	names    []string // names[g]: the name of group g, or ""
	hasNames bool
	nregs    int
	utf      bool
	anchored bool       // a match can only start at the start offset
	first    *[256]bool // the bytes a match can start with; nil when unknown
	lookback int        // longest lookbehind in characters
	minLen   int        // a lower bound of the length of a match, in characters
	hasReq   bool       // every match contains the byte req (or req2)
	req      byte
	req2     byte
	pool     sync.Pool
}

// lowByte is the byte of a character known to be below 256: any
// character in byte mode, or an ASCII one.
func lowByte(r rune) byte { return byte(r & 0xFF) }

// maxProgram bounds the program size ("regular expression is too large").
const maxProgram = 1 << 20

// The regex cache maps pattern literals to their cacheEntry.
var (
	regexpCache     sync.Map
	regexpCacheSize atomic.Int64
)

type cacheEntry struct {
	re  *Regexp
	err *PatternError
}

// Compile compiles a PHP regular expression literal, delimiters and
// modifiers included, as preg_* functions do. Compiled patterns are cached
// like PHP's per-process regex cache. On failure it returns a
// *PatternError carrying the warning PHP would emit.
func Compile(pattern string) (*Regexp, error) {
	if v, ok := regexpCache.Load(pattern); ok {
		if e, _ := v.(cacheEntry); e.err != nil {
			return nil, e.err
		} else if e.re != nil {
			return e.re, nil
		}
	}
	re, perr := compile(pattern)
	if perr != nil {
		perr.Pattern = pattern
	}
	// Like PHP's PCRE_CACHE_SIZE, keep at most 4096 patterns.
	if regexpCacheSize.Load() >= 4096 {
		regexpCache.Clear()
		regexpCacheSize.Store(0)
	}
	if _, loaded := regexpCache.LoadOrStore(pattern, cacheEntry{re, perr}); !loaded {
		regexpCacheSize.Add(1)
	}
	if perr != nil {
		return nil, perr
	}
	return re, nil
}

// MustCompile is Compile for patterns known to be valid; it panics on
// error.
func MustCompile(pattern string) *Regexp {
	re, err := Compile(pattern)
	if err != nil {
		panic("php: " + err.Error() + " in " + pattern)
	}
	return re
}

// String returns the pattern literal.
func (re *Regexp) String() string { return re.pattern }

func compile(pattern string) (*Regexp, *PatternError) {
	o, perr := parseDelimiters(pattern)
	if perr != nil {
		return nil, perr
	}
	if o.utf && !utf8.ValidString(o.body) {
		i := 0
		for i < len(o.body) {
			r, n := utf8.DecodeRuneInString(o.body[i:])
			if r == utf8.RuneError && n == 1 {
				break
			}
			i += n
		}
		return nil, compileError("UTF-8 error: isolated byte with 0x80 bit set", i)
	}
	p := &parser{src: o.body, utf: o.utf, dollarEnd: o.dollarEnd, dupNames: o.dupNames}
	tree := p.parse(o.flags)
	if p.err == nil {
		p.resolve()
	}
	if p.err != nil {
		return nil, p.err
	}
	re := &Regexp{pattern: pattern, ncap: p.ncap, utf: o.utf}
	re.names = make([]string, p.ncap+1)
	for _, g := range p.names {
		re.names[g.n] = g.name
		re.hasNames = true
	}
	c := &compiler{re: re, utf: o.utf, groupStart: make(map[int]int), nregs: p.ncap + 1}
	c.gen(tree)
	c.emit(inst{op: opCapClose, n: 0})
	c.emit(inst{op: opMatch})
	for _, pc := range c.calls {
		if g := c.prog[pc].n; g > 0 {
			c.prog[pc].x = c.groupStart[g]
		}
	}
	if c.err == nil && len(c.prog) > maxProgram {
		c.err = compileError("regular expression is too large", len(o.body))
	}
	if c.err != nil {
		return nil, c.err
	}
	re.prog = c.prog
	re.nregs = c.nregs
	re.lookback = c.lookback
	re.anchored = o.anchored || isAnchored(tree)
	re.minLen = minChars(tree)
	re.req, re.req2, re.hasReq = reqByte(tree, o.utf)
	if set, nullable, ok := firstBytes(tree, o.utf); ok && !nullable {
		re.first = &set
	}
	return re, nil
}

// resolve checks group references and resolves names, once all groups
// are known.
func (p *parser) resolve() {
	lookup := func(n *reNode) []int {
		var gs []int
		for _, g := range p.names {
			if g.name == n.name {
				gs = append(gs, g.n)
			}
		}
		return gs
	}
	for _, n := range p.refs {
		if n.name != "" {
			gs := lookup(n)
			if len(gs) == 0 {
				p.fail("reference to non-existent subpattern", n.offset)
				return
			}
			n.n = gs[0]
			if len(gs) > 1 {
				n.dups = gs
			}
			continue
		}
		if n.n > p.ncap || n.op == reCall && n.n < 0 {
			p.fail("reference to non-existent subpattern", n.offset)
			return
		}
	}
}

// groupsOf returns the groups a back reference or condition refers to.
func (n *reNode) groupsOf() []int {
	if n.dups != nil {
		return n.dups
	}
	return []int{n.n}
}

type compiler struct {
	re         *Regexp
	prog       []inst
	nregs      int
	utf        bool
	groupStart map[int]int
	calls      []int
	lookback   int
	err        *PatternError
}

func (c *compiler) emit(i inst) int {
	c.prog = append(c.prog, i)
	return len(c.prog) - 1
}

func (c *compiler) newReg() int {
	c.nregs++
	return c.nregs - 1
}

func (c *compiler) gen(n *reNode) {
	if c.err != nil || len(c.prog) > maxProgram {
		return
	}
	switch n.op {
	case reEmpty:
	case reLit:
		c.emit(c.litInst(n))
	case reClass:
		c.emit(inst{op: opClass, cls: n.cls})
	case reAny:
		if n.dotall {
			c.emit(inst{op: opAnyNL})
		} else {
			c.emit(inst{op: opAny})
		}
	case reNewline:
		c.emit(inst{op: opNewline})
	case reAnchor:
		c.emit(inst{op: opAssert, arg: n.anchor})
	case reKeep:
		c.emit(inst{op: opKeep})
	case reConcat:
		c.genConcat(n.subs)
	case reAlt:
		c.genAlt(n.subs, nil)
	case reCapture:
		pc := c.emit(inst{op: opCapOpen, n: n.n})
		if _, ok := c.groupStart[n.n]; !ok {
			c.groupStart[n.n] = pc
		}
		c.gen(n.subs[0])
		c.emit(inst{op: opCapClose, n: n.n})
	case reGroup:
		c.gen(n.subs[0])
	case reAtomic:
		r := c.newReg()
		c.emit(inst{op: opAtomicOpen, n: r})
		c.gen(n.subs[0])
		c.emit(inst{op: opAtomicClose, n: r})
	case reLook:
		kind := markLookPos
		if n.look&lookNeg != 0 {
			kind = markLookNeg
		}
		open, _ := c.genLook(n, kind)
		c.prog[open].x = len(c.prog)
	case reRepeat:
		c.genRepeat(n)
	case reBackref:
		var fold uint8
		if n.fold {
			fold = 1
		}
		c.emit(inst{op: opBackref, arg: fold, groups: n.groupsOf()})
	case reCall:
		c.calls = append(c.calls, c.emit(inst{op: opCall, n: n.n}))
	case reCond:
		c.genCond(n)
	}
}

// genConcat emits a sequence, merging runs of case-sensitive literals.
func (c *compiler) genConcat(subs []*reNode) {
	var buf []byte
	flush := func() {
		switch {
		case len(buf) == 1:
			c.emit(inst{op: opByte, c: buf[0]})
		case len(buf) > 1:
			c.emit(inst{op: opStr, s: string(buf)})
		}
		buf = buf[:0]
	}
	for _, s := range subs {
		if s.op == reLit && !c.foldsTo(s) {
			if c.utf {
				buf = utf8.AppendRune(buf, s.r)
			} else {
				buf = append(buf, lowByte(s.r))
			}
			continue
		}
		flush()
		c.gen(s)
	}
	flush()
}

// genAlt emits alternatives; prefix, when set, is emitted at the start of
// each (the lookbehind reverse step).
func (c *compiler) genAlt(subs []*reNode, prefix func(*reNode)) {
	var jumps []int
	for i, s := range subs {
		split := -1
		if i < len(subs)-1 {
			split = c.emit(inst{op: opSplit})
			c.prog[split].x = split + 1
		}
		if prefix != nil {
			prefix(s)
		}
		c.gen(s)
		if split >= 0 {
			jumps = append(jumps, c.emit(inst{op: opJmp}))
			c.prog[split].y = len(c.prog)
		}
	}
	for _, j := range jumps {
		c.prog[j].x = len(c.prog)
	}
}

// genLook emits an assertion with the given mark kind and returns the pcs
// of its open and close instructions.
func (c *compiler) genLook(n *reNode, kind uint8) (open, closePC int) {
	r := c.newReg()
	open = c.emit(inst{op: opLookOpen, arg: kind, n: r})
	body := n.subs[0]
	var behind uint8
	if n.look&lookBehind != 0 {
		behind = 1
		alts := []*reNode{body}
		if body.op == reAlt {
			alts = body.subs
		}
		c.genAlt(alts, func(a *reNode) {
			lo, hi, ok := charLengths(a)
			if !ok || hi < 0 {
				if c.err == nil {
					c.err = compileError("length of lookbehind assertion is not limited", n.offset)
				}
				return
			}
			if hi > 65535 {
				if c.err == nil {
					c.err = compileError("branch too long in variable-length lookbehind assertion", n.offset)
				}
				return
			}
			c.lookback = max(c.lookback, hi)
			c.emit(inst{op: opReverse, min: lo, max: hi})
		})
	} else {
		c.gen(body)
	}
	closePC = c.emit(inst{op: opLookClose, n: r, arg: behind})
	return open, closePC
}

func (c *compiler) genCond(n *reNode) {
	yes := n.subs[0]
	var no *reNode
	if len(n.subs) > 1 {
		no = n.subs[1]
	}
	if n.cond == condAssert {
		neg := n.assert.look&lookNeg != 0
		kind := markCondPos
		if neg {
			kind = markCondNeg
		}
		open, closePC := c.genLook(n.assert, kind)
		yesPC := len(c.prog)
		c.gen(yes)
		j := c.emit(inst{op: opJmp})
		noPC := len(c.prog)
		if no != nil {
			c.gen(no)
		}
		c.prog[j].x = len(c.prog)
		if neg {
			c.prog[open].x = yesPC
			c.prog[closePC].x = noPC
		} else {
			c.prog[open].x = noPC
		}
		return
	}
	var test int
	switch n.cond {
	case condDefine:
		test = c.emit(inst{op: opJmp})
	case condGroup:
		test = c.emit(inst{op: opCondRef, groups: n.groupsOf()})
	case condRecurse:
		test = c.emit(inst{op: opCondRecurse, n: n.n})
	}
	c.gen(yes)
	if n.cond == condDefine {
		c.prog[test].x = len(c.prog)
		return
	}
	j := c.emit(inst{op: opJmp})
	c.prog[test].x = len(c.prog)
	if no != nil {
		c.gen(no)
	}
	c.prog[j].x = len(c.prog)
}

func (c *compiler) genRepeat(n *reNode) {
	sub := n.subs[0]
	if item, ok := c.singleItem(sub); ok {
		item.op, item.item = opRepeat, item.op
		item.min, item.max, item.arg = n.min, n.max, n.mode
		c.emit(item)
		return
	}
	if n.max == 0 {
		// Never obeyed, but compiled: its groups can still be called.
		j := c.emit(inst{op: opJmp})
		c.gen(sub)
		c.prog[j].x = len(c.prog)
		return
	}
	if n.mode == repPossessive {
		r := c.newReg()
		c.emit(inst{op: opAtomicOpen, n: r})
		c.genRepeatBody(sub, n.min, n.max, false)
		c.emit(inst{op: opAtomicClose, n: r})
		return
	}
	c.genRepeatBody(sub, n.min, n.max, n.mode == repLazy)
}

func (c *compiler) genRepeatBody(sub *reNode, minN, maxN int, lazy bool) {
	for range minN {
		c.gen(sub)
	}
	if maxN < 0 {
		// An iteration that matches the empty string ends the loop, as in
		// PCRE2.
		lo, _, ok := charLengths(sub)
		empty := !ok || lo == 0
		head := c.emit(inst{op: opSplit})
		body := len(c.prog)
		r := 0
		if empty {
			r = c.newReg()
			c.emit(inst{op: opLoopMark, n: r})
		}
		c.gen(sub)
		var back int
		if empty {
			back = c.emit(inst{op: opLoopCheck, n: r, y: head})
		} else {
			back = c.emit(inst{op: opJmp, x: head})
		}
		exit := len(c.prog)
		if empty {
			c.prog[back].x = exit
		}
		c.setSplit(head, body, exit, lazy)
		return
	}
	var splits []int
	for i := minN; i < maxN; i++ {
		s := c.emit(inst{op: opSplit})
		c.prog[s].x = len(c.prog)
		c.gen(sub)
		splits = append(splits, s)
		if len(c.prog) > maxProgram {
			return
		}
	}
	end := len(c.prog)
	for _, s := range splits {
		c.setSplit(s, s+1, end, lazy)
	}
}

func (c *compiler) setSplit(pc, body, exit int, lazy bool) {
	if lazy {
		c.prog[pc].x, c.prog[pc].y = exit, body
	} else {
		c.prog[pc].x, c.prog[pc].y = body, exit
	}
}

// foldsTo reports whether a caseless literal matches more than itself.
func (c *compiler) foldsTo(n *reNode) bool {
	if !n.fold {
		return false
	}
	if !c.utf {
		return n.r < 128 && unicode.IsLetter(n.r)
	}
	return unicode.SimpleFold(n.r) != n.r
}

// litInst returns the instruction matching one literal character.
func (c *compiler) litInst(n *reNode) inst {
	r := n.r
	if c.foldsTo(n) {
		if !c.utf {
			return inst{op: opByteFold, c: lowByte(r), c2: lowByte(r) ^ 0x20}
		}
		if asciiFoldPair(r) {
			return inst{op: opByteFold, c: lowByte(r), c2: lowByte(r) ^ 0x20}
		}
		b := newClassBuilder(true, true)
		b.addRune(r)
		return inst{op: opClass, cls: b.build(false)}
	}
	if !c.utf || r < 128 {
		return inst{op: opByte, c: lowByte(r)}
	}
	return inst{op: opRune, r: r, s: string(r)}
}

// singleItem returns the instruction of a node that matches exactly one
// character, for opRepeat.
func (c *compiler) singleItem(n *reNode) (inst, bool) {
	for n.op == reGroup && n.subs[0] != nil {
		n = n.subs[0]
	}
	switch n.op {
	case reLit:
		return c.litInst(n), true
	case reClass:
		return inst{op: opClass, cls: n.cls}, true
	case reAny:
		if n.dotall {
			return inst{op: opAnyNL}, true
		}
		return inst{op: opAny}, true
	}
	return inst{}, false
}

// charLengths returns the minimum and maximum number of characters n can
// match (max < 0: unbounded); ok is false when that is unknown (back
// references and calls).
func charLengths(n *reNode) (lo, hi int, ok bool) {
	switch n.op {
	case reEmpty, reAnchor, reLook, reKeep:
		return 0, 0, true
	case reLit, reClass, reAny:
		return 1, 1, true
	case reNewline:
		return 1, 2, true
	case reConcat:
		for _, s := range n.subs {
			l, h, k := charLengths(s)
			if !k {
				return 0, 0, false
			}
			lo += l
			if hi >= 0 {
				if h < 0 {
					hi = -1
				} else {
					hi += h
				}
			}
		}
		return lo, hi, true
	case reAlt:
		return altLengths(n.subs)
	case reCapture, reGroup, reAtomic:
		return charLengths(n.subs[0])
	case reCond:
		subs := n.subs
		if len(subs) == 1 && n.cond != condDefine {
			subs = append(subs[:1:1], &reNode{op: reEmpty})
		}
		if n.cond == condDefine {
			return 0, 0, true
		}
		return altLengths(subs)
	case reRepeat:
		l, h, k := charLengths(n.subs[0])
		if !k {
			return 0, 0, false
		}
		lo = l * n.min
		switch {
		case n.max < 0 && h != 0:
			hi = -1
		case h < 0:
			hi = -1
		default:
			hi = h * max(n.max, 0)
		}
		return lo, hi, true
	}
	return 0, 0, false
}

// asciiFoldPair reports whether the case variants of r are r and at most
// its ASCII other case (false for k and s, which fold to K and ſ).
func asciiFoldPair(r rune) bool {
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f >= utf8.RuneSelf {
			return false
		}
	}
	return r < utf8.RuneSelf
}

// reqByte ports PCRE2's "last required code unit": the byte of the last
// literal every match must contain, and its other case when caseless.
// pcre2_match fails at once when it is absent, which is what keeps
// hopeless patterns from reaching the match limit.
func reqByte(n *reNode, utf bool) (b, b2 byte, ok bool) {
	switch n.op {
	case reLit:
		if n.fold && utf && !asciiFoldPair(n.r) {
			// A caseless character with non-ASCII case variants has no
			// single other-case byte.
			return 0, 0, false
		}
		if utf && n.r >= utf8.RuneSelf {
			var buf [4]byte
			k := utf8.EncodeRune(buf[:], n.r)
			return buf[k-1], buf[k-1], true
		}
		b = lowByte(n.r)
		b2 = b
		if n.fold && n.r < 128 && unicode.IsLetter(n.r) {
			b2 = b ^ 0x20
		}
		return b, b2, true
	case reConcat:
		for _, sub := range slices.Backward(n.subs) {
			if b, b2, ok = reqByte(sub, utf); ok {
				return b, b2, true
			}
		}
	case reAlt:
		for i, sub := range n.subs {
			x, x2, k := reqByte(sub, utf)
			if !k || i > 0 && (x != b || x2 != b2) {
				return 0, 0, false
			}
			b, b2 = x, x2
		}
		return b, b2, len(n.subs) > 0
	case reCapture, reGroup, reAtomic:
		return reqByte(n.subs[0], utf)
	case reRepeat:
		if n.min > 0 {
			return reqByte(n.subs[0], utf)
		}
	}
	return 0, 0, false
}

// minChars is a lower bound of the number of characters n matches, like
// PCRE2's minimum length: back references and calls count as empty.
func minChars(n *reNode) int {
	switch n.op {
	case reLit, reClass, reAny, reNewline:
		return 1
	case reConcat:
		total := 0
		for _, s := range n.subs {
			total += minChars(s)
		}
		return total
	case reAlt:
		lo := -1
		for _, s := range n.subs {
			if l := minChars(s); lo < 0 || l < lo {
				lo = l
			}
		}
		return max(lo, 0)
	case reCapture, reGroup, reAtomic:
		return minChars(n.subs[0])
	case reRepeat:
		return n.min * minChars(n.subs[0])
	case reCond:
		if n.cond == condDefine || len(n.subs) < 2 {
			return 0
		}
		return min(minChars(n.subs[0]), minChars(n.subs[1]))
	}
	return 0
}

func altLengths(subs []*reNode) (lo, hi int, ok bool) {
	for i, s := range subs {
		l, h, k := charLengths(s)
		if !k {
			return 0, 0, false
		}
		if i == 0 {
			lo, hi = l, h
			continue
		}
		lo = min(lo, l)
		if hi >= 0 && (h < 0 || h > hi) {
			hi = h
		}
	}
	return lo, hi, true
}

// isAnchored reports whether every match must start at the start offset:
// each alternative begins with \A, \G or ^ outside multiline mode.
func isAnchored(n *reNode) bool {
	switch n.op {
	case reAnchor:
		return n.anchor == anchorBOL || n.anchor == anchorStart || n.anchor == anchorG
	case reConcat:
		return len(n.subs) > 0 && isAnchored(n.subs[0])
	case reAlt:
		for _, s := range n.subs {
			if !isAnchored(s) {
				return false
			}
		}
		return true
	case reCapture, reGroup, reAtomic:
		return isAnchored(n.subs[0])
	case reRepeat:
		return n.min > 0 && isAnchored(n.subs[0])
	}
	return false
}

// firstBytes computes the set of bytes a match of n can start with, and
// whether n can match the empty string. ok is false when the set is
// unknown.
func firstBytes(n *reNode, utf bool) (set [256]bool, nullable, ok bool) {
	addRune := func(r rune) {
		if utf {
			var b [4]byte
			utf8.EncodeRune(b[:], r)
			set[b[0]] = true
		} else {
			set[lowByte(r)] = true
		}
	}
	switch n.op {
	case reEmpty, reAnchor, reLook, reKeep:
		return set, true, true
	case reLit:
		addRune(n.r)
		if n.fold {
			if utf {
				for f := unicode.SimpleFold(n.r); f != n.r; f = unicode.SimpleFold(f) {
					addRune(f)
				}
			} else if n.r < 128 && unicode.IsLetter(n.r) {
				set[lowByte(n.r)^0x20] = true
			}
		}
		return set, false, true
	case reClass:
		for b := range rune(256) {
			if utf && b >= 128 {
				break
			}
			set[b] = n.cls.has(b)
		}
		if utf {
			// Any non-ASCII member starts with a lead byte.
			for b := 0xC2; b <= 0xF4; b++ {
				set[b] = true
			}
		}
		return set, false, true
	case reNewline:
		for _, b := range []byte{'\n', '\v', '\f', '\r'} {
			set[b] = true
		}
		if utf {
			set[0xC2], set[0xE2] = true, true
		} else {
			set[0x85] = true
		}
		return set, false, true
	case reConcat:
		for _, s := range n.subs {
			sub, null, k := firstBytes(s, utf)
			if !k {
				return set, false, false
			}
			for i, v := range sub {
				set[i] = set[i] || v
			}
			if !null {
				return set, false, true
			}
		}
		return set, true, true
	case reAlt:
		for _, s := range n.subs {
			sub, null, k := firstBytes(s, utf)
			if !k {
				return set, false, false
			}
			for i, v := range sub {
				set[i] = set[i] || v
			}
			nullable = nullable || null
		}
		return set, nullable, true
	case reCapture, reGroup, reAtomic:
		return firstBytes(n.subs[0], utf)
	case reRepeat:
		set, nullable, ok = firstBytes(n.subs[0], utf)
		return set, nullable || n.min == 0, ok
	}
	return set, false, false
}
