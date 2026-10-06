// Package jsonlint ports seld/jsonlint 1.12.1 (Seld\JsonLint), the JSON
// parser Composer falls back to for its "Parse error on line N" messages.
//
// Parsed values follow internal/php's value model. Behaviour is that of the
// library on PHP 8.4: the "_empty_" key substitution it applies on PHP < 7.1
// does not happen.
package jsonlint

// Ports src/Seld/JsonLint/JsonParser.php.

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// JsonParser flags.
const (
	DetectKeyConflicts        = 1
	AllowDuplicateKeys        = 2
	ParseToAssoc              = 4
	AllowComments             = 8
	AllowDuplicateKeysToArray = 16
	ValidateUTF8Encoding      = 32
)

// terminals is JsonParser::$terminals_.
var terminals = [...]string{
	2: "error", 4: "STRING", 6: "NUMBER", 8: "NULL", 10: "TRUE", 11: "FALSE", 14: "EOF",
	17: "{", 18: "}", 21: ":", 22: ",", 23: "[", 24: "]",
}

// terminal returns $this->terminals_[$sym] and whether it is set.
func terminal(sym int) (string, bool) {
	if sym < 0 || sym >= len(terminals) || terminals[sym] == "" {
		return "", false
	}

	return terminals[sym], true
}

// production is a JsonParser::$productions_ entry: the nonterminal and the
// length of its right-hand side.
type production struct{ symbol, length int }

var productions = [...]production{
	1: {3, 1}, 2: {5, 1}, 3: {7, 1}, 4: {9, 1}, 5: {9, 1}, 6: {12, 2}, 7: {13, 1},
	8: {13, 1}, 9: {13, 1}, 10: {13, 1}, 11: {13, 1}, 12: {13, 1}, 13: {15, 2},
	14: {15, 3}, 15: {20, 3}, 16: {19, 1}, 17: {19, 3}, 18: {16, 2}, 19: {16, 3},
	20: {25, 1}, 21: {25, 3},
}

// Action kinds of the parse table; a goto (a plain int in PHP) has kind 0.
const (
	actGoto   = 0
	actShift  = 1
	actReduce = 2
	actAccept = 3
)

// entry is one symbol => action pair of a JsonParser::$table row.
type entry struct {
	sym  int
	kind int
	arg  int
}

func gotoTo(sym, state int) entry { return entry{sym, actGoto, state} }
func shift(sym, state int) entry  { return entry{sym, actShift, state} }
func reduce(sym, prod int) entry  { return entry{sym, actReduce, prod} }
func reduceRow(prod int, syms ...int) []entry {
	row := make([]entry, len(syms))
	for i, sym := range syms {
		row[i] = reduce(sym, prod)
	}

	return row
}

// table is JsonParser::$table, rows in PHP's key order.
var table = [...][]entry{
	0:  {gotoTo(3, 5), shift(4, 12), gotoTo(5, 6), shift(6, 13), gotoTo(7, 3), shift(8, 9), gotoTo(9, 4), shift(10, 10), shift(11, 11), gotoTo(12, 1), gotoTo(13, 2), gotoTo(15, 7), gotoTo(16, 8), shift(17, 14), shift(23, 15)},
	1:  {{1, actAccept, 0}},
	2:  {shift(14, 16)},
	3:  reduceRow(7, 14, 18, 22, 24),
	4:  reduceRow(8, 14, 18, 22, 24),
	5:  reduceRow(9, 14, 18, 22, 24),
	6:  reduceRow(10, 14, 18, 22, 24),
	7:  reduceRow(11, 14, 18, 22, 24),
	8:  reduceRow(12, 14, 18, 22, 24),
	9:  reduceRow(3, 14, 18, 22, 24),
	10: reduceRow(4, 14, 18, 22, 24),
	11: reduceRow(5, 14, 18, 22, 24),
	12: reduceRow(1, 14, 18, 21, 22, 24),
	13: reduceRow(2, 14, 18, 22, 24),
	14: {gotoTo(3, 20), shift(4, 12), shift(18, 17), gotoTo(19, 18), gotoTo(20, 19)},
	15: {gotoTo(3, 5), shift(4, 12), gotoTo(5, 6), shift(6, 13), gotoTo(7, 3), shift(8, 9), gotoTo(9, 4), shift(10, 10), shift(11, 11), gotoTo(13, 23), gotoTo(15, 7), gotoTo(16, 8), shift(17, 14), shift(23, 15), shift(24, 21), gotoTo(25, 22)},
	16: {reduce(1, 6)},
	17: reduceRow(13, 14, 18, 22, 24),
	18: {shift(18, 24), shift(22, 25)},
	19: reduceRow(16, 18, 22),
	20: {shift(21, 26)},
	21: reduceRow(18, 14, 18, 22, 24),
	22: {shift(22, 28), shift(24, 27)},
	23: reduceRow(20, 22, 24),
	24: reduceRow(14, 14, 18, 22, 24),
	25: {gotoTo(3, 20), shift(4, 12), gotoTo(20, 29)},
	26: {gotoTo(3, 5), shift(4, 12), gotoTo(5, 6), shift(6, 13), gotoTo(7, 3), shift(8, 9), gotoTo(9, 4), shift(10, 10), shift(11, 11), gotoTo(13, 30), gotoTo(15, 7), gotoTo(16, 8), shift(17, 14), shift(23, 15)},
	27: reduceRow(19, 14, 18, 22, 24),
	28: {gotoTo(3, 5), shift(4, 12), gotoTo(5, 6), shift(6, 13), gotoTo(7, 3), shift(8, 9), gotoTo(9, 4), shift(10, 10), shift(11, 11), gotoTo(13, 31), gotoTo(15, 7), gotoTo(16, 8), shift(17, 14), shift(23, 15)},
	29: reduceRow(17, 18, 22),
	30: reduceRow(15, 18, 22),
	31: reduceRow(21, 22, 24),
}

func lookup(state, sym int) (entry, bool) {
	for _, e := range table[state] {
		if e.sym == sym {
			return e, true
		}
	}

	return entry{}, false
}

// defaultActionState is the state of JsonParser::$defaultActions, whose
// action is reduce by production 6.
const defaultActionState = 16

// member is the [key, value] pair production 15 builds.
type member struct {
	key   string
	value any
}

// Lint ports JsonParser::lint: nil when input is valid JSON, otherwise the
// error Parse returns (a *ParsingError, *DuplicateKeyError or
// *InvalidEncodingError for what PHP returns; anything else is what PHP
// would throw out of lint).
func Lint(input string, flags int) error {
	_, err := Parse(input, flags)

	return err
}

var (
	unescapedBackslash = php.MustCompile(`{".+?(\\[^"bfnrt/\\u](...)?)}`)
	unterminatedString = php.MustCompile(`{"(?:[^"]+|\\")*$}m`)
)

// Parse ports JsonParser::parse. Errors are *ParsingError,
// *DuplicateKeyError, *InvalidEncodingError, *util.InvalidArgumentError,
// and *PHPError for PHP's \Error.
func Parse(input string, flags int) (any, error) {
	if flags&AllowDuplicateKeysToArray != 0 && flags&AllowDuplicateKeys != 0 {
		return nil, &util.InvalidArgumentError{Message: "Only one of ALLOW_DUPLICATE_KEYS and ALLOW_DUPLICATE_KEYS_TO_ARRAY can be used, you passed in both."}
	}
	if flags&ValidateUTF8Encoding != 0 {
		if err := ValidateUTF8(input); err != nil {
			return nil, err
		}
	}

	if strings.HasPrefix(input, "\xEF\xBB\xBF") {
		return nil, &ParsingError{Message: "BOM detected, make sure your input does not include a Unicode Byte-Order-Mark"}
	}

	p := parser{flags: flags, lexer: newLexer(input, flags)}

	return p.parse()
}

// parser holds JsonParser's per-parse state.
type parser struct {
	flags  int
	lexer  *lexer
	stack  []int
	vstack []any
}

func (p *parser) parse() (any, error) {
	p.stack = append(make([]int, 0, 32), 0)
	p.vstack = append(make([]any, 0, 16), nil)

	yytext := ""
	yylineno := 0
	yyloc := p.lexer.yylloc

	symbol, haveSymbol := 0, false
	for {
		state := p.stack[len(p.stack)-1]

		var action entry
		ok := true
		if state == defaultActionState {
			action = entry{kind: actReduce, arg: 6}
		} else {
			if !haveSymbol {
				var err error
				if symbol, err = p.lexer.lex(); err != nil {
					return nil, err
				}
				haveSymbol = true
			}
			action, ok = lookup(state, symbol)
		}

		// handle parse error; error recovery never succeeds as no state
		// handles the error symbol, so the first error is the result.
		if !ok || action.kind == actGoto {
			return nil, p.syntaxError(state, symbol, yylineno, yyloc)
		}

		switch action.kind {
		case actShift:
			p.stack = append(p.stack, symbol, action.arg)
			p.vstack = append(p.vstack, p.lexer.yytext)
			haveSymbol = false
			yytext = p.lexer.yytext
			yylineno = p.lexer.yylineno
			yyloc = p.lexer.yylloc

		case actReduce:
			prod := productions[action.arg]
			newToken, done, err := p.performAction(p.vstack[len(p.vstack)-prod.length], yytext, yylineno, action.arg)
			if err != nil || done {
				return newToken, err
			}

			p.stack = p.stack[:len(p.stack)-2*prod.length]
			p.vstack = p.vstack[:len(p.vstack)-prod.length]

			p.stack = append(p.stack, prod.symbol)
			p.vstack = append(p.vstack, newToken)
			next, _ := lookup(p.stack[len(p.stack)-2], prod.symbol)
			p.stack = append(p.stack, next.arg)

		case actAccept:
			return true, nil
		}
	}
}

// syntaxError builds the "Parse error on line N" ParsingException.
func (p *parser) syntaxError(state, symbol, yylineno int, yyloc Location) error {
	l := p.lexer
	expected := []string{}
	for _, e := range table[state] {
		if name, ok := terminal(e.sym); ok && e.sym > 2 {
			expected = append(expected, "'"+name+"'")
		}
	}

	message := ""
	if slices.Contains(expected, "'STRING'") && (strings.HasPrefix(l.match, `"`) || strings.HasPrefix(l.match, "'")) {
		message = "Invalid string"
		if strings.HasPrefix(l.match, "'") {
			message += ", it appears you used single quotes instead of double quotes"
		} else if m, err := unescapedBackslash.Match(l.fullUpcomingInput()); err == nil && m != nil {
			message += ", it appears you have an unescaped backslash at: " + m.Get(1)
		} else if ok, err := unterminatedString.IsMatch(l.fullUpcomingInput()); err == nil && ok {
			message += ", it appears you forgot to terminate a string, or attempted to write a multiline string which is invalid"
		}
	}

	var b strings.Builder
	b.WriteString("Parse error on line ")
	b.WriteString(itoa(yylineno + 1))
	b.WriteString(":\n")
	b.WriteString(l.showPosition())
	b.WriteByte('\n')
	if message != "" {
		b.WriteString(message)
	} else {
		if len(expected) > 1 {
			b.WriteString("Expected one of: ")
		} else {
			b.WriteString("Expected: ")
		}
		b.WriteString(strings.Join(expected, ", "))
	}

	if strings.HasSuffix(php.Trim(l.pastInput()), ",") {
		b.WriteString(" - It appears you have an extra trailing comma")
	}

	var token any = int64(symbol)
	if name, ok := terminal(symbol); ok {
		token = name
	}

	return &ParsingError{Message: b.String(), Details: Details{
		Kind:     SyntaxDetails,
		Text:     l.match,
		Token:    token,
		Line:     l.yylineno,
		Loc:      yyloc,
		Expected: expected,
	}}
}

// performAction ports JsonParser::performAction. done reports that the
// parse is complete with token as its result.
func (p *parser) performAction(token any, yytext string, yylineno, yystate int) (_ any, done bool, _ error) {
	vs := p.vstack
	top := len(vs) - 1
	assoc := p.flags&ParseToAssoc != 0

	switch yystate {
	case 1:
		token = interpolate(yytext)
	case 2:
		if strings.ContainsAny(yytext, "eE") || strings.Contains(yytext, ".") {
			token = php.ToFloat(yytext)
		} else {
			token = php.ToInt(yytext)
		}
	case 3:
		token = nil
	case 4:
		token = true
	case 5:
		token = false
	case 6:
		return vs[top-1], true, nil
	case 13:
		if assoc {
			token = php.NewArray()
		} else {
			token = php.NewObject()
		}
	case 14, 19:
		token = vs[top-1]
	case 15:
		key, _ := vs[top-2].(string)
		token = member{key: key, value: vs[top]}
	case 16:
		m, _ := vs[top].(member)
		if assoc {
			a := php.NewArray()
			a.Set(m.key, m.value)
			token = a
		} else {
			o := php.NewObject()
			if err := setProperty(o, m.key, m.value); err != nil {
				return nil, false, err
			}
			token = o
		}
	case 17:
		m, _ := vs[top].(member)
		token = vs[top-2]
		var err error
		if a, ok := token.(*php.Array); ok {
			err = p.addArrayMember(a, m, yylineno)
		} else if o, ok := token.(*php.Object); ok {
			err = p.addObjectMember(o, m, yylineno)
		}
		if err != nil {
			return nil, false, err
		}
	case 18:
		token = php.NewArray()
	case 20:
		token = php.ListOf(vs[top])
	case 21:
		a, _ := vs[top-2].(*php.Array)
		a.Append(vs[top])
		token = a
	}

	return token, false, nil
}

// isset reports isset($a[$key]).
func isset(a *php.Array, key string) bool {
	v, ok := a.Get(key)

	return ok && v != nil
}

func (p *parser) duplicateKeyError(key string, yylineno int) error {
	return &DuplicateKeyError{ParsingError{
		Message: "Parse error on line " + itoa(yylineno+1) + ":\n" + p.lexer.showPosition() + "\nDuplicate key: " + key,
		Details: Details{Kind: DuplicateKeyDetails, Line: yylineno + 1, Key: key},
	}}
}

// addArrayMember is production 17 with PARSE_TO_ASSOC.
func (p *parser) addArrayMember(a *php.Array, m member, yylineno int) error {
	key := m.key
	switch {
	case !isset(a, key):
		a.Set(key, m.value)
	case p.flags&DetectKeyConflicts != 0:
		return p.duplicateKeyError(key, yylineno)
	case p.flags&AllowDuplicateKeys != 0:
		for n := 1; ; n++ {
			if duplicateKey := key + "." + itoa(n); !isset(a, duplicateKey) {
				a.Set(duplicateKey, m.value)

				break
			}
		}
	case p.flags&AllowDuplicateKeysToArray != 0:
		existing, _ := a.Get(key)
		var list *php.Array
		if dup, ok := existing.(*php.Array); ok {
			if v, ok := dup.Get("__duplicates__"); ok {
				list, _ = v.(*php.Array)
			}
		}
		if list == nil {
			list = php.ListOf(existing)
			a.Set(key, php.ArrayOf("__duplicates__", list))
		}
		list.Append(m.value)
	default:
		a.Set(key, m.value)
	}

	return nil
}

// issetProperty reports isset($o->$name).
func issetProperty(o *php.Object, name string) bool {
	v, ok := o.Get(name)

	return ok && v != nil
}

// setProperty is $o->$name = $value, which PHP refuses for names starting
// with a NUL byte.
func setProperty(o *php.Object, name string, value any) error {
	if strings.HasPrefix(name, "\x00") {
		return &PHPError{Message: `Cannot access property starting with "\0"`}
	}
	o.Set(name, value)

	return nil
}

// addObjectMember is production 17 without PARSE_TO_ASSOC.
func (p *parser) addObjectMember(o *php.Object, m member, yylineno int) error {
	key := m.key
	switch {
	case !issetProperty(o, key):
		return setProperty(o, key, m.value)
	case p.flags&DetectKeyConflicts != 0:
		return p.duplicateKeyError(key, yylineno)
	case p.flags&AllowDuplicateKeys != 0:
		for n := 1; ; n++ {
			if duplicateKey := key + "." + itoa(n); !issetProperty(o, duplicateKey) {
				return setProperty(o, duplicateKey, m.value)
			}
		}
	case p.flags&AllowDuplicateKeysToArray != 0:
		existing, _ := o.Get(key)
		dup, ok := existing.(*php.Object)
		if !ok || !issetProperty(dup, "__duplicates__") {
			dup = php.NewObject()
			dup.Set("__duplicates__", php.ListOf(existing))
			o.Set(key, dup)
		}
		list, _ := dup.Get("__duplicates__")
		switch l := list.(type) {
		case *php.Array:
			l.Append(m.value)
		case bool:
			if l {
				return &PHPError{Message: "Cannot use a scalar value as an array"}
			}
			// Automatic conversion of false to array (deprecated).
			dup.Set("__duplicates__", php.ListOf(m.value))
		case string:
			return &PHPError{Message: "[] operator not supported for strings"}
		case *php.Object:
			return &PHPError{Message: "Cannot use object of type stdClass as array"}
		default:
			return &PHPError{Message: "Cannot use a scalar value as an array"}
		}

		return nil
	default:
		return setProperty(o, key, m.value)
	}
}

// interpolate is the preg_replace_callback of production 1 with
// JsonParser::stringInterpolation: it resolves the escape sequences of a
// string token.
func interpolate(s string) string {
	i := strings.IndexByte(s, '\\')
	if i < 0 {
		return s
	}
	b := make([]byte, 0, len(s))
	b = append(b, s[:i]...)
	for ; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b = append(b, c)

			continue
		}
		switch s[i+1] {
		case '\\':
			b = append(b, '\\')
		case '"':
			b = append(b, '"')
		case 'b':
			b = append(b, 8)
		case 'f':
			b = append(b, 12)
		case 'n':
			b = append(b, '\n')
		case 'r':
			b = append(b, '\r')
		case 't':
			b = append(b, '\t')
		case '/':
			b = append(b, '/')
		case 'u':
			if i+6 > len(s) || !isHex4(s[i+2:i+6]) {
				b = append(b, c)

				continue
			}
			b = appendEntity(b, s[i+2:i+6])
			i += 4
		default:
			b = append(b, c)

			continue
		}
		i++
	}

	return string(b)
}

func isHex4(s string) bool {
	for i := range 4 {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}

	return true
}

// appendEntity appends html_entity_decode('&#x'.ltrim($hex, '0').';',
// ENT_QUOTES, 'UTF-8'): the character when HTML 4.01 allows that code point,
// else the entity text itself.
func appendEntity(b []byte, hex string) []byte {
	trimmed := strings.TrimLeft(hex, "0")
	var cp rune
	for i := range len(hex) {
		cp = cp<<4 | hexDigit(hex[i])
	}
	if trimmed == "" || !html401Allowed(cp) {
		b = append(b, "&#x"...)
		b = append(b, trimmed...)

		return append(b, ';')
	}

	return utf8.AppendRune(b, cp)
}

func hexDigit(c byte) rune {
	switch {
	case c <= '9':
		return rune(c - '0')
	case c >= 'a':
		return rune(c-'a') + 10
	default:
		return rune(c-'A') + 10
	}
}

// html401Allowed ports php-src's unicode_cp_is_allowed for
// ENT_HTML_DOC_HTML401.
func html401Allowed(cp rune) bool {
	return (cp >= 0x20 && cp <= 0x7E) ||
		cp == 0x0A || cp == 0x09 || cp == 0x0D ||
		(cp >= 0xA0 && cp <= 0xD7FF) ||
		(cp >= 0xE000 && cp <= 0x10FFFF)
}

func itoa(i int) string { return strconv.Itoa(i) }
