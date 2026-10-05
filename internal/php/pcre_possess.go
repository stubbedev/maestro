// Ports pcre2_auto_possess.c (PCRE2 10.48): single-character repeats
// whose item cannot match what follows them are made possessive. This
// changes no match result, but it changes how many backtracking frames
// pcre2_match creates, and so which subjects reach the match limit; the
// engine counts like PCRE2, so it possessifies exactly the same repeats.
// The opcodes of items follow pcre2_compile.c (OP_CHAR lists, character
// types, OP_PROP, OP_CLASS/OP_NCLASS/OP_XCLASS).

package php

import (
	"slices"
	"strings"
	"unicode"
)

// Item opcodes, numbered from OP_NOT_DIGIT like PCRE2's auto-possess
// table; the ones after pcDOLLM are not in the table.
const (
	pcNOTDIGIT = iota
	pcDIGIT
	pcNOTWS
	pcWS
	pcNOTWORD
	pcWORD
	pcANY
	pcALLANY
	pcANYBYTE
	pcNOTPROP
	pcPROP
	pcANYNL
	pcNOTHSPACE
	pcHSPACE
	pcNOTVSPACE
	pcVSPACE
	pcEXTUNI
	pcEODN
	pcEOD
	pcDOLL
	pcDOLLM
	pcCHAR
	pcNOT
	pcCLASS
	pcNCLASS
	pcXCLASS
)

// autoposstab is PCRE2's table: whether a repeated type (row) followed by
// a type (column) can be possessified.
var autoposstab = [17][21]uint8{
	{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0}, // \D
	{1, 0, 0, 1, 1, 0, 0, 0, 0, 0, 0, 1, 0, 1, 0, 1, 0, 1, 1, 1, 1}, // \d
	{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1, 0, 1, 0, 1, 0, 1, 1, 1, 1}, // \S
	{0, 1, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0}, // \s
	{0, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0}, // \W
	{0, 0, 0, 1, 1, 0, 0, 0, 0, 0, 0, 1, 0, 1, 0, 1, 0, 1, 1, 1, 1}, // \w
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1, 0, 0}, // .
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0}, // .+
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0}, // \C
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, // \P
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, // \p
	{0, 1, 0, 1, 0, 1, 1, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0}, // \R
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0}, // \H
	{0, 1, 1, 0, 0, 1, 0, 0, 0, 0, 0, 1, 1, 0, 0, 1, 0, 0, 1, 0, 0}, // \h
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 1, 0, 0}, // \V
	{0, 1, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1, 1, 0, 0, 0, 1, 0, 0}, // \v
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0}, // \X
}

// Property types (PT_*).
const (
	ptLAMP = iota
	ptGC
	ptPC
	ptSC
	ptSCX
	ptALNUM
	ptSPACE
	ptPXSPACE
	ptWORD
	ptCLIST
	ptUCNC
	ptBIDICL
	ptBOOL
)

var propposstab = [13][13]uint8{
	{3, 0, 0, 0, 0, 3, 1, 1, 0, 0, 0, 0, 0},     // LAMP
	{0, 2, 4, 0, 0, 9, 10, 10, 11, 0, 0, 0, 0},  // GC
	{0, 5, 2, 0, 0, 15, 16, 16, 17, 0, 0, 0, 0}, // PC
	{0, 0, 0, 2, 2, 0, 0, 0, 0, 0, 0, 0, 0},     // SC
	{0, 0, 0, 2, 2, 0, 0, 0, 0, 0, 0, 0, 0},     // SCX
	{3, 6, 12, 0, 0, 3, 1, 1, 0, 0, 0, 0, 0},    // ALNUM
	{1, 7, 13, 0, 0, 1, 3, 3, 1, 0, 0, 0, 0},    // SPACE
	{1, 7, 13, 0, 0, 1, 3, 3, 1, 0, 0, 0, 0},    // PXSPACE
	{0, 8, 14, 0, 0, 0, 1, 1, 3, 0, 0, 0, 0},    // WORD
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},     // CLIST
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0},     // UCNC
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},     // BIDICL
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},     // BOOL
}

// General categories (ucp_C...) and particular categories (ucp_Cc...).
const (
	ucpC = iota
	ucpL
	ucpM
	ucpN
	ucpP
	ucpS
	ucpZ
)

var ucpPCNames = []string{"Cc", "Cf", "Cn", "Co", "Cs", "Ll", "Lm", "Lo", "Lt", "Lu", "Mc", "Me", "Mn", "Nd", "Nl", "No", "Pc", "Pd", "Pe", "Pf", "Pi", "Po", "Ps", "Sc", "Sk", "Sm", "So", "Zl", "Zp", "Zs"}

const (
	ucpCc = 0
	ucpLl = 5
	ucpLt = 8
	ucpLu = 9
	ucpNd = 13
	ucpNl = 14
	ucpPo = 21
)

var catposstab = [7][30]uint8{
	{0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
	{1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
	{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
	{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
	{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1},
	{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 1, 1, 1},
	{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0},
}

var posspropstab = [3][4]int{
	{ucpL, ucpN, ucpN, ucpNl}, // ALNUM
	{ucpZ, ucpZ, ucpC, ucpCc}, // SPACE and PXSPACE
	{ucpL, ucpN, ucpP, ucpPo}, // WORD
}

// ucpChartype is the particular category of r (ucp_Cn when unassigned).
func ucpChartype(r rune) int {
	for i, name := range ucpPCNames {
		if t, ok := unicode.Categories[name]; ok && unicode.Is(t, r) {
			return i
		}
	}
	return 2 // Cn
}

// ucpGentype maps a particular category to its general category.
func ucpGentype(pc int) int {
	switch {
	case pc <= 4:
		return ucpC
	case pc <= 9:
		return ucpL
	case pc <= 12:
		return ucpM
	case pc <= 15:
		return ucpN
	case pc <= 22:
		return ucpP
	case pc <= 26:
		return ucpS
	}
	return ucpZ
}

var scriptNames = func() []string {
	names := make([]string, 0, len(unicode.Scripts))
	for n := range unicode.Scripts {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}()

// pinfo is what get_chr_property_list extracts from an item.
type pinfo struct {
	op            int
	chars         []rune    // pcCHAR, pcNOT
	cbuf          [4]rune   // storage for chars
	ptype, pvalue int       // pcPROP, pcNOTPROP
	bits          [4]uint64 // pcCLASS, pcNCLASS; pcXCLASS: the map
	xprop, xmap   bool
	xnot          bool
	cls           *charClass
	canEmpty      bool // the item is repeated with a zero minimum
	greedy        bool // a base: its repeat is greedy
}

// caseList is the characters a literal matches (OP_CHAR, OP_CHARI, or
// an OP_PROP character list).
func caseList(r rune, fold, utf bool) []rune {
	return appendCaseList(nil, r, fold, utf)
}

// appendCaseList appends the characters of caseList to dst.
func appendCaseList(dst []rune, r rune, fold, utf bool) []rune {
	if !fold {
		return append(dst, r)
	}
	if !utf {
		if r < 128 && isASCIIWord(r) && !isASCIIDigit(r) && r != '_' {
			return append(dst, r, r^0x20)
		}
		return append(dst, r)
	}
	out := append(dst, r)
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		out = append(out, f)
	}
	return out
}

// otherCase is PCRE2's single other case of c (the fcc table below 128 or
// in byte mode, else UCD_OTHERCASE), and whether c has more than one.
func otherCase(c rune, utf bool) (rune, bool) {
	if !utf || c < 128 {
		if c < 128 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return c ^ 0x20, utf && len(caseList(c, true, true)) > 2
		}
		return c, false
	}
	l := caseList(c, true, true)
	if len(l) > 2 {
		return l[1], true
	}
	if len(l) == 2 {
		return l[1], false
	}
	return c, false
}

// propInfo returns the PCRE2 property type and value of a \p name.
func propInfo(name string, fold bool) (ptype, pvalue int, isAny bool) {
	key := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '_':
			return -1
		}
		return unicode.ToLower(r)
	}, name)
	switch key {
	case "any":
		return 0, 0, true
	case "l&", "lc":
		return ptLAMP, 0, false
	case "xan":
		return ptALNUM, 0, false
	case "xps":
		return ptPXSPACE, 0, false
	case "xsp":
		return ptSPACE, 0, false
	case "xwd":
		return ptWORD, 0, false
	case "xuc":
		return ptUCNC, 0, false
	}
	for i, g := range []string{"c", "l", "m", "n", "p", "s", "z"} {
		if key == g {
			return ptGC, i, false
		}
	}
	for i, pc := range ucpPCNames {
		if key == strings.ToLower(pc) {
			if fold && (i == ucpLu || i == ucpLl || i == ucpLt) {
				return ptLAMP, 0, false
			}
			return ptPC, i, false
		}
	}
	ptype = ptSCX
	if strings.HasPrefix(key, "sc:") {
		ptype = ptSC
	}
	key = strings.TrimPrefix(strings.TrimPrefix(key, "sc:"), "scx:")
	for i, n := range scriptNames {
		if strings.ToLower(strings.ReplaceAll(n, "_", "")) == key {
			return ptype, i, false
		}
	}
	return ptBOOL, 0, false
}

// itemInfo ports get_chr_property_list for an item.
func itemInfo(p *pnode, utf bool) pinfo {
	n := p.node
	if !n.hasInfo {
		nodeInfo(n, utf, &n.info)
		n.hasInfo = true
	}
	in := n.info
	in.canEmpty = p.min == 0
	in.greedy = p.mode == repGreedy
	return in
}

// nodeInfo fills in the opcode data of a single-character node.
func nodeInfo(n *reNode, utf bool, in *pinfo) {
	switch n.op {
	case reLit:
		in.op, in.chars = pcCHAR, appendCaseList(in.cbuf[:0], n.r, n.fold, utf)
	case reAny:
		in.op = pcANY
		if n.dotall {
			in.op = pcALLANY
		}
	case reNewline:
		in.op = pcANYNL
	case reClass:
		classInfo(n, utf, in)
	}
}

func classInfo(n *reNode, utf bool, in *pinfo) {
	pf := n.pf
	in.cls = n.cls
	in.bits = n.cls.bits
	if pf == nil {
		in.op = pcCLASS
		return
	}
	switch pf.kind {
	case pfEscape:
		if utf {
			switch pf.esc {
			case 'd', 'D':
				in.op, in.ptype, in.pvalue = pcPROP, ptPC, ucpNd
			case 's', 'S':
				in.op, in.ptype = pcPROP, ptSPACE
			case 'w', 'W':
				in.op, in.ptype = pcPROP, ptWORD
			}
			if in.op == pcPROP {
				if pf.esc < 'a' {
					in.op = pcNOTPROP
				}
				return
			}
		}
		switch pf.esc {
		case 'd':
			in.op = pcDIGIT
		case 'D':
			in.op = pcNOTDIGIT
		case 's':
			in.op = pcWS
		case 'S':
			in.op = pcNOTWS
		case 'w':
			in.op = pcWORD
		case 'W':
			in.op = pcNOTWORD
		case 'h':
			in.op = pcHSPACE
		case 'H':
			in.op = pcNOTHSPACE
		case 'v':
			in.op = pcVSPACE
		case 'V':
			in.op = pcNOTVSPACE
		}
	case pfProp:
		ptype, pvalue, isAny := propInfo(pf.prop, pf.fold)
		switch {
		case isAny && !pf.neg:
			in.op = pcALLANY
		case isAny:
			in.op, in.bits = pcCLASS, [4]uint64{}
		default:
			in.op, in.ptype, in.pvalue = pcPROP, ptype, pvalue
			if pf.neg {
				in.op = pcNOTPROP
			}
		}
	case pfBracket:
		if pf.litsOnly && len(pf.lits) == 1 {
			in.chars = appendCaseList(in.cbuf[:0], pf.lits[0], pf.fold, utf)
			in.op = pcCHAR
			if pf.neg {
				in.op = pcNOT
			}
			return
		}
		if pf.litsOnly && len(pf.lits) == 2 && !pf.neg {
			c, d := pf.lits[0], pf.lits[1]
			if o, multi := otherCase(c, utf); !multi && o != c && o == d {
				in.op, in.chars = pcCHAR, appendCaseList(in.cbuf[:0], c, true, utf)
				return
			}
		}
		all := in.bits == [4]uint64{^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0)}
		wide := pf.hasWide || utf && n.cls.foldU && classFoldsWide(n.cls)
		switch {
		case !utf && all:
			in.op = pcALLANY
		case utf && (pf.hasProp || wide):
			in.op = pcXCLASS
			in.xprop, in.xnot = pf.hasProp, pf.neg
			in.xmap = in.bits != [4]uint64{}
		case pf.neg:
			in.op = pcNCLASS
		default:
			in.op = pcCLASS
		}
	}
}

// classFoldsWide reports whether a caseless class has a member below 256
// with a case variant above 255.
func classFoldsWide(c *charClass) bool {
	for r := range rune(256) {
		if c.raw[r>>6]&(1<<(r&63)) == 0 {
			continue
		}
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if f > 255 {
				return true
			}
		}
	}
	return false
}

// anchorInfo returns the opcode of an anchor that can follow a repeat.
func anchorInfo(n *reNode) (int, bool) {
	switch {
	case n.dollar && n.anchor == anchorMEOL:
		return pcDOLLM, true
	case n.dollar:
		return pcDOLL, true
	case n.anchor == anchorEnd:
		return pcEOD, true
	case n.anchor == anchorEndZ:
		return pcEODN, true
	}
	return 0, false
}

// C locale ctype bitmaps.
var cbitDigit, cbitSpace, cbitWord = func() (d, s, w [4]uint64) {
	for r := range rune(256) {
		if isASCIIDigit(r) {
			d[r>>6] |= 1 << (r & 63)
		}
		if isASCIISpace(r) {
			s[r>>6] |= 1 << (r & 63)
		}
		if isASCIIWord(r) {
			w[r>>6] |= 1 << (r & 63)
		}
	}
	return
}()

func isHSpaceCase(c rune) bool {
	switch c {
	case 0x09, 0x20, 0xa0, 0x1680, 0x180e, 0x202f, 0x205f, 0x3000:
		return true
	}
	return c >= 0x2000 && c <= 0x200a
}

func isVSpaceCase(c rune) bool {
	return c >= 0x0a && c <= 0x0d || c == 0x85 || c == 0x2028 || c == 0x2029
}

// checkCharProp ports check_char_prop: TRUE when c and the property
// cannot both match.
func checkCharProp(c rune, ptype, pdata int, negated bool) bool {
	ct := ucpChartype(c)
	gt := ucpGentype(ct)
	switch ptype {
	case ptLAMP:
		return (ct == ucpLu || ct == ucpLl || ct == ucpLt) == negated
	case ptGC:
		return (pdata == gt) == negated
	case ptPC:
		return (pdata == ct) == negated
	case ptSC, ptSCX:
		t := unicode.Scripts[scriptNames[pdata]]
		return unicode.Is(t, c) == negated
	case ptALNUM:
		return (gt == ucpL || gt == ucpN) == negated
	case ptSPACE, ptPXSPACE:
		if isHSpaceCase(c) || isVSpaceCase(c) {
			return negated
		}
		return (gt == ucpZ) == negated
	case ptWORD:
		return (gt == ucpL || gt == ucpN || c == '_') == negated
	}
	return false
}

// Results of comparing a base with the next item.
const (
	cmpFalse = iota
	cmpTrue
	cmpContinue
)

// compareItems ports the item comparison of compare_opcodes.
func compareItems(base, list *pinfo, utf bool) int {
	done := func() int {
		if !list.canEmpty {
			return cmpTrue
		}
		return cmpContinue
	}
	var chrs []rune
	var other *pinfo
	switch {
	case base.op == pcCHAR:
		chrs, other = base.chars, list
	case list.op == pcCHAR:
		chrs, other = list.chars, base
	case base.op == pcCLASS || list.op == pcCLASS || !utf && (base.op == pcNCLASS || list.op == pcNCLASS):
		var set1 [4]uint64
		if base.op == pcCLASS || !utf && base.op == pcNCLASS {
			set1, other = base.bits, list
		} else {
			set1, other = list.bits, base
		}
		invert := false
		var set2 [4]uint64
		switch other.op {
		case pcCLASS, pcNCLASS:
			set2 = other.bits
		case pcXCLASS:
			if other.xprop {
				return cmpFalse
			}
			if !other.xmap {
				if !list.canEmpty {
					if other.xnot {
						return cmpFalse
					}
					return cmpTrue
				}
				return cmpContinue
			}
			set2 = other.bits
		case pcNOTDIGIT:
			invert, set2 = true, cbitDigit
		case pcDIGIT:
			set2 = cbitDigit
		case pcNOTWS:
			invert, set2 = true, cbitSpace
		case pcWS:
			set2 = cbitSpace
		case pcNOTWORD:
			invert, set2 = true, cbitWord
		case pcWORD:
			set2 = cbitWord
		default:
			return cmpFalse
		}
		for i := range set1 {
			if invert && set1[i]&^set2[i] != 0 || !invert && set1[i]&set2[i] != 0 {
				return cmpFalse
			}
		}
		return done()
	default:
		left, right := base.op, list.op
		accepted := false
		if left == pcPROP || left == pcNOTPROP {
			switch right {
			case pcEOD:
				accepted = true
			case pcPROP, pcNOTPROP:
				accepted = compareProps(base, list)
			}
		} else {
			accepted = left <= pcEXTUNI && right <= pcDOLLM && autoposstab[left][right] != 0
		}
		if !accepted {
			return cmpFalse
		}
		return done()
	}
	for _, chr := range chrs {
		switch other.op {
		case pcCHAR:
			if slices.Contains(other.chars, chr) {
				return cmpFalse
			}
		case pcNOT:
			if !slices.Contains(other.chars, chr) {
				return cmpFalse
			}
		case pcDIGIT:
			if chr < 256 && isASCIIDigit(chr) {
				return cmpFalse
			}
		case pcNOTDIGIT:
			if chr > 255 || !isASCIIDigit(chr) {
				return cmpFalse
			}
		case pcWS:
			if chr < 256 && isASCIISpace(chr) {
				return cmpFalse
			}
		case pcNOTWS:
			if chr > 255 || !isASCIISpace(chr) {
				return cmpFalse
			}
		case pcWORD:
			if chr < 255 && isASCIIWord(chr) {
				return cmpFalse
			}
		case pcNOTWORD:
			if chr > 255 || !isASCIIWord(chr) {
				return cmpFalse
			}
		case pcHSPACE:
			if isHSpaceCase(chr) {
				return cmpFalse
			}
		case pcNOTHSPACE:
			if !isHSpaceCase(chr) {
				return cmpFalse
			}
		case pcANYNL, pcVSPACE:
			if isVSpaceCase(chr) {
				return cmpFalse
			}
		case pcNOTVSPACE:
			if !isVSpaceCase(chr) {
				return cmpFalse
			}
		case pcDOLL, pcEODN:
			switch chr {
			case '\r', '\n', '\v', '\f', 0x85, 0x2028, 0x2029:
				return cmpFalse
			}
		case pcEOD:
		case pcPROP, pcNOTPROP:
			if !checkCharProp(chr, other.ptype, other.pvalue, other.op == pcNOTPROP) {
				return cmpFalse
			}
		case pcNCLASS:
			if chr > 255 {
				return cmpFalse
			}
			if other.bits[chr>>6]&(1<<(chr&63)) != 0 {
				return cmpFalse
			}
		case pcCLASS:
			if chr > 255 {
				break
			}
			if other.bits[chr>>6]&(1<<(chr&63)) != 0 {
				return cmpFalse
			}
		case pcXCLASS:
			if other.cls.has(chr) {
				return cmpFalse
			}
		default:
			return cmpFalse
		}
	}
	return done()
}

// compareProps ports the OP_PROP/OP_NOTPROP pair check.
func compareProps(base, list *pinfo) bool {
	same := base.op == list.op
	lisprop := base.op == pcPROP
	risprop := list.op == pcPROP
	bothprop := lisprop && risprop
	if base.ptype > ptBOOL || list.ptype > ptBOOL {
		return false
	}
	b2, b3, l2, l3 := base.ptype, base.pvalue, list.ptype, list.pvalue
	bool01 := func(b bool) uint8 {
		if b {
			return 1
		}
		return 0
	}
	switch n := propposstab[b2][l2]; n {
	case 1:
		return bothprop
	case 2:
		return (b3 == l3) != same
	case 3:
		return !same
	case 4:
		return risprop && catposstab[b3][l3] == bool01(same)
	case 5:
		return lisprop && catposstab[l3][b3] == bool01(same)
	case 6, 7, 8:
		p := posspropstab[n-6]
		return risprop && lisprop == (l3 != p[0] && l3 != p[1] && (l3 != p[2] || !lisprop))
	case 9, 10, 11:
		p := posspropstab[n-9]
		return lisprop && risprop == (b3 != p[0] && b3 != p[1] && (b3 != p[2] || !risprop))
	case 12, 13, 14:
		p := posspropstab[n-12]
		return risprop && lisprop == (catposstab[p[0]][l3] != 0 && catposstab[p[1]][l3] != 0 && (l3 != p[3] || !lisprop))
	case 15, 16, 17:
		p := posspropstab[n-15]
		return lisprop && risprop == (catposstab[p[0]][b3] != 0 && catposstab[p[1]][b3] != 0 && (b3 != p[3] || !risprop))
	}
	return false
}

// possessifier ports auto_possessify over the pnodes.
type possessifier struct {
	utf        bool
	hadRecurse bool
	recLimit   int
}

// autoPossessify makes the repeats PCRE2 would make possessive so.
func autoPossessify(top *pnode, utf, hadRecurse bool) {
	a := &possessifier{utf: utf, hadRecurse: hadRecurse, recLimit: 1000}
	a.walk(top)
}

func (a *possessifier) walk(p *pnode) {
	if p.assert != nil {
		a.walk(p.assert)
	}
	for _, br := range p.branches {
		for _, c := range br {
			switch c.kind {
			case pkBracket:
				a.walk(c)
			case pkItem:
				if c.mode == repPossessive || c.min == 1 && c.max == 1 {
					continue
				}
				base := itemInfo(c, a.utf)
				// A fixed repeat is OP_EXACT, except for a class, whose
				// OP_CRRANGE is still a candidate.
				if c.min == c.max && base.op != pcCLASS && base.op != pcNCLASS && base.op != pcXCLASS {
					continue
				}
				if a.compare(c.parent, c.branch, c.index+1, &base) {
					c.mode = repPossessive
				}
			}
		}
	}
}

// compare ports compare_opcodes, scanning from item idx of a branch of c.
func (a *possessifier) compare(c *pnode, br, idx int, base *pinfo) bool {
	a.recLimit--
	if a.recLimit <= 0 {
		return false
	}
	entered := false
	for {
		var seq []*pnode
		if br >= 0 {
			seq = c.branches[br]
		}
		if br < 0 || idx >= len(seq) {
			// The end of a branch: what follows the group.
			if c.wrapper {
				return base.greedy
			}
			if c.ket == ketRMax || c.ket == ketRMin {
				return false
			}
			if !base.greedy {
				return false
			}
			switch c.bra {
			case pbCBRA:
				if a.hadRecurse {
					return false
				}
			case pbASSERT, pbASSERTNOT, pbONCE:
				return !entered
			case pbASSERTBACK, pbASSERTBACKNOT:
				for _, alt := range lookAlts(c.node) {
					if lo, hi, _ := charLengths(alt); lo != hi {
						return false
					}
				}
				return !entered
			}
			c, br, idx = c.parent, c.branch, c.index+1
			continue
		}
		n := seq[idx]
		switch n.kind {
		case pkBracket:
			plain := (n.bra == pbBRA || n.bra == pbCBRA || n.bra == pbONCE) && !n.sform && !n.pos
			switch n.prefix {
			case pfxZero, pfxMinZero:
				if !plain {
					return false
				}
				if !a.compare(c, br, idx+1, base) {
					return false
				}
			case pfxNone:
			default:
				return false
			}
			if !plain {
				return false
			}
			last := len(n.branches) - 1
			for i := range last {
				if !a.compare(n, i, 0, base) {
					return false
				}
			}
			c, br, idx = n, last, 0
			entered = true
			continue
		case pkItem:
			list := itemInfo(n, a.utf)
			switch compareItems(base, &list, a.utf) {
			case cmpFalse:
				return false
			case cmpTrue:
				return true
			}
			idx++
			continue
		case pkAnchor:
			op, ok := anchorInfo(n.node)
			if !ok {
				return false
			}
			list := pinfo{op: op}
			switch compareItems(base, &list, a.utf) {
			case cmpTrue:
				return true
			default:
				return false
			}
		}
		return false
	}
}

// lookAlts returns the branches of a lookaround.
func lookAlts(n *reNode) []*reNode {
	if n == nil || len(n.subs) == 0 {
		return nil
	}
	if body := n.subs[0]; body.op == reAlt {
		return body.subs
	}
	return n.subs[:1]
}
