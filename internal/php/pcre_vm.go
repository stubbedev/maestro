// Ports the matching semantics of pcre2_match (PCRE2 10.48,
// pcre2_match.c) as a backtracking machine with an explicit stack: start
// position scanning, PCRE2_ANCHORED, PCRE2_NOTEMPTY_ATSTART, captures that
// are set when their group closes, atomic groups and assertions, bounded
// lookbehind, empty-iteration loop termination, and subroutine calls and
// recursion with captures reverting on return (PCRE2 >= 10.30).
//
// PCRE2's match limit (pcre.backtrack_limit, 1000000 in PHP) bounds the
// work of one match; here it counts resumptions after backtracking, which
// is close to but not exactly what PCRE2 counts.

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
	pc   int
	pos  int
	a, b int
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
	btReverse   // lookbehind: next start position pos+1 char, a tries left
	btMark      // atomic group or assertion boundary
)

type frame struct {
	parent int
	group  int
	ret    int
	pos    int // subject position of the call
	off    int // saved caps and regs in arena[off:]
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
}

func (re *Regexp) getMachine(s string) *machine {
	m, _ := re.pool.Get().(*machine)
	if m == nil {
		m = &machine{re: re, caps: make([]int, 2*(re.ncap+1)), regs: make([]int, re.nregs)}
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
	re := m.re
	m.searchStart, m.notEmpty, m.steps, m.err = start, notEmpty, 0, matchOK
	s := m.s
	// The required byte must occur; PCRE2 skips this search for very long
	// subjects.
	if re.hasReq && (len(s)-start < reqMax || !anchored && !re.anchored && len(s)-start < reqMax*1000) {
		rest := s[start:]
		if strings.IndexByte(rest, re.req) < 0 && (re.req2 == re.req || strings.IndexByte(rest, re.req2) < 0) {
			return false, matchOK
		}
	}
	for st := start; st <= len(s); {
		// pcre2_match does not try where the rest of the subject is
		// shorter than the shortest possible match.
		if len(s)-st < re.minLen {
			return false, matchOK
		}
		if re.first != nil && !anchored && !re.anchored {
			for st < len(s) && !re.first[s[st]] {
				st++
			}
			if st >= len(s) {
				return false, matchOK
			}
		}
		if m.run(st) {
			return true, matchOK
		}
		if m.err != matchOK {
			return false, m.err
		}
		if anchored || re.anchored || st >= len(s) {
			break
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
			if in.cls.has(rune(s[pos])) {
				return pos + 1
			}
			return -1
		}
		if r, n := utf8.DecodeRuneInString(s[pos:]); in.cls.has(r) {
			return pos + n
		}
	case opAny:
		if s[pos] != '\n' {
			return pos + m.charLen(pos)
		}
	case opAnyNL:
		return pos + m.charLen(pos)
	}
	return -1
}

func (m *machine) push(e btEntry) { m.stack = append(m.stack, e) }

func (m *machine) setCap(i, v int) {
	m.push(btEntry{kind: btCap, a: i, pos: m.caps[i]})
	m.caps[i] = v
}

func (m *machine) setReg(i, v int) {
	m.push(btEntry{kind: btReg, a: i, pos: m.regs[i]})
	m.regs[i] = v
}

// run tries to match at st.
func (m *machine) run(st int) bool {
	re, s := m.re, m.s
	prog := re.prog
	for i := range m.caps {
		m.caps[i] = -1
	}
	for i := range m.regs {
		m.regs[i] = -1
	}
	m.stack = m.stack[:0]
	m.frames, m.arena = m.frames[:0], m.arena[:0]
	m.fp, m.keep = -1, st
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
			if ok = len(s)-pos >= len(in.s) && s[pos:pos+len(in.s)] == in.s; ok {
				pos += len(in.s)
				pc++
			}
		case opByteFold, opRune, opClass, opAny, opAnyNL:
			np := m.step(in.op, in, pos)
			if ok = np >= 0; ok {
				pos = np
				pc++
			}
		case opNewline:
			np := -1
			if pos < len(s) {
				if s[pos] == '\r' && pos+1 < len(s) && s[pos+1] == '\n' {
					np = pos + 2
				} else if r, n := m.char(pos); isVSpace(r) {
					np = pos + n
				}
			}
			if ok = np >= 0; ok {
				pos = np
				pc++
			}
		case opRepeat:
			pos, ok = m.repeat(in, pc, pos)
			pc++
		case opSplit:
			m.push(btEntry{kind: btChoice, pc: in.y, pos: pos})
			pc = in.x
		case opJmp:
			pc = in.x
		case opCapOpen:
			m.setReg(in.n, pos)
			pc++
		case opCapClose:
			if m.fp >= 0 && m.frames[m.fp].group == in.n {
				pc = m.ret()
				break
			}
			if in.n > 0 {
				m.setCap(2*in.n, m.regs[in.n])
				m.setCap(2*in.n+1, pos)
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
			// The mark goes right after the undo entry setReg pushes.
			m.setReg(in.n, len(m.stack)+1)
			sub := markAtomic
			if in.op == opLookOpen {
				sub = in.arg
			}
			m.push(btEntry{kind: btMark, sub: sub, pc: in.x, pos: pos})
			pc++
		case opAtomicClose:
			m.cut(m.regs[in.n])
			pc++
		case opLookClose:
			idx := m.regs[in.n]
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
				pc = in.x
			}
		case opReverse:
			k, p := 0, pos
			for k < in.max && p > 0 {
				p = m.prevChar(p)
				k++
			}
			if ok = k >= in.min; ok {
				if k > in.min {
					m.push(btEntry{kind: btReverse, pc: pc + 1, pos: p, a: k - in.min})
				}
				pos = p
				pc++
			}
		case opLoopMark:
			m.setReg(in.n, pos)
			pc++
		case opLoopCheck:
			if pos == m.regs[in.n] {
				pc = in.x
			} else {
				pc = in.y
			}
		case opCall:
			if ok = m.call(in, pc, pos); ok {
				pc = in.x
			}
		case opCondRef:
			next := in.x
			for _, g := range in.groups {
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
				pc = in.x
			}
		case opKeep:
			m.push(btEntry{kind: btKeep, pos: m.keep})
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

// repeat runs opRepeat at pos.
func (m *machine) repeat(in *inst, pc, pos int) (int, bool) {
	count := 0
	switch in.arg {
	case repLazy:
		for ; count < in.min; count++ {
			if pos = m.step(in.item, in, pos); pos < 0 {
				return 0, false
			}
		}
		if in.max < 0 || count < in.max {
			m.push(btEntry{kind: btRepLazy, pc: pc + 1, pos: pos, a: count, b: pc})
		}
		return pos, true
	default:
		lo := pos
		for in.max < 0 || count < in.max {
			np := m.step(in.item, in, pos)
			if np < 0 {
				break
			}
			pos = np
			count++
			if count == in.min {
				lo = pos
			}
		}
		if count < in.min {
			return 0, false
		}
		if in.arg == repGreedy && count > in.min {
			m.push(btEntry{kind: btRepGreedy, pc: pc + 1, pos: pos, a: lo})
		}
		return pos, true
	}
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
			m.stack = m.stack[:top]
			return m.resume(e.pc, e.pos)
		case btRepGreedy:
			e.pos = m.prevChar(e.pos)
			p, at := e.pos, e.pc
			if p <= e.a {
				m.stack = m.stack[:top]
			}
			return m.resume(at, p)
		case btRepLazy:
			in := &m.re.prog[e.b]
			if in.max < 0 || e.a < in.max {
				if np := m.step(in.item, in, e.pos); np >= 0 {
					e.pos = np
					e.a++
					p, at := e.pos, e.pc
					if in.max >= 0 && e.a >= in.max {
						m.stack = m.stack[:top]
					}
					return m.resume(at, p)
				}
			}
		case btReverse:
			if e.a > 0 {
				e.pos += m.charLen(e.pos)
				e.a--
				p, at := e.pos, e.pc
				if e.a == 0 {
					m.stack = m.stack[:top]
				}
				return m.resume(at, p)
			}
		case btMark:
			if e.sub == markLookNeg || e.sub == markCondPos || e.sub == markCondNeg {
				m.stack = m.stack[:top]
				return m.resume(e.pc, e.pos)
			}
		}
		m.stack = m.stack[:top]
	}
	return 0, 0, false
}

// resume counts a backtrack against the match limit.
func (m *machine) resume(pc, pos int) (int, int, bool) {
	m.steps++
	if m.steps > matchLimit {
		m.err = matchErrBacktrack
		return 0, 0, false
	}
	return pc, pos, true
}

// cut ends an atomic group or positive assertion whose mark is at idx:
// its choice points are discarded, its undo entries kept.
func (m *machine) cut(idx int) {
	w := idx
	for _, e := range m.stack[idx+1:] {
		if e.kind <= btFrame {
			m.stack[w] = e
			w++
		}
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
	m.stack = m.stack[:idx]
}

// call enters a subroutine call of group in.n.
func (m *machine) call(in *inst, pc, pos int) bool {
	for f := m.fp; f >= 0; f = m.frames[f].parent {
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
	m.push(btEntry{kind: btFrame, a: m.fp, b: len(m.frames), pos: len(m.arena)})
	m.frames = append(m.frames, frame{parent: m.fp, group: in.n, ret: pc + 1, pos: pos, off: len(m.arena)})
	m.arena = append(m.arena, m.caps...)
	m.arena = append(m.arena, m.regs...)
	m.fp = len(m.frames) - 1
	return true
}

// ret returns from the current call: captures and registers revert to
// their values before the call.
func (m *machine) ret() int {
	f := m.frames[m.fp]
	saved := m.arena[f.off:]
	for i, v := range saved[:len(m.caps)] {
		if m.caps[i] != v {
			m.setCap(i, v)
		}
	}
	for i, v := range saved[len(m.caps) : len(m.caps)+len(m.regs)] {
		if m.regs[i] != v {
			m.setReg(i, v)
		}
	}
	m.push(btEntry{kind: btFrame, a: m.fp, b: -1})
	m.fp = f.parent
	return f.ret
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
	g := -1
	for _, x := range in.groups {
		if m.caps[2*x] >= 0 {
			g = x
			break
		}
	}
	if g < 0 {
		return -1
	}
	ref := m.s[m.caps[2*g]:m.caps[2*g+1]]
	s := m.s[pos:]
	if in.arg == 0 {
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
