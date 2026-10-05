// Ports src/Seld/JsonLint/Lexer.php.

package jsonlint

import (
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
)

// Lexer symbols (Lexer::* constants).
const (
	symEOF            = 1
	symInvalid        = -1
	symSkipWhitespace = 0
	symError          = 2
	symBreakLine      = 3
	symComment        = 30
	symOpenComment    = 31
	symCloseComment   = 32
)

// lexRules are Lexer::$rules, tried in order at the current offset.
var lexRules = [...]*php.Regexp{
	php.MustCompile(`/\G\s*\n\r?/`),
	php.MustCompile(`/\G\s+/`),
	php.MustCompile(`/\G-?([0-9]|[1-9][0-9]+)(\.[0-9]+)?([eE][+-]?[0-9]+)?\b/`),
	php.MustCompile(`{\G"(?>\\["bfnrt/\\]|\\u[a-fA-F0-9]{4}|[^\0-\x1f\\"]++)*+"}`),
	php.MustCompile(`/\G\{/`),
	php.MustCompile(`/\G\}/`),
	php.MustCompile(`/\G\[/`),
	php.MustCompile(`/\G\]/`),
	php.MustCompile(`/\G,/`),
	php.MustCompile(`/\G:/`),
	php.MustCompile(`/\Gtrue\b/`),
	php.MustCompile(`/\Gfalse\b/`),
	php.MustCompile(`/\Gnull\b/`),
	php.MustCompile(`/\G$/`),
	php.MustCompile(`/\G\/\//`),
	php.MustCompile(`/\G\/\*/`),
	php.MustCompile(`/\G\*\//`),
	php.MustCompile(`/\G./`),
}

// ruleSymbols are the symbols Lexer::performAction returns per rule.
var ruleSymbols = [...]int{
	symBreakLine, symSkipWhitespace, 6, 4, 17, 18, 23, 24, 22, 21, 10, 11, 8, 14,
	symComment, symOpenComment, symCloseComment, symInvalid,
}

// Location is a yylloc array.
type Location struct {
	FirstLine, FirstColumn, LastLine, LastColumn int

	// initial marks the location setInput creates, whose keys PHP orders
	// first_line, first_column, last_line, last_column; those next()
	// creates are ordered first_line, last_line, first_column, last_column.
	initial bool
}

// lexer is Seld\JsonLint\Lexer.
type lexer struct {
	input  string
	done   bool
	offset int
	flags  int

	match    string
	yylineno int
	yyleng   int
	yytext   string
	yylloc   Location
}

func newLexer(input string, flags int) *lexer {
	return &lexer{
		input:  input,
		flags:  flags,
		yylloc: Location{FirstLine: 1, LastLine: 1, initial: true},
	}
}

// lex ports Lexer::lex.
func (l *lexer) lex() (int, error) {
	for {
		symbol, err := l.next()
		if err != nil {
			return 0, err
		}
		switch symbol {
		case symSkipWhitespace, symBreakLine:
		case symComment, symOpenComment:
			if l.flags&AllowComments == 0 {
				return 0, &ParsingError{Site: phperr.At("Lexer.php", 203), Message: "Lexical error on line " + itoa(l.yylineno+1) + ". Comments are not allowed.\n" + l.showPosition()}
			}
			until := symCloseComment
			if symbol == symComment {
				until = symBreakLine
			}
			if err := l.skipUntil(until); err != nil {
				return 0, err
			}
			if l.done {
				// last symbol '/\G$/' before EOF
				return 14, nil
			}
		case symCloseComment:
			return 0, &ParsingError{Site: phperr.At("Lexer.php", 203), Message: "Lexical error on line " + itoa(l.yylineno+1) + ". Unexpected token.\n" + l.showPosition()}
		default:
			return symbol, nil
		}
	}
}

// showPosition ports Lexer::showPosition.
func (l *lexer) showPosition() string {
	if l.yylineno == 0 && l.offset == 1 && l.match != "{" {
		return l.match + "...\n^"
	}

	pre := strings.ReplaceAll(l.pastInput(), "\n", "")

	return pre + strings.ReplaceAll(l.upcomingInput(), "\n", "") + "\n" + strings.Repeat("-", max(0, len(pre)-1)) + "^"
}

// pastInput ports Lexer::getPastInput.
func (l *lexer) pastInput() string {
	pastLength := l.offset - len(l.match)
	prefix := ""
	if pastLength > 20 {
		prefix = "..."
	}
	start := max(0, pastLength-20)

	return prefix + l.input[start:start+min(20, pastLength)]
}

// upcomingInput ports Lexer::getUpcomingInput.
func (l *lexer) upcomingInput() string {
	next := l.match
	if len(next) < 20 {
		next += l.input[l.offset:min(len(l.input), l.offset+20-len(next))]
	}
	if len(next) > 20 {
		return next[:20] + "..."
	}

	return next
}

// fullUpcomingInput ports Lexer::getFullUpcomingInput.
func (l *lexer) fullUpcomingInput() string {
	next := l.match
	if strings.HasPrefix(next, `"`) && strings.Count(next, `"`) == 1 {
		n := len(l.input)
		strEnd := n
		if n != l.offset {
			strEnd = min(indexFrom(l.input, `"`, l.offset+1, n), indexFrom(l.input, "\n", l.offset+1, n))
		}
		next += l.input[l.offset:strEnd]
	} else if len(next) < 20 {
		next += l.input[l.offset:min(len(l.input), l.offset+20-len(next))]
	}

	return next
}

// indexFrom is strpos($s, $needle, $from) ?: $def, for from <= len(s).
func indexFrom(s, needle string, from, def int) int {
	if i := strings.Index(s[from:], needle); i >= 0 {
		return from + i
	}

	return def
}

// skipUntil ports Lexer::skipUntil.
func (l *lexer) skipUntil(token int) error {
	symbol, err := l.next()
	for err == nil && symbol != token && !l.done {
		symbol, err = l.next()
	}

	return err
}

// next ports Lexer::next.
func (l *lexer) next() (int, error) {
	if l.done {
		return symEOF, nil
	}
	if l.offset == len(l.input) {
		l.done = true
	}

	l.yytext = ""
	l.match = ""

	for i, rule := range lexRules {
		m, err := rule.MatchAt(l.input, l.offset)
		if err != nil || m == nil {
			continue
		}
		matched := m.Get(0)
		lineCount := strings.Count(matched, "\n")
		l.yylineno += lineCount
		lastColumn := l.yylloc.LastColumn + len(matched)
		if lineCount > 0 {
			lastColumn = len(matched) - strings.LastIndexByte(matched, '\n') - 1
		}
		l.yylloc = Location{
			FirstLine:   l.yylloc.LastLine,
			LastLine:    l.yylineno + 1,
			FirstColumn: l.yylloc.LastColumn,
			LastColumn:  lastColumn,
		}
		l.yytext += matched
		l.match += matched
		l.yyleng = len(l.yytext)
		l.offset += len(matched)
		if i == 3 {
			l.yytext = l.yytext[1 : l.yyleng-1]
		}

		return ruleSymbols[i], nil
	}

	if l.offset == len(l.input) {
		return symEOF, nil
	}

	return 0, &ParsingError{Site: phperr.At("Lexer.php", 203), Message: "Lexical error on line " + itoa(l.yylineno+1) + ". Unrecognized text.\n" + l.showPosition()}
}
