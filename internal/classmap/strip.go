// Ports php_strip_whitespace() (ext/standard/basic_functions.c), zend_strip()
// (Zend/zend_highlight.c) and the parts of the Zend language scanner
// (Zend/zend_language_scanner.l) that decide its output, as of every PHP
// minor version Composer 2.10 runs on (7.2 to 8.5; see phpVersion).
//
// PhpFileParser::findClasses() runs every file through php_strip_whitespace()
// before looking for classes, so the classes found depend on exactly how PHP
// tokenizes the file: which bytes are whitespace or comments (dropped),
// where strings, heredocs and inline HTML start and end (kept verbatim), and
// the quirks of zend_strip() around heredoc terminators. That changes from
// one PHP version to the next, so the scanner follows the version of the
// PHP Composer runs on (Parser.PHPVersionID).
//
// The scanner is reproduced rule by rule, including re2c's longest-match
// choice between rules, the state stack, the heredoc label stack and the
// heredoc "scan ahead" pass that PHP runs to learn the indentation of the
// closing marker. Rules whose only effect is the token type the parser sees
// are folded together; token boundaries only matter where they decide which
// bytes are whitespace or comments, so tokens that are copied verbatim are
// merged freely as long as every state transition is preserved.
//
// Lexical errors in PHP throw a ParseError but zend_strip() ignores them and
// keeps scanning, with one exception that changes output: an error raised
// during the heredoc scan-ahead stops it early, leaving whatever indentation
// it had found so far. Errors are therefore tracked (lexer.exc) and honoured
// only there.
//
// Settings: the CLI SAPI sets CG(skip_shebang) (PHP >= 8.0), so a leading
// "#!" line is dropped; short_open_tag is configurable (PHP's built-in
// default is On); zend.multibyte is assumed Off (its default).

package classmap

import (
	"bytes"
	"slices"
)

// stripPadding mirrors ZEND_MMAP_AHEAD: the scanner reads up to 32 NUL bytes
// past the end of the input. Twice that is allocated so that bounded
// lookahead from any reachable position never leaves the buffer.
const (
	zendMmapAhead = 32
	stripPadding  = 2 * zendMmapAhead
)

type lexState uint8

const (
	stShebang lexState = iota
	stInitial
	stInScripting
	stLookingForProperty
	stLookingForVarname
	stVarOffset
	stDoubleQuotes
	stBackquote
	stHeredoc
	stNowdoc
	stEndHeredoc
)

type token uint8

const (
	tEnd          token = iota // END (0): stops zend_strip()
	tWhitespace                // T_WHITESPACE
	tComment                   // T_COMMENT, T_DOC_COMMENT
	tStartHeredoc              // T_START_HEREDOC
	tEndHeredoc                // T_END_HEREDOC
	tOther                     // any other token, copied verbatim
)

const (
	heredocUsingSpaces = 1
	heredocUsingTabs   = 2
)

// heredocLabel is zend_heredoc_label; the label bytes live in the source.
type heredocLabel struct {
	start, length int
	indentation   int
	usesSpaces    bool
}

// lexer holds the scanner globals (SCNG) that lex_scan() works on. src is
// the file contents followed by stripPadding zero bytes; lim is the content
// length (YYLIMIT).
type lexer struct {
	src       []byte
	lim       int
	cur       int // YYCURSOR
	text      int // yytext: start of the current token
	state     lexState
	stack     []lexState // state_stack
	nest      []byte     // nest_location_stack (only tracked during scan-ahead)
	labels    []heredocLabel
	shortTags bool
	v         phpVersion

	scanAhead bool // SCNG(heredoc_scan_ahead)
	hdIndent  int  // SCNG(heredoc_indentation)
	hdSpaces  bool // SCNG(heredoc_indentation_uses_spaces)
	dqScanned int  // SCNG(scanned_string_len)
	exc       bool // a ParseError was thrown (EG(exception))
}

// Character classes of the scanner, as lookup tables.
var (
	labelStart [256]bool // [a-zA-Z_\x80-\xff]
	labelSucc  [256]bool // [a-zA-Z0-9_\x80-\xff]
	isWS       [256]bool // [ \n\r\t]
	// badChar holds the bytes that only {ANY_CHAR} matches in
	// ST_IN_SCRIPTING and ST_VAR_OFFSET: the control characters other
	// than [\t\n\r], and DEL.
	badChar [256]bool
)

func init() {
	for c := range 256 {
		start := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c >= 0x80
		labelStart[c] = start
		labelSucc[c] = start || c >= '0' && c <= '9'
	}
	for _, c := range []byte(" \n\r\t") {
		isWS[c] = true
	}
	for c := range 0x20 {
		badChar[c] = !isWS[c]
	}
	badChar[0x7f] = true
}

// StripWhitespace returns what php_strip_whitespace() returns for a file
// with the given contents under the parser's PHP: the source with comments
// removed and whitespace collapsed.
func (p Parser) StripWhitespace(contents []byte) []byte {
	buf := make([]byte, len(contents)+stripPadding)
	copy(buf, contents)
	var l lexer

	return l.strip(nil, buf, len(contents), p.ShortOpenTag, p.phpVersion())
}

// strip is zend_strip() over src[:lim], which must be followed by
// stripPadding zero bytes (len(src) >= lim+stripPadding). The output is
// appended to dst. The lexer is reset first; its stacks are reused. v is
// the PHP version whose scanner is followed.
//
// Tokens copied verbatim are contiguous in src, so runs of them are copied
// at once.
func (l *lexer) strip(dst, src []byte, lim int, shortTags bool, v phpVersion) []byte {
	state := stShebang
	if v < php80 {
		// open_file_for_scanning() of PHP 7.4 starts in SHEBANG for the
		// first file scanned only (it resets CG(skip_shebang)), which is
		// the main script; 7.2 and 7.3 have no SHEBANG state.
		state = stInitial
	}
	*l = lexer{
		src: src, lim: lim, shortTags: shortTags, v: v, state: state,
		stack: l.stack[:0], nest: l.nest[:0], labels: l.labels[:0],
	}
	prevSpace := false
	run, runEnd := -1, -1 // pending run of verbatim tokens
	for {
		t := l.lex()
		if t == tOther || t == tStartHeredoc {
			if run < 0 || l.text != runEnd {
				if run >= 0 {
					dst = append(dst, src[run:runEnd]...)
				}
				run = l.text
			}
			runEnd = l.cur
			prevSpace = false

			continue
		}
		if run >= 0 {
			dst = append(dst, src[run:runEnd]...)
			run = -1
		}
		switch t {
		case tEnd:
			return dst
		case tWhitespace:
			if !prevSpace {
				dst = append(dst, ' ')
				prevSpace = true
			}
		case tEndHeredoc:
			dst = append(dst, src[l.text:l.cur]...)
			// read the following character, either newline or ;
			if l.lex() != tWhitespace {
				dst = append(dst, src[l.text:l.cur]...)
			}
			dst = append(dst, '\n')
			prevSpace = true
		}
	}
}

func (l *lexer) push(s lexState) {
	l.stack = append(l.stack, l.state)
	l.state = s
}

func (l *lexer) pop() {
	n := len(l.stack) - 1
	l.state = l.stack[n]
	l.stack = l.stack[:n]
}

// enterNesting is enter_nesting() (PHP >= 8.0). The nesting stack is only
// consulted for the errors it raises, which only matter during scan-ahead
// (it starts out empty there), so it is not tracked otherwise.
func (l *lexer) enterNesting(c byte) {
	if l.scanAhead && l.v >= php80 {
		l.nest = append(l.nest, c)
	}
}

// exitNesting is exit_nesting() (PHP >= 8.0).
func (l *lexer) exitNesting(closing byte) {
	if !l.scanAhead || l.v < php80 {
		return
	}
	n := len(l.nest)
	if n == 0 {
		l.exc = true // Unmatched '%c'

		return
	}
	opening := l.nest[n-1]
	if opening == '{' && closing != '}' || opening == '[' && closing != ']' || opening == '(' && closing != ')' {
		l.exc = true // report_bad_nesting()

		return
	}
	l.nest = l.nest[:n-1]
}

// lex is lex_scan() in non-parser mode. The token text is src[text:cur].
func (l *lexer) lex() token {
	for {
		l.text = l.cur
		var t token
		var again bool
		switch l.state {
		case stShebang:
			if l.lexShebang() {
				return tEnd
			}
			again = true
		case stInitial:
			t = l.lexInitial()
		case stInScripting:
			t = l.lexScripting()
		case stLookingForProperty:
			t, again = l.lexLookingForProperty()
		case stLookingForVarname:
			t, again = l.lexLookingForVarname()
		case stVarOffset:
			t = l.lexVarOffset()
		case stDoubleQuotes, stBackquote, stHeredoc:
			t = l.lexEncapsed()
		case stNowdoc:
			t = l.lexNowdoc()
		case stEndHeredoc:
			t = l.lexEndHeredoc()
		}
		if !again {
			return t
		}
	}
}

// lexShebang handles <SHEBANG>"#!" .* {NEWLINE} and <SHEBANG>{ANY_CHAR},
// which produce no token; it reports whether the scanner ends instead.
func (l *lexer) lexShebang() (end bool) {
	l.state = stInitial
	if l.src[l.cur] == '#' && l.src[l.cur+1] == '!' {
		// ".*" also runs over the zero padding, so without a "\n" the
		// scanner hits YYFILL and lex_scan() returns 0.
		i := bytes.IndexByte(l.src[l.cur+2:l.lim], '\n')
		if i < 0 {
			return true
		}
		l.cur += 2 + i + 1
	}

	return false
}

func (l *lexer) lexInitial() token {
	s := l.src
	if s[l.cur] == '<' && s[l.cur+1] == '?' {
		switch {
		case s[l.cur+2] == '=':
			l.cur += 3
			l.state = stInScripting

			return tOther
		case s[l.cur+2]|0x20 == 'p' && s[l.cur+3]|0x20 == 'h' && s[l.cur+4]|0x20 == 'p':
			switch s[l.cur+5] {
			case ' ', '\t', '\n':
				l.cur += 6
				l.state = stInScripting

				return tOther
			case '\r':
				l.cur += 6
				if s[l.cur] == '\n' {
					l.cur++
				}
				l.state = stInScripting

				return tOther
			}
			// <INITIAL>"<?php" (PHP >= 7.4). Before, "<?php" not followed
			// by whitespace was "<?" (with short tags) or inline HTML; both
			// copy the same bytes, and nothing follows it at the end of
			// the file, so the rule is applied to every version (and so is
			// the "<?php" check of inline_char_handler, which only changes
			// where verbatim inline HTML is split).
			l.cur += 5
			if l.cur == l.lim {
				l.state = stInScripting

				return tOther
			}
			if l.shortTags {
				l.cur = l.text + 2
				l.state = stInScripting

				return tOther
			}
		default:
			l.cur += 2
			if l.shortTags {
				l.state = stInScripting

				return tOther
			}
		}
	} else {
		l.cur++
		if l.cur > l.lim {
			return tEnd
		}
	}

	// inline_char_handler
	for {
		i := bytes.IndexByte(s[l.cur:l.lim], '<')
		if i < 0 {
			l.cur = l.lim

			break
		}
		l.cur += i + 1
		if l.cur >= l.lim {
			break
		}
		if s[l.cur] == '?' {
			if l.shortTags || s[l.cur+1] == '=' ||
				s[l.cur+1]|0x20 == 'p' && s[l.cur+2]|0x20 == 'h' && s[l.cur+3]|0x20 == 'p' &&
					(l.cur+4 == l.lim || s[l.cur+4] == ' ' || s[l.cur+4] == '\t' || s[l.cur+4] == '\n' || s[l.cur+4] == '\r') {
				l.cur--

				break
			}
		}
	}

	return tOther
}

// labelEnd returns the end of the {LABEL} starting at i (src[i] must be a
// label start).
func (l *lexer) labelEnd(i int) int {
	i++
	for labelSucc[l.src[i]] {
		i++
	}

	return i
}

// wsEnd returns the end of the {WHITESPACE} run starting at i.
func (l *lexer) wsEnd(i int) int {
	for isWS[l.src[i]] {
		i++
	}

	return i
}

func (l *lexer) lexScripting() token {
	s := l.src
	c := s[l.cur]
	switch {
	case isWS[c]:
		l.cur = l.wsEnd(l.cur + 1)

		return tWhitespace
	case labelStart[c]:
		l.cur = l.scanLabelToken(l.cur)

		return tOther
	case c >= '0' && c <= '9':
		l.scanNumber()

		return tOther
	}

	switch c {
	case '$':
		if labelStart[s[l.cur+1]] {
			l.cur = l.labelEnd(l.cur + 1)
		} else {
			l.cur++
		}
	case '\\':
		l.cur++
		if labelStart[s[l.cur]] && l.v >= php80 {
			// T_NAME_FULLY_QUALIFIED; before PHP 8.0 "\" is a token of
			// its own.
			l.cur = l.qualifiedEnd(l.cur)
		}
	case '#':
		if s[l.cur+1] == '[' && l.v >= php80 {
			// T_ATTRIBUTE; before PHP 8.0 "#[" starts a comment.
			l.cur += 2
			l.enterNesting('[')

			return tOther
		}
		l.cur++
		l.scanLineComment()

		return tComment
	case '/':
		switch s[l.cur+1] {
		case '/':
			l.cur += 2
			l.scanLineComment()

			return tComment
		case '*':
			l.scanBlockComment()

			return tComment
		case '=':
			l.cur += 2
		default:
			l.cur++
		}
	case '(':
		if end := l.castEnd(l.cur); end > 0 {
			l.cur = end

			return tOther
		}
		l.cur++
		l.enterNesting('(')
	case '[':
		l.cur++
		l.enterNesting('[')
	case ')', ']':
		l.cur++
		l.exitNesting(c)
	case '{':
		l.cur++
		l.push(stInScripting)
		l.enterNesting('{')
	case '}':
		l.cur++
		if len(l.stack) > 0 {
			l.pop()
		}
		l.exitNesting('}')
	case '?':
		switch {
		case s[l.cur+1] == '?':
			if s[l.cur+2] == '=' && l.v >= php74 { // "??=" is PHP 7.4's
				l.cur += 3
			} else {
				l.cur += 2
			}
		case s[l.cur+1] == '-' && s[l.cur+2] == '>':
			if l.v < php80 {
				// "?" before PHP 8.0's "?->"; "->" follows.
				l.cur++

				break
			}
			l.cur += 3
			l.push(stLookingForProperty)
		case s[l.cur+1] == '>':
			// "?>"{NEWLINE}?
			l.cur += 2
			switch s[l.cur] {
			case '\r':
				l.cur++
				if s[l.cur] == '\n' {
					l.cur++
				}
			case '\n':
				l.cur++
			}
			l.state = stInitial
		default:
			l.cur++
		}
	case '-':
		switch s[l.cur+1] {
		case '>':
			l.cur += 2
			l.push(stLookingForProperty)
		case '-', '=':
			l.cur += 2
		default:
			l.cur++
		}
	case '<':
		if s[l.cur+1] == '<' && s[l.cur+2] == '<' {
			if t, ok := l.scanHeredocStart(); ok {
				return t
			}
		}
		switch {
		case s[l.cur+1] == '<' && s[l.cur+2] == '=', s[l.cur+1] == '=' && s[l.cur+2] == '>':
			l.cur += 3
		case s[l.cur+1] == '<', s[l.cur+1] == '=', s[l.cur+1] == '>':
			l.cur += 2
		default:
			l.cur++
		}
	case '.':
		if isDec(s[l.cur+1]) {
			l.scanNumber()
		} else {
			l.cur += operatorLen(s, l.cur, l.v)
		}
	case '\'':
		l.scanSingleQuoted()
	case '"':
		l.scanDoubleQuoted()
	case '`':
		l.cur++
		l.state = stBackquote
	default:
		// The remaining operators, {TOKENS} and {ANY_CHAR}. Their exact
		// extent matters after a heredoc's closing marker, where
		// zend_strip() copies exactly one token.
		l.cur += operatorLen(s, l.cur, l.v)
		if l.cur > l.lim {
			return tEnd
		}
		if l.v < php74 && badChar[c] {
			// Before PHP 7.4, {ANY_CHAR} warns and restarts the scanner
			// (no T_BAD_CHARACTER), so the character is dropped (and the
			// ones that follow it).
			for l.cur < l.lim && badChar[s[l.cur]] {
				l.cur++
			}
			l.text = l.cur

			return l.lexScripting()
		}
	}

	return tOther
}

// operatorLen returns the length of the ST_IN_SCRIPTING token starting at
// i, for a first character not handled by its own rules: the longest
// operator, else 1 ({TOKENS} or {ANY_CHAR}). "&" followed by a variable or
// "..." is also 1 (yyless(1)). v is the PHP version.
func operatorLen(s []byte, i int, v phpVersion) int {
	c, n1, n2 := s[i], s[i+1], s[i+2]
	switch c {
	case '|':
		if n1 == c || n1 == '=' || n1 == '>' && v >= php85 { // "|>" is PHP 8.5's
			return 2
		}
	case ':':
		if n1 == ':' {
			return 2
		}
	case '.':
		if n1 == '.' && n2 == '.' {
			return 3
		}
		if n1 == '=' {
			return 2
		}
	case '=', '!':
		if n1 == '=' {
			if n2 == '=' {
				return 3
			}

			return 2
		}
		if c == '=' && n1 == '>' {
			return 2
		}
	case '>', '*':
		if n1 == c {
			if n2 == '=' {
				return 3
			}

			return 2
		}
		if n1 == '=' {
			return 2
		}
	case '+', '&':
		if n1 == c || n1 == '=' {
			return 2
		}
	case '%', '^':
		if n1 == '=' {
			return 2
		}
	}

	return 1
}

// scanLabelToken scans a token starting with a label character in
// ST_IN_SCRIPTING: a (qualified) name, or one of the keyword rules that
// match more than a label: "yield" ... "from", "public(set)" and friends
// (PHP >= 8.4).
func (l *lexer) scanLabelToken(i int) int {
	end := l.labelEnd(i)
	switch end - i {
	case 5:
		if equalFold(l.src[i:end], "yield") {
			if e := l.yieldFromEnd(end); e > 0 {
				return e
			}
		}
	case 6, 7, 9:
		if l.src[end] == '(' && l.v >= php84 && (equalFold(l.src[i:end], "public") || equalFold(l.src[i:end], "private") ||
			equalFold(l.src[i:end], "protected")) && equalFold(l.src[end+1:end+5], "set)") {
			return end + 5
		}
	}

	return l.qualifiedEnd(i)
}

// qualifiedEnd returns the end of {LABEL}("\\"{LABEL})* starting at i: a
// T_NAME_QUALIFIED (or T_NAME_RELATIVE) since PHP 8.0, only the {LABEL}
// before.
func (l *lexer) qualifiedEnd(i int) int {
	end := l.labelEnd(i)
	if l.v < php80 {
		return end
	}
	for l.src[end] == '\\' && labelStart[l.src[end+1]] {
		end = l.labelEnd(end + 1)
	}

	return end
}

// yieldFromEnd matches {WHITESPACE_OR_COMMENTS}"from"[^a-zA-Z0-9_\x80-\xff]
// at i and returns the end of the T_YIELD_FROM token (before the final
// character), or 0. The comment alternatives can overlap (a hash comment may
// run over a newline into the next line), so like re2c's DFA this considers
// every way to match and keeps the longest.
//
// Before PHP 8.3 the rule is "yield"{WHITESPACE}"from"[^a-zA-Z0-9_\x80-\xff]:
// a comment between the words is a token of its own.
func (l *lexer) yieldFromEnd(i int) int {
	if l.v < php83 {
		if !isWS[l.src[i]] {
			return 0
		}
		p := l.wsEnd(i)
		if equalFold(l.src[p:p+4], "from") && !labelSucc[l.src[p+4]] {
			return p + 4
		}

		return 0
	}
	best := 0
	// pending holds the reachable positions not yet expanded, in increasing
	// order; it stays tiny.
	pending := l.wsOrCommentEnds(nil, i)
	for len(pending) > 0 {
		p := pending[0]
		pending = pending[1:]
		if equalFold(l.src[p:p+4], "from") && !labelSucc[l.src[p+4]] {
			best = p + 4
		}
		for _, e := range l.wsOrCommentEnds(nil, p) {
			if k, found := slices.BinarySearch(pending, e); !found {
				pending = slices.Insert(pending, k, e)
			}
		}
	}

	return best
}

// wsOrCommentEnds appends to dst the possible ends of one {WHITESPACE},
// {MULTI_LINE_COMMENT}, {SINGLE_LINE_COMMENT} or {HASH_COMMENT} starting at
// p, in increasing order. A whitespace run may end anywhere within it, but
// its end is enough: shorter ends only lead to the rest of the run.
func (l *lexer) wsOrCommentEnds(dst []int, p int) []int {
	s := l.src
	switch {
	case isWS[s[p]]:
		return append(dst, l.wsEnd(p))
	case s[p] == '/' && s[p+1] == '*':
		for j := p + 2; j < l.lim && s[j] != 0; j++ {
			if s[j] == '*' && s[j+1] == '/' {
				return append(dst, j+2)
			}
		}
	case s[p] == '/' && s[p+1] == '/':
		if e := newlineAfter(s, p+2, l.lim); e > 0 {
			return append(dst, e)
		}
	case s[p] == '#':
		// "#"(([^[\x00][^\x00\n\r]*[\n\r])|[\n\r])
		if s[p+1] == '\n' || s[p+1] == '\r' {
			dst = append(dst, p+2)
		}
		if s[p+1] != '[' && s[p+1] != 0 {
			if e := newlineAfter(s, p+2, l.lim); e > 0 {
				dst = append(dst, e)
			}
		}
	}

	return dst
}

// newlineAfter matches [^\x00\n\r]*[\n\r] at i and returns its end, or 0.
func newlineAfter(s []byte, i, lim int) int {
	for ; i < lim; i++ {
		switch s[i] {
		case 0:
			return 0
		case '\n', '\r':
			return i + 1
		}
	}

	return 0
}

// castEnd matches "("{TABS_AND_SPACES}type{TABS_AND_SPACES}")" at i and
// returns its end, or 0.
func (l *lexer) castEnd(i int) int {
	s := l.src
	j := i + 1
	for s[j] == ' ' || s[j] == '\t' {
		j++
	}
	switch s[j] | 0x20 {
	case 'i', 'd', 'f', 'r', 's', 'b', 'a', 'o', 'u', 'v':
	default:
		return 0
	}
	best := 0
	for _, typ := range castTypes {
		if !equalFold(s[j:j+len(typ)], typ) || typ == "void" && l.v < php85 {
			continue
		}
		k := j + len(typ)
		for s[k] == ' ' || s[k] == '\t' {
			k++
		}
		if s[k] == ')' && k+1 > best {
			best = k + 1
		}
	}

	return best
}

// castTypes are the types of the cast rules; "void" is PHP 8.5's.
var castTypes = []string{
	"int", "integer", "double", "float", "real", "string", "binary", "array", "object", "bool", "boolean", "unset", "void",
}

// scanNumber scans the longest of {LNUM}, {DNUM}, {EXPONENT_DNUM}, {HNUM},
// {BNUM} and {ONUM} (PHP >= 8.1) at the cursor. An octal {LNUM} containing
// 8 or 9 raises "Invalid numeric literal". Digits are separated by "_"
// since PHP 7.4.
func (l *lexer) scanNumber() {
	s := l.src
	i := l.cur
	sep := l.v >= php74
	best, isLNUM := 0, false
	if e := digitsEnd(s, i, isDec, sep); e > 0 {
		best, isLNUM = e, true
	}
	if s[i] == '0' {
		var e int
		switch s[i+1] | 0x20 {
		case 'x':
			e = digitsEnd(s, i+2, isHex, sep)
		case 'b':
			e = digitsEnd(s, i+2, isBin, sep)
		case 'o':
			if l.v >= php81 {
				e = digitsEnd(s, i+2, isOct, sep)
			}
		}
		if e > best {
			best, isLNUM = e, false
		}
	}
	// {DNUM}: ({LNUM}?"."{LNUM}) | ({LNUM}"."{LNUM}?)
	mant := 0
	lnum := digitsEnd(s, i, isDec, sep)
	dot := i
	if lnum > 0 {
		dot = lnum
	}
	if s[dot] == '.' {
		if e := digitsEnd(s, dot+1, isDec, sep); e > 0 {
			mant = e
		} else if lnum > 0 {
			mant = dot + 1
		}
	}
	if mant > best {
		best, isLNUM = mant, false
	}
	// {EXPONENT_DNUM}: ({LNUM}|{DNUM})[eE][+-]?{LNUM}
	for _, m := range [2]int{lnum, mant} {
		if m == 0 || s[m]|0x20 != 'e' {
			continue
		}
		j := m + 1
		if s[j] == '+' || s[j] == '-' {
			j++
		}
		if e := digitsEnd(s, j, isDec, sep); e > best {
			best, isLNUM = e, false
		}
	}
	if isLNUM && s[i] == '0' && bytes.ContainsAny(s[i:best], "89") {
		l.exc = true // Invalid numeric literal
	}
	l.cur = best
}

func isDec(c byte) bool { return c >= '0' && c <= '9' }
func isOct(c byte) bool { return c >= '0' && c <= '7' }
func isBin(c byte) bool { return c == '0' || c == '1' }
func isHex(c byte) bool { return isDec(c) || c|0x20 >= 'a' && c|0x20 <= 'f' }

// digitsEnd matches D+("_"D+)* (D+ without sep) at i and returns its end,
// or 0.
func digitsEnd(s []byte, i int, digit func(byte) bool, sep bool) int {
	if !digit(s[i]) {
		return 0
	}
	for {
		i++
		for digit(s[i]) {
			i++
		}
		if !sep || s[i] != '_' || !digit(s[i+1]) {
			return i
		}
		i++
	}
}

// scanLineComment scans the rest of a "#" or "//" comment: up to a newline
// or "?>". Before PHP 8.0 the newline ("\r\n" counting as one) is part of
// the comment.
func (l *lexer) scanLineComment() {
	s := l.src
	for l.cur < l.lim {
		switch s[l.cur] {
		case '\r', '\n':
			if l.v < php80 {
				l.cur++
				if s[l.cur-1] == '\r' && s[l.cur] == '\n' {
					l.cur++
				}
			}

			return
		case '?':
			if s[l.cur+1] == '>' {
				return
			}
		}
		l.cur++
	}
}

// scanBlockComment scans "/*"|"/**"{WHITESPACE} and the rest of the comment.
func (l *lexer) scanBlockComment() {
	s := l.src
	l.cur += 2
	if s[l.cur] == '*' && isWS[s[l.cur+1]] {
		l.cur = l.wsEnd(l.cur + 1)
	}
	for l.cur < l.lim {
		l.cur++
		if s[l.cur-1] == '*' && s[l.cur] == '/' {
			break
		}
	}
	if l.cur < l.lim {
		l.cur++
	} else if l.v >= php80 {
		l.exc = true // Unterminated comment (a warning before PHP 8.0)
	}
}

// scanSingleQuoted scans b?['] and the rest of the string.
func (l *lexer) scanSingleQuoted() {
	s := l.src
	l.cur++
	for l.cur < l.lim {
		c := s[l.cur]
		l.cur++
		if c == '\'' {
			return
		}
		if c == '\\' && l.cur < l.lim {
			l.cur++
		}
	}
	// Unclosed single quotes: the token runs to the end of the input.
	l.cur = l.lim
}

// scanDoubleQuoted scans b?["]: either a whole string without variables, or
// just the opening quote, after which ST_DOUBLE_QUOTES takes over.
func (l *lexer) scanDoubleQuoted() {
	s := l.src
	l.cur++
	for l.cur < l.lim {
		c := s[l.cur]
		l.cur++
		switch c {
		case '"':
			if l.scanAhead && !validEscapes(s[l.text+1:l.cur-1]) {
				l.exc = true
			}

			return
		case '$':
			if !labelStart[s[l.cur]] && s[l.cur] != '{' {
				continue
			}
		case '{':
			if s[l.cur] != '$' {
				continue
			}
		case '\\':
			if l.cur < l.lim {
				l.cur++
			}

			continue
		default:
			continue
		}
		l.cur--

		break
	}
	// Remember how much was scanned to save rescanning.
	l.dqScanned = l.cur - l.text - 1
	l.cur = l.text + 1
	l.state = stDoubleQuotes
}

// scanHeredocStart matches
// "<<<"{TABS_AND_SPACES}({LABEL}|(['"]){LABEL}\2){NEWLINE} at the cursor and
// runs its action.
func (l *lexer) scanHeredocStart() (token, bool) {
	s := l.src
	j := l.cur + 3
	for s[j] == ' ' || s[j] == '\t' {
		j++
	}
	quote := s[j]
	if quote == '\'' || quote == '"' {
		j++
	} else {
		quote = 0
	}
	if !labelStart[s[j]] {
		return 0, false
	}
	start := j
	j = l.labelEnd(j)
	length := j - start
	if quote != 0 {
		if s[j] != quote {
			return 0, false
		}
		j++
	}
	switch s[j] {
	case '\r':
		j++
		if s[j] == '\n' {
			j++
		}
	case '\n':
		j++
	default:
		return 0, false
	}

	l.cur = j
	isHeredoc := quote != '\''
	if isHeredoc {
		l.state = stHeredoc
	} else {
		l.state = stNowdoc
	}
	l.labels = append(l.labels, heredocLabel{start: start, length: length})
	label := &l.labels[len(l.labels)-1]

	if l.v < php73 {
		// Check for ending label on the next line (no indentation, no
		// scan-ahead).
		if l.atClosingPHP72(*label) {
			l.state = stEndHeredoc
		}

		return tStartHeredoc, true
	}

	saved := l.cur
	indentation, spacing := l.indentation()
	if l.cur == l.lim {
		l.cur = saved

		return tStartHeredoc, true
	}

	// Check for ending label on the next line.
	if length < l.lim-l.cur && bytes.Equal(s[l.cur:l.cur+length], s[start:start+length]) && !labelSucc[s[l.cur+length]] {
		if spacing == heredocUsingSpaces|heredocUsingTabs {
			l.exc = true // Invalid indentation - tabs and spaces cannot be mixed
		}
		l.cur = saved
		label.indentation = indentation
		l.state = stEndHeredoc

		return tStartHeredoc, true
	}

	l.cur = saved
	if isHeredoc && !l.scanAhead {
		indentation, usesSpaces := l.heredocScanAhead()
		label = &l.labels[len(l.labels)-1]
		label.indentation = indentation
		label.usesSpaces = usesSpaces
	}

	return tStartHeredoc, true
}

// indentation consumes spaces and tabs at the cursor, returning their count
// and which kinds were seen.
func (l *lexer) indentation() (n, spacing int) {
	for l.cur < l.lim {
		switch l.src[l.cur] {
		case '\t':
			spacing |= heredocUsingTabs
		case ' ':
			spacing |= heredocUsingSpaces
		default:
			return n, spacing
		}
		l.cur++
		n++
	}

	return n, spacing
}

// heredocScanAhead lexes the heredoc body ahead of time, as the
// T_START_HEREDOC action does, to learn the indentation of its closing
// marker. It returns SCNG(heredoc_indentation) and
// SCNG(heredoc_indentation_uses_spaces); the lexer state is restored.
func (l *lexer) heredocScanAhead() (int, bool) {
	saved := *l
	l.stack = nil
	l.nest = nil
	l.labels = append([]heredocLabel(nil), saved.labels...)
	l.scanAhead = true
	l.hdIndent = 0
	l.hdSpaces = false
	l.exc = false

	for level := 1; level > 0; {
		t := l.lex()
		if l.exc {
			break
		}
		switch t {
		case tStartHeredoc:
			level++
		case tEndHeredoc:
			level--
		case tEnd:
			level = 0
		}
	}

	indentation, usesSpaces, dqScanned := l.hdIndent, l.hdSpaces, l.dqScanned
	*l = saved
	// SCNG(scanned_string_len) is not part of the saved lexical state.
	l.dqScanned = dqScanned

	return indentation, usesSpaces
}

func (l *lexer) lexLookingForProperty() (token, bool) {
	s := l.src
	c := s[l.cur]
	switch {
	case isWS[c]:
		l.cur = l.wsEnd(l.cur + 1)

		return tWhitespace, false
	case c == '-' && s[l.cur+1] == '>':
		l.cur += 2

		return tOther, false
	case c == '?' && s[l.cur+1] == '-' && s[l.cur+2] == '>' && l.v >= php80:
		l.cur += 3

		return tOther, false
	case labelStart[c]:
		l.cur = l.labelEnd(l.cur)
		l.pop()

		return tOther, false
	case l.v < php82:
		// Comments are only scanned in this state since PHP 8.2; before,
		// {ANY_CHAR} leaves it and ST_IN_SCRIPTING scans them (where "#["
		// is an attribute in 8.0 and 8.1).
	case c == '#':
		l.cur++
		l.scanLineComment()

		return tComment, false
	case c == '/' && s[l.cur+1] == '/':
		l.cur += 2
		l.scanLineComment()

		return tComment, false
	case c == '/' && s[l.cur+1] == '*':
		l.scanBlockComment()

		return tComment, false
	}
	l.pop()

	return 0, true
}

func (l *lexer) lexLookingForVarname() (token, bool) {
	if labelStart[l.src[l.cur]] {
		end := l.labelEnd(l.cur)
		if l.src[end] == '[' || l.src[end] == '}' {
			l.cur = end
			l.pop()
			l.push(stInScripting)

			return tOther, false
		}
	}
	l.pop()
	l.push(stInScripting)

	return 0, true
}

func (l *lexer) lexVarOffset() token {
	s := l.src
	c := s[l.cur]
	switch {
	case c == ']':
		l.cur++
		l.pop()
	case c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '\\' || c == '\'' || c == '#':
		// Zero-length T_ENCAPSED_AND_WHITESPACE.
		l.pop()
	case labelStart[c]:
		l.cur = l.labelEnd(l.cur)
	case c == '$' && labelStart[s[l.cur+1]]:
		l.cur = l.labelEnd(l.cur + 1)
	case isDec(c):
		// [0]|([1-9][0-9]*) or {LNUM}|{HNUM}|{BNUM}|{ONUM}; the exact split
		// does not matter as nothing in them is significant.
		l.cur++
		for labelSucc[s[l.cur]] {
			l.cur++
		}
	default:
		l.cur++
		if l.cur > l.lim {
			return tEnd
		}
		if l.v < php74 && badChar[c] {
			// dropped, as in ST_IN_SCRIPTING
			for l.cur < l.lim && badChar[s[l.cur]] {
				l.cur++
			}
			l.text = l.cur

			return l.lexVarOffset()
		}
	}

	return tOther
}

// lexEncapsed handles ST_DOUBLE_QUOTES, ST_BACKQUOTE and ST_HEREDOC.
func (l *lexer) lexEncapsed() token {
	s := l.src
	c := s[l.cur]
	switch {
	case c == '$' && s[l.cur+1] == '{':
		l.cur += 2
		l.push(stLookingForVarname)
		l.enterNesting('{')

		return tOther
	case c == '{' && s[l.cur+1] == '$':
		l.cur++
		l.push(stInScripting)
		l.enterNesting('{')

		return tOther
	case c == '$' && labelStart[s[l.cur+1]]:
		l.cur = l.labelEnd(l.cur + 1)
		switch {
		case s[l.cur] == '-' && s[l.cur+1] == '>' && labelStart[s[l.cur+2]],
			s[l.cur] == '?' && s[l.cur+1] == '-' && s[l.cur+2] == '>' && labelStart[s[l.cur+3]] && l.v >= php80:
			l.push(stLookingForProperty)
		case s[l.cur] == '[':
			l.push(stVarOffset)
		}

		return tOther
	case c == '"' && l.state == stDoubleQuotes, c == '`' && l.state == stBackquote:
		l.cur++
		l.state = stInScripting

		return tOther
	}

	if l.state == stHeredoc {
		return l.lexHeredocBody()
	}

	// <ST_DOUBLE_QUOTES>{ANY_CHAR} and <ST_BACKQUOTE>{ANY_CHAR}
	l.cur++
	if l.state == stDoubleQuotes && l.dqScanned != 0 {
		l.cur += l.dqScanned - 1
		l.dqScanned = 0
	} else {
		if l.cur > l.lim {
			return tEnd
		}
		if c == '\\' && l.cur < l.lim {
			l.cur++
		}
		end := byte('"')
		if l.state == stBackquote {
			end = '`'
		}
		l.scanEncapsedText(end)
	}
	if l.scanAhead && !validEscapes(s[l.text:l.cur]) {
		l.exc = true
	}

	return tOther
}

// scanEncapsedText scans string text up to the closing quote or the start of
// a variable.
func (l *lexer) scanEncapsedText(end byte) {
	s := l.src
	for l.cur < l.lim {
		c := s[l.cur]
		l.cur++
		switch c {
		case end:
		case '$':
			if !labelStart[s[l.cur]] && s[l.cur] != '{' {
				continue
			}
		case '{':
			if s[l.cur] != '$' {
				continue
			}
		case '\\':
			if l.cur < l.lim {
				l.cur++
			}

			continue
		default:
			continue
		}
		l.cur--

		return
	}
}

// lexHeredocBody is <ST_HEREDOC>{ANY_CHAR}.
func (l *lexer) lexHeredocBody() token {
	s := l.src
	if l.cur+1 > l.lim {
		l.cur++

		return tEnd
	}
	label := l.labels[len(l.labels)-1]
	for l.cur < l.lim {
		c := s[l.cur]
		l.cur++
		switch c {
		case '\r', '\n':
			if c == '\r' && s[l.cur] == '\n' {
				l.cur++
			}
			if l.v < php73 {
				if l.atClosingPHP72(label) {
					l.state = stEndHeredoc

					return tOther
				}

				continue
			}
			indentation, spacing := l.indentation()
			if l.cur == l.lim {
				return tOther
			}
			if l.atLabel(label) {
				if labelSucc[s[l.cur+label.length]] {
					continue
				}
				if spacing == heredocUsingSpaces|heredocUsingTabs {
					l.exc = true // Invalid indentation - tabs and spaces cannot be mixed
				}
				if l.scanAhead {
					l.hdIndent = indentation
					l.hdSpaces = spacing == heredocUsingSpaces
				} else {
					l.cur -= indentation
				}
				l.state = stEndHeredoc

				return tOther
			}

			continue
		case '$':
			if !labelStart[s[l.cur]] && s[l.cur] != '{' {
				continue
			}
		case '{':
			if s[l.cur] != '$' {
				continue
			}
		case '\\':
			if l.cur < l.lim && s[l.cur] != '\n' && s[l.cur] != '\r' {
				l.cur++
			}

			continue
		default:
			continue
		}
		l.cur--

		break
	}

	return tOther
}

// lexNowdoc is <ST_NOWDOC>{ANY_CHAR}.
func (l *lexer) lexNowdoc() token {
	s := l.src
	if l.cur+1 > l.lim {
		l.cur++

		return tEnd
	}
	label := &l.labels[len(l.labels)-1]
	for l.cur < l.lim {
		c := s[l.cur]
		l.cur++
		if c != '\r' && c != '\n' {
			continue
		}
		if c == '\r' && s[l.cur] == '\n' {
			l.cur++
		}
		if l.v < php73 {
			if l.atClosingPHP72(*label) {
				l.state = stEndHeredoc

				return tOther
			}

			continue
		}
		indentation, spacing := l.indentation()
		if l.cur == l.lim {
			return tOther
		}
		if l.atLabel(*label) && !labelSucc[s[l.cur+label.length]] {
			if spacing == heredocUsingSpaces|heredocUsingTabs {
				l.exc = true // Invalid indentation - tabs and spaces cannot be mixed
			}
			l.cur -= indentation
			label.indentation = indentation
			l.state = stEndHeredoc

			return tOther
		}
	}

	return tOther
}

// atLabel reports whether the heredoc closing label starts at the cursor.
func (l *lexer) atLabel(label heredocLabel) bool {
	return labelStart[l.src[l.cur]] && label.length < l.lim-l.cur &&
		bytes.Equal(l.src[l.cur:l.cur+label.length], l.src[label.start:label.start+label.length])
}

// atClosingPHP72 reports whether a closing marker as PHP 7.2 knows it (before
// the flexible heredoc syntax of 7.3) starts at the cursor: the label at the
// start of the line, followed by an optional ";" and a newline.
func (l *lexer) atClosingPHP72(label heredocLabel) bool {
	if !l.atLabel(label) {
		return false
	}
	end := l.cur + label.length
	if l.src[end] == ';' {
		end++
	}

	return l.src[end] == '\n' || l.src[end] == '\r'
}

// lexEndHeredoc is <ST_END_HEREDOC>{ANY_CHAR}.
func (l *lexer) lexEndHeredoc() token {
	n := len(l.labels) - 1
	label := l.labels[n]
	l.labels = l.labels[:n]
	l.cur += label.indentation + label.length
	// Only an aborted scan-ahead can make the marker run past the input;
	// PHP then reads its zero padding (and beyond), which is clamped here.
	l.cur = min(l.cur, l.lim+zendMmapAhead)
	l.state = stInScripting

	return tEndHeredoc
}

// validEscapes reports whether zend_scan_escape_string() accepts str: it
// only fails on a malformed or too large "\u{...}" escape.
func validEscapes(str []byte) bool {
	for i := 0; i < len(str); i++ {
		if str[i] != '\\' {
			continue
		}
		i++
		if i >= len(str) {
			return true
		}
		if str[i] != 'u' || i+1 >= len(str) || str[i+1] != '{' {
			continue
		}
		// \u{hex+}
		j := i + 2
		for j < len(str) && isHex(str[j]) {
			j++
		}
		if j == i+2 || j >= len(str) || str[j] != '}' {
			return false
		}
		digits := bytes.TrimLeft(str[i+2:j], "0")
		if len(digits) > 6 {
			return false
		}
		var cp int
		for _, d := range digits {
			cp = cp*16 + hexVal(d)
		}
		if cp > 0x10FFFF {
			return false
		}
		i = j
	}

	return true
}

func hexVal(c byte) int {
	if c <= '9' {
		return int(c - '0')
	}

	return int(c|0x20-'a') + 10
}

// equalFold reports whether b equals the lower-case ASCII word w, ignoring
// ASCII case only (re2c's case-insensitive keywords, PCRE's i flag without
// u).
func equalFold[T string | []byte](b T, w string) bool {
	if len(b) != len(w) {
		return false
	}
	for i := range len(w) {
		c := b[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != w[i] {
			return false
		}
	}

	return true
}
