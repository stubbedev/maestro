// Ports the PCRE patterns of src/VersionParser.php as hand-written
// matchers.
//
// Each matcher reproduces PCRE2's semantics for its one pattern exactly
// (non-UTF mode, as PHP runs these patterns): alternatives and greedy or
// lazy quantifiers are tried in PCRE's priority order with backtracking,
// possessive quantifiers never give back, and the captures reported are
// those of the first path that matches. \s is [ \t\n\v\f\r], `.` is any
// byte but \n, /i folds ASCII letters only, and $ matches at the end of
// the subject or before a newline that ends it.
//
// Backtracking is written in continuation-passing style: a matcher calls k
// with every position its sub-pattern can end at, in priority order, and
// stops at the first k that reports success. Every path assigns all the
// captures it owns before calling k, so captures left over from a failed
// path never leak into the successful one.

package semver

// span is a capture group's [start, end) in the subject; start is -1 when
// the group did not participate in the match.
type span struct{ start, end int }

var unset = span{-1, -1}

func (sp span) isSet() bool { return sp.start >= 0 }

// text is the group's text, "" when it is unset (as PHP reports groups
// that precede a matching one).
func (sp span) text(s string) string {
	if sp.start < 0 {
		return ""
	}

	return s[sp.start:sp.end]
}

// nonEmpty reports whether the group captured at least one byte, which is
// !empty($match[i]) for groups that can never capture "0".
func (sp span) nonEmpty() bool { return sp.end > sp.start }

// pos is a continuation: the rest of the pattern, tried at a position.
type cont func(pos int) bool

func dollar(s string) cont { return func(pos int) bool { return atDollar(s, pos) } }

func isXStar(c byte) bool { return c == 'x' || c == 'X' || c == '*' }

// isNotCommaSpace is [^,\s].
func isNotCommaSpace(c byte) bool { return c != ',' && !isSpace(c) }

// runOf returns the end of the run of bytes from pos that satisfy in.
func runOf(s string, pos int, in func(byte) bool) int {
	for pos < len(s) && in(s[pos]) {
		pos++
	}

	return pos
}

func digitsEnd(s string, pos int) int { return runOf(s, pos, isDigit) }

func spacesEnd(s string, pos int) int {
	for pos < len(s) && s[pos] == ' ' {
		pos++
	}

	return pos
}

// byteIn reports whether s[pos] exists and is one of set.
func byteIn(s string, pos int, set string) bool {
	if pos < 0 || pos >= len(s) {
		return false
	}
	for i := range len(set) {
		if s[pos] == set[i] {
			return true
		}
	}

	return false
}

// litAt reports whether the literal lit occurs at s[pos:].
func litAt(s string, pos int, lit string, ci bool) bool {
	return pos >= 0 && pos <= len(s) && hasPrefixLit(s[pos:], lit, ci)
}

// modifierWords is the alternation of VersionParser::$modifierRegex, in
// order.
var modifierWords = [...]string{"stable", "beta", "b", "RC", "alpha", "a", "patch", "pl", "p"}

// modCaps are the groups of VersionParser::$modifierRegex.
type modCaps struct {
	word span // (stable|beta|b|RC|alpha|a|patch|pl|p)
	nums span // ((?:[.-]?\d+)*+)
	dev  span // ([.-]?dev)
}

// matchModifier matches VersionParser::$modifierRegex,
// `[._-]?(?:(stable|beta|b|RC|alpha|a|patch|pl|p)((?:[.-]?\d+)*+)?)?([.-]?dev)?`,
// at pos, case-insensitively when ci is set.
func matchModifier(s string, pos int, ci bool, c *modCaps, k cont) bool {
	if byteIn(s, pos, "._-") && modifierBody(s, pos+1, ci, c, k) {
		return true
	}

	return modifierBody(s, pos, ci, c, k)
}

func modifierBody(s string, pos int, ci bool, c *modCaps, k cont) bool {
	for _, w := range modifierWords {
		if !litAt(s, pos, w, ci) {
			continue
		}
		end := pos + len(w)
		// ((?:[.-]?\d+)*+)? is atomic and always matches. Skipping the
		// optional group instead ends at the same place, so it cannot
		// succeed where taking it failed.
		numsEnd := end
		for {
			if byteIn(s, numsEnd, ".-") && byteIn(s, numsEnd+1, "0123456789") {
				numsEnd = digitsEnd(s, numsEnd+1)
			} else if byteIn(s, numsEnd, "0123456789") {
				numsEnd = digitsEnd(s, numsEnd)
			} else {
				break
			}
		}
		c.word, c.nums = span{pos, end}, span{end, numsEnd}
		if modifierDev(s, numsEnd, ci, c, k) {
			return true
		}
	}
	c.word, c.nums = unset, unset

	return modifierDev(s, pos, ci, c, k)
}

// modifierDev matches ([.-]?dev)?.
func modifierDev(s string, pos int, ci bool, c *modCaps, k cont) bool {
	if byteIn(s, pos, ".-") && litAt(s, pos+1, "dev", ci) {
		c.dev = span{pos, pos + 4}
		if k(pos + 4) {
			return true
		}
	}
	if litAt(s, pos, "dev", ci) {
		c.dev = span{pos, pos + 3}
		if k(pos + 3) {
			return true
		}
	}
	c.dev = unset

	return k(pos)
}

// optNumbers matches len(groups) consecutive optional `(?:\.(\d++))?`
// groups at pos, recording the digits of each.
func optNumbers(s string, pos int, groups []span, k cont) bool {
	if len(groups) == 0 {
		return k(pos)
	}
	if byteIn(s, pos, ".") {
		if end := digitsEnd(s, pos+1); end > pos+1 {
			groups[0] = span{pos + 1, end}
			if optNumbers(s, end, groups[1:], k) {
				return true
			}
		}
	}
	groups[0] = unset

	return optNumbers(s, pos, groups[1:], k)
}

// stabilitiesWord matches VersionParser::$stabilitiesRegex,
// stable|RC|beta|alpha|dev, at pos and returns its length, 0 for none.
// None of the words is a prefix of another, so at most one can match.
func stabilitiesWord(s string, pos int, ci bool) int {
	for _, w := range [...]string{"stable", "RC", "beta", "alpha", "dev"} {
		if litAt(s, pos, w, ci) {
			return len(w)
		}
	}

	return 0
}

// matchAlias matches `^([^,\s]++) ++as ++([^,\s]++)$` and returns group 1.
func matchAlias(s string) (string, bool) {
	g1 := runOf(s, 0, isNotCommaSpace)
	if g1 == 0 {
		return "", false
	}
	p := spacesEnd(s, g1)
	if p == g1 || !litAt(s, p, "as", false) {
		return "", false
	}
	g2 := spacesEnd(s, p+2)
	if g2 == p+2 {
		return "", false
	}
	end := runOf(s, g2, isNotCommaSpace)
	if end == g2 || !atDollar(s, end) {
		return "", false
	}

	return s[:g1], true
}

// matchStabilityFlag matches `@(?:stable|RC|beta|alpha|dev)$` (/i,
// unanchored) and returns the length of the match, 0 for none.
func matchStabilityFlag(s string) int {
	for i := range len(s) {
		if s[i] != '@' {
			continue
		}
		if w := stabilitiesWord(s, i+1, true); w > 0 && atDollar(s, i+1+w) {
			return 1 + w
		}
	}

	return 0
}

// matchBuildMetadata matches `^([^,\s+]++)\+[^\s]++$` and returns group 1.
func matchBuildMetadata(s string) (string, bool) {
	g1 := runOf(s, 0, func(c byte) bool { return c != '+' && isNotCommaSpace(c) })
	if g1 == 0 || !byteIn(s, g1, "+") {
		return "", false
	}
	end := runOf(s, g1+1, func(c byte) bool { return !isSpace(c) })

	return s[:g1], end > g1+1 && atDollar(s, end)
}

// classicalCaps are the groups of VersionParser::normalize()'s classical
// versioning pattern.
type classicalCaps struct {
	major span    // (\d{1,5}+)
	minor [3]span // the digits of (\.\d++)? x3, without their dot
	mod   modCaps
}

// matchClassical matches
// `^v?(\d{1,5}+)(\.\d++)?(\.\d++)?(\.\d++)?` + $modifierRegex + `$` (/i).
func matchClassical(s string, c *classicalCaps) bool {
	// Without the v, \d would have to match it: v? never backtracks
	// usefully.
	p := 0
	if byteIn(s, 0, "vV") {
		p = 1
	}
	end := digitsEnd(s, p)
	if end == p {
		return false
	}
	end = min(end, p+5)
	c.major = span{p, end}

	return optNumbers(s, end, c.minor[:], func(pos int) bool {
		return matchModifier(s, pos, true, &c.mod, dollar(s))
	})
}

// dateCaps are the groups of VersionParser::normalize()'s date pattern.
type dateCaps struct {
	date span
	mod  modCaps
}

// matchDate matches
// `^v?(\d{4}(?:[.:-]?\d{2}){1,6}(?:[.:-]?\d{1,3}){0,2})` + $modifierRegex +
// `$` (/i).
func matchDate(s string, c *dateCaps) bool {
	p := 0
	if byteIn(s, 0, "vV") {
		p = 1
	}
	if digitsEnd(s, p) < p+4 {
		return false
	}
	m := dateMatcher{s: s, c: c, start: p}

	return m.pairs(p+4, 0)
}

type dateMatcher struct {
	s     string
	c     *dateCaps
	start int
}

func (m *dateMatcher) twoDigits(pos int) bool { return digitsEnd(m.s, pos) >= pos+2 }

// pairs matches (?:[.:-]?\d{2}){1,6} after count iterations.
func (m *dateMatcher) pairs(pos, count int) bool {
	if count < 6 {
		if byteIn(m.s, pos, ".:-") && m.twoDigits(pos+1) && m.pairs(pos+3, count+1) {
			return true
		}
		if m.twoDigits(pos) && m.pairs(pos+2, count+1) {
			return true
		}
	}

	return count >= 1 && m.triples(pos, 0)
}

// triples matches (?:[.:-]?\d{1,3}){0,2} after count iterations, then the
// rest of the pattern.
func (m *dateMatcher) triples(pos, count int) bool {
	if count < 2 {
		if byteIn(m.s, pos, ".:-") && m.digits(pos+1, count) {
			return true
		}
		if m.digits(pos, count) {
			return true
		}
	}
	m.c.date = span{m.start, pos}

	return matchModifier(m.s, pos, true, &m.c.mod, dollar(m.s))
}

func (m *dateMatcher) digits(pos, count int) bool {
	for end := min(digitsEnd(m.s, pos), pos+3); end > pos; end-- {
		if m.triples(end, count+1) {
			return true
		}
	}

	return false
}

// matchDevSuffix matches `(.*?)[.-]?dev$` (/i, unanchored) and returns
// group 1.
//
// The match must end at the one place $ allows "dev" to end, and the lazy
// group stops before an optional [.-] there. The leftmost start is just
// after the last newline before the group's end, as `.` cannot cross one.
func matchDevSuffix(s string) (string, bool) {
	e := dollarEnd(s)
	if e < 3 || !litAt(s, e-3, "dev", true) {
		return "", false
	}
	j := e - 3
	if byteIn(s, j-1, ".-") {
		j--
	}
	start := 0
	for i := j - 1; i >= 0; i-- {
		if s[i] == '\n' {
			start = i + 1

			break
		}
	}

	return s[start:j], true
}

// matchNumericAliasPrefix matches
// `^(?P<version>(\d++\.)*\d++)(?:\.x)?-dev$` (/i) and returns the end of
// the version group.
func matchNumericAliasPrefix(s string, pos int) (int, bool) {
	end := digitsEnd(s, pos)
	if end == pos {
		return 0, false
	}
	// (\d++\.)* is greedy: one more repetition first.
	if byteIn(s, end, ".") {
		if v, ok := matchNumericAliasPrefix(s, end+1); ok {
			return v, true
		}
	}
	devAt := func(p int) bool { return litAt(s, p, "-dev", true) && atDollar(s, p+4) }
	if (litAt(s, end, ".x", true) && devAt(end+2)) || devAt(end) {
		return end, true
	}

	return 0, false
}

// branchCaps are the groups of normalizeBranch()'s pattern, dots included.
type branchCaps [4]span

// matchBranch matches
// `^v?(\d++)(\.(?:\d++|[xX*]))?(\.(?:\d++|[xX*]))?(\.(?:\d++|[xX*]))?$`
// (/i).
func matchBranch(s string, c *branchCaps) bool {
	p := 0
	if byteIn(s, 0, "vV") {
		p = 1
	}
	end := digitsEnd(s, p)
	if end == p {
		return false
	}
	c[0] = span{p, end}

	return branchParts(s, end, c[1:])
}

func branchParts(s string, pos int, groups []span) bool {
	if len(groups) == 0 {
		return atDollar(s, pos)
	}
	if byteIn(s, pos, ".") {
		end := digitsEnd(s, pos+1)
		if end == pos+1 && byteIn(s, pos+1, "xX*") {
			end = pos + 2
		}
		if end > pos+1 {
			groups[0] = span{pos, end}
			if branchParts(s, end, groups[1:]) {
				return true
			}
		}
	}
	groups[0] = unset

	return branchParts(s, pos, groups[1:])
}

// matchStabilitySuffix matches `^([^,\s]*?)@(stable|RC|beta|alpha|dev)$`
// (/i) and returns both groups.
func matchStabilitySuffix(s string) (constraint, stability string, ok bool) {
	for i := range len(s) {
		if s[i] == '@' {
			if w := stabilitiesWord(s, i+1, true); w > 0 && atDollar(s, i+1+w) {
				return s[:i], s[i+1 : i+1+w], true
			}
		}
		if !isNotCommaSpace(s[i]) {
			break
		}
	}

	return "", "", false
}

// referenceEnd reports whether `#.+$` matches at the '#' at s[h], and
// where it ends.
func referenceEnd(s string, h int) (int, bool) {
	e := h + 1
	for e < len(s) && s[e] != '\n' {
		e++
	}

	return e, e > h+1 && atDollar(s, e)
}

// stripReference is preg_replace('{#.+$}', ”, $version).
func stripReference(s string) string {
	for i := range len(s) {
		if s[i] != '#' {
			continue
		}
		if e, ok := referenceEnd(s, i); ok {
			if e == len(s) {
				return s[:i]
			}

			return s[:i] + s[e:]
		}
	}

	return s
}

// matchDevReference matches `^(dev-[^,\s@]+?|[^,\s@]+?\.x-dev)#.+$` (/i)
// and returns group 1.
func matchDevReference(s string) (string, bool) {
	inClass := func(c byte) bool { return c != '@' && isNotCommaSpace(c) }
	if litAt(s, 0, "dev-", true) {
		for i := 4; i < len(s); i++ {
			if i > 4 && s[i] == '#' {
				if _, ok := referenceEnd(s, i); ok {
					return s[:i], true
				}
			}
			if !inClass(s[i]) {
				break
			}
		}
	}
	for i := 1; i <= len(s); i++ {
		if !inClass(s[i-1]) {
			break
		}
		if h := i + 6; litAt(s, i, ".x-dev", true) && byteIn(s, h, "#") {
			if _, ok := referenceEnd(s, h); ok {
				return s[:h], true
			}
		}
	}

	return "", false
}

// matchWildcard matches `^(v)?[xX*](\.[xX*])*$` (/i) and reports whether
// either group is non-empty.
func matchWildcard(s string) (matched, hasGroup bool) {
	p := 0
	if byteIn(s, 0, "vV") {
		p = 1
	}
	if !byteIn(s, p, "xX*") {
		return false, false
	}
	end := p + 1
	for byteIn(s, end, ".") && byteIn(s, end+1, "xX*") {
		end += 2
	}
	if !atDollar(s, end) {
		return false, false
	}

	return true, p == 1 || end > p+1
}

// vrCaps are the groups of the $versionRegex of parseConstraint().
type vrCaps struct {
	num  [4]span // (\d++) and the three (?:\.(\d++))?
	mod  modCaps
	xDev span // ([xX*][.-]?dev)
}

// matchVersionRegex matches parseConstraint()'s $versionRegex (always used
// with /i),
// `v?(\d++)(?:\.(\d++))?(?:\.(\d++))?(?:\.(\d++))?(?:` + $modifierRegex +
// `|\.([xX*][.-]?dev))(?:\+[^\s]+)?`, at pos.
func matchVersionRegex(s string, pos int, c *vrCaps, k cont) bool {
	if byteIn(s, pos, "vV") && versionRegexNumbers(s, pos+1, c, k) {
		return true
	}

	return versionRegexNumbers(s, pos, c, k)
}

func versionRegexNumbers(s string, pos int, c *vrCaps, k cont) bool {
	end := digitsEnd(s, pos)
	if end == pos {
		return false
	}
	c.num[0] = span{pos, end}

	return optNumbers(s, end, c.num[1:], func(pos int) bool {
		c.xDev = unset
		if matchModifier(s, pos, true, &c.mod, func(end int) bool { return buildMetadata(s, end, k) }) {
			return true
		}
		c.mod = modCaps{unset, unset, unset}
		if byteIn(s, pos, ".") && byteIn(s, pos+1, "xX*") {
			p := pos + 2
			if byteIn(s, p, ".-") && litAt(s, p+1, "dev", true) {
				c.xDev = span{pos + 1, p + 4}
				if buildMetadata(s, p+4, k) {
					return true
				}
			}
			if litAt(s, p, "dev", true) {
				c.xDev = span{pos + 1, p + 3}
				if buildMetadata(s, p+3, k) {
					return true
				}
			}
			c.xDev = unset
		}

		return false
	})
}

// buildMetadata matches (?:\+[^\s]+)?, whose [^\s]+ backtracks.
func buildMetadata(s string, pos int, k cont) bool {
	if byteIn(s, pos, "+") {
		for end := runOf(s, pos+1, func(c byte) bool { return !isSpace(c) }); end > pos+1; end-- {
			if k(end) {
				return true
			}
		}
	}

	return k(pos)
}

// matchTilde matches `^~>?` + $versionRegex + `$` (/i).
func matchTilde(s string, c *vrCaps) bool {
	if !byteIn(s, 0, "~") {
		return false
	}
	if byteIn(s, 1, ">") && matchVersionRegex(s, 2, c, dollar(s)) {
		return true
	}

	return matchVersionRegex(s, 1, c, dollar(s))
}

// matchCaret matches `^\^` + $versionRegex + `($)` (/i).
func matchCaret(s string, c *vrCaps) bool {
	return byteIn(s, 0, "^") && matchVersionRegex(s, 1, c, dollar(s))
}

// xRangeCaps are the groups of parseConstraint()'s X-range pattern.
type xRangeCaps [3]span

// matchXRange matches
// `^v?(\d++)(?:\.(\d++))?(?:\.(\d++))?(?:\.[xX*])++$` (case-sensitive).
func matchXRange(s string, c *xRangeCaps) bool {
	p := 0
	if byteIn(s, 0, "v") {
		p = 1
	}
	end := digitsEnd(s, p)
	if end == p {
		return false
	}
	c[0] = span{p, end}

	return optNumbers(s, end, c[1:], func(pos int) bool {
		end := pos
		for byteIn(s, end, ".") && byteIn(s, end+1, "xX*") {
			end += 2
		}

		return end > pos && atDollar(s, end)
	})
}

// hyphenCaps are the groups of parseConstraint()'s hyphen range pattern.
type hyphenCaps struct {
	from, to       vrCaps
	fromEnd        int // (?P<from>...) is s[:fromEnd]
	toStart, toEnd int
}

// matchHyphen matches `^(?P<from>` + $versionRegex + `) +- +(?P<to>` +
// $versionRegex + `)($)` (/i).
func matchHyphen(s string, c *hyphenCaps) bool {
	return matchVersionRegex(s, 0, &c.from, func(fromEnd int) bool {
		// ` +- +`: giving back a space could only put one before the -.
		p := spacesEnd(s, fromEnd)
		if p == fromEnd || !byteIn(s, p, "-") {
			return false
		}
		toStart := spacesEnd(s, p+1)
		if toStart == p+1 {
			return false
		}
		c.fromEnd, c.toStart = fromEnd, toStart

		return matchVersionRegex(s, toStart, &c.to, func(toEnd int) bool {
			c.toEnd = toEnd

			return atDollar(s, toEnd)
		})
	})
}

// basicOperators is the alternation (<>|!=|>=?|<=?|==?), in the order PCRE
// tries it.
var basicOperators = [...]string{"<>", "!=", ">=", ">", "<=", "<", "==", "="}

// matchBasicComparator matches `^(<>|!=|>=?|<=?|==?)?\s*(.*)`, which
// always matches: nothing after the operator can fail, so the operator
// group never gives anything back.
func matchBasicComparator(s string) (operator, version string) {
	p := 0
	for _, op := range basicOperators {
		if litAt(s, 0, op, false) {
			operator, p = op, len(op)

			break
		}
	}
	p = runOf(s, p, isSpace)
	end := p
	for end < len(s) && s[end] != '\n' {
		end++
	}

	return operator, s[p:end]
}

// matchSimpleDevChars matches `^[0-9a-zA-Z-./]+$`.
func matchSimpleDevChars(s string) bool {
	end := runOf(s, 0, func(c byte) bool { return isAlnum(c) || c == '-' || c == '.' || c == '/' })

	return end > 0 && atDollar(s, end)
}

// matchDashModifier matches `-` + $modifierRegex + `$` (case-sensitive,
// unanchored).
func matchDashModifier(s string) bool {
	var c modCaps
	for i := range len(s) {
		if s[i] == '-' && matchModifier(s, i+1, false, &c, dollar(s)) {
			return true
		}
	}

	return false
}

// matchStabilityModifier matches $modifierRegex + `(?:\+.*)?$` (/i,
// unanchored), as parseStability() does.
func matchStabilityModifier(s string, c *modCaps) bool {
	k := func(end int) bool {
		if byteIn(s, end, "+") {
			// .* is greedy and only its longest run can end at $.
			e := end + 1
			for e < len(s) && s[e] != '\n' {
				e++
			}
			if atDollar(s, e) {
				return true
			}
		}

		return atDollar(s, end)
	}
	for start := 0; start <= len(s); start++ {
		if matchModifier(s, start, true, c, k) {
			return true
		}
	}

	return false
}

// matchAliasOf matches `{ +as +` + preg_quote($version) +
// `(?:@(?:stable|RC|beta|alpha|dev))?$}` (unanchored, case-sensitive)
// against full.
func matchAliasOf(full, version string) bool {
	for i := range len(full) {
		if full[i] != ' ' {
			continue
		}
		p := spacesEnd(full, i)
		if !litAt(full, p, "as", false) {
			continue
		}
		q := spacesEnd(full, p+2)
		for v := q; v > p+2; v-- {
			if litAt(full, v, version, false) && stabilityThenDollar(full, v+len(version)) {
				return true
			}
		}
	}

	return false
}

// stabilityThenDollar matches (?:@(?:stable|RC|beta|alpha|dev))?$.
func stabilityThenDollar(s string, pos int) bool {
	if byteIn(s, pos, "@") {
		if w := stabilitiesWord(s, pos+1, false); w > 0 && atDollar(s, pos+1+w) {
			return true
		}
	}

	return atDollar(s, pos)
}

// matchAliasSourceOf matches `{^` + preg_quote($version) +
// `(?:@(?:stable|RC|beta|alpha|dev))? +as +}` (case-sensitive) against
// full.
func matchAliasSourceOf(full, version string) bool {
	if !litAt(full, 0, version, false) {
		return false
	}
	asAt := func(p int) bool {
		q := spacesEnd(full, p)

		return q > p && litAt(full, q, "as", false) && spacesEnd(full, q+2) > q+2
	}
	p := len(version)
	if byteIn(full, p, "@") {
		if w := stabilitiesWord(full, p+1, false); w > 0 && asAt(p+1+w) {
			return true
		}
	}

	return asAt(p)
}

// splitOr is preg_split('{\s*\|\|?\s*}', $s).
func splitOr(s string) []string {
	parts := make([]string, 0, 2)
	last := 0
	for i := 0; i < len(s); {
		j := runOf(s, i, isSpace)
		if !byteIn(s, j, "|") {
			i = max(j, i+1)

			continue
		}
		j++
		if byteIn(s, j, "|") {
			j++
		}
		j = runOf(s, j, isSpace)
		parts = append(parts, s[last:i])
		last, i = j, j
	}

	return append(parts, s[last:])
}

// splitAnd is
// preg_split('{(?<!^|as|[=>< ,]) *(?<!-)[, ](?!-) *(?!,|as|$)}', $s).
func splitAnd(s string) []string {
	parts := make([]string, 0, 2)
	last := 0
	for p := 1; p < len(s); p++ {
		if byteIn(s, p-1, "=>< ,") || (p >= 2 && s[p-2:p] == "as") {
			continue
		}
		if end, ok := andSeparatorAt(s, p); ok {
			parts = append(parts, s[last:p])
			last = end
			p = end - 1
		}
	}

	return append(parts, s[last:])
}

// andSeparatorAt matches ` *(?<!-)[, ](?!-) *(?!,|as|$)` at p.
func andSeparatorAt(s string, p int) (int, bool) {
	for q := spacesEnd(s, p); q >= p; q-- {
		if s[q-1] == '-' || !byteIn(s, q, ", ") || byteIn(s, q+1, "-") {
			continue
		}
		for r := spacesEnd(s, q+1); r > q; r-- {
			if !byteIn(s, r, ",") && !litAt(s, r, "as", false) && !atDollar(s, r) {
				return r, true
			}
		}
	}

	return 0, false
}
