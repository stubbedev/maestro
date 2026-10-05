// Ports the idea of scan_prefix and fast_forward_first_n_chars of
// pcre2_jit_compile.c (PCRE2 10.48): the bytes a match can begin with, at
// each of its first few offsets, so that start positions where no match
// can begin are skipped. The JIT checks two of those offsets; this checks
// them all, which skips the same impossible starts and possibly more.

package php

import (
	"math/bits"
	"strings"
	"unicode/utf8"
)

// byteSet is a set of bytes.
type byteSet [4]uint64

func (b *byteSet) add(c byte)      { b[c>>6] |= 1 << (c & 63) }
func (b *byteSet) has(c byte) bool { return b[c>>6]&(1<<(c&63)) != 0 }
func (b *byteSet) union(o *byteSet) {
	b[0], b[1], b[2], b[3] = b[0]|o[0], b[1]|o[1], b[2]|o[2], b[3]|o[3]
}

// size returns the number of bytes in the set, and one of them.
func (b *byteSet) size() (n int, c byte) {
	for i, w := range b {
		if w != 0 {
			n += bits.OnesCount64(w)
			c = byte(i*64 + bits.TrailingZeros64(w)) //nolint:gosec // below 256
		}
	}
	return n, c
}

// maxPrefix is MAX_N_CHARS.
const maxPrefix = 12

// startPrefix is the byte sets of the first offsets of every match.
type startPrefix struct {
	sets    []byteSet
	key     int  // the offset searched first: the one with the fewest bytes
	keyByte byte // when the key set holds one byte
	single  bool
}

// prefixScanner explores the paths through the pnodes.
type prefixScanner struct {
	utf    bool
	sets   [maxPrefix]byteSet
	length int // the shortest prefix any path determined
	steps  int
	conts  []cont // storage for the path stacks
}

// buildPrefix computes the prefix of the pattern, or nil when none is
// known.
func buildPrefix(top *pnode, utf bool) *startPrefix {
	ps := prefixScanner{utf: utf, length: maxPrefix, conts: make([]cont, 0, 64)}
	ps.walk([]cont{{seq: nil}}, top, 0)
	if ps.steps > 10000 || ps.length == 0 {
		return nil
	}
	p := &startPrefix{sets: append([]byteSet(nil), ps.sets[:ps.length]...)}
	best := 257
	for i := range p.sets {
		if n, c := p.sets[i].size(); n < best {
			best, p.key, p.keyByte = n, i, c
		}
	}
	if best == 256 {
		return nil
	}
	p.single = best == 1
	return p
}

// cont is what follows the end of a sequence: the rest of an enclosing
// sequence.
type cont struct {
	seq []*pnode
	idx int
}

// stop ends a path at offset off: nothing is known beyond it.
func (ps *prefixScanner) stop(off int) {
	ps.length = min(ps.length, off)
}

// walk explores the paths entering the bracket b at offset off, with
// stack holding what follows it.
func (ps *prefixScanner) walk(stack []cont, b *pnode, off int) {
	for _, br := range b.branches {
		ps.run(append(ps.clone(stack), cont{seq: br}), off)
	}
}

// run explores the path continuing with the top of stack at offset off.
func (ps *prefixScanner) run(stack []cont, off int) {
	for {
		if ps.steps++; ps.steps > 10000 {
			ps.stop(0)
			return
		}
		if off >= ps.length {
			return
		}
		top := &stack[len(stack)-1]
		if top.idx >= len(top.seq) {
			stack = stack[:len(stack)-1]
			if len(stack) == 0 || len(stack) == 1 && stack[0].seq == nil {
				// The end of the pattern: a match may end here.
				ps.stop(off)
				return
			}
			continue
		}
		n := top.seq[top.idx]
		top.idx++
		switch n.kind {
		case pkAnchor, pkKeep:
			continue
		case pkItem:
			set, ok := ps.itemSet(n)
			if !ok {
				ps.stop(off)
				return
			}
			if n.min == 1 && n.max == 1 {
				ps.sets[off].union(&set)
				off++
				continue
			}
			if n.min == 0 && n.max == 1 {
				// The character, or nothing.
				ps.run(ps.clone(stack), off)
				ps.sets[off].union(&set)
				off++
				continue
			}
			for k := 0; k < n.min && off < ps.length; k++ {
				ps.sets[off].union(&set)
				off++
			}
			if n.min == n.max {
				continue
			}
			ps.stop(off)
			return
		case pkBracket:
			if n.bra >= pbASSERT && n.bra <= pbASSERTBACKNOT || n.bra == pbCOND && n.node.cond == condDefine {
				// Assertions and DEFINE groups match nothing.
				continue
			}
			if n.bra == pbCOND || n.prefix == pfxSkipZero || n.sform {
				ps.stop(off)
				return
			}
			if n.prefix != pfxNone {
				// Optional: the group is skipped, or entered.
				ps.run(ps.clone(stack), off)
			}
			if n.ket != ketPlain {
				// After an iteration it may iterate again.
				ps.walk([]cont{{seq: nil}, {seq: nil}}, n, off)
				return
			}
			ps.walk(ps.clone(stack), n, off)
			return
		default:
			ps.stop(off)
			return
		}
	}
}

// clone copies a path stack, with room for one more entry.
func (ps *prefixScanner) clone(s []cont) []cont {
	n := len(s) + 1
	if cap(ps.conts)-len(ps.conts) < n {
		ps.conts = make([]cont, 0, max(256, 2*n))
	}
	c := ps.conts[len(ps.conts) : len(ps.conts)+len(s) : len(ps.conts)+n]
	copy(c, s)
	ps.conts = ps.conts[:len(ps.conts)+n]
	return c
}

// itemSet returns the bytes a single-character item starts with, when
// the item is one byte long.
func (ps *prefixScanner) itemSet(n *pnode) (byteSet, bool) {
	var set byteSet
	nd := n.node
	switch nd.op {
	case reLit:
		for _, r := range caseList(nd.r, nd.fold, ps.utf) {
			if ps.utf && r >= utf8.RuneSelf || r > 0xFF {
				return set, false
			}
			set.add(lowByte(r))
		}
		return set, true
	case reClass:
		c := nd.cls
		if ps.utf && (c.neg || len(c.ranges) > 0 || len(c.props) > 0 || c.foldU || c.bits[2]|c.bits[3] != 0) {
			return set, false
		}
		return byteSet(c.bits), true
	case reAny:
		if ps.utf {
			return set, false
		}
		for i := range 256 {
			if i != '\n' || nd.dotall {
				set.add(byte(i))
			}
		}
		return set, true
	}
	return set, false
}

// next returns the first position from st where every prefix offset
// matches, or -1.
func (p *startPrefix) next(s string, st int) int {
	n := len(p.sets)
	for st+n <= len(s) {
		if p.single {
			i := strings.IndexByte(s[st+p.key:len(s)-n+p.key+1], p.keyByte)
			if i < 0 {
				return -1
			}
			st += i
		} else {
			for !p.sets[p.key].has(s[st+p.key]) {
				if st++; st+n > len(s) {
					return -1
				}
			}
		}
		ok := true
		for k := range p.sets {
			if !p.sets[k].has(s[st+k]) {
				ok = false
				break
			}
		}
		if ok {
			return st
		}
		st++
	}
	return -1
}
