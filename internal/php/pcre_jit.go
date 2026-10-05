// Ports the analyses of pcre2_jit_compile.c (PCRE2 10.48) that decide
// where the JIT counts against the match limit, for the pnode layout of
// pcre_ir.go. PHP runs patterns with the JIT, and the JIT counts
// differently from pcre2_match: an iterator counts each position it
// tries, a bracket counts when control leaves it (when it has
// alternatives, is optional or repeats), a subroutine call counts once,
// and identical copies of a repeated group are run as one loop
// (detect_repeat). The JIT also prunes the search with "early fail"
// (detect_early_fail): a greedy iterator remembers how far it scanned,
// and is not tried again from inside that range; the first iterator of a
// pattern moves the next start position past its scan (fast forward).
// These change which subjects reach the limit, so the engine reproduces
// them.

package php

import "slices"

// Early-fail kinds (type_skip, type_fail, type_fail_range).
const (
	efNone uint8 = iota
	efSkip
	efFail
	efRange
)

// earlyFailMax is EARLY_FAIL_ENHANCE_MAX.
const earlyFailMax = 3 + 3

// Iterator opcode shapes of a repeated single-character item.
const (
	vNone  = iota // no variable part: OP_EXACT only
	vStar         // OP_STAR (also OP_PLUS)
	vQuery        // OP_QUERY
	vUpto         // OP_UPTO, or a class OP_CRRANGE
)

// iterShape is how PCRE2 compiles a repeated single-character item: a
// class is one iterator (OP_CR*); a character or type can be an OP_EXACT
// for the minimum, the item itself for a minimum of one with a limited
// maximum, and an iterator for the rest.
type iterShape struct {
	class   bool
	exact   bool // a separate OP_EXACT (or OP_TYPEEXACT) for the minimum
	lead    bool // {1,m}: the plain item before an OP_UPTO
	v       int  // the variable part
	plus    bool // OP_PLUS: the iterator covers the first character too
	anynl   bool // a repeated \R
	allany  bool // OP_ALLANY
	charLit bool // the item compiles to OP_CHAR or OP_CHARI
}

func shapeOf(p *pnode, utf bool) iterShape {
	in := itemInfo(p, utf)
	sh := iterShape{anynl: in.op == pcANYNL, allany: in.op == pcALLANY}
	switch in.op {
	case pcCLASS, pcNCLASS, pcXCLASS:
		sh.class = true
	case pcCHAR:
		// A caseless character with several other cases is an OP_PROP
		// list.
		sh.charLit = !utf || len(in.chars) <= 2
	}
	minN, maxN := p.min, p.max
	if sh.class {
		switch {
		case minN == maxN:
			sh.v = vNone
		case maxN < 0 && minN <= 1:
			sh.v, sh.plus = vStar, minN == 1
		case minN == 0 && maxN == 1:
			sh.v = vQuery
		default:
			sh.v = vUpto
		}
		return sh
	}
	if minN == maxN {
		sh.exact, sh.v = minN >= 2, vNone
		return sh
	}
	switch minN {
	case 0:
		switch {
		case maxN < 0:
			sh.v = vStar
		case maxN == 1:
			sh.v = vQuery
		default:
			sh.v = vUpto
		}
	case 1:
		if maxN < 0 {
			sh.v, sh.plus = vStar, true
		} else {
			sh.lead, sh.v = true, vUpto
		}
	default:
		sh.exact = true
		switch {
		case maxN < 0:
			sh.v = vStar
		case maxN-minN == 1:
			sh.v = vQuery
		default:
			sh.v = vUpto
		}
	}
	return sh
}

// jitAnalysis holds the pattern-wide state of the analyses.
type jitAnalysis struct {
	utf     bool
	unopt   map[int]bool // capture groups that are not "optimized" brackets
	slots   int          // early-fail slots allocated
	fastFwd *pnode       // the type_skip iterator
}

// analyzeJIT annotates the pnodes below top.
func analyzeJIT(top *pnode, tree *reNode, utf, anchored bool) *jitAnalysis {
	a := &jitAnalysis{utf: utf, unopt: map[int]bool{}}
	collectUnoptimized(tree, a.unopt)
	markPosCaptures(top, a.unopt)
	if !anchored {
		a.earlyFail(top, 0, 0)
	}
	a.repeats(top)
	return a
}

// collectUnoptimized records the groups that back references and
// conditions refer to: the JIT does not optimize their brackets.
func collectUnoptimized(n *reNode, out map[int]bool) {
	switch n.op {
	case reBackref:
		for _, g := range n.groupsOf() {
			out[g] = true
		}
	case reCond:
		if n.cond == condGroup {
			for _, g := range n.groupsOf() {
				out[g] = true
			}
		}
		if n.assert != nil {
			collectUnoptimized(n.assert, out)
		}
	}
	for _, s := range n.subs {
		collectUnoptimized(s, out)
	}
}

func markPosCaptures(p *pnode, out map[int]bool) {
	if p.kind != pkBracket {
		return
	}
	if p.bra == pbCBRA && p.pos {
		out[p.n] = true
	}
	if p.assert != nil {
		markPosCaptures(p.assert, out)
	}
	for _, br := range p.branches {
		for _, c := range br {
			markPosCaptures(c, out)
		}
	}
}

// earlyFail ports detect_early_fail for bracket b.
func (a *jitAnalysis) earlyFail(b *pnode, depth, start int) int {
	result := 0
	if len(b.branches) > 1 && start < 1 {
		start = 1
	}
	for _, br := range b.branches {
		count := start
		stopped := false // the scan stopped before the end of the branch
		i := 0
	scan:
		for ; i < len(br); i++ {
			n := br[i]
			var accel *pnode
			switch n.kind {
			case pkAnchor, pkKeep:
				continue
			case pkItem:
				if n.min == 1 && n.max == 1 {
					if itemInfo(n, a.utf).op == pcANYNL {
						count = max(count, 3)
					} else {
						count = max(count, 1)
					}
					continue
				}
				sh := shapeOf(n, a.utf)
				if sh.exact || sh.lead {
					count = max(count, 1)
				}
				switch {
				case sh.v == vNone:
					count = max(count, 1)
					continue
				case sh.v == vStar && !sh.anynl:
					if n.mode == repLazy && count == 2 {
						count = 3
					}
					accel = n
				default:
					count = max(count, 3)
					continue
				}
			case pkBracket:
				if n.prefix != pfxNone || n.sform || n.pos ||
					n.bra != pbBRA && (n.bra != pbCBRA || a.unopt[n.n]) {
					stopped = true
					break scan
				}
				prev := count
				count = max(count, 1)
				if depth >= 4 {
					stopped = true
					break scan
				}
				if count < 3 && len(n.branches) > 1 {
					count = 3
				}
				if n.ket != ketPlain {
					stopped = true
					break scan
				}
				prev = a.earlyFail(n, depth+1, prev)
				count = max(count, prev)
				if n.noRepeat {
					b.noRepeat = true
				}
				if count < earlyFailMax {
					continue
				}
				stopped = true
				break scan
			default:
				stopped = true
				break scan
			}
			switch {
			case count == 0:
				accel.efType = efSkip
				a.fastFwd = accel
				count = 4
			case count < 3:
				accel.efType = efFail
				count = 4
			default:
				accel.efType = efRange
				count++
			}
			accel.efSlot = a.slots
			a.slots++
			b.noRepeat = true
			if count >= earlyFailMax {
				stopped = i+1 < len(br)
				break scan
			}
		}
		if stopped {
			result = earlyFailMax
		} else if result < count {
			result = count
		}
	}
	return result
}

// repeats ports detect_repeat over every sequence below p.
func (a *jitAnalysis) repeats(p *pnode) {
	if p.assert != nil {
		a.repeats(p.assert)
	}
	for _, br := range p.branches {
		for i, c := range br {
			if c.kind != pkBracket {
				continue
			}
			if c.prefix == pfxNone && !c.sform && !c.pos && c.ket == ketPlain && !c.noRepeat &&
				!c.jitExact && !c.jitNoCount &&
				(c.bra == pbONCE || c.bra == pbBRA || c.bra == pbCBRA || c.bra == pbCOND) {
				detectRepeat(br, i)
			}
			a.repeats(c)
		}
	}
}

// detectRepeat ports detect_repeat for the bracket seq[i].
func detectRepeat(seq []*pnode, i int) {
	p := seq[i]
	j := i + 1
	minN := 1
	for j < len(seq) && seq[j].kind == pkBracket && pnodeEqual(seq[j], p, pfxNone) {
		j++
		minN++
	}
	if minN == 2 {
		return
	}
	if j < len(seq) && seq[j].kind == pkBracket && (seq[j].prefix == pfxZero || seq[j].prefix == pfxMinZero) {
		typ := seq[j].prefix
		cur := seq[j]
		var run []*pnode
		maxN := 0
		for cur.bra == pbBRA && cur.prefix == typ && !cur.sform && !cur.pos && cur.ket == ketPlain &&
			len(cur.branches) == 1 && len(cur.branches[0]) == 2 &&
			cur.branches[0][0].kind == pkBracket && pnodeEqual(cur.branches[0][0], p, pfxNone) {
			run = append(run, cur, cur.branches[0][0])
			maxN++
			cur = cur.branches[0][1]
		}
		if maxN >= 1 && cur.kind == pkBracket && pnodeEqual(cur, p, typ) {
			// The last copy of the minimum and the optional copies run as
			// one OP_UPTO loop.
			head := seq[j-1]
			head.jitNoCount = true
			for _, r := range append(run, cur) {
				r.jitNoCount = true
			}
			seq[j].jitRunEnd = true
			if minN == 1 {
				return
			}
			minN--
		}
	}
	if minN >= 3 {
		for k := range minN {
			seq[i+k].jitExact = true
		}
	}
}

// pnodeEqual reports whether a and b compile to the same code, b with
// the prefix prefix.
func pnodeEqual(a, b *pnode, prefix uint8) bool {
	if a.kind != b.kind || a.node != b.node || a.min != b.min || a.max != b.max || a.mode != b.mode ||
		a.bra != b.bra || a.n != b.n || a.sform != b.sform || a.pos != b.pos || a.ket != b.ket ||
		a.prefix != prefix || a.wrapper != b.wrapper || len(a.branches) != len(b.branches) ||
		(a.assert == nil) != (b.assert == nil) {
		return false
	}
	if a.assert != nil && !pnodeEqual(a.assert, b.assert, b.assert.prefix) {
		return false
	}
	for i, br := range a.branches {
		if len(br) != len(b.branches[i]) {
			return false
		}
		for k, x := range br {
			if !pnodeEqual(x, b.branches[i][k], b.branches[i][k].prefix) {
				return false
			}
		}
	}
	return true
}

// inlineRecursion reports whether the JIT compiles a call of the group g
// inline (get_framesize returns no_stack): a single branch of items that
// never backtrack.
func inlineRecursion(g *pnode, utf bool) bool {
	if len(g.branches) != 1 {
		return false
	}
	for _, n := range g.branches[0] {
		switch n.kind {
		case pkAnchor:
			if n.node.anchor == anchorStart || n.node.anchor == anchorG {
				return false
			}
		case pkItem:
			if n.min == 1 && n.max == 1 {
				continue
			}
			if sh := shapeOf(n, utf); sh.class || n.mode != repPossessive && sh.v != vNone {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// minLength ports find_minlength of pcre2_study.c over the pnodes: the
// minimum subject length pcre2_match and the JIT require before trying a
// start position. Unlike minChars it follows subroutine calls and back
// references into the groups they refer to.
type minLength struct {
	groups []*pnode // the first bracket of each group number
	top    *pnode
	dupCap bool
	calls  int
}

func patternMinLength(top *pnode, groups []*pnode, dupCap bool) int {
	ml := &minLength{groups: groups, top: top, dupCap: dupCap}
	d := ml.group(top, nil)
	return max(d, 0)
}

// group returns the minimum length of the bracket p, or -1 when the
// pattern is too complex.
func (ml *minLength) group(p *pnode, recurses []*pnode) int {
	if p.sform {
		return 0
	}
	if ml.calls++; ml.calls > 1000 {
		return -1
	}
	length := -1
	for _, br := range p.branches {
		bl, hadRecurse := 0, false
		for _, n := range br {
			if bl >= 65535 {
				bl = 65535
				break
			}
			d, rec := ml.item(n, recurses)
			if d < 0 {
				return d
			}
			bl += d
			hadRecurse = hadRecurse || rec
		}
		if length < 0 || !hadRecurse && bl < length {
			length = bl
		}
		if length == 0 {
			return 0
		}
	}
	return max(length, 0)
}

func (ml *minLength) item(n *pnode, recurses []*pnode) (int, bool) {
	switch n.kind {
	case pkBracket:
		if n.prefix != pfxNone || n.bra >= pbASSERT && n.bra <= pbASSERTBACKNOT {
			return 0, false
		}
		if n.bra == pbCOND && len(n.branches) < 2 {
			return 0, false
		}
		return ml.group(n, recurses), false
	case pkItem:
		if n.min == 1 && n.max == 1 {
			return 1, false
		}
		sh := shapeOf(n, false)
		if sh.class {
			return n.min, false
		}
		d := 0
		if sh.exact {
			d = n.min
		}
		if sh.lead || sh.plus {
			d++
		}
		return d, false
	case pkRef:
		minN := n.min
		d, rec := 0, false
		for _, g := range n.node.groupsOf() {
			dd, r := ml.call(n, g, recurses, true)
			if dd < 0 {
				return dd, false
			}
			if g == n.node.groupsOf()[0] || dd < d {
				d = dd
			}
			rec = rec || r
		}
		if ml.dupCap && len(n.node.groupsOf()) > 1 {
			d = 0
		}
		return min(minN*d, 65535), rec
	case pkRecurse:
		return ml.call(n, n.node.n, recurses, false)
	}
	return 0, false
}

// call returns the minimum length of group g referred to from n; a
// recursion into a group n is inside, or one already being measured,
// counts as nothing and marks the branch.
func (ml *minLength) call(n *pnode, g int, recurses []*pnode, ref bool) (int, bool) {
	cs := ml.top
	if g > 0 {
		cs = ml.groups[g]
	}
	if cs == nil {
		return 0, false
	}
	if ref && ml.dupCap && ml.numberReused(g) {
		return 0, false
	}
	for a := n.parent; a != nil; a = a.parent {
		if a == cs {
			return 0, true
		}
	}
	if slices.Contains(recurses, cs) {
		return 0, true
	}
	return ml.group(cs, append(slices.Clip(recurses), cs)), false
}

// numberReused reports whether more than one bracket has number g.
func (ml *minLength) numberReused(g int) bool {
	count := 0
	var walk func(p *pnode)
	walk = func(p *pnode) {
		if p.kind != pkBracket {
			return
		}
		if p.bra == pbCBRA && p.n == g {
			count++
		}
		if p.assert != nil {
			walk(p.assert)
		}
		for _, br := range p.branches {
			for _, c := range br {
				walk(c)
			}
		}
	}
	walk(ml.top)
	return count > 1
}
