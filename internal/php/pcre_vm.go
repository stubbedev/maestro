// Ports the matching semantics of pcre2_match (PCRE2 10.48,
// pcre2_match.c) as a backtracking machine with an explicit stack: start
// position scanning, PCRE2_ANCHORED, PCRE2_NOTEMPTY_ATSTART, captures that
// are set when their group closes, atomic groups and assertions, bounded
// lookbehind, empty-iteration loop termination, and subroutine calls and
// recursion with captures reverting on return (PCRE2 >= 10.30).
//
// PCRE2's match limit (pcre.backtrack_limit, 1000000 in PHP) bounds the
// count of one start position. PHP matches with the JIT, which counts at
// iterator backtracks, group exits and recursion entries
// (pcre2_jit_compile.c); the anchored retry after an empty match runs on
// pcre2_match, which counts its backtracking frames. A Regexp has a
// program for each model (progJIT, progInterp), marking where it counts
// (opCount, and the repeats, calls and backreferences), so the count,
// and which subjects exhaust the limit, are PHP's.

package php

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits of one pcre2_match call, and PCRE2's REQ_CU_MAX.
const (
	matchLimit = 1000000
	depthLimit = 100000
	reqMax     = 5000
)

// Match errors, mapped to preg_last_error() codes by the preg functions.
// PHP runs patterns with the JIT by default, which reports recursion that
// loops or nests too deeply as an exhausted JIT stack.
const (
	matchOK = iota
	matchErrBacktrack
	matchErrRecursion
)

// btEntry is a backtrack stack entry.
type btEntry struct {
	kind uint8
	sub  uint8 // btMark: the mark kind
	prev int32 // choice points and marks: the previous one (machine.choiceTop)
	pc   int32
	b    int32
	pos  int
	a    int
}

const (
	// Undo entries restore state when backtracking passes them.
	btCap   uint8 = iota // caps[a] = pos
	btReg                // regs[a] = pos
	btKeep               // keep = pos
	btFrame              // fp = a; when b >= 0, frames[:b] and arena[:pos]
	// Choice points resume matching.
	btChoice    // at pc, pos
	btRepGreedy // give back one character: at pc, prev(pos) while pos > a
	btRepLazy   // take one more item of prog[b]: at pc; a counts items
	btReverse   // lookbehind: next start position pos+1 char, a tries left; b 1: counted
	btRefGreedy // back reference repeat: one repetition less, b left; a: position after the minimum
	btRefLazy   // back reference repeat: one more repetition; a counts them
	btMark      // atomic group or assertion boundary
)

type frame struct {
	parent int32
	group  int32
	ret    int32
	save   int32 // the Regexp.saves entry listing the saved values
	pos    int   // subject position of the call
	off    int   // saved captures and registers in arena[off:]
}

// machine is the per-match state; machines are pooled per Regexp.
type machine struct {
	re          *Regexp
	s           string
	caps        []int
	regs        []int
	stack       []btEntry
	frames      []frame
	arena       []int
	fp          int
	keep        int
	searchStart int
	notEmpty    bool // PCRE2_NOTEMPTY_ATSTART
	steps       int
	err         int
	seen        []uint32 // cut: undo entries already kept, by slot
	gen         uint32
	ef          []int  // early-fail slots: per slot, the end and start of a range (-1: unset)
	interp      bool   // count as pcre2_match does, not as the JIT
	peak        int    // the highest count of the start positions tried
	prog        []inst // re.progJIT or re.progInterp
	strs        []string
	classes     []*charClass
	groups      [][]int
	jits        []jitIter
	saves       [][]int32
	choiceTop   int     // index of the latest choice point or mark on the stack, or -1
	lastTrail   []int32 // per capture, register and \K: index of its latest undo entry
}

func (re *Regexp) getMachine(s string) *machine {
	m, _ := re.pool.Get().(*machine)
	if m == nil {
		m = &machine{re: re, caps: make([]int, 2*(re.ncap+1)), regs: make([]int, re.nregs)}
		m.lastTrail = make([]int32, len(m.caps)+len(m.regs)+1)
		m.strs, m.classes, m.groups, m.jits, m.saves = re.strs, re.classes, re.groups, re.jits, re.saves
	}
	m.s = s
	return m
}

func (re *Regexp) putMachine(m *machine) {
	m.s = ""
	if cap(m.stack) > 1<<16 {
		m.stack = nil
	}
	m.frames, m.arena = m.frames[:0], m.arena[:0]
	re.pool.Put(m)
}

// exec ports one pcre2_match call from start: it reports whether a match
// was found (its groups are then in m.caps) and the error, if any.
func (m *machine) exec(start int, anchored, notEmpty bool) (bool, int) {
	// PHP matches with the JIT, except for the anchored retry after an
	// empty match, which pcre2_match runs without it.
	return m.execModel(start, anchored, notEmpty, anchored)
}

// execModel is exec counting against the match limit as pcre2_match
// (interp) or as the JIT.
func (m *machine) execModel(start int, anchored, notEmpty, interp bool) (bool, int) {
	re := m.re
	m.searchStart, m.notEmpty, m.err = start, notEmpty, matchOK
	m.interp, m.peak = interp, 0
	prog := re.progJIT
	if m.interp {
		prog = re.interpProg()
	}
	if len(m.prog) == 0 || &m.prog[0] != &prog[0] {
		// Stored only on a change, to save the write barrier.
		m.prog = prog
	}
	s := m.s
	// The required byte must occur; PCRE2 skips this search for very long
	// subjects.
	if re.hasReq && (len(s)-start < reqMax || !anchored && !re.anchored && len(s)-start < reqMax*1000) {
		rest := s[start:]
		if strings.IndexByte(rest, re.req) < 0 && (re.req2 == re.req || strings.IndexByte(rest, re.req2) < 0) {
			return false, matchOK
		}
	}
	// A run that fails backtracks through all its changes, so the groups
	// and registers need resetting only once.
	for i := range m.caps {
		m.caps[i] = -1
	}
	for i := range m.regs {
		m.regs[i] = -1
	}
	if n := 2 * re.efSlots; n > 0 {
		if cap(m.ef) < n {
			m.ef = make([]int, n)
		}
		m.ef = m.ef[:n]
		for i := range m.ef {
			m.ef[i] = -1
		}
	}
	for st := start; st <= len(s); {
		if re.prefix != nil && !anchored && !re.anchored {
			if st = re.prefix.next(s, st); st < 0 {
				return false, matchOK
			}
		} else if re.first != nil && !anchored && !re.anchored {
			for st < len(s) && !re.first[s[st]] {
				st++
			}
			if st >= len(s) {
				return false, matchOK
			}
		}
		// pcre2_match does not try where the rest of the subject is
		// shorter than the shortest possible match.
		if len(s)-st < re.minLen {
			return false, matchOK
		}
		if re.fastFwd && !m.interp {
			m.ef[2*re.ffSlot] = st
		}
		found := m.run(st)
		m.peak = max(m.peak, m.steps)
		if found {
			return true, matchOK
		}
		if m.err != matchOK {
			return false, m.err
		}
		if anchored || re.anchored || st >= len(s) {
			break
		}
		if re.fastFwd && !m.interp {
			// The JIT continues after where the first iterator stopped.
			if st = m.ef[2*re.ffSlot]; st >= len(s) {
				break
			}
		}
		st += m.charLen(st)
	}
	return false, matchOK
}

// charLen is the length of the character at pos (1 in byte mode).
func (m *machine) charLen(pos int) int {
	if !m.re.utf || m.s[pos] < utf8.RuneSelf {
		return 1
	}
	_, n := utf8.DecodeRuneInString(m.s[pos:])
	return n
}

// prevChar is the start of the character before pos.
func (m *machine) prevChar(pos int) int {
	pos--
	if m.re.utf {
		for pos > 0 && m.s[pos]&0xC0 == 0x80 {
			pos--
		}
	}
	return pos
}

// char decodes the character at pos (pos < len(s)).
func (m *machine) char(pos int) (rune, int) {
	c := m.s[pos]
	if !m.re.utf || c < utf8.RuneSelf {
		return rune(c), 1
	}
	return utf8.DecodeRuneInString(m.s[pos:])
}

// step matches one single-character item at pos and returns the position
// after it, or -1.
func (m *machine) step(op opcode, in *inst, pos int) int {
	s := m.s
	if pos >= len(s) {
		return -1
	}
	switch op {
	case opByte:
		if s[pos] == in.c {
			return pos + 1
		}
	case opByteFold:
		if c := s[pos]; c == in.c || c == in.c2 {
			return pos + 1
		}
	case opRune:
		if r, n := m.char(pos); r == in.r {
			return pos + n
		}
	case opClass:
		if !m.re.utf || s[pos] < utf8.RuneSelf {
			if m.classes[in.aux].has(rune(s[pos])) {
				return pos + 1
			}
			return -1
		}
		if r, n := utf8.DecodeRuneInString(s[pos:]); m.classes[in.aux].has(r) {
			return pos + n
		}
	case opAny:
		if s[pos] != '\n' {
			return pos + m.charLen(pos)
		}
	case opAnyNL:
		return pos + m.charLen(pos)
	case opNewline:
		if s[pos] == '\r' && pos+1 < len(s) && s[pos+1] == '\n' {
			return pos + 2
		}
		if r, n := m.char(pos); isVSpace(r) {
			return pos + n
		}
	}
	return -1
}

// count records a backtracking frame of pcre2_match against the match
// limit.
func (m *machine) count() bool {
	m.steps++
	if m.steps > matchLimit {
		m.err = matchErrBacktrack
		return false
	}
	return true
}

// next appends an entry to the stack and returns it. Callers fill it
// field by field: copying in a whole btEntry assembled from narrower
// stores stalls on store forwarding.
func (m *machine) next() *btEntry {
	n := len(m.stack)
	if n == cap(m.stack) {
		m.stack = append(m.stack, btEntry{})
	} else {
		m.stack = m.stack[:n+1]
	}
	return &m.stack[n]
}

// push pushes an undo entry.
func (m *machine) push(kind uint8, pos, a int, b int32) {
	e := m.next()
	e.kind, e.sub, e.prev, e.pc, e.b, e.pos, e.a = kind, 0, -1, 0, b, pos, a
}

// innerMark returns the index of the latest mark on the stack: groups
// nest, so it is the one of the group being closed.
func (m *machine) innerMark() int {
	i := m.choiceTop
	for m.stack[i].kind != btMark {
		i = int(m.stack[i].prev)
	}
	return i
}

// pushChoice pushes a choice point or mark.
func (m *machine) pushChoice(kind, sub uint8, pc int32, pos, a int, b int32) {
	e := m.next()
	e.kind, e.sub, e.prev, e.pc, e.b, e.pos, e.a = kind, sub, int32(m.choiceTop), pc, b, pos, a //nolint:gosec // a stack index
	m.choiceTop = len(m.stack) - 1
}

// popTo truncates the stack to n entries.
func (m *machine) popTo(n int) {
	for m.choiceTop >= n {
		m.choiceTop = int(m.stack[m.choiceTop].prev)
	}
	m.stack = m.stack[:n]
}

// trail pushes the undo entry e of slot, unless one is already above the
// latest choice point: backtracking there restores the oldest such entry,
// which holds the value to restore (the trail check of a WAM).
func (m *machine) trail(slot int, kind uint8, a, pos int) {
	if t := int(m.lastTrail[slot]); t > m.choiceTop && t < len(m.stack) && m.stack[t].kind == kind && m.stack[t].a == a {
		return
	}
	m.lastTrail[slot] = int32(len(m.stack)) //nolint:gosec // a stack index
	m.push(kind, pos, a, 0)
}

func (m *machine) setCap(i, v int) {
	m.trail(i, btCap, i, m.caps[i])
	m.caps[i] = v
}

func (m *machine) setReg(i, v int) {
	m.trail(len(m.caps)+i, btReg, i, m.regs[i])
	m.regs[i] = v
}

// run tries to match at st.
func (m *machine) run(st int) bool {
	s := m.s
	prog := m.prog
	m.stack = m.stack[:0]
	m.choiceTop = -1
	m.frames, m.arena = m.frames[:0], m.arena[:0]
	m.fp, m.keep = -1, st
	m.steps = 0
	if m.interp {
		// The frame pcre2_match starts with.
		m.steps = 1
	}
	pc, pos := 0, st
	for {
		in := &prog[pc]
		ok := true
		switch in.op {
		case opByte:
			if ok = pos < len(s) && s[pos] == in.c; ok {
				pos++
				pc++
			}
		case opStr:
			if ok = len(s)-pos >= len(m.strs[in.aux]) && s[pos:pos+len(m.strs[in.aux])] == m.strs[in.aux]; ok {
				pos += len(m.strs[in.aux])
				pc++
			}
		case opByteFold, opRune, opClass, opAny, opAnyNL:
			np := m.step(in.op, in, pos)
			if ok = np >= 0; ok {
				pos = np
				pc++
			}
		case opNewline:
			np := m.step(opNewline, in, pos)
			if ok = np >= 0; ok {
				pos = np
				pc++
			}
		case opCount:
			if ok = m.count(); ok {
				pc++
			}
		case opRefRepeat:
			pos, ok = m.refRepeat(in, pc, pos)
			pc++
		case opRepeat:
			pos, ok = m.repeat(in, pc, pos)
			pc++
		case opSplit:
			m.pushChoice(btChoice, 0, in.y, pos, 0, 0)
			pc = int(in.x)
		case opJmp:
			pc = int(in.x)
		case opCapOpen:
			m.setReg(int(in.n), pos)
			pc++
		case opCapClose:
			if m.fp >= 0 && m.frames[m.fp].group == in.n {
				pc = m.ret()
				break
			}
			if int(in.n) > 0 {
				m.setCap(2*int(in.n), m.regs[int(in.n)])
				m.setCap(2*int(in.n)+1, pos)
			}
			pc++
		case opAssert:
			if ok = m.assert(in.arg, pos); ok {
				pc++
			}
		case opBackref:
			np := m.backref(in, pos)
			if ok = np >= 0; ok {
				pos = np
				pc++
			}
		case opAtomicOpen, opLookOpen:
			sub := markAtomic
			if in.op == opLookOpen {
				sub = in.arg
			}
			m.pushChoice(btMark, sub, in.x, pos, len(m.frames), i32(m.fp))
			pc++
		case opAtomicClose:
			m.cut(m.innerMark())
			pc++
		case opLookClose:
			idx := m.innerMark()
			mark := m.stack[idx]
			if ok = in.arg == 0 || pos == mark.pos; !ok {
				break
			}
			switch mark.sub {
			case markLookPos, markCondPos:
				m.cut(idx)
				pos = mark.pos
				pc++
			case markLookNeg:
				m.unwind(idx)
				ok = false
			case markCondNeg:
				m.unwind(idx)
				pos = mark.pos
				pc = int(in.x)
			}
		case opReverse:
			k, p := 0, pos
			for k < int(in.max) && p > 0 {
				p = m.prevChar(p)
				k++
			}
			if ok = k >= int(in.min); ok {
				if in.arg == 1 && m.interp {
					ok = m.count()
				}
				if k > int(in.min) {
					m.pushChoice(btReverse, 0, i32(pc+1), p, k-int(in.min), int32(in.arg))
				}
				pos = p
				pc++
			}
		case opLoopMark:
			m.setReg(int(in.n), pos)
			pc++
		case opLoopCheck:
			if pos == m.regs[int(in.n)] {
				pc = int(in.x)
			} else {
				pc = int(in.y)
			}
		case opCall:
			if ok = m.call(in, pc, pos); ok {
				pc = int(in.x)
			}
		case opCondRef:
			next := int(in.x)
			for _, g := range m.groups[in.aux] {
				if m.caps[2*g] >= 0 {
					next = pc + 1
					break
				}
			}
			pc = next
		case opCondRecurse:
			if m.fp >= 0 && (in.n < 0 || m.frames[m.fp].group == in.n) {
				pc++
			} else {
				pc = int(in.x)
			}
		case opKeep:
			m.trail(len(m.caps)+len(m.regs), btKeep, 0, m.keep)
			m.keep = pos
			pc++
		case opMatch:
			// PCRE2_NOTEMPTY_ATSTART rejects an empty match at the start.
			if ok = !m.notEmpty || pos != m.searchStart || m.keep != m.searchStart; ok {
				m.caps[0], m.caps[1] = m.keep, pos
				return true
			}
		}
		if ok {
			continue
		}
		if m.err != matchOK {
			return false
		}
		var resumed bool
		if pc, pos, resumed = m.backtrack(); !resumed {
			return false
		}
	}
}

// repeat runs opRepeat at pos. Counting as the JIT, the separate OP_EXACT
// of a minimum counts once and the iterator counts each position it
// continues from; counting as pcre2_match, each continuation from a
// position above the minimum (from any position, for a class) is a frame
// (see pcre_jit.go).
func (m *machine) repeat(in *inst, pc, pos int) (int, bool) {
	j := &m.jits[in.jit]
	ef := j.efType != efNone && m.fp < 0 && !m.interp
	if ef && !j.efAfterMin && m.earlyFail(j, pos) {
		return 0, false
	}
	count := 0
	for ; count < int(in.min); count++ {
		if pos = m.step(in.item, in, pos); pos < 0 {
			return 0, false
		}
	}
	if !m.interp && j.exactCount && !m.count() {
		return 0, false
	}
	if int(in.min) == int(in.max) {
		if !m.interp && !j.exactCount && !m.count() {
			return 0, false
		}
		return pos, true
	}
	if ef && j.efAfterMin && m.earlyFail(j, pos) {
		return 0, false
	}
	lo := pos
	if in.arg == repLazy {
		if ef {
			m.ef[2*j.efSlot] = pos
		}
		if !m.count() {
			return 0, false
		}
		m.pushChoice(btRepLazy, 0, i32(pc+1), pos, count, i32(pc))
		return pos, true
	}
	for int(in.max) < 0 || count < int(in.max) {
		np := m.step(in.item, in, pos)
		if np < 0 {
			break
		}
		pos = np
		count++
	}
	if ef {
		m.ef[2*j.efSlot] = pos
	}
	if in.arg == repPossessive {
		if !m.interp && !m.count() {
			return 0, false
		}
		return pos, true
	}
	if !m.interp && j.charpos {
		if pos = m.charpos(j, lo, pos); pos < 0 {
			return 0, false
		}
	}
	if (!m.interp || pos > lo || j.class) && !m.count() {
		return 0, false
	}
	if pos > lo {
		m.pushChoice(btRepGreedy, 0, i32(pc+1), pos, lo, 0)
	}
	return pos, true
}

// charpos returns the last position from hi down to lo where the literal
// following a charpos iterator matches, or -1.
func (m *machine) charpos(j *jitIter, lo, hi int) int {
	s := m.s
	for p := hi; ; p = m.prevChar(p) {
		if p < len(s) && s[p]|j.cpBit == j.cpChar {
			return p
		}
		if p <= lo {
			return -1
		}
	}
}

// earlyFail ports the JIT's early-fail check before an iterator: it fails
// at once where an earlier scan of the same iterator already failed.
func (m *machine) earlyFail(j *jitIter, pos int) bool {
	k := 2 * j.efSlot
	switch j.efType {
	case efFail:
		return m.ef[k] >= 0 && pos <= m.ef[k]
	case efRange:
		if m.ef[k+1] >= 0 && m.ef[k+1] <= pos && pos <= m.ef[k] {
			return true
		}
		m.ef[k], m.ef[k+1] = pos, pos
	}
	return false
}

// backtrack resumes at the most recent choice point, undoing state on
// the way.
func (m *machine) backtrack() (pc, pos int, ok bool) {
	for len(m.stack) > 0 {
		top := len(m.stack) - 1
		e := &m.stack[top]
		switch e.kind {
		case btCap:
			m.caps[e.a] = e.pos
		case btReg:
			m.regs[e.a] = e.pos
		case btKeep:
			m.keep = e.pos
		case btFrame:
			m.fp = e.a
			if e.b >= 0 {
				m.frames, m.arena = m.frames[:e.b], m.arena[:e.pos]
			}
		case btChoice:
			m.popTo(top)
			return int(e.pc), e.pos, true
		case btRepGreedy:
			in := &m.prog[e.pc-1]
			p := m.prevChar(e.pos)
			if in.item == opNewline && p > e.a && m.s[p] == '\n' && m.s[p-1] == '\r' {
				p--
			}
			if m.jits[in.jit].charpos && !m.interp {
				if p = m.charpos(&m.jits[in.jit], e.a, p); p < 0 {
					break
				}
			}
			e.pos = p
			at := int(e.pc)
			if p <= e.a {
				m.popTo(top)
			}
			if (!m.interp || p > e.a || m.jits[in.jit].class) && !m.count() {
				return 0, 0, false
			}
			return at, p, true
		case btRepLazy:
			in := &m.prog[e.b]
			if int(in.max) < 0 || e.a < int(in.max) {
				if np := m.step(in.item, in, e.pos); np >= 0 {
					e.pos = np
					e.a++
					p, at := e.pos, int(e.pc)
					if int(in.max) >= 0 && e.a >= int(in.max) {
						m.popTo(top)
					}
					if !m.count() {
						return 0, 0, false
					}
					return at, p, true
				}
			}
		case btReverse:
			if e.a > 0 {
				e.pos += m.charLen(e.pos)
				e.a--
				p, at := e.pos, int(e.pc)
				if e.b == 1 && m.interp && !m.count() {
					return 0, 0, false
				}
				if e.a == 0 {
					m.popTo(top)
				}
				return at, p, true
			}
		case btRefGreedy:
			if pc, pos, ok := m.refBack(top); ok || m.err != matchOK {
				return pc, pos, ok
			}
		case btRefLazy:
			if pc, pos, ok := m.refMore(top); ok || m.err != matchOK {
				return pc, pos, ok
			}
		case btMark:
			if e.sub == markLookNeg || e.sub == markCondPos || e.sub == markCondNeg {
				m.popTo(top)
				return int(e.pc), e.pos, true
			}
		}
		m.popTo(top)
	}
	return 0, 0, false
}

// cut ends an atomic group or positive assertion whose mark is at idx:
// its choice points are discarded. Of its undo entries only the oldest
// per capture, register and \K is kept, since backtracking past them
// restores the state before the group. Subroutine calls made inside the
// group have returned, so their frames are dropped; this is what keeps
// the stack and the frames as small as PCRE2's frame vector, which
// collapses the frames of a completed atomic group into one.
func (m *machine) cut(idx int) {
	mark := m.stack[idx]
	noChoices := m.choiceTop == idx
	m.choiceTop = int(mark.prev)
	balanced := m.fp == int(mark.b) && m.fp < mark.a
	dropFrames := balanced && len(m.frames) > mark.a
	if dropFrames {
		m.arena = m.arena[:m.frames[mark.a].off]
		m.frames = m.frames[:mark.a]
	}
	if noChoices && !dropFrames {
		// Nothing to discard but the mark: the trail check kept the undo
		// entries of the group to one per slot.
		m.stack = append(m.stack[:idx], m.stack[idx+1:]...)
		return
	}
	m.gen++
	if m.gen == 0 {
		clear(m.seen)
		m.gen = 1
	}
	if n := len(m.caps) + len(m.regs) + 1; len(m.seen) < n {
		m.seen = make([]uint32, n)
	}
	ncaps := len(m.caps)
	w := idx
	for _, e := range m.stack[idx+1:] {
		var slot int
		switch e.kind {
		case btCap:
			slot = e.a
		case btReg:
			slot = ncaps + e.a
		case btKeep:
			slot = len(m.seen) - 1
		case btFrame:
			if balanced {
				continue
			}
			m.stack[w] = e
			w++
			continue
		default:
			continue
		}
		if m.seen[slot] == m.gen {
			continue
		}
		m.seen[slot] = m.gen
		m.stack[w] = e
		w++
	}
	m.stack = m.stack[:w]
}

// unwind discards everything from idx up, undoing state changes.
func (m *machine) unwind(idx int) {
	for top := len(m.stack) - 1; top >= idx; top-- {
		e := &m.stack[top]
		switch e.kind {
		case btCap:
			m.caps[e.a] = e.pos
		case btReg:
			m.regs[e.a] = e.pos
		case btKeep:
			m.keep = e.pos
		case btFrame:
			m.fp = e.a
			if e.b >= 0 {
				m.frames, m.arena = m.frames[:e.b], m.arena[:e.pos]
			}
		}
	}
	m.popTo(idx)
}

// call enters a subroutine call of group int(in.n).
func (m *machine) call(in *inst, pc, pos int) bool {
	for f := m.fp; f >= 0; f = int(m.frames[f].parent) {
		if m.frames[f].group == in.n {
			if m.frames[f].pos == pos {
				m.err = matchErrRecursion
				return false
			}
			break
		}
	}
	if len(m.frames) >= depthLimit {
		m.err = matchErrRecursion
		return false
	}
	if in.arg == 1 && !m.interp && !m.count() {
		return false
	}
	m.push(btFrame, len(m.arena), m.fp, i32(len(m.frames)))
	m.frames = append(m.frames, frame{parent: i32(m.fp), group: in.n, ret: i32(pc + 1), pos: pos, off: len(m.arena), save: in.aux})
	ncaps := len(m.caps)
	for _, sl := range m.saves[in.aux] {
		if int(sl) < ncaps {
			m.arena = append(m.arena, m.caps[sl])
		} else {
			m.arena = append(m.arena, m.regs[int(sl)-ncaps])
		}
	}
	m.fp = len(m.frames) - 1
	return true
}

// ret returns from the current call: captures and registers revert to
// their values before the call.
func (m *machine) ret() int {
	f := m.frames[m.fp]
	saved := m.arena[f.off:]
	ncaps := len(m.caps)
	for k, sl := range m.saves[f.save] {
		v := saved[k]
		if i := int(sl); i < ncaps {
			if m.caps[i] != v {
				m.setCap(i, v)
			}
		} else if m.regs[i-ncaps] != v {
			m.setReg(i-ncaps, v)
		}
	}
	m.push(btFrame, 0, m.fp, -1)
	m.fp = int(f.parent)
	return int(f.ret)
}

// assert tests a zero-width assertion at pos.
func (m *machine) assert(kind uint8, pos int) bool {
	s := m.s
	switch kind {
	case anchorBOL, anchorStart:
		return pos == 0
	case anchorMBOL:
		return pos == 0 || pos < len(s) && s[pos-1] == '\n'
	case anchorEOL, anchorEndZ:
		return pos == len(s) || pos == len(s)-1 && s[pos] == '\n'
	case anchorMEOL:
		return pos == len(s) || s[pos] == '\n'
	case anchorEnd:
		return pos == len(s)
	case anchorG:
		return pos == m.searchStart
	case anchorWordB, anchorNotWordB:
		before, after := false, false
		if pos > 0 {
			r, _ := m.char(m.prevChar(pos))
			before = isWordChar(r, m.re.utf)
		}
		if pos < len(s) {
			r, _ := m.char(pos)
			after = isWordChar(r, m.re.utf)
		}
		return (before != after) == (kind == anchorWordB)
	}
	return false
}

// backref matches the text of the first set group of in.groups at pos.
func (m *machine) backref(in *inst, pos int) int {
	g := m.refGroup(in)
	if g < 0 {
		return -1
	}
	return m.refAt(g, in.arg != 0, pos)
}

// refGroup returns the first set group of in.groups, or -1.
func (m *machine) refGroup(in *inst) int {
	for _, x := range m.groups[in.aux] {
		if m.caps[2*x] >= 0 {
			return x
		}
	}
	return -1
}

// refAt matches the text of group g at pos.
func (m *machine) refAt(g int, fold bool, pos int) int {
	ref := m.s[m.caps[2*g]:m.caps[2*g+1]]
	s := m.s[pos:]
	if !fold {
		if len(s) >= len(ref) && s[:len(ref)] == ref {
			return pos + len(ref)
		}
		return -1
	}
	if !m.re.utf {
		if len(s) < len(ref) {
			return -1
		}
		for i := range len(ref) {
			if lowerASCII(ref[i]) != lowerASCII(s[i]) {
				return -1
			}
		}
		return pos + len(ref)
	}
	i := 0
	for _, r := range ref {
		if i >= len(s) {
			return -1
		}
		c, n := utf8.DecodeRuneInString(s[i:])
		if !foldEqual(r, c) {
			return -1
		}
		i += n
	}
	return pos + i
}

// refRepeat runs opRefRepeat at pos, as the PCRE2 JIT repeats a back
// reference: an unset group matches nothing when the minimum is zero, a
// set empty one matches any number of times without backtracking.
func (m *machine) refRepeat(in *inst, pc, pos int) (int, bool) {
	g := m.refGroup(in)
	if g < 0 && int(in.min) > 0 {
		return 0, false
	}
	if g < 0 || m.caps[2*g] == m.caps[2*g+1] {
		return pos, m.interp || m.count()
	}
	fold := in.arg != 0
	for range int(in.min) {
		if pos = m.refAt(g, fold, pos); pos < 0 {
			return 0, false
		}
	}
	if int(in.min) == int(in.max) {
		return pos, m.interp || m.count()
	}
	if in.c == repLazy {
		if !m.count() {
			return 0, false
		}
		m.pushChoice(btRefLazy, 0, i32(pc+1), pos, int(in.min), i32(pc))
		return pos, true
	}
	start, n := pos, 0
	for int(in.max) < 0 || int(in.min)+n < int(in.max) {
		np := m.refAt(g, fold, pos)
		if np < 0 {
			break
		}
		pos = np
		n++
	}
	if (!m.interp || in.c != repPossessive) && !m.count() {
		return 0, false
	}
	if in.c != repPossessive && n > 0 {
		m.pushChoice(btRefGreedy, 0, i32(pc+1), pos, start, i32(n))
	}
	return pos, true
}

// refBack resumes a greedy back reference repeat with one repetition
// less; the JIT counts every count down to the minimum.
func (m *machine) refBack(top int) (int, int, bool) {
	e := &m.stack[top]
	in := &m.prog[e.pc-1]
	e.b--
	pos := e.a
	g := m.refGroup(in)
	for range e.b {
		pos = m.refAt(g, in.arg != 0, pos)
	}
	at := int(e.pc)
	if e.b == 0 {
		m.popTo(top)
	}
	if !m.count() {
		return 0, 0, false
	}
	return at, pos, true
}

// refMore resumes a lazy back reference repeat with one more repetition.
func (m *machine) refMore(top int) (int, int, bool) {
	e := &m.stack[top]
	in := &m.prog[e.b]
	if int(in.max) >= 0 && e.a >= int(in.max) {
		return 0, 0, false
	}
	np := m.refAt(m.refGroup(in), in.arg != 0, e.pos)
	if np < 0 {
		return 0, 0, false
	}
	e.pos = np
	e.a++
	at := int(e.pc)
	if !m.count() {
		return 0, 0, false
	}
	return at, np, true
}

// foldEqual reports whether two characters are equal under Unicode simple
// case folding.
func foldEqual(a, b rune) bool {
	if a == b {
		return true
	}
	for f := unicode.SimpleFold(a); f != a; f = unicode.SimpleFold(f) {
		if f == b {
			return true
		}
	}
	return false
}
