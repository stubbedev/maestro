// Ports FilesystemRepository::safelyLoadInstalledVersions
// (src/Composer/Repository/FilesystemRepository.php): reading back the
// installed.php write() dumps.

package repository

import (
	"math"
	"os"
	"strconv"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// installedPhpPattern is the grammar safelyLoadInstalledVersions accepts:
// a PHP file returning an array literal of numbers, booleans, null,
// strings and arrays.
var installedPhpPattern = php.MustCompile(`{(?(DEFINE)
   (?<number>  -? \s*+ \d++ (?:\.\d++)? )
   (?<boolean> true | false | null )
   (?<strings> (?&string) (?: \s*+ \. \s*+ (?&string))*+ )
   (?<string>  (?: " (?:[^"\\$]*+ | \\ ["\\0] )* " | ' (?:[^'\\]*+ | \\ ['\\] )* ' ) )
   (?<array>   array\( \s*+ (?: (?:(?&number)|(?&strings)) \s*+ => \s*+ (?: (?:__DIR__ \s*+ \. \s*+)? (?&strings) | (?&value) ) \s*+, \s*+ )*+  \s*+ \) )
   (?<value>   (?: (?&number) | (?&boolean) | (?&strings) | (?&array) ) )
)
^<\?php\s++return\s++(?&array)\s*+;$}ix`)

var installedPhpDir = php.MustCompile(`{=>\s*+__DIR__\s*+\.\s*+(['"])}`)

// SafelyLoadInstalledVersions ports
// FilesystemRepository::safelyLoadInstalledVersions: the data of the
// installed.php at path, as Composer hands it to
// InstalledVersions::reload, when the file only contains the code write()
// generates (so that evaluating it is safe). ok is false otherwise.
//
// It treats the PcreException Preg throws as "not loaded"; Factory lets
// that exception through, so callers porting Factory use
// SafelyLoadInstalledVersionsChecked.
func SafelyLoadInstalledVersions(path string) (data *php.Array, ok bool) {
	data, ok, _ = SafelyLoadInstalledVersionsChecked(path)

	return data, ok
}

// SafelyLoadInstalledVersionsChecked is SafelyLoadInstalledVersions with
// the PcreException (a *php.PcreError) Preg::isMatch/Preg::replace throw,
// e.g. when a large installed.php exhausts the backtrack limit.
func SafelyLoadInstalledVersionsChecked(path string) (data *php.Array, ok bool, err error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, false, nil
	}
	installedVersionsData := string(content)
	matched, err := installedPhpPattern.IsMatch(php.Trim(installedVersionsData))
	if err != nil || !matched {
		return nil, false, err
	}
	code, _, err := installedPhpDir.Replace(installedVersionsData, "=> "+php.VarExport(util.Dirname(path))+" . $1", -1)
	if err != nil {
		return nil, false, err
	}
	data, ok = evalInstalledPhp(code)

	return data, ok, nil
}

// evalInstalledPhp evaluates `?>` . code for code in the grammar of
// installedPhpPattern (once __DIR__ is replaced): <?php return <array>;
func evalInstalledPhp(code string) (*php.Array, bool) {
	p := &phpLiteralParser{s: code}
	p.skipSpace()
	if !p.consume("<?php") {
		return nil, false
	}
	p.skipSpace()
	if !p.consumeFold("return") {
		return nil, false
	}
	value, ok := p.value()
	if !ok {
		return nil, false
	}
	p.skipSpace()
	if !p.consume(";") {
		return nil, false
	}
	array, ok := value.(*php.Array)

	return array, ok
}

// phpLiteralParser reads the PHP literals of installed.php.
type phpLiteralParser struct {
	s   string
	pos int
}

func (p *phpLiteralParser) skipSpace() {
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			p.pos++
		default:
			return
		}
	}
}

func (p *phpLiteralParser) consume(token string) bool {
	if len(p.s)-p.pos >= len(token) && p.s[p.pos:p.pos+len(token)] == token {
		p.pos += len(token)

		return true
	}

	return false
}

// consumeFold consumes a keyword in any case (the pattern is /i).
func (p *phpLiteralParser) consumeFold(word string) bool {
	if len(p.s)-p.pos >= len(word) && php.Strcasecmp(p.s[p.pos:p.pos+len(word)], word) == 0 {
		p.pos += len(word)

		return true
	}

	return false
}

func (p *phpLiteralParser) peek() byte {
	if p.pos < len(p.s) {
		return p.s[p.pos]
	}

	return 0
}

// value reads a number, boolean, null, string concatenation or array.
func (p *phpLiteralParser) value() (any, bool) {
	p.skipSpace()
	switch c := p.peek(); {
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	case c == '\'' || c == '"':
		return p.strings()
	case p.consumeFold("array("):
		return p.array()
	case p.consumeFold("true"):
		return true, true
	case p.consumeFold("false"):
		return false, true
	case p.consumeFold("null"):
		return nil, true
	}

	return nil, false
}

// number reads -? \s* \d+ (\.\d+)?: an int, or a float when it has a
// fraction or overflows.
func (p *phpLiteralParser) number() (any, bool) {
	start := p.pos
	negative := p.consume("-")
	p.skipSpace()
	digits := p.pos
	for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == digits {
		p.pos = start

		return nil, false
	}
	text := p.s[digits:p.pos]
	isFloat := false
	if p.peek() == '.' && p.pos+1 < len(p.s) && p.s[p.pos+1] >= '0' && p.s[p.pos+1] <= '9' {
		isFloat = true
		p.pos++
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
		text = p.s[digits:p.pos]
	}
	if negative {
		text = "-" + text
	}
	if !isFloat {
		if i, err := strconv.ParseInt(text, 10, 64); err == nil {
			return i, true
		}
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil && !math.IsInf(f, 0) {
		return nil, false
	}

	return f, true
}

// strings reads string (\s* . \s* string)*, concatenated.
func (p *phpLiteralParser) strings() (any, bool) {
	s, ok := p.string()
	if !ok {
		return nil, false
	}
	for {
		save := p.pos
		p.skipSpace()
		if !p.consume(".") {
			p.pos = save

			return s, true
		}
		p.skipSpace()
		next, ok := p.string()
		if !ok {
			return nil, false
		}
		s += next
	}
}

// string reads one single or double quoted string with the escapes the
// pattern allows (\' and \\, or \", \\ and octal \0).
func (p *phpLiteralParser) string() (string, bool) {
	quote := p.peek()
	if quote != '\'' && quote != '"' {
		return "", false
	}
	p.pos++
	var b []byte
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		p.pos++
		switch {
		case c == quote:
			return string(b), true
		case c != '\\' || p.pos >= len(p.s):
			b = append(b, c)
		case quote == '\'':
			if next := p.s[p.pos]; next == '\'' || next == '\\' {
				b = append(b, next)
				p.pos++
			} else {
				b = append(b, c)
			}
		default:
			switch next := p.s[p.pos]; {
			case next == '"' || next == '\\':
				b = append(b, next)
				p.pos++
			case next >= '0' && next <= '7':
				// \[0-7]{1,3}
				v := 0
				for n := 0; n < 3 && p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '7'; n++ {
					v = v*8 + int(p.s[p.pos]-'0')
					p.pos++
				}
				b = append(b, byte(v))
			default:
				b = append(b, c)
			}
		}
	}

	return "", false
}

// array reads the entries of array( ... ) after its opening parenthesis.
func (p *phpLiteralParser) array() (any, bool) {
	a := php.NewArray()
	for {
		p.skipSpace()
		if p.consume(")") {
			return a, true
		}
		var key any
		var ok bool
		if c := p.peek(); c == '\'' || c == '"' {
			key, ok = p.strings()
		} else {
			key, ok = p.number()
		}
		if !ok {
			return nil, false
		}
		p.skipSpace()
		if !p.consume("=>") {
			return nil, false
		}
		value, ok := p.value()
		if !ok {
			return nil, false
		}
		p.skipSpace()
		if !p.consume(",") {
			return nil, false
		}
		a.Set(key, value)
	}
}
