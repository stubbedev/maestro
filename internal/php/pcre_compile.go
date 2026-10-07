// Ports the compilation side of PCRE2 10.48 (pcre2_compile.c): group
// numbering and name resolution, lookbehind length checks, and the
// "first code unit" and anchoring analysis pcre2_match uses to skip start
// positions. The program it produces runs on the backtracking machine of
// pcre_vm.go; it is generated from the PCRE2 code layout of pcre_ir.go
// and marks with opCount where pcre2_match creates backtracking frames.

package php

import (
	"fmt"
	"maps"
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
	opRepeat             // item (op, c, c2, r, cls) repeated min..max times in mode arg (n 1: a class, counting the minimum too)
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
	opReverse            // lookbehind branch start: move back min..max characters (arg 1: counting each length, OP_VREVERSE)
	opLoopMark           // regs[n] = start of an iteration that may be empty
	opLoopCheck          // after such an iteration: to x if it was empty, else to y
	opCall               // subroutine call of group n at x
	opCondRef            // continue if one of groups is set, else at x
	opCondRecurse        // continue if in a call (of group n when n >= 0), else at x
	opKeep               // \K
	opCount              // pcre2_match creates a backtracking frame here
	opRefRepeat          // opBackref repeated min..max times in mode c
)

// Assertion mark kinds.
const (
	markAtomic uint8 = iota
	markLookPos
	markLookNeg
	markCondPos
	markCondNeg
)

// inst is an instruction. It holds no pointers: strings, classes, group
// lists and iterator data are in tables of the Regexp, indexed by aux and
// jit.
type inst struct {
	op       opcode
	item     opcode // opRepeat: the repeated single-character op
	arg      uint8
	c, c2    byte
	r        rune
	n        int32
	x, y     int32
	min, max int32
	aux      int32 // Regexp.strs (opStr, opRune), .classes (opClass, class items), .groups (opBackref, opRefRepeat, opCondRef)
	jit      int32 // opRepeat: Regexp.jits
}

// jitIter describes the JIT behaviour of an iterator that affects its
// count: see pcre_jit.go.
type jitIter struct {
	exactCount bool  // a separate OP_EXACT counts the minimum
	efAfterMin bool  // the early-fail check comes after the minimum
	efType     uint8 // early fail
	efSlot     int
	class      bool // an OP_CLASS iterator: pcre2_match counts its minimum position too
	charpos    bool // only positions followed by cpChar (with cpBit set) are tried
	cpChar     byte
	cpBit      byte
}

// Regexp is a compiled PHP regular expression literal. It is safe for
// concurrent use.
type Regexp struct {
	pattern string
	// lazy is set on a pattern from MustCompile, which is compiled on
	// first use; every exported method works on lazy.get() instead.
	lazy *lazyRegexp
	// The program for each count model: progJIT counts as the PCRE2 JIT
	// (every ordinary match in PHP), progInterp as pcre2_match (the
	// anchored retry after an empty match).
	progJIT, progInterp []inst
	strs                []string
	classes             []*charClass
	groups              [][]int
	jits                []jitIter
	saves               [][]int32 // opCall: the captures and registers a call may change
	interpOnce          sync.Once
	ncap                int      // number of capture groups
	names               []string // names[g]: the name of group g, or ""
	hasNames            bool
	nregs               int
	utf                 bool
	anchored            bool         // a match can only start at the start offset
	first               *[256]bool   // the bytes a match can start with; nil when unknown
	prefix              *startPrefix // the bytes of the first offsets of a match; nil when unknown
	lookback            int          // longest lookbehind in characters
	minLen              int          // a lower bound of the length of a match, in characters
	hasReq              bool         // every match contains the byte req (or req2)
	req                 byte
	req2                byte
	efSlots             int  // early-fail slots of the JIT iterators
	fastFwd             bool // the first iterator moves the next start position (JIT fast forward)
	ffSlot              int
	pool                sync.Pool
}

// lowByte is the byte of a character known to be below 256: any
// character in byte mode, or an ASCII one.
func lowByte(r rune) byte { return byte(r & 0xFF) }

// i32 converts a program position, size or count to an instruction
// field: programs are bounded by maxProgram and counts by 65535.
func i32(n int) int32 { return int32(n) } //nolint:gosec // bounded, see above

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

// MustCompile is Compile for patterns known to be valid. The pattern is
// compiled on first use, not by MustCompile: most package-level patterns
// are never used by a given run, and compiling them all made up most of
// the program's initialization (#29). It panics on that first use when
// the pattern is invalid; CheckMustCompiled compiles every pattern given
// to MustCompile so far, for tests. A pattern given again (by a function
// called repeatedly) is the same *Regexp.
func MustCompile(pattern string) *Regexp {
	lazyPatterns.Lock()
	defer lazyPatterns.Unlock()
	if re, ok := lazyPatterns.m[pattern]; ok {
		return re
	}
	if lazyPatterns.m == nil {
		// some 300 package-level patterns
		lazyPatterns.m = make(map[string]*Regexp, 512)
	}
	re := &Regexp{pattern: pattern, lazy: &lazyRegexp{pattern: pattern}}
	lazyPatterns.m[pattern] = re

	return re
}

type lazyRegexp struct {
	pattern string
	once    sync.Once
	re      *Regexp
	err     error
}

func (l *lazyRegexp) compile() {
	l.once.Do(func() { l.re, l.err = Compile(l.pattern) })
}

func (l *lazyRegexp) get() *Regexp {
	l.compile()
	if l.err != nil {
		panic("php: " + l.err.Error() + " in " + l.pattern)
	}
	return l.re
}

// lazyPatterns are the patterns given to MustCompile.
var lazyPatterns struct {
	sync.Mutex
	m map[string]*Regexp
}

// compiled returns the compiled form of re: re itself, or for a pattern
// from MustCompile, the pattern compiled on first use.
func (re *Regexp) compiled() *Regexp {
	if re.lazy != nil {
		return re.lazy.get()
	}
	return re
}

// CheckMustCompiled compiles every pattern given to MustCompile so far
// and returns the first error.
func CheckMustCompiled() error {
	lazyPatterns.Lock()
	patterns := slices.Sorted(maps.Keys(lazyPatterns.m))
	list := make([]*lazyRegexp, len(patterns))
	for i, pattern := range patterns {
		list[i] = lazyPatterns.m[pattern].lazy
	}
	lazyPatterns.Unlock()
	for _, l := range list {
		l.compile()
		if l.err != nil {
			return fmt.Errorf("%w in %s", l.err, l.pattern)
		}
	}
	return nil
}

// String returns the pattern literal.
func (re *Regexp) String() string { return re.pattern }

func compile(pattern string) (*Regexp, *PatternError) {
	return compileModel(pattern, countJIT)
}

// interpProg returns the program counting as pcre2_match, which only the
// anchored retry after an empty match needs: it is built on first use, by
// compiling the pattern again.
func (re *Regexp) interpProg() []inst {
	re.interpOnce.Do(func() {
		r, _ := compileModel(re.pattern, countInterp)
		re.progInterp = r.progInterp
	})
	return re.progInterp
}

// compileModel compiles pattern with the program of the count model.
func compileModel(pattern string, model uint8) (*Regexp, *PatternError) {
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
	ws, _ := compileSpaces.Get().(*compileSpace)
	defer func() {
		ws.reset()
		compileSpaces.Put(ws)
	}()
	p := &parser{src: o.body, utf: o.utf, dollarEnd: o.dollarEnd, dupNames: o.dupNames, slab: &ws.nodes, seqs: &ws.nodeSeqs}
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
	top, hadRecurse := buildIR(tree, o.utf, ws)
	autoPossessify(top, o.utf, hadRecurse)
	anchored := o.anchored || isAnchored(tree)
	ja := analyzeJIT(top, tree, o.utf, anchored)
	c := &compiler{re: re, utf: o.utf, groupStart: make([]int, p.ncap+1), groupEnd: make([]int, p.ncap+1), groupNode: make([]*pnode, p.ncap+1), nregs: p.ncap + 1}
	c.prog = ws.prog[:0]
	c.genBody(top)
	if bracketCounts(top) {
		c.countJ()
	}
	c.emit(inst{op: opCapClose, n: 0})
	c.emit(inst{op: opMatch})
	for _, pc := range c.calls {
		g := int(c.prog[pc].n)
		target := top
		if g > 0 {
			// A call skips the opCapOpen of the group: the group start
			// it records is only read when the group closes, which a
			// call returns at instead.
			c.prog[pc].x = i32(c.groupStart[g] + 1)
			target = c.groupNode[g]
		}
		// A call counts once, unless the JIT compiles it inline.
		if target != nil && !inlineRecursion(target, o.utf) {
			c.prog[pc].arg = 1
		}
	}
	c.saveSets(2 * (p.ncap + 1))
	c.threadJumps()
	re.efSlots = ja.slots
	if ja.fastFwd != nil {
		re.fastFwd, re.ffSlot = true, ja.fastFwd.efSlot
	}
	if c.err == nil && len(c.prog) > maxProgram {
		c.err = compileError("regular expression is too large", len(o.body))
	}
	if c.err != nil {
		return nil, c.err
	}
	re.strs, re.classes, re.groups, re.jits, re.saves = c.strs, c.classes, c.groupLists, c.jits, c.saves
	ws.prog = c.prog
	if model == countJIT {
		re.progJIT, ws.newPC = filterCounts(c.prog, countInterp, ws.newPC)
	} else {
		re.progInterp, ws.newPC = filterCounts(c.prog, countJIT, ws.newPC)
	}
	re.nregs = c.nregs
	re.lookback = c.lookback
	re.anchored = anchored
	// pcre2_study measures the minimum length only when the pattern
	// cannot match the empty string (PCRE2_MATCH_EMPTY unset).
	if matchesChar(tree) {
		re.minLen = patternMinLength(top, c.groupNode, p.dupCap)
	}
	re.req, re.req2, re.hasReq = reqByte(tree, o.utf)
	if re.prefix = buildPrefix(top, o.utf); re.prefix == nil {
		if set, nullable, ok := firstBytes(tree, o.utf); ok && !nullable {
			re.first = &set
		}
	} else if len(re.prefix.sets) == 1 && !re.prefix.single {
		// One offset: the table of first bytes is quicker.
		var set [256]bool
		for c := range set {
			set[c] = re.prefix.sets[0].has(byte(c))
		}
		re.first, re.prefix = &set, nil
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
	saves      [][]int32
	strs       []string
	classes    []*charClass
	classIdx   map[*charClass]int32
	groupLists [][]int
	jits       []jitIter
	prog       []inst
	nregs      int
	utf        bool
	groupStart []int    // pc of the opCapOpen of the first copy of each group
	groupEnd   []int    // pc of its opCapClose
	groupNode  []*pnode // its bracket
	calls      []int
	lookback   int
	err        *PatternError
}

func (c *compiler) str(s string) int32 {
	c.strs = append(c.strs, s)
	return i32(len(c.strs) - 1)
}

func (c *compiler) class(cls *charClass) int32 {
	if i, ok := c.classIdx[cls]; ok {
		return i
	}
	if c.classIdx == nil {
		c.classIdx = map[*charClass]int32{}
	}
	c.classes = append(c.classes, cls)
	i := i32(len(c.classes) - 1)
	c.classIdx[cls] = i
	return i
}

func (c *compiler) groupList(g []int) int32 {
	c.groupLists = append(c.groupLists, g)
	return i32(len(c.groupLists) - 1)
}

func (c *compiler) emit(i inst) int {
	c.prog = append(c.prog, i)
	return len(c.prog) - 1
}

func (c *compiler) newReg() int32 {
	c.nregs++
	return i32(c.nregs - 1)
}

// threadJumps makes jumps and splits that lead to an opJmp go to its
// target.
func (c *compiler) threadJumps() {
	final := func(pc int32) int32 {
		for n := 0; c.prog[pc].op == opJmp && n < 16; n++ {
			pc = c.prog[pc].x
		}
		return pc
	}
	for i := range c.prog {
		in := &c.prog[i]
		switch in.op {
		case opJmp, opLookOpen, opCondRef, opCondRecurse:
			in.x = final(in.x)
		case opSplit, opLoopCheck:
			in.x, in.y = final(in.x), final(in.y)
		}
	}
}

// saveSets gives each call the captures and registers (registers after
// the ncaps capture offsets) that the code it can reach sets: those are
// what a return restores.
func (c *compiler) saveSets(ncaps int) {
	if len(c.calls) == 0 {
		return
	}
	setOf := make([]int32, len(c.groupStart))
	for i := range setOf {
		setOf[i] = -1
	}
	seen := make([]bool, ncaps+c.nregs)
	done := make([]bool, len(c.groupStart))
	for _, pc := range c.calls {
		g := int(c.prog[pc].n)
		if setOf[g] >= 0 {
			c.prog[pc].aux = setOf[g]
			continue
		}
		clear(seen)
		clear(done)
		var slots []int32
		add := func(sl int) {
			if !seen[sl] {
				seen[sl] = true
				slots = append(slots, i32(sl))
			}
		}
		todo := []int{g}
		for len(todo) > 0 {
			h := todo[len(todo)-1]
			todo = todo[:len(todo)-1]
			if done[h] {
				continue
			}
			done[h] = true
			lo, hi := 0, len(c.prog)-1
			if h > 0 {
				lo, hi = c.groupStart[h], c.groupEnd[h]
			}
			if h > 0 {
				// Calls skip the opCapOpen and return at the opCapClose.
				lo, hi = lo+1, hi-1
			}
			for _, in := range c.prog[lo : hi+1] {
				switch in.op {
				case opCapOpen, opLoopMark:
					add(ncaps + int(in.n))
				case opCapClose:
					if in.n > 0 {
						add(2 * int(in.n))
						add(2*int(in.n) + 1)
					}
				case opCall:
					todo = append(todo, int(in.n))
				}
			}
		}
		slices.Sort(slots)
		c.saves = append(c.saves, slots)
		setOf[g] = i32(len(c.saves) - 1)
		c.prog[pc].aux = setOf[g]
	}
}

// genSeq emits a sequence, merging runs of case-sensitive literals.
func (c *compiler) genSeq(seq []*pnode) {
	var buf []byte
	flush := func() {
		switch {
		case len(buf) == 1:
			c.emit(inst{op: opByte, c: buf[0]})
		case len(buf) > 1:
			c.emit(inst{op: opStr, aux: c.str(string(buf))})
		}
		buf = buf[:0]
	}
	for i, p := range seq {
		if p.kind == pkItem && p.min == 1 && p.max == 1 && p.node.op == reLit && !c.foldsTo(p.node) {
			if c.utf {
				buf = utf8.AppendRune(buf, p.node.r)
			} else {
				buf = append(buf, lowByte(p.node.r))
			}
			continue
		}
		flush()
		var next *pnode
		if i+1 < len(seq) {
			next = seq[i+1]
		}
		c.genNode(p, next)
		if p.jitRunEnd {
			c.countJ()
		}
	}
	flush()
}

func (c *compiler) genNode(p, next *pnode) {
	if c.err != nil || len(c.prog) > maxProgram {
		return
	}
	switch p.kind {
	case pkItem:
		item := c.itemInst(p.node)
		if p.min == 1 && p.max == 1 {
			c.emit(item)
			return
		}
		item.op, item.item = opRepeat, item.op
		item.min, item.max, item.arg = i32(p.min), i32(p.max), p.mode
		c.jits = append(c.jits, c.iterJIT(p, next))
		item.jit = i32(len(c.jits) - 1)
		c.emit(item)
	case pkRef:
		var fold uint8
		if p.node.fold {
			fold = 1
		}
		in := inst{op: opBackref, arg: fold, aux: c.groupList(p.node.groupsOf())}
		if p.min != 1 || p.max != 1 {
			in.op, in.min, in.max, in.c = opRefRepeat, i32(p.min), i32(p.max), p.mode
		}
		c.emit(in)
	case pkRecurse:
		c.calls = append(c.calls, c.emit(inst{op: opCall, n: i32(p.node.n)}))
	case pkAnchor:
		c.emit(inst{op: opAssert, arg: p.node.anchor})
	case pkKeep:
		c.emit(inst{op: opKeep})
	case pkBracket:
		c.genBracket(p)
	}
}

// iterJIT returns how the JIT runs a repeated item followed by next.
func (c *compiler) iterJIT(p, next *pnode) jitIter {
	sh := shapeOf(p, c.utf)
	j := jitIter{exactCount: sh.exact, efSlot: p.efSlot, efType: p.efType, class: sh.class}
	// The early-fail check is made where the iterator opcode starts:
	// after a separate OP_EXACT or a {1,m} leading item.
	j.efAfterMin = sh.exact || sh.lead
	if p.efType == efSkip {
		j.efType = efSkip
	}
	// An iterator other than of a literal, followed by a literal, only
	// stops where that literal follows ("charpos").
	if p.mode == repGreedy && (sh.v == vStar || sh.v == vUpto) && !sh.charLit && !sh.allany &&
		next != nil && next.kind == pkItem && next.min == 1 && next.max == 1 && next.node.op == reLit {
		ni := itemInfo(next, c.utf)
		r := next.node.r
		switch {
		case c.utf && r >= utf8.RuneSelf:
		case len(ni.chars) == 1:
			j.charpos, j.cpChar = true, lowByte(r)
		case len(ni.chars) == 2 && ni.chars[0]^ni.chars[1] == 0x20 && ni.chars[1] < utf8.RuneSelf:
			j.charpos, j.cpChar, j.cpBit = true, lowByte(r)|0x20, 0x20
		}
	}
	return j
}

// itemInst returns the instruction matching one character of a
// single-character item.
func (c *compiler) itemInst(n *reNode) inst {
	switch n.op {
	case reLit:
		return c.litInst(n)
	case reClass:
		return inst{op: opClass, aux: c.class(n.cls)}
	case reAny:
		if n.dotall {
			return inst{op: opAnyNL}
		}
		return inst{op: opAny}
	}
	return inst{op: opNewline}
}

// filterCounts returns prog without the opCount instructions of model
// drop, its jump targets remapped.
func filterCounts(prog []inst, drop uint8, buf []int32) ([]inst, []int32) {
	if cap(buf) < len(prog)+1 {
		buf = make([]int32, len(prog)+1)
	}
	newPC := buf[:len(prog)+1]
	n := 0
	for pc, in := range prog {
		newPC[pc] = i32(n)
		if in.op != opCount || in.arg != drop {
			n++
		}
	}
	newPC[len(prog)] = i32(n)
	out := make([]inst, 0, n)
	for _, in := range prog {
		if in.op == opCount && in.arg == drop {
			continue
		}
		switch in.op {
		case opSplit, opLoopCheck:
			in.x, in.y = newPC[in.x], newPC[in.y]
		case opJmp, opLookOpen, opLookClose, opCall, opCondRef, opCondRecurse:
			in.x = newPC[in.x]
		}
		out = append(out, in)
	}
	return out, newPC
}

// Count models of opCount (arg): the PCRE2 JIT, used by PHP for every
// ordinary match, and pcre2_match, used for the anchored retries after an
// empty match.
const (
	countJIT uint8 = iota
	countInterp
)

func (c *compiler) countJ() { c.emit(inst{op: opCount, arg: countJIT}) }
func (c *compiler) countI() { c.emit(inst{op: opCount, arg: countInterp}) }

// bracketCounts reports whether the JIT counts when control leaves a
// group that does not repeat: it has alternatives, or is optional.
func bracketCounts(p *pnode) bool {
	if p.jitNoCount || p.bra >= pbASSERT && p.bra <= pbASSERTBACKNOT {
		return false
	}
	if p.jitExact || p.prefix == pfxZero || p.prefix == pfxMinZero {
		return true
	}
	if p.bra == pbCOND {
		n := p.node
		return n.cond == condGroup || n.cond == condAssert
	}
	return len(p.branches) > 1
}

// genBracket emits a group with its prefix and repetition, and the
// count the JIT makes when control leaves it.
func (c *compiler) genBracket(p *pnode) {
	if p.prefix == pfxSkipZero {
		j := c.emit(inst{op: opJmp})
		c.genBody(p)
		c.prog[j].x = i32(len(c.prog))
		return
	}
	if p.ket != ketPlain {
		c.genLoop(p)
		return
	}
	switch p.prefix {
	case pfxZero:
		// OP_BRAZERO: pcre2_match makes a frame for the group.
		s := c.emit(inst{op: opSplit})
		c.prog[s].x = i32(s + 1)
		c.countI()
		c.genBody(p)
		c.prog[s].y = i32(len(c.prog))
	case pfxMinZero:
		// OP_BRAMINZERO: a frame for what follows, else the group.
		s := c.emit(inst{op: opSplit})
		c.prog[s].x = i32(s + 1)
		c.countI()
		j := c.emit(inst{op: opJmp})
		c.prog[s].y = i32(len(c.prog))
		c.genBody(p)
		c.prog[j].x = i32(len(c.prog))
	default:
		c.genBody(p)
	}
	if bracketCounts(p) {
		c.countJ()
	}
}

// genLoop emits a group ending in OP_KETRMAX, OP_KETRMIN or OP_KETRPOS.
// An iteration that matches the empty string ends the loop, as in PCRE2.
// pcre2_match makes a frame for each iteration it tries (greedy) or for
// what follows each iteration (lazy); the JIT counts each time control
// leaves the loop.
func (c *compiler) genLoop(p *pnode) {
	pos := p.ket == ketRPos
	lazy := p.ket == ketRMin
	var atomic int32
	if pos {
		atomic = c.newReg()
		c.emit(inst{op: opAtomicOpen, n: atomic})
	}
	var exitRefs [][2]int // (pc, 0 for x or 1 for y) to point at the exit
	var loopRefs []int    // jumps to the loop start
	switch p.prefix {
	case pfxZero:
		s := c.emit(inst{op: opSplit})
		c.prog[s].x = i32(s + 1)
		exitRefs = append(exitRefs, [2]int{s, 1})
		c.countI()
	case pfxPosZero:
		s := c.emit(inst{op: opSplit})
		c.prog[s].x = i32(s + 1)
		exitRefs = append(exitRefs, [2]int{s, 1})
	case pfxMinZero:
		s := c.emit(inst{op: opSplit})
		c.prog[s].x = i32(s + 1)
		c.countI()
		exitRefs = append(exitRefs, [2]int{c.emit(inst{op: opJmp}), 0})
		c.prog[s].y = i32(len(c.prog))
	}
	loop := len(c.prog)
	check := loopMayBeEmpty(p)
	var reg int32
	if check {
		reg = c.newReg()
		c.emit(inst{op: opLoopMark, n: reg})
	}
	if pos {
		// Each iteration of OP_BRAPOS is atomic: PCRE2 discards its
		// backtracking frames when it ends. This also keeps the stack
		// from growing with the iterations.
		r := c.newReg()
		c.emit(inst{op: opAtomicOpen, n: r})
		c.genBody(p)
		c.emit(inst{op: opAtomicClose, n: r})
	} else {
		c.genBody(p)
	}
	if check {
		lc := c.emit(inst{op: opLoopCheck, n: reg})
		exitRefs = append(exitRefs, [2]int{lc, 0})
		c.prog[lc].y = i32(len(c.prog))
	}
	k := c.emit(inst{op: opSplit})
	switch {
	case pos:
		c.prog[k].x = i32(loop)
		exitRefs = append(exitRefs, [2]int{k, 1})
	case lazy:
		// OP_KETRMIN: a frame for what follows, else another iteration.
		c.prog[k].x, c.prog[k].y = i32(k+1), i32(loop)
		c.countI()
		exitRefs = append(exitRefs, [2]int{c.emit(inst{op: opJmp}), 0})
	default:
		// OP_KETRMAX: a frame for another iteration, else what follows.
		c.prog[k].x = i32(k + 1)
		exitRefs = append(exitRefs, [2]int{k, 1})
		c.countI()
		loopRefs = append(loopRefs, c.emit(inst{op: opJmp}))
	}
	exit := len(c.prog)
	for _, r := range exitRefs {
		if r[1] == 0 {
			c.prog[r[0]].x = i32(exit)
		} else {
			c.prog[r[0]].y = i32(exit)
		}
	}
	for _, j := range loopRefs {
		c.prog[j].x = i32(loop)
	}
	if pos {
		c.emit(inst{op: opAtomicClose, n: atomic})
	}
	if !p.jitNoCount {
		c.countJ()
	}
}

// loopMayBeEmpty reports whether an iteration of a repeated group may
// match the empty string.
func loopMayBeEmpty(p *pnode) bool {
	for p.node == nil {
		if len(p.branches) != 1 || len(p.branches[0]) != 1 {
			return true
		}
		p = p.branches[0][0]
	}
	lo, _, ok := charLengths(p.node)
	return !ok || lo == 0
}

// genBody emits one execution of a group.
func (c *compiler) genBody(p *pnode) {
	switch p.bra {
	case pbBRA:
		c.genAlts(p.branches, p.sform || p.pos || p.wrapper)
	case pbCBRA:
		pc := c.emit(inst{op: opCapOpen, n: i32(p.n)})
		if c.groupNode[p.n] == nil {
			c.groupStart[p.n] = pc
			c.groupNode[p.n] = p
		}
		c.genAlts(p.branches, true)
		end := c.emit(inst{op: opCapClose, n: i32(p.n)})
		if c.groupNode[p.n] == p {
			c.groupEnd[p.n] = end
		}
	case pbONCE:
		r := c.newReg()
		c.emit(inst{op: opAtomicOpen, n: r})
		c.genAlts(p.branches, true)
		c.emit(inst{op: opAtomicClose, n: r})
	case pbASSERT, pbASSERTBACK:
		open, _ := c.genLook(p, markLookPos)
		c.prog[open].x = i32(len(c.prog))
	case pbASSERTNOT, pbASSERTBACKNOT:
		open, _ := c.genLook(p, markLookNeg)
		c.prog[open].x = i32(len(c.prog))
	case pbCOND:
		c.genCond(p)
	}
}

// genAlts emits the branches of a group. pcre2_match makes a frame for
// each branch it tries, except the last one of an OP_BRA.
func (c *compiler) genAlts(branches [][]*pnode, countAll bool) {
	var jumps []int
	for i, b := range branches {
		split := -1
		last := i == len(branches)-1
		if !last {
			split = c.emit(inst{op: opSplit})
			c.prog[split].x = i32(split + 1)
		}
		if countAll || !last {
			c.countI()
		}
		c.genSeq(b)
		if !last {
			jumps = append(jumps, c.emit(inst{op: opJmp}))
			c.prog[split].y = i32(len(c.prog))
		}
	}
	for _, j := range jumps {
		c.prog[j].x = i32(len(c.prog))
	}
}

// genLook emits an assertion with the given mark kind and returns the pcs
// of its open and close instructions.
func (c *compiler) genLook(p *pnode, kind uint8) (open, closePC int) {
	r := c.newReg()
	open = c.emit(inst{op: opLookOpen, arg: kind, n: r})
	var behind uint8
	if p.bra == pbASSERTBACK || p.bra == pbASSERTBACKNOT {
		behind = 1
	}
	alts := lookAlts(p.node)
	var jumps []int
	for i, b := range p.branches {
		split := -1
		last := i == len(p.branches)-1
		if !last {
			split = c.emit(inst{op: opSplit})
			c.prog[split].x = i32(split + 1)
		}
		c.countI()
		if behind == 1 {
			lo, hi, ok := charLengths(alts[i])
			if !ok || hi < 0 {
				if c.err == nil {
					c.err = compileError("length of lookbehind assertion is not limited", p.node.offset)
				}
				return open, open
			}
			if hi > 65535 {
				if c.err == nil {
					c.err = compileError("branch too long in variable-length lookbehind assertion", p.node.offset)
				}
				return open, open
			}
			c.lookback = max(c.lookback, hi)
			// pcre2_match's OP_VREVERSE makes a frame for each length.
			var vrev uint8
			if lo != hi {
				vrev = 1
			}
			c.emit(inst{op: opReverse, min: i32(lo), max: i32(hi), arg: vrev})
		}
		c.genSeq(b)
		if !last {
			jumps = append(jumps, c.emit(inst{op: opJmp}))
			c.prog[split].y = i32(len(c.prog))
		}
	}
	for _, j := range jumps {
		c.prog[j].x = i32(len(c.prog))
	}
	closePC = c.emit(inst{op: opLookClose, n: r, arg: behind})
	return open, closePC
}

// genCond emits a conditional group.
func (c *compiler) genCond(p *pnode) {
	n := p.node
	yes := p.branches[0]
	var no []*pnode
	if len(p.branches) > 1 {
		no = p.branches[1]
	}
	if n.cond == condAssert {
		neg := p.assert.bra == pbASSERTNOT || p.assert.bra == pbASSERTBACKNOT
		kind := markCondPos
		if neg {
			kind = markCondNeg
		}
		open, closePC := c.genLook(p.assert, kind)
		yesPC := len(c.prog)
		c.condCount(p)
		c.genSeq(yes)
		j := c.emit(inst{op: opJmp})
		noPC := len(c.prog)
		c.condCount(p)
		c.genSeq(no)
		c.prog[j].x = i32(len(c.prog))
		if neg {
			c.prog[open].x = i32(yesPC)
			c.prog[closePC].x = i32(noPC)
		} else {
			c.prog[open].x = i32(noPC)
		}
		return
	}
	var test int
	switch n.cond {
	case condDefine:
		test = c.emit(inst{op: opJmp})
	case condGroup:
		test = c.emit(inst{op: opCondRef, aux: c.groupList(n.groupsOf())})
	case condRecurse:
		test = c.emit(inst{op: opCondRecurse, n: i32(n.n)})
	}
	c.condCount(p)
	c.genSeq(yes)
	if n.cond == condDefine {
		c.prog[test].x = i32(len(c.prog))
		return
	}
	j := c.emit(inst{op: opJmp})
	c.prog[test].x = i32(len(c.prog))
	c.condCount(p)
	c.genSeq(no)
	c.prog[j].x = i32(len(c.prog))
}

// condCount: pcre2_match's OP_SCOND makes a frame for the branch it
// chooses.
func (c *compiler) condCount(p *pnode) {
	if p.sform {
		c.countI()
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
		return inst{op: opClass, aux: c.class(b.build(false))}
	}
	if !c.utf || r < 128 {
		return inst{op: opByte, c: lowByte(r)}
	}
	return inst{op: opRune, r: r, aux: c.str(string(r))}
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
