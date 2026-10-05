// Ports the shape of the code pcre2_compile.c (PCRE2 10.48) generates:
// how compile_branch expands repeated groups (copies, OP_BRAZERO nesting,
// the OP_SBRA forms of groups that can match an empty string, OP_BRAPOS
// for possessive repeats, OP_ONCE wrapping) and how it decides that a
// group "could match an empty string". pcre2_match counts every
// backtracking frame it creates against the match limit, and where it
// creates them follows this shape, so the program of pcre_vm.go is
// generated from it to count exactly as PCRE2 does.

package php

import (
	"slices"
	"sync"
)

// pkind is the kind of a pnode.
type pkind uint8

const (
	pkItem    pkind = iota // a single-character item (node: reLit, reClass, reAny, reNewline), repeated min..max
	pkRef                  // a back reference, repeated min..max
	pkRecurse              // OP_RECURSE
	pkAnchor               // a zero-width assertion
	pkKeep                 // \K
	pkBracket              // a group of the kind bra
)

// Bracket kinds (the opcode that opens the group).
type pbra uint8

const (
	pbBRA pbra = iota
	pbCBRA
	pbONCE
	pbASSERT
	pbASSERTNOT
	pbASSERTBACK
	pbASSERTBACKNOT
	pbCOND
)

// Closing kinds: OP_KET, OP_KETRMAX, OP_KETRMIN, OP_KETRPOS.
const (
	ketPlain uint8 = iota
	ketRMax
	ketRMin
	ketRPos
)

// Prefixes: OP_BRAZERO, OP_BRAMINZERO, OP_BRAPOSZERO, OP_SKIPZERO.
const (
	pfxNone uint8 = iota
	pfxZero
	pfxMinZero
	pfxPosZero
	pfxSkipZero
)

// pnode is an item of the compiled pattern as PCRE2 lays it out.
type pnode struct {
	kind     pkind
	node     *reNode // the source item (for brackets: the group, or the reCond)
	min, max int     // pkItem, pkRef: the repeat (max < 0: unlimited)
	mode     uint8   // pkItem, pkRef: repGreedy, repLazy or repPossessive
	bra      pbra
	n        int  // pbCBRA: the group number
	sform    bool // OP_SBRA, OP_SCBRA, OP_SCOND...: the group can match an empty string
	pos      bool // OP_BRAPOS forms
	ket      uint8
	prefix   uint8
	wrapper  bool // the bracket around the whole pattern
	branches [][]*pnode
	assert   *pnode // pbCOND with an assertion condition: the assertion

	// Set by pcre_jit.go: how the PCRE2 JIT compiles the item.
	noRepeat   bool  // a bracket holding an early-fail iterator, never a repeat
	jitExact   bool  // a copy of a bracket repeated a fixed number of times
	jitNoCount bool  // part of a repeat the JIT runs as one loop: no count of its own
	jitRunEnd  bool  // the end of such a loop: counted after the item
	efType     uint8 // pkItem: early-fail optimization
	efSlot     int

	// Set by linking, for auto-possessification.
	parent *pnode
	branch int // index of the branch of parent holding this node (-1: the condition of a pbCOND)
	index  int
}

// irBuilder turns a parsed pattern into pnodes.
type irBuilder struct {
	utf        bool
	hadRecurse bool
	slab       *slab[pnode]
	ws         *compileSpace
	stack      []*pnode // items of the sequences being built
}

// pn allocates a pnode.
func (b *irBuilder) pn(n pnode) *pnode { return b.slab.alloc(n) }

// buildIR returns the bracket around the whole pattern.
func buildIR(tree *reNode, utf bool, ws *compileSpace) (*pnode, bool) {
	b := &irBuilder{utf: utf, slab: &ws.pnodes, ws: ws}
	top := b.pn(pnode{kind: pkBracket, bra: pbBRA, wrapper: true, branches: b.alts(tree)})
	link(top)
	return top, b.hadRecurse
}

// link sets the parent pointers below p.
func link(p *pnode) {
	if p.assert != nil {
		p.assert.parent, p.assert.branch, p.assert.index = p, -1, 0
		link(p.assert)
	}
	for bi, br := range p.branches {
		for i, c := range br {
			c.parent, c.branch, c.index = p, bi, i
			if c.kind == pkBracket {
				link(c)
			}
		}
	}
}

// alts returns the branches of a group body.
func (b *irBuilder) alts(n *reNode) [][]*pnode {
	if n.op == reAlt {
		out := b.ws.branches.alloc(len(n.subs))
		for i, s := range n.subs {
			out[i] = b.seq(s)
		}
		return out
	}
	out := b.ws.branches.alloc(1)
	out[0] = b.seq(n)
	return out
}

// seq returns the items of a sequence.
func (b *irBuilder) seq(n *reNode) []*pnode {
	mark := len(b.stack)
	b.collect(n)
	if len(b.stack) == mark {
		return nil
	}
	out := b.ws.seqs.alloc(len(b.stack) - mark)
	copy(out, b.stack[mark:])
	b.stack = b.stack[:mark]
	return out
}

// collect pushes the items of n onto b.stack.
func (b *irBuilder) collect(n *reNode) {
	var p *pnode
	switch n.op {
	case reConcat:
		for _, s := range n.subs {
			b.collect(s)
		}
		return
	case reEmpty:
		return
	case reLit, reClass, reAny, reNewline:
		p = b.pn(pnode{kind: pkItem, node: n, min: 1, max: 1})
	case reAnchor:
		p = b.pn(pnode{kind: pkAnchor, node: n})
	case reKeep:
		p = b.pn(pnode{kind: pkKeep, node: n})
	case reBackref:
		p = b.pn(pnode{kind: pkRef, node: n, min: 1, max: 1})
	case reCall:
		b.hadRecurse = true
		p = b.pn(pnode{kind: pkRecurse, node: n})
	case reCapture, reGroup, reAtomic, reLook, reCond:
		p = b.bracket(n)
	case reRepeat:
		items := b.repeat(n)
		b.stack = append(b.stack, items...)
		return
	case reAlt:
		p = b.pn(pnode{kind: pkBracket, bra: pbBRA, node: n, branches: b.alts(n)})
	default:
		return
	}
	b.stack = append(b.stack, p)
}

// bracket returns a fresh copy of a group.
func (b *irBuilder) bracket(n *reNode) *pnode {
	p := b.pn(pnode{kind: pkBracket, node: n})
	switch n.op {
	case reCapture:
		p.bra, p.n = pbCBRA, n.n
	case reGroup:
		p.bra = pbBRA
	case reAtomic:
		p.bra = pbONCE
	case reLook:
		switch n.look {
		case 0:
			p.bra = pbASSERT
		case lookNeg:
			p.bra = pbASSERTNOT
		case lookBehind:
			p.bra = pbASSERTBACK
		default:
			p.bra = pbASSERTBACKNOT
		}
	case reCond:
		p.bra = pbCOND
		for _, s := range n.subs {
			p.branches = append(p.branches, b.seq(s))
		}
		if n.cond == condAssert {
			p.assert = b.bracket(n.assert)
		}
		return p
	}
	p.branches = b.alts(n.subs[0])
	return p
}

// repeat ports the handling of a quantifier in compile_branch.
func (b *irBuilder) repeat(n *reNode) []*pnode {
	sub := n.subs[0]
	switch sub.op {
	case reLit, reClass, reAny, reNewline:
		if n.max == 0 {
			return nil
		}
		it := b.pn(pnode{kind: pkItem, node: sub, min: n.min, max: n.max, mode: n.mode})
		if n.mode == repPossessive && n.min == 1 && n.max > 1 && b.isType(it) {
			// {1,m} keeps the item and adds an OP_UPTO; a character type
			// has no possessive form for the item, so PCRE2 wraps both in
			// OP_ONCE.
			return []*pnode{{kind: pkBracket, bra: pbONCE, branches: [][]*pnode{{it}}}}
		}
		return []*pnode{it}
	case reBackref:
		if n.max == 0 {
			return nil
		}
		return []*pnode{{kind: pkRef, node: sub, min: n.min, max: n.max, mode: n.mode}}
	case reCall:
		b.hadRecurse = true
		return b.repeatRecurse(sub, n.min, n.max, n.mode)
	}
	mk := func() *pnode { return b.bracket(sub) }
	return b.repeatGroup(mk, groupMayBeEmpty(sub), n.min, n.max, n.mode)
}

// isType reports whether a single-character item compiles to a
// character type opcode (or OP_PROP) rather than OP_CHAR, OP_NOT or a
// class.
func (b *irBuilder) isType(p *pnode) bool {
	in := itemInfo(p, b.utf)
	switch in.op {
	case pcCHAR, pcNOT:
		// A caseless character with several other cases is an OP_PROP
		// list.
		return b.utf && p.node.op == reLit && p.node.fold && len(in.chars) > 2 ||
			b.utf && p.node.op == reClass && p.node.pf != nil && p.node.pf.fold && len(in.chars) > 2
	case pcCLASS, pcNCLASS, pcXCLASS:
		return false
	}
	return true
}

// repeatRecurse ports the OP_RECURSE case: copies for the minimum, then
// the rest wrapped in OP_BRA and repeated as a group.
func (b *irBuilder) repeatRecurse(call *reNode, minN, maxN int, mode uint8) []*pnode {
	rec := func() *pnode { return b.pn(pnode{kind: pkRecurse, node: call}) }
	poss := mode == repPossessive
	if minN == 1 && maxN == 1 && !poss {
		return []*pnode{rec()}
	}
	var out []*pnode
	if minN > 0 && (minN != 1 || maxN >= 0) {
		replicate := minN
		if minN == maxN {
			replicate--
		}
		for range replicate {
			out = append(out, rec())
		}
		if minN == maxN {
			// The last copy is the original item.
			out = append(out, rec())
			if poss {
				return []*pnode{{kind: pkBracket, bra: pbONCE, branches: [][]*pnode{out}}}
			}
			return out
		}
		if maxN >= 0 {
			maxN -= minN
		}
		minN = 0
	}
	mk := func() *pnode {
		return b.pn(pnode{kind: pkBracket, bra: pbBRA, node: call, branches: [][]*pnode{{rec()}}})
	}
	return append(out, b.repeatGroup(mk, true, minN, maxN, mode)...)
}

// repeatGroup ports the repetition of a bracket: mk returns a fresh copy.
func (b *irBuilder) repeatGroup(mk func() *pnode, mayBeEmpty bool, minN, maxN int, mode uint8) []*pnode {
	poss := mode == repPossessive
	zero := pfxZero
	ket := ketRMax
	if mode == repLazy {
		zero, ket = pfxMinZero, ketRMin
	}
	g := mk()
	if minN == 1 && maxN == 1 && !poss {
		return []*pnode{g}
	}
	if g.bra == pbCOND && g.node.cond == condDefine {
		return []*pnode{g}
	}
	if g.bra >= pbASSERT && g.bra <= pbASSERTBACKNOT && maxN < 0 {
		maxN = minN + 1
	}
	var out []*pnode
	nest := &out
	if minN == 0 {
		if maxN <= 1 {
			if maxN == 0 {
				g.prefix = pfxSkipZero
				return []*pnode{g}
			}
			g.prefix = zero
			out = []*pnode{g}
		} else {
			outer := b.pn(pnode{kind: pkBracket, bra: pbBRA, prefix: zero, branches: [][]*pnode{{g}}})
			out = []*pnode{outer}
			nest = &outer.branches[0]
		}
		if maxN > 0 {
			maxN--
		}
	} else {
		out = []*pnode{g}
		for i := 1; i < minN; i++ {
			out = append(out, mk())
		}
		if maxN >= 0 {
			maxN -= minN
		}
	}
	if maxN >= 0 {
		for i := maxN; i >= 1; i-- {
			c := mk()
			if i != 1 {
				br := b.pn(pnode{kind: pkBracket, bra: pbBRA, prefix: zero, branches: [][]*pnode{{c}}})
				*nest = append(*nest, br)
				nest = &br.branches[0]
			} else {
				c.prefix = zero
				*nest = append(*nest, c)
			}
		}
	} else {
		// An unlimited maximum: the last copy repeats.
		last := len(out) - 1
		l := out[last]
		if l.bra == pbONCE && poss {
			l.bra = pbBRA
		}
		if l.bra == pbONCE {
			l.ket = ket
		} else {
			if mayBeEmpty || l.bra == pbCOND && len(l.branches) == 1 {
				l.sform = true
			}
			if poss {
				if l.bra == pbCOND {
					w := b.pn(pnode{kind: pkBracket, bra: pbBRA, pos: true, sform: l.sform, ket: ketRPos, prefix: l.prefix, branches: [][]*pnode{{l}}})
					l.prefix = pfxNone
					out[last] = w
					l = w
				} else {
					l.pos, l.ket = true, ketRPos
				}
				if l.prefix == pfxZero {
					l.prefix = pfxPosZero
				}
				if minN < 2 {
					poss = false
				}
			} else {
				l.ket = ket
			}
		}
	}
	if poss {
		return []*pnode{{kind: pkBracket, bra: pbONCE, branches: [][]*pnode{out}}}
	}
	return out
}

// groupMayBeEmpty ports the "could be empty" result of compile_regex for
// a group (group_return < 0): some branch has no item that must match a
// character.
func groupMayBeEmpty(n *reNode) bool {
	switch n.op {
	case reCond:
		if n.cond == condDefine {
			return true
		}
		for _, s := range n.subs {
			if !matchesChar(s) {
				return true
			}
		}
		return false
	case reCapture, reGroup, reAtomic, reLook:
		return !matchesChar(n.subs[0])
	}
	return true
}

// matchesChar ports compile_branch's matched_char: whether n must match at
// least one character. Back references, calls and assertions do not count.
func matchesChar(n *reNode) bool {
	switch n.op {
	case reLit, reClass, reAny, reNewline:
		return true
	case reConcat:
		return slices.ContainsFunc(n.subs, matchesChar)
	case reAlt:
		for _, s := range n.subs {
			if !matchesChar(s) {
				return false
			}
		}
		return len(n.subs) > 0
	case reCapture, reGroup, reAtomic:
		return matchesChar(n.subs[0])
	case reCond:
		if n.cond == condDefine || len(n.subs) < 2 {
			return false
		}
		return matchesChar(n.subs[0]) && matchesChar(n.subs[1])
	case reRepeat:
		return n.min > 0 && matchesChar(n.subs[0])
	}
	return false
}

// slab allocates values of T in chunks that are reused by later
// compilations (see compileSpace).
type slab[T any] struct {
	chunks [][]T
	ci, n  int
}

func (s *slab[T]) alloc(v T) *T {
	for {
		if s.ci < len(s.chunks) {
			if c := s.chunks[s.ci]; s.n < len(c) {
				p := &c[s.n]
				*p = v
				s.n++
				return p
			}
			s.ci, s.n = s.ci+1, 0
			continue
		}
		s.chunks = append(s.chunks, make([]T, min(16<<len(s.chunks), 4096)))
	}
}

func (s *slab[T]) reset() { s.ci, s.n = 0, 0 }

// sliceSlab allocates slices of T in chunks reused by later compilations.
// The slices have no spare capacity, so appending to one copies it.
type sliceSlab[T any] struct {
	chunks [][]T
	ci, n  int
}

func (s *sliceSlab[T]) alloc(n int) []T {
	for {
		if s.ci < len(s.chunks) {
			if c := s.chunks[s.ci]; s.n+n <= len(c) {
				out := c[s.n : s.n+n : s.n+n]
				s.n += n
				return out
			}
			s.ci, s.n = s.ci+1, 0
			continue
		}
		s.chunks = append(s.chunks, make([]T, max(n, min(64<<len(s.chunks), 8192))))
	}
}

func (s *sliceSlab[T]) reset() { s.ci, s.n = 0, 0 }

// compileSpace is the scratch memory of a compilation: the parsed
// pattern, its pnodes and the program before it is split by count model.
// None of it outlives the compilation, so it is pooled.
type compileSpace struct {
	nodes    slab[reNode]
	pnodes   slab[pnode]
	nodeSeqs sliceSlab[*reNode]
	seqs     sliceSlab[*pnode]
	branches sliceSlab[[]*pnode]
	prog     []inst
	newPC    []int32
}

func (ws *compileSpace) reset() {
	ws.nodes.reset()
	ws.pnodes.reset()
	ws.nodeSeqs.reset()
	ws.seqs.reset()
	ws.branches.reset()
	ws.prog = ws.prog[:0]
}

var compileSpaces = sync.Pool{New: func() any { return new(compileSpace) }}
