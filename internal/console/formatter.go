// Ports src/Formatter/OutputFormatter.php, OutputFormatterInterface.php,
// WrappableOutputFormatterInterface.php and NullOutputFormatter.php
// (symfony/console).

package console

import (
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/maestro/internal/php"
)

// Formatter is OutputFormatterInterface.
type Formatter interface {
	SetDecorated(decorated bool)
	IsDecorated() bool
	SetStyle(name string, style Style)
	HasStyle(name string) bool
	// Style returns the named style, or an InvalidArgument error.
	Style(name string) (Style, error)
	// Format renders tags. A badly nested closing tag or an invalid inline
	// colour panics with a *Error, as the PHP formatter throws.
	Format(message string) string
}

// WrappableFormatter is WrappableOutputFormatterInterface.
type WrappableFormatter interface {
	Formatter
	FormatAndWrap(message string, width int) string
}

// OutputFormatter is the standard tag formatter.
type OutputFormatter struct {
	decorated  bool
	styles     map[string]Style
	styleStack *OutputFormatterStyleStack
}

// NamedStyle pairs a style with the tag name it is registered under.
type NamedStyle struct {
	Name  string
	Style Style
}

// NewOutputFormatter registers the default error/info/comment/question styles
// followed by styles.
func NewOutputFormatter(decorated bool, styles ...NamedStyle) *OutputFormatter {
	f := &OutputFormatter{decorated: decorated, styles: make(map[string]Style, 4+len(styles))}
	f.SetStyle("error", MustStyle("white", "red"))
	f.SetStyle("info", MustStyle("green", ""))
	f.SetStyle("comment", MustStyle("yellow", ""))
	f.SetStyle("question", MustStyle("black", "cyan"))
	for _, s := range styles {
		f.SetStyle(s.Name, s.Style)
	}
	f.styleStack = NewOutputFormatterStyleStack(nil)

	return f
}

// Clone deep-copies the formatter like PHP's __clone.
func (f *OutputFormatter) Clone() *OutputFormatter {
	c := &OutputFormatter{decorated: f.decorated, styles: make(map[string]Style, len(f.styles))}
	for k, v := range f.styles {
		if s, ok := v.(*OutputFormatterStyle); ok {
			c.styles[k] = s.clone()
		} else {
			c.styles[k] = v
		}
	}
	st := *f.styleStack
	st.styles = append([]Style(nil), f.styleStack.styles...)
	c.styleStack = &st

	return c
}

// Escape escapes "<" and ">" so they are not read as tags.
func Escape(text string) string {
	// preg_replace('/([^\\\\]|^)([<>])/', '$1\\\\$2', $text), scanned
	// left to right with the first alternative preferred.
	var b []byte
	n := len(text)
	last := 0
	for i := 0; i < n; {
		if text[i] != '\\' && i+1 < n && (text[i+1] == '<' || text[i+1] == '>') {
			b = append(b, text[last:i+1]...)
			b = append(b, '\\', text[i+1])
			last = i + 2
			i += 2

			continue
		}
		if i == 0 && (text[0] == '<' || text[0] == '>') {
			b = append(b, '\\', text[0])
			last = 1
		}
		i++
	}
	if b == nil {
		return EscapeTrailingBackslash(text)
	}

	return EscapeTrailingBackslash(string(append(b, text[last:]...)))
}

// EscapeTrailingBackslash escapes trailing "\" so it cannot escape a
// following closing tag.
func EscapeTrailingBackslash(text string) string {
	if strings.HasSuffix(text, `\`) {
		l := len(text)
		text = strings.TrimRight(text, `\`)
		text = strings.ReplaceAll(text, "\x00", "")
		text += strings.Repeat("\x00", l-len(text))
	}

	return text
}

// SetDecorated implements Formatter.
func (f *OutputFormatter) SetDecorated(decorated bool) { f.decorated = decorated }

// IsDecorated implements Formatter.
func (f *OutputFormatter) IsDecorated() bool { return f.decorated }

// SetStyle implements Formatter.
func (f *OutputFormatter) SetStyle(name string, style Style) {
	f.styles[php.Strtolower(name)] = style
}

// HasStyle implements Formatter.
func (f *OutputFormatter) HasStyle(name string) bool {
	_, ok := f.styles[php.Strtolower(name)]

	return ok
}

// Style implements Formatter.
func (f *OutputFormatter) Style(name string) (Style, error) {
	s, ok := f.styles[php.Strtolower(name)]
	if !ok {
		return nil, newError(KindInvalidArgument, "OutputFormatter.php", 126, `Undefined style: "%s".`, name)
	}

	return s, nil
}

// StyleStack returns the formatter's style stack.
func (f *OutputFormatter) StyleStack() *OutputFormatterStyleStack { return f.styleStack }

// Format implements Formatter.
func (f *OutputFormatter) Format(message string) string {
	return f.FormatAndWrap(message, 0)
}

// matchTag matches #<(([a-z](?:[^\\<>]*+|\\.)*)|/([a-z][^<>]*+)?)># (case
// insensitive) at message[pos] == '<'. It returns the end offset (exclusive),
// whether the tag is an opening one and the tag name.
func matchTag(message string, pos int) (end int, open bool, tag string, ok bool) {
	n := len(message)
	i := pos + 1
	if i >= n {
		return 0, false, "", false
	}
	if message[i] == '/' {
		i++
		start := i
		if i < n && isASCIIAlpha(message[i]) {
			i++
			for i < n && message[i] != '<' && message[i] != '>' {
				i++
			}
		}
		if i < n && message[i] == '>' {
			return i + 1, false, message[start:i], true
		}

		return 0, false, "", false
	}
	if !isASCIIAlpha(message[i]) {
		return 0, false, "", false
	}
	start := i
	i++
	for i < n {
		c := message[i]
		if c == '\\' {
			// "\\." — any character except a newline.
			if i+1 < n && message[i+1] != '\n' {
				i += 2

				continue
			}

			return 0, false, "", false
		}
		if c == '<' || c == '>' {
			break
		}
		i++
	}
	if i < n && message[i] == '>' {
		return i + 1, true, message[start:i], true
	}

	return 0, false, "", false
}

func isASCIIAlpha(c byte) bool { return (c|0x20) >= 'a' && (c|0x20) <= 'z' }

// FormatAndWrap implements WrappableFormatter.
func (f *OutputFormatter) FormatAndWrap(message string, width int) string {
	if message == "" {
		return ""
	}

	offset := 0
	var out strings.Builder
	out.Grow(len(message) + 16)
	currentLineLength := 0

	for pos := 0; pos < len(message); {
		idx := strings.IndexByte(message[pos:], '<')
		if idx < 0 {
			break
		}
		pos += idx
		end, open, tag, ok := matchTag(message, pos)
		if !ok {
			pos++

			continue
		}
		text := message[pos:end]

		if pos != 0 && message[pos-1] == '\\' {
			pos = end

			continue
		}

		// add the text up to the next tag
		f.applyCurrentStyle(&out, message[offset:pos], width, &currentLineLength)
		offset = end
		pos = end

		if !open && tag == "" {
			// </>
			_, _ = f.styleStack.Pop(nil)
		} else if style := f.createStyleFromString(tag); style == nil {
			f.applyCurrentStyle(&out, text, width, &currentLineLength)
		} else if open {
			f.styleStack.Push(style)
		} else if _, err := f.styleStack.Pop(style); err != nil {
			panic(err)
		}
	}

	f.applyCurrentStyle(&out, message[offset:], width, &currentLineLength)

	return unescapeFormatted(out.String())
}

// unescapeFormatted is strtr($output, ["\0" => '\\', '\\<' => '<', '\\>' => '>']).
func unescapeFormatted(s string) string {
	if strings.IndexByte(s, 0) < 0 && !strings.Contains(s, `\<`) && !strings.Contains(s, `\>`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == 0:
			b.WriteByte('\\')
		case c == '\\' && i+1 < len(s) && (s[i+1] == '<' || s[i+1] == '>'):
			b.WriteByte(s[i+1])
			i++
		default:
			b.WriteByte(c)
		}
	}

	return b.String()
}

var inlineStyleRe = php.MustCompile(`/([^=]+)=([^;]+)(;|$)/`)

func (f *OutputFormatter) createStyleFromString(s string) Style {
	if style, ok := f.styles[s]; ok {
		return style
	}

	// !preg_match_all(): no match and failure (false) both return null.
	matches, err := inlineStyleRe.MatchAll(s)
	if err != nil || len(matches) == 0 {
		return nil
	}

	style := MustStyle("", "")
	for _, match := range matches {
		m := []string{match.Get(0), match.Get(1), match.Get(2)}
		key := php.Strtolower(m[1])
		switch key {
		case "fg":
			if err := style.SetForeground(php.Strtolower(m[2])); err != nil {
				panic(err)
			}
		case "bg":
			if err := style.SetBackground(php.Strtolower(m[2])); err != nil {
				panic(err)
			}
		case "href":
			style.SetHref(unescapeHref(m[2]))
		case "options":
			for opt := range strings.FieldsFuncSeq(php.Strtolower(m[2]), func(r rune) bool { return r == ',' || r == ';' }) {
				if err := style.SetOption(opt); err != nil {
					panic(err)
				}
			}
		default:
			return nil
		}
	}

	return style
}

// unescapeHref is preg_replace('{\\\\([<>])}', '$1', $url).
func unescapeHref(s string) string {
	if !strings.Contains(s, `\<`) && !strings.Contains(s, `\>`) {
		return s
	}

	return strings.NewReplacer(`\<`, "<", `\>`, ">").Replace(s)
}

func (f *OutputFormatter) applyCurrentStyle(out *strings.Builder, text string, width int, currentLineLength *int) {
	if text == "" {
		return
	}

	if width == 0 {
		if f.decorated {
			out.WriteString(f.styleStack.Current().Apply(text))
		} else {
			out.WriteString(text)
		}

		return
	}

	current := out.Len()
	var lastCurrent byte
	if current > 0 {
		s := out.String()
		lastCurrent = s[len(s)-1]
	}

	if *currentLineLength == 0 && current > 0 {
		text = strings.TrimLeft(text, " \t\n\r\x00\x0B")
	}

	prefix := ""
	if *currentLineLength > 0 {
		i := width - *currentLineLength
		prefix = php.SubstrLen(text, 0, i) + "\n"
		text = php.Substr(text, i)
	}

	trailingNewline := strings.HasSuffix(text, "\n")
	text = prefix + addLineBreaks(text, width)
	text = strings.TrimRight(text, "\n")
	if trailingNewline {
		text += "\n"
	}

	if *currentLineLength == 0 && current > 0 && lastCurrent != '\n' {
		text = "\n" + text
	}

	lines := strings.Split(text, "\n")
	for _, line := range lines {
		*currentLineLength += len(line)
		if width <= *currentLineLength {
			*currentLineLength = 0
		}
	}

	if f.decorated {
		cur := f.styleStack.Current()
		for i, line := range lines {
			lines[i] = cur.Apply(line)
		}
	}

	out.WriteString(strings.Join(lines, "\n"))
}

// addLineBreaks ports the call to symfony/string's
// AbstractString::wordwrap($width, "\n", true) on the code points of text.
//
// toCodePointString() throws on invalid UTF-8; so does this (as a panic,
// like every exception escaping the formatter). A multibyte character split
// by the byte-based substr() of applyCurrentStyle triggers it too.
func addLineBreaks(text string, width int) string {
	if text == "" {
		return ""
	}
	if !utf8.ValidString(text) {
		panic(newError(KindStringInvalidArgument, "ByteString.php", 463, `Invalid "UTF-8" string.`))
	}

	// One mask byte per code point: '#' for the existing "\n" breaks, ' '
	// for spaces, '?' otherwise; starts holds each code point's offset.
	mask := make([]byte, 0, len(text))
	starts := make([]int, 0, len(text)+1)
	for i := 0; i < len(text); {
		starts = append(starts, i)
		switch text[i] {
		case '\n':
			mask = append(mask, '#')
		case ' ':
			mask = append(mask, ' ')
		default:
			mask = append(mask, '?')
		}
		_, size := utf8.DecodeRuneInString(text[i:])
		i += size
	}
	n := len(starts)
	starts = append(starts, len(text))

	wrapped := php.Wordwrap(string(mask), width, "#", true)

	var b strings.Builder
	b.Grow(len(text) + len(wrapped) - len(mask))
	j := 0 // next code point to copy
	i := -1
	for bpos := range len(wrapped) {
		if wrapped[bpos] != '#' {
			continue
		}
		// copy the code points up to this break
		if k := min(j+bpos-i-1, n); k > j {
			b.WriteString(text[starts[j]:starts[k]])
		}
		j += bpos - i - 1
		i = bpos
		// a break replaces the "\n" or space it was placed on
		if j < n && (text[starts[j]] == '\n' || text[starts[j]] == ' ') {
			j++
		}
		b.WriteByte('\n')
	}
	if j < n {
		b.WriteString(text[starts[j]:])
	}

	return b.String()
}

// NullOutputFormatter formats nothing.
type NullOutputFormatter struct {
	style *NullOutputFormatterStyle
}

// SetDecorated implements Formatter.
func (*NullOutputFormatter) SetDecorated(bool) {}

// IsDecorated implements Formatter.
func (*NullOutputFormatter) IsDecorated() bool { return false }

// SetStyle implements Formatter.
func (*NullOutputFormatter) SetStyle(string, Style) {}

// HasStyle implements Formatter.
func (*NullOutputFormatter) HasStyle(string) bool { return false }

// Style implements Formatter; it always returns the same
// NullOutputFormatterStyle.
func (f *NullOutputFormatter) Style(string) (Style, error) {
	if f.style == nil {
		f.style = &NullOutputFormatterStyle{}
	}

	return f.style, nil
}

// Format implements Formatter; PHP returns null, written as "".
func (*NullOutputFormatter) Format(string) string { return "" }
