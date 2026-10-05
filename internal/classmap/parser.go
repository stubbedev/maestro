// Ports src/PhpFileParser.php.

package classmap

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/stubbedev/maestro/internal/phperr"
)

// Parser is PhpFileParser together with the PHP runtime setting its result
// depends on.
type Parser struct {
	// ShortOpenTag is PHP's short_open_tag ini setting, which decides
	// whether "<?" opens PHP code for php_strip_whitespace(). PHP's
	// built-in default is On.
	ShortOpenTag bool
}

// DefaultParser uses PHP's built-in defaults.
var DefaultParser = Parser{ShortOpenTag: true}

// FindClasses is PhpFileParser::findClasses() with DefaultParser.
func FindClasses(path string) ([]string, error) {
	return DefaultParser.FindClasses(path)
}

// FindClasses extracts the classes, interfaces, traits and enums declared in
// the given file (PhpFileParser::findClasses()).
func (p Parser) FindClasses(path string) ([]string, error) {
	var b parseBuffers

	return p.findClasses(&b, path)
}

// parseBuffers are the scratch buffers of one parsing goroutine, reused from
// file to file.
type parseBuffers struct {
	src   []byte // file contents followed by stripPadding zero bytes
	strip []byte
	clean []byte
	lex   lexer
}

func (p Parser) findClasses(b *parseBuffers, path string) ([]string, error) {
	n, err := b.readFile(path)
	if err != nil {
		return nil, readError(path, err)
	}

	return p.classesIn(b, n, path)
}

// classesIn is findClasses() for the contents in b.src[:n].
func (p Parser) classesIn(b *parseBuffers, n int, path string) ([]string, error) {
	b.strip = b.lex.strip(b.strip[:0], b.src, n, p.ShortOpenTag)
	if len(b.strip) == 0 {
		if len(bytes.Trim(b.src[:n], " \t\n\r\x00\x0B")) == 0 {
			// The input file was really empty and thus contains no classes
			return nil, nil
		}

		// PHP appends error_get_last() here, which holds whatever error
		// happened last anywhere in the process; nothing is appended.
		return nil, newException(parseSite, classRuntime, `File at "`+path+`" could not be parsed as PHP, it may be binary or corrupted`)
	}

	// return early if there is no chance of matching anything in this file
	maxMatches := countTypeKeywords(b.strip)
	if maxMatches == 0 {
		return nil, nil
	}
	b.clean = cleanPhpFile(b.clean[:0], b.strip, maxMatches)

	return extractClasses(b.clean), nil
}

// readFile reads the file into b.src, followed by the zero padding the
// scanner expects, and returns the content length.
func (b *parseBuffers) readFile(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	size := 0
	if info, err := f.Stat(); err == nil && info.Mode().IsRegular() {
		size = int(info.Size())
	}
	if cap(b.src) < size+stripPadding+1 {
		b.src = make([]byte, 0, size+stripPadding+512)
	}
	src := b.src[:cap(b.src)]
	n := 0
	for {
		if len(src)-n < stripPadding+1 {
			src = append(src, make([]byte, len(src))...)
			src = src[:cap(src)]
		}
		m, err := f.Read(src[n : len(src)-stripPadding])
		n += m
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if errors.Is(err, syscall.EISDIR) {
				// A directory opens but reads nothing.
				return 0, nil
			}

			return 0, err
		}
	}
	clear(src[n : n+stripPadding])
	b.src = src

	return n, nil
}

// parseSite is where findClasses() throws when the file cannot be read.
var parseSite = phperr.At("PhpFileParser.php", 58)

// readError builds the exception findClasses() throws when
// php_strip_whitespace() cannot open the file, with the message
// error_get_last() returns at that point.
func readError(path string, err error) error {
	info, statErr := os.Stat(path)
	if statErr != nil {
		return newException(parseSite, classRuntime, `File at "`+path+`" does not exist, check your classmap definitions`+
			helpful("php_strip_whitespace", path, err))
	}
	// isReadable() tries file_get_contents() on regular files, which
	// replaces the error.
	fn := "php_strip_whitespace"
	if info.Mode().IsRegular() {
		fn = "file_get_contents"
	}

	return newException(parseSite, classRuntime, `File at "`+path+`" is not readable, check its permissions`+helpful(fn, path, err))
}

// helpful is the "may be helpful" suffix with PHP's warning for a failed
// fopen() in fn.
func helpful(fn, path string, err error) string {
	return "\nThe following message may be helpful:\n" + fn + "(" + path + "): Failed to open stream: " + strerror(err)
}

// strerror returns the C library's message for the errno behind err: Go's
// messages are glibc's with the first letter lowered.
func strerror(err error) string {
	if errno, ok := errors.AsType[syscall.Errno](err); ok {
		msg := errno.Error()
		if msg != "" && msg[0] >= 'a' && msg[0] <= 'z' {
			return string(msg[0]-'a'+'A') + msg[1:]
		}

		return msg
	}

	return err.Error()
}

// Character classes of the PCRE patterns (non-UTF mode, C locale tables).
var (
	isWordChar  [256]bool // \w: [a-zA-Z0-9_]
	isPcreSpace [256]bool // \s: [ \t\n\v\f\r]
	isNameStart [256]bool // [a-zA-Z_\x7f-\xff:]
	isNameChar  [256]bool // [a-zA-Z0-9_\x7f-\xff:\-]
	isNsStart   [256]bool // [a-zA-Z_\x7f-\xff]
	isNsChar    [256]bool // [a-zA-Z0-9_\x7f-\xff]
	typeStart   [256]bool // first letters of class|interface|trait|enum|namespace, any case
)

func init() {
	for c := range 256 {
		alpha := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		digit := c >= '0' && c <= '9'
		isWordChar[c] = alpha || digit || c == '_'
		isNsStart[c] = alpha || c == '_' || c >= 0x7f
		isNsChar[c] = isNsStart[c] || digit
		isNameStart[c] = isNsStart[c] || c == ':'
		isNameChar[c] = isNsChar[c] || c == ':' || c == '-'
	}
	for _, c := range []byte(" \t\n\v\f\r") {
		isPcreSpace[c] = true
	}
	for _, c := range []byte("citen") {
		typeStart[c] = true
		typeStart[c-'a'+'A'] = true
	}
}

// typeKeyword returns the type keyword that may start at c[i] (matching
// ASCII case-insensitively, as the patterns use the i flag), or "".
func typeKeyword(c []byte, i int) string {
	var kw string
	switch c[i] | 0x20 {
	case 'c':
		kw = "class"
	case 'i':
		kw = "interface"
	case 't':
		kw = "trait"
	case 'e':
		kw = "enum"
	case 'n':
		kw = "namespace"
	default:
		return ""
	}
	if len(c)-i < len(kw) || !equalFold(c[i:i+len(kw)], kw) {
		return ""
	}

	return kw
}

// countTypeKeywords counts the matches of '{\b(?:class|interface|trait|enum)\s}i',
// stopping at 2 as only "none" and "exactly one" matter to the caller.
func countTypeKeywords(c []byte) int {
	n := 0
	for i := 0; i < len(c); i++ {
		if !typeStart[c[i]] || i > 0 && isWordChar[c[i-1]] {
			continue
		}
		kw := typeKeyword(c, i)
		if kw == "" || kw == "namespace" {
			continue
		}
		end := i + len(kw)
		if end < len(c) && isPcreSpace[c[end]] {
			n++
			if n == 2 {
				return n
			}
			i = end
		}
	}

	return n
}

// extractClasses runs findClasses()' main pattern over the cleaned contents
// and turns its matches into class names:
//
//	(?:
//	     \b(?<![\\$:>])(?P<type>class|interface|trait|enum) \s++ (?P<name>[a-zA-Z_\x7f-\xff:][a-zA-Z0-9_\x7f-\xff:\-]*+)
//	   | \b(?<![\\$:>])(?P<ns>namespace) (?P<nsname>\s++[a-zA-Z_\x7f-\xff][a-zA-Z0-9_\x7f-\xff]*+(?:\s*+\\\s*+[a-zA-Z_\x7f-\xff][a-zA-Z0-9_\x7f-\xff]*+)*+)? \s*+ [\{;]
//	)
func extractClasses(c []byte) []string {
	var classes []string
	namespace := ""
	for i := 0; i < len(c); i++ {
		if !typeStart[c[i]] {
			continue
		}
		if i > 0 {
			if prev := c[i-1]; isWordChar[prev] || prev == '\\' || prev == '$' || prev == ':' || prev == '>' {
				continue
			}
		}
		kw := typeKeyword(c, i)
		if kw == "" {
			continue
		}
		if kw == "namespace" {
			end, nsname, ok := matchNamespace(c, i+len(kw))
			if !ok {
				continue
			}
			namespace = namespacePrefix(nsname)
			i = end - 1

			continue
		}

		j := i + len(kw)
		if j >= len(c) || !isPcreSpace[c[j]] {
			continue
		}
		for j < len(c) && isPcreSpace[c[j]] {
			j++
		}
		if j >= len(c) || !isNameStart[c[j]] {
			continue
		}
		start := j
		for j++; j < len(c) && isNameChar[c[j]]; j++ {
		}
		i = j - 1
		name := c[start:j]
		// skip anon classes extending/implementing
		if string(name) == "extends" || string(name) == "implements" {
			continue
		}
		if name[0] == ':' {
			// This is an XHP class, https://github.com/facebook/xhp
			name = append([]byte("xhp"), xhpReplacer.Replace(string(name))[1:]...)
		} else if kw == "enum" {
			// enum Foo: int / enum Foo:int: the pattern captures the colon
			// (and type), which isn't part of the class name.
			if colon := bytes.LastIndexByte(name, ':'); colon >= 0 {
				name = name[:colon]
			}
		}
		// ltrim($namespace.$name, '\\'): only the global namespace "\\"
		// starts with a backslash, names never do.
		if namespace == `\` {
			namespace = ""
		}
		classes = append(classes, concat(namespace, name))
	}

	return classes
}

// concat returns a+b as a new string with a single allocation.
func concat(a string, b []byte) string {
	var sb strings.Builder
	sb.Grow(len(a) + len(b))
	sb.WriteString(a)
	sb.Write(b)

	return sb.String()
}

// xhpReplacer is str_replace(['-', ':'], ['_', '__'], ...); the
// replacements cannot feed each other, so replacing at once is equivalent.
var xhpReplacer = strings.NewReplacer("-", "_", ":", "__")

// matchNamespace matches the namespace alternative after the keyword (at i)
// and returns the end of the match and the nsname group ("" when it did not
// participate).
func matchNamespace(c []byte, i int) (int, []byte, bool) {
	// (?P<nsname>...)? is greedy: try with the group first.
	if groupEnd := matchNsName(c, i); groupEnd > 0 {
		if end := matchNsTerminator(c, groupEnd); end > 0 {
			return end, c[i:groupEnd], true
		}
	}
	if end := matchNsTerminator(c, i); end > 0 {
		return end, nil, true
	}

	return 0, nil, false
}

// matchNsName matches \s++label(?:\s*+\\\s*+label)*+ at i and returns its
// end, or 0.
func matchNsName(c []byte, i int) int {
	if i >= len(c) || !isPcreSpace[c[i]] {
		return 0
	}
	for i < len(c) && isPcreSpace[c[i]] {
		i++
	}
	end := nsLabelEnd(c, i)
	if end == 0 {
		return 0
	}
	for {
		j := skipPcreSpace(c, end)
		if j >= len(c) || c[j] != '\\' {
			return end
		}
		next := nsLabelEnd(c, skipPcreSpace(c, j+1))
		if next == 0 {
			return end
		}
		end = next
	}
}

// matchNsTerminator matches \s*+[\{;] at i and returns its end, or 0.
func matchNsTerminator(c []byte, i int) int {
	i = skipPcreSpace(c, i)
	if i < len(c) && (c[i] == '{' || c[i] == ';') {
		return i + 1
	}

	return 0
}

// nsLabelEnd matches [a-zA-Z_\x7f-\xff][a-zA-Z0-9_\x7f-\xff]*+ at i and
// returns its end, or 0.
func nsLabelEnd(c []byte, i int) int {
	if i >= len(c) || !isNsStart[c[i]] {
		return 0
	}
	for i++; i < len(c) && isNsChar[c[i]]; i++ {
	}

	return i
}

func skipPcreSpace(c []byte, i int) int {
	for i < len(c) && isPcreSpace[c[i]] {
		i++
	}

	return i
}

// namespacePrefix is str_replace([' ', "\t", "\r", "\n"], ”, $nsname).'\\'.
func namespacePrefix(nsname []byte) string {
	var sb strings.Builder
	sb.Grow(len(nsname) + 1)
	for _, ch := range nsname {
		if ch != ' ' && ch != '\t' && ch != '\r' && ch != '\n' {
			sb.WriteByte(ch)
		}
	}
	sb.WriteByte('\\')

	return sb.String()
}
