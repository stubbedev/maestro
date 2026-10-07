// Ports the preg_* functions of ext/pcre/php_pcre.c (php_pcre_match_impl,
// php_pcre_replace_impl, php_pcre_split_impl, preg_grep, preg_quote,
// populate_subpat_array) and the Composer\Pcre\Preg wrapper
// (vendor/composer/pcre/src/Preg.php, PcreException.php,
// UnexpectedNullMatchException.php).

package php

import (
	"strconv"
	"unicode/utf8"
)

// PregFlag holds the flags of the preg_* functions.
type PregFlag int

// preg_match/preg_match_all flags.
const (
	PregPatternOrder    PregFlag = 1
	PregSetOrder        PregFlag = 2
	PregOffsetCapture   PregFlag = 256
	PregUnmatchedAsNull PregFlag = 512
)

// preg_split and preg_grep flags.
const (
	PregSplitNoEmpty       PregFlag = 1
	PregSplitDelimCapture  PregFlag = 2
	PregSplitOffsetCapture PregFlag = 4
	PregGrepInvert         PregFlag = 1
)

// preg_last_error() codes.
const (
	PregNoError             = 0
	PregInternalError       = 1
	PregBacktrackLimitError = 2
	PregRecursionLimitError = 3
	PregBadUTF8Error        = 4
	PregBadUTF8OffsetError  = 5
	PregJITStackLimitError  = 6
)

// PregErrorMsg returns preg_last_error_msg() for an error code.
func PregErrorMsg(code int) string {
	switch code {
	case PregNoError:
		return "No error"
	case PregInternalError:
		return "Internal error"
	case PregBacktrackLimitError:
		return "Backtrack limit exhausted"
	case PregRecursionLimitError:
		return "Recursion limit exhausted"
	case PregBadUTF8Error:
		return "Malformed UTF-8 characters, possibly incorrectly encoded"
	case PregBadUTF8OffsetError:
		return "The offset did not correspond to the beginning of a valid UTF-8 code point"
	case PregJITStackLimitError:
		return "JIT stack limit exhausted"
	}
	return "Unknown error"
}

// PcreError is Composer\Pcre\PcreException: a preg_* function failed.
// Code is the preg_last_error() code; Warning is the warning PHP emitted
// for a pattern that does not compile.
type PcreError struct {
	Function string
	Pattern  string
	Code     int
	Warning  string
}

func (e *PcreError) Error() string {
	return e.Function + "(): failed executing \"" + e.Pattern + "\": " + PregErrorMsg(e.Code)
}

// UnexpectedNullMatchError is Composer\Pcre\UnexpectedNullMatchException,
// returned by the strict-groups variants.
type UnexpectedNullMatchError struct {
	msg string
}

func (e *UnexpectedNullMatchError) Error() string { return e.msg }

// Match is the result of one successful match.
type Match struct {
	re      *Regexp
	subject string
	offs    []int // start and end of each group, -1 when unset
	count   int   // pcre2_match's return code: highest set group + 1
}

func (re *Regexp) newMatch(subject string, caps []int) *Match {
	offs := make([]int, len(caps))
	copy(offs, caps)
	return &Match{re: re, subject: subject, offs: offs, count: capsCount(caps)}
}

// capsCount is pcre2_match's return code for a match: the number of groups
// up to the highest one that is set.
func capsCount(caps []int) int {
	for g := len(caps)/2 - 1; g > 0; g-- {
		if caps[2*g] >= 0 {
			return g + 1
		}
	}
	return 1
}

// Group returns group i and whether it participated in the match
// (PREG_UNMATCHED_AS_NULL: false means null).
func (m *Match) Group(i int) (string, bool) {
	if i < 0 || 2*i >= len(m.offs) || m.offs[2*i] < 0 {
		return "", false
	}
	return m.subject[m.offs[2*i]:m.offs[2*i+1]], true
}

// Get returns group i, or "" when it did not participate.
func (m *Match) Get(i int) string {
	s, _ := m.Group(i)
	return s
}

// Named returns the group called name as $matches[name] holds it. With
// duplicate names (J), PHP keeps the first group of that name and lets
// later ones replace it only when they are set.
func (m *Match) Named(name string) (string, bool) {
	value, ok, seen := "", false, false
	for g, n := range m.re.names {
		if n != name {
			continue
		}
		if v, set := m.Group(g); set || !seen {
			value, ok, seen = v, set, true
		}
	}
	return value, ok
}

// Offset returns the byte offset of group i, or -1 when it did not
// participate.
func (m *Match) Offset(i int) int {
	if i < 0 || 2*i >= len(m.offs) {
		return -1
	}
	return m.offs[2*i]
}

// Groups returns the number of groups including group 0.
func (m *Match) Groups() int { return len(m.offs) / 2 }

// Count returns pcre2_match's result: the number of groups up to the last
// one that participated. Without PREG_UNMATCHED_AS_NULL, PHP leaves the
// groups after it out of $matches.
func (m *Match) Count() int { return m.count }

// Array returns $matches as preg_match fills it with flags
// (PREG_OFFSET_CAPTURE, PREG_UNMATCHED_AS_NULL): ports
// populate_subpat_array.
func (m *Match) Array(flags PregFlag) *Array {
	n := m.Groups()
	a := NewArrayCap(n)
	for g := range n {
		if g >= m.count && flags&PregUnmatchedAsNull == 0 {
			break
		}
		v := m.value(g, flags)
		if name := m.re.names[g]; name != "" {
			// A duplicate name only takes the value of a later group
			// that is set.
			if k := StrKey(name); m.offs[2*g] >= 0 || !a.Has(name) {
				a.SetKey(k, v)
			}
		}
		a.Append(v)
	}
	return a
}

// value is one entry of $matches.
func (m *Match) value(g int, flags PregFlag) any {
	var v any
	if s, ok := m.Group(g); ok {
		v = s
	} else if flags&PregUnmatchedAsNull == 0 {
		v = ""
	}
	if flags&PregOffsetCapture != 0 {
		return ListOf(v, int64(m.Offset(g)))
	}
	return v
}

// checkStrict ports Preg::enforceNonNullMatches: the first unmatched group
// in $matches order is an error.
func (m *Match) checkStrict(variant string) error {
	for g := range m.Groups() {
		if _, ok := m.Group(g); ok {
			continue
		}
		key := strconv.Itoa(g)
		if name := m.re.names[g]; name != "" {
			key = name
		}
		return &UnexpectedNullMatchError{msg: "Pattern \"" + m.re.pattern + "\" had an unexpected unmatched group \"" + key + "\", make sure the pattern always matches or use " + variant + "() instead."}
	}
	return nil
}

// begin validates a subject and start offset as pcre2_match does before
// matching and returns a machine for it.
func (re *Regexp) begin(fn, subject string, offset int) (*machine, int, error) {
	if offset < 0 {
		offset = max(len(subject)+offset, 0)
	}
	if offset > len(subject) {
		return nil, 0, re.fail(fn, PregInternalError)
	}
	if code := re.checkUTF(subject, offset); code != PregNoError {
		return nil, 0, re.fail(fn, code)
	}
	return re.getMachine(subject), offset, nil
}

// checkUTF ports pcre2_match's UTF check: the offset must start a
// character, and the subject from the start offset, less the longest
// lookbehind, must be valid UTF-8.
func (re *Regexp) checkUTF(s string, offset int) int {
	if !re.utf {
		return PregNoError
	}
	if offset > 0 && offset < len(s) && s[offset]&0xC0 == 0x80 {
		return PregBadUTF8OffsetError
	}
	from := offset
	for i := re.lookback; i > 0 && from > 0; i-- {
		from--
		for from > 0 && s[from]&0xC0 == 0x80 {
			from--
		}
	}
	if !utf8.ValidString(s[from:]) {
		return PregBadUTF8Error
	}
	return PregNoError
}

func (re *Regexp) fail(fn string, code int) *PcreError {
	return &PcreError{Function: fn, Pattern: re.pattern, Code: code}
}

func execError(code int) int {
	switch code {
	case matchErrBacktrack:
		return PregBacktrackLimitError
	case matchErrRecursion:
		return PregJITStackLimitError
	}
	return PregInternalError
}

// errEndBeforeStart is a match that ends before it starts (\K in a
// lookahead): preg_match and preg_match_all return false without setting
// an error code, the other functions report an internal error.
const errEndBeforeStart = -1

// each ports the global matching loop of preg_match_all and
// preg_replace: after an empty match it retries at the same position with
// PCRE2_NOTEMPTY_ATSTART | PCRE2_ANCHORED, then advances one character.
// fn returns false to stop.
func (m *machine) each(start int, fn func(caps []int) bool) int {
	pos, notEmpty := start, false
	for {
		found, code := m.exec(pos, notEmpty, notEmpty)
		if code != matchOK {
			return execError(code)
		}
		if !found {
			if notEmpty && pos < len(m.s) {
				pos += m.charLen(pos)
				notEmpty = false
				continue
			}
			return PregNoError
		}
		if m.caps[1] < m.caps[0] {
			return errEndBeforeStart
		}
		if !fn(m.caps) {
			return PregNoError
		}
		pos = m.caps[1]
		notEmpty = pos == m.caps[0]
	}
}

// MatchAt ports preg_match($pattern, $subject, $m, PREG_UNMATCHED_AS_NULL,
// $offset). It returns nil when there is no match.
func (re *Regexp) MatchAt(subject string, offset int) (*Match, error) {
	re = re.compiled()
	m, start, err := re.begin("preg_match", subject, offset)
	if err != nil {
		return nil, err
	}
	defer re.putMachine(m)
	found, code := m.exec(start, false, false)
	switch {
	case code != matchOK:
		return nil, re.fail("preg_match", execError(code))
	case !found:
		return nil, nil
	case m.caps[1] < m.caps[0]:
		return nil, re.fail("preg_match", PregNoError)
	}
	return re.newMatch(subject, m.caps), nil
}

// Match ports Preg::match: nil when the pattern does not match.
func (re *Regexp) Match(subject string) (*Match, error) { return re.MatchAt(subject, 0) }

// IsMatch ports Preg::isMatch.
func (re *Regexp) IsMatch(subject string) (bool, error) {
	re = re.compiled()
	m, _, err := re.begin("preg_match", subject, 0)
	if err != nil {
		return false, err
	}
	defer re.putMachine(m)
	found, code := m.exec(0, false, false)
	switch {
	case code != matchOK:
		return false, re.fail("preg_match", execError(code))
	case found && m.caps[1] < m.caps[0]:
		return false, re.fail("preg_match", PregNoError)
	}
	return found, nil
}

// MatchStrictGroups ports Preg::matchStrictGroups.
func (re *Regexp) MatchStrictGroups(subject string) (*Match, error) {
	m, err := re.MatchAt(subject, 0)
	if err != nil || m == nil {
		return m, err
	}
	if err := m.checkStrict("match"); err != nil {
		return nil, err
	}
	return m, nil
}

// MatchAllAt ports preg_match_all from offset, returning the matches in
// set order.
func (re *Regexp) MatchAllAt(subject string, offset int) ([]*Match, error) {
	re = re.compiled()
	m, start, err := re.begin("preg_match_all", subject, offset)
	if err != nil {
		return nil, err
	}
	defer re.putMachine(m)
	var ms []*Match
	if code := m.each(start, func(caps []int) bool {
		ms = append(ms, re.newMatch(subject, caps))
		return true
	}); code != PregNoError {
		return nil, re.fail("preg_match_all", max(code, PregNoError))
	}
	return ms, nil
}

// MatchAll ports Preg::matchAll, in set order.
func (re *Regexp) MatchAll(subject string) ([]*Match, error) { return re.MatchAllAt(subject, 0) }

// MatchAllStrictGroups ports Preg::matchAllStrictGroups.
func (re *Regexp) MatchAllStrictGroups(subject string) ([]*Match, error) {
	ms, err := re.MatchAll(subject)
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		if err := m.checkStrict("matchAll"); err != nil {
			return nil, err
		}
	}
	return ms, nil
}

// MatchArray ports preg_match($pattern, $subject, $matches, $flags,
// $offset): it returns the result and $matches.
func (re *Regexp) MatchArray(subject string, flags PregFlag, offset int) (int, *Array, error) {
	m, err := re.MatchAt(subject, offset)
	if err != nil {
		return 0, nil, err
	}
	if m == nil {
		return 0, NewArray(), nil
	}
	return 1, m.Array(flags), nil
}

// MatchAllArray ports preg_match_all($pattern, $subject, $matches, $flags,
// $offset) with PREG_PATTERN_ORDER (the default) or PREG_SET_ORDER.
func (re *Regexp) MatchAllArray(subject string, flags PregFlag, offset int) (int, *Array, error) {
	re = re.compiled()
	ms, err := re.MatchAllAt(subject, offset)
	if err != nil {
		return 0, nil, err
	}
	if flags&PregSetOrder != 0 {
		a := NewArrayCap(len(ms))
		for _, m := range ms {
			a.Append(m.Array(flags))
		}
		return len(ms), a, nil
	}
	n := re.ncap + 1
	sets := make([]*Array, n)
	for g := range sets {
		sets[g] = NewArrayCap(len(ms))
	}
	for _, m := range ms {
		// Groups after the last set one are padded like unset ones.
		for g := range n {
			sets[g].Append(m.value(g, flags))
		}
	}
	a := NewArrayCap(n)
	for g, set := range sets {
		if name := re.names[g]; name != "" {
			a.SetKey(StrKey(name), set)
		}
		a.Append(set)
	}
	return len(ms), a, nil
}

// Replace ports Preg::replace (preg_replace with a string pattern and
// replacement): $n, \n and ${n} (n up to 99) are replaced by groups.
// limit < 0 means no limit. It returns the result and the number of
// replacements.
func (re *Regexp) Replace(subject, replacement string, limit int) (string, int, error) {
	return re.replace("preg_replace", subject, limit, func(b []byte, caps []int, count int) []byte {
		return expandReplacement(b, replacement, subject, caps, count)
	})
}

// ReplaceCallback ports Preg::replaceCallback.
func (re *Regexp) ReplaceCallback(subject string, fn func(*Match) string, limit int) (string, int, error) {
	re = re.compiled()
	return re.replace("preg_replace_callback", subject, limit, func(b []byte, caps []int, _ int) []byte {
		return append(b, fn(re.newMatch(subject, caps))...)
	})
}

// replace ports php_pcre_replace_impl.
func (re *Regexp) replace(fn, subject string, limit int, add func(b []byte, caps []int, count int) []byte) (string, int, error) {
	re = re.compiled()
	if limit == 0 {
		return subject, 0, nil
	}
	m, _, err := re.begin(fn, subject, 0)
	if err != nil {
		return "", 0, err
	}
	defer re.putMachine(m)
	var b []byte
	n, last := 0, 0
	if limit < 0 {
		limit = -1
	}
	code := m.each(0, func(caps []int) bool {
		if b == nil {
			b = make([]byte, 0, len(subject)+16)
		}
		b = append(b, subject[last:caps[0]]...)
		b = add(b, caps, capsCount(caps))
		last = caps[1]
		n++
		if limit > 0 {
			limit--
		}
		return limit != 0
	})
	if code == errEndBeforeStart {
		code = PregInternalError
	}
	if code != PregNoError {
		return "", 0, re.fail(fn, code)
	}
	if b == nil {
		return subject, 0, nil
	}
	return string(append(b, subject[last:]...)), n, nil
}

// expandReplacement ports the replacement expansion of
// php_pcre_replace_impl: \n, $n and ${n} with n < 100, and \\ or \$
// escaping the backslash or dollar.
func expandReplacement(b []byte, repl, subject string, caps []int, count int) []byte {
	walkLast := byte(0)
	for i := 0; i < len(repl); {
		c := repl[i]
		if c == '\\' || c == '$' {
			if walkLast == '\\' {
				b[len(b)-1] = c
				i++
				walkLast = 0
				continue
			}
			if ref, n, ok := backrefAt(repl, i); ok {
				if ref < count && caps[2*ref] >= 0 {
					b = append(b, subject[caps[2*ref]:caps[2*ref+1]]...)
				}
				i += n
				// walk_last keeps the last copied literal character.
				continue
			}
		}
		b = append(b, c)
		walkLast = c
		i++
	}
	return b
}

// backrefAt ports preg_get_backref for the reference at repl[i].
func backrefAt(repl string, i int) (ref, n int, ok bool) {
	j := i
	if j+1 >= len(repl) {
		return 0, 0, false
	}
	brace := repl[j] == '$' && repl[j+1] == '{'
	if brace {
		j++
	}
	j++
	if j >= len(repl) || !isDigit(repl[j]) {
		return 0, 0, false
	}
	ref = int(repl[j] - '0')
	j++
	if j < len(repl) && isDigit(repl[j]) {
		ref = ref*10 + int(repl[j]-'0')
		j++
	}
	if brace {
		if j >= len(repl) || repl[j] != '}' {
			return 0, 0, false
		}
		j++
	}
	return ref, j - i, true
}

// SplitPiece is one element of preg_split with PREG_SPLIT_OFFSET_CAPTURE.
type SplitPiece struct {
	Value  string
	Offset int
}

// Split ports Preg::split (preg_split without PREG_SPLIT_OFFSET_CAPTURE).
// limit <= 0 means no limit.
func (re *Regexp) Split(subject string, limit int, flags PregFlag) ([]string, error) {
	pieces, err := re.SplitWithOffsets(subject, limit, flags)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(pieces))
	for i, p := range pieces {
		out[i] = p.Value
	}
	return out, nil
}

// SplitWithOffsets ports Preg::splitWithOffsets (php_pcre_split_impl with
// PREG_SPLIT_OFFSET_CAPTURE).
func (re *Regexp) SplitWithOffsets(subject string, limit int, flags PregFlag) ([]SplitPiece, error) {
	re = re.compiled()
	noEmpty := flags&PregSplitNoEmpty != 0
	delim := flags&PregSplitDelimCapture != 0
	var out []SplitPiece
	add := func(start, end int) {
		if start < 0 {
			out = append(out, SplitPiece{"", -1})
			return
		}
		out = append(out, SplitPiece{subject[start:end], start})
	}
	if limit == 0 {
		limit = -1
	}
	last := 0
	if limit == -1 || limit > 1 {
		m, _, err := re.begin("preg_split", subject, 0)
		if err != nil {
			return nil, err
		}
		defer re.putMachine(m)
		pos, notEmpty := 0, false
		for limit == -1 || limit > 1 {
			found, code := m.exec(pos, notEmpty, notEmpty)
			if code != matchOK {
				return nil, re.fail("preg_split", execError(code))
			}
			if !found {
				if notEmpty && pos < len(subject) {
					pos += m.charLen(pos)
					notEmpty = false
					continue
				}
				break
			}
			caps := m.caps
			if caps[1] < caps[0] {
				return nil, re.fail("preg_split", PregInternalError)
			}
			if !noEmpty || caps[0] != last {
				add(last, caps[0])
				if limit != -1 {
					limit--
				}
			}
			if delim {
				for g := 1; g < capsCount(caps); g++ {
					if !noEmpty || caps[2*g] != caps[2*g+1] {
						add(caps[2*g], caps[2*g+1])
					}
				}
			}
			pos, last = caps[1], caps[1]
			notEmpty = pos == caps[0]
			if notEmpty && limit != -1 && limit <= 1 {
				break
			}
		}
	}
	if !noEmpty || last < len(subject) {
		add(last, len(subject))
	}
	return out, nil
}

// Grep ports preg_grep($pattern, $input, $flags): the entries of input
// whose string value matches (or, with PregGrepInvert, does not), keys
// kept. Like PHP it stops at the first matching error and returns what it
// has, which Preg::grep does not report.
func (re *Regexp) Grep(input *Array, flags PregFlag) *Array {
	re = re.compiled()
	out := NewArray()
	invert := flags&PregGrepInvert != 0
	for k, v := range input.All() {
		s := ToString(v)
		m, _, err := re.begin("preg_grep", s, 0)
		if err != nil {
			break
		}
		found, code := m.exec(0, false, false)
		re.putMachine(m)
		if code != matchOK {
			break
		}
		if found != invert {
			out.SetKey(k, v)
		}
	}
	return out
}

// pregQuoteChars marks the bytes preg_quote escapes.
var pregQuoteChars = func() (t [256]bool) {
	for _, c := range []byte(`.\+*?[^]$(){}=!><|:-#`) {
		t[c] = true
	}
	t[0] = true
	return t
}()

// PregQuote ports preg_quote($str, $delimiter); delimiter may be "" (only
// its first byte counts).
func PregQuote(s, delimiter string) string {
	needs := func(c byte) bool { return pregQuoteChars[c] || delimiter != "" && c == delimiter[0] }
	i := 0
	for i < len(s) && !needs(s[i]) {
		i++
	}
	if i == len(s) {
		return s
	}
	b := make([]byte, i, len(s)+8)
	copy(b, s)
	for ; i < len(s); i++ {
		switch c := s[i]; {
		case c == 0:
			b = append(b, `\000`...)
		case needs(c):
			b = append(b, '\\', c)
		default:
			b = append(b, c)
		}
	}
	return string(b)
}

// compileFor compiles pattern for a Preg function, turning a compilation
// failure into the PcreException Composer throws.
func compileFor(fn, pattern string) (*Regexp, error) {
	re, err := Compile(pattern)
	if err != nil {
		return nil, &PcreError{Function: fn, Pattern: pattern, Code: PregInternalError, Warning: fn + "(): " + err.Error()}
	}
	return re, nil
}

// PregMatch ports Preg::match: nil when the pattern does not match.
func PregMatch(pattern, subject string) (*Match, error) {
	re, err := compileFor("preg_match", pattern)
	if err != nil {
		return nil, err
	}
	return re.Match(subject)
}

// PregIsMatch ports Preg::isMatch.
func PregIsMatch(pattern, subject string) (bool, error) {
	re, err := compileFor("preg_match", pattern)
	if err != nil {
		return false, err
	}
	return re.IsMatch(subject)
}

// PregMatchStrictGroups ports Preg::matchStrictGroups.
func PregMatchStrictGroups(pattern, subject string) (*Match, error) {
	re, err := compileFor("preg_match", pattern)
	if err != nil {
		return nil, err
	}
	return re.MatchStrictGroups(subject)
}

// PregMatchAll ports Preg::matchAll, returning the matches in set order.
func PregMatchAll(pattern, subject string) ([]*Match, error) {
	re, err := compileFor("preg_match_all", pattern)
	if err != nil {
		return nil, err
	}
	return re.MatchAll(subject)
}

// PregMatchAllStrictGroups ports Preg::matchAllStrictGroups.
func PregMatchAllStrictGroups(pattern, subject string) ([]*Match, error) {
	re, err := compileFor("preg_match_all", pattern)
	if err != nil {
		return nil, err
	}
	return re.MatchAllStrictGroups(subject)
}

// PregReplace ports Preg::replace with a string pattern and replacement.
func PregReplace(pattern, replacement, subject string, limit int) (string, int, error) {
	re, err := compileFor("preg_replace", pattern)
	if err != nil {
		return "", 0, err
	}
	return re.Replace(subject, replacement, limit)
}

// PregReplaceCallback ports Preg::replaceCallback.
func PregReplaceCallback(pattern, subject string, fn func(*Match) string, limit int) (string, int, error) {
	re, err := compileFor("preg_replace_callback", pattern)
	if err != nil {
		return "", 0, err
	}
	return re.ReplaceCallback(subject, fn, limit)
}

// PregSplit ports Preg::split.
func PregSplit(pattern, subject string, limit int, flags PregFlag) ([]string, error) {
	re, err := compileFor("preg_split", pattern)
	if err != nil {
		return nil, err
	}
	return re.Split(subject, limit, flags)
}

// PregGrep ports Preg::grep.
func PregGrep(pattern string, input *Array, flags PregFlag) (*Array, error) {
	re, err := compileFor("preg_grep", pattern)
	if err != nil {
		return nil, err
	}
	return re.Grep(input, flags), nil
}
