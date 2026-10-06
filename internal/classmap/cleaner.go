// Ports src/PhpFileCleaner.php.

package classmap

import "bytes"

// classTypes are the type keywords PhpFileParser::getExtraTypes() hands to
// PhpFileCleaner::setTypeConfig() on PHP >= 8.1, keyed by first character.
// Before 8.1 "enum" is not one (phpFileCleaner.enums).
var classTypes = [256]string{'c': "class", 'i': "interface", 't': "trait", 'e': "enum"}

// cleanerReject is PhpFileCleaner::$rejectChars: ?"'</ plus the type keys.
// Without enums "e" is not one, which only changes how PHP copies the
// text between reject characters, not the result.
var cleanerReject = [256]bool{'?': true, '"': true, '\'': true, '<': true, '/': true, 'c': true, 'i': true, 't': true, 'e': true}

// phpFileCleaner is PhpFileCleaner: it blanks out strings, heredocs and
// comments of php_strip_whitespace() output so that class keywords inside
// them are not found.
type phpFileCleaner struct {
	contents   []byte
	len        int
	maxMatches int
	index      int
	enums      bool // "enum" is a type keyword
}

// cleanPhpFile is PhpFileCleaner::clean() for the given contents and number
// of candidate type keywords. The result is appended to dst.
func cleanPhpFile(dst, contents []byte, maxMatches int, enums bool) []byte {
	p := phpFileCleaner{contents: contents, len: len(contents), maxMatches: maxMatches, enums: enums}

	return p.clean(dst)
}

func (p *phpFileCleaner) clean(clean []byte) []byte {
	c := p.contents
	for p.index < p.len {
		p.skipToPhp()
		clean = append(clean, '<', '?')

	inner:
		for p.index < p.len {
			char := c[p.index]
			switch {
			case char == '?' && p.peek('>'):
				clean = append(clean, '?', '>')
				p.index += 2

				break inner
			case char == '"' || char == '\'':
				p.skipString(char)
				clean = append(clean, "null"...)

				continue
			case char == '<' && p.peek('<'):
				if n, label := p.matchHeredocStart(); n > 0 {
					p.index += n
					p.skipHeredoc(label)
					clean = append(clean, "null"...)

					continue
				}
			case char == '/' && p.peek('/'):
				p.skipToNewline()

				continue
			case char == '/' && p.peek('*'):
				p.skipComment()

				continue
			}

			if p.maxMatches == 1 && classTypes[char] != "" && (char != 'e' || p.enums) {
				typ := classTypes[char]
				if bytes.HasPrefix(c[p.index:], []byte(typ)) {
					if end := p.matchType(typ); end > 0 {
						return append(clean, c[p.index-1:end]...)
					}
				}
			}

			p.index++
			skip := p.index
			for skip < p.len && !cleanerReject[c[skip]] {
				skip++
			}
			clean = append(clean, c[p.index-1:skip]...)
			p.index = skip
		}
	}

	return clean
}

// matchType matches '{.\b(?<![\$:>])TYPE\s++[a-zA-Z_\x7f-\xff:][a-zA-Z0-9_\x7f-\xff:\-]*+}Ais'
// at index-1 (contents[index:] is known to start with typ) and returns the
// end of the match, or 0.
func (p *phpFileCleaner) matchType(typ string) int {
	c := p.contents
	prev := c[p.index-1]
	if isWordChar[prev] || prev == '$' || prev == ':' || prev == '>' {
		return 0
	}
	i := p.index + len(typ)
	if i >= p.len || !isPcreSpace[c[i]] {
		return 0
	}
	for i < p.len && isPcreSpace[c[i]] {
		i++
	}
	if i >= p.len || !isNameStart[c[i]] {
		return 0
	}
	i++
	for i < p.len && isNameChar[c[i]] {
		i++
	}

	return i
}

// matchHeredocStart matches
// '{<<<[ \t]*+([\'"]?)([a-zA-Z_\x80-\xff][a-zA-Z0-9_\x80-\xff]*+)\1(?:\r\n|\n|\r)}A'
// at index and returns the match length and the label (group 2), or 0.
func (p *phpFileCleaner) matchHeredocStart() (int, []byte) {
	c := p.contents
	i := p.index + 2
	if i >= p.len || c[i] != '<' {
		return 0, nil
	}
	i++
	for i < p.len && (c[i] == ' ' || c[i] == '\t') {
		i++
	}
	var quote byte
	if i < p.len && (c[i] == '\'' || c[i] == '"') {
		quote = c[i]
		i++
	}
	if i >= p.len || !labelStart[c[i]] {
		return 0, nil
	}
	start := i
	for i < p.len && labelSucc[c[i]] {
		i++
	}
	label := c[start:i]
	if quote != 0 {
		if i >= p.len || c[i] != quote {
			return 0, nil
		}
		i++
	}
	switch {
	case i+1 < p.len && c[i] == '\r' && c[i+1] == '\n':
		i += 2
	case i < p.len && (c[i] == '\n' || c[i] == '\r'):
		i++
	default:
		return 0, nil
	}

	return i - p.index, label
}

func (p *phpFileCleaner) skipToPhp() {
	if i := bytes.Index(p.contents[p.index:], []byte("<?")); i >= 0 {
		p.index += i + 2
	} else {
		p.index = p.len
	}
}

func (p *phpFileCleaner) skipString(delimiter byte) {
	c := p.contents
	p.index++
	for p.index < p.len {
		for p.index < p.len && c[p.index] != '\\' && c[p.index] != delimiter {
			p.index++
		}
		if p.index >= p.len {
			break
		}
		if c[p.index] == '\\' && (p.peek('\\') || p.peek(delimiter)) {
			p.index += 2

			continue
		}
		if c[p.index] == delimiter {
			p.index++

			break
		}
		p.index++
	}
}

func (p *phpFileCleaner) skipComment() {
	p.index += 2
	for p.index < p.len {
		if i := bytes.IndexByte(p.contents[p.index:], '*'); i >= 0 {
			p.index += i
		} else {
			p.index = p.len
		}
		if p.peek('/') {
			p.index += 2

			break
		}
		p.index++
	}
}

func (p *phpFileCleaner) skipToNewline() {
	if i := bytes.IndexAny(p.contents[p.index:], "\r\n"); i >= 0 {
		p.index += i
	} else {
		p.index = p.len
	}
}

func (p *phpFileCleaner) skipHeredoc(delimiter []byte) {
	c := p.contents
	for p.index < p.len {
		// check if we find the delimiter after some spaces/tabs
		switch c[p.index] {
		case '\t', ' ':
			p.index++

			continue
		case delimiter[0]:
			end := p.index + len(delimiter)
			if bytes.HasPrefix(c[p.index:], delimiter) && (end >= p.len || !labelSucc[c[end]]) {
				p.index = end

				return
			}
		}

		// skip the rest of the line
		p.skipToNewline()

		// skip newlines
		for p.index < p.len && (c[p.index] == '\r' || c[p.index] == '\n') {
			p.index++
		}
	}
}

func (p *phpFileCleaner) peek(char byte) bool {
	return p.index+1 < p.len && p.contents[p.index+1] == char
}
