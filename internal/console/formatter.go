// Ports src/Formatter/OutputFormatter.php, OutputFormatterInterface.php,
// WrappableOutputFormatterInterface.php and NullOutputFormatter.php
// (symfony/console).

package console

import (
	"regexp"
	"strings"
	"unicode/utf8"
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
	f.styles[strings.ToLower(name)] = style
}

// HasStyle implements Formatter.
func (f *OutputFormatter) HasStyle(name string) bool {
	_, ok := f.styles[strings.ToLower(name)]

	return ok
}

// Style implements Formatter.
func (f *OutputFormatter) Style(name string) (Style, error) {
	s, ok := f.styles[strings.ToLower(name)]
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

var inlineStyleRe = regexp.MustCompile(`([^=]+)=([^;]+)(;|$)`)

func (f *OutputFormatter) createStyleFromString(s string) Style {
	if style, ok := f.styles[s]; ok {
		return style
	}

	matches := inlineStyleRe.FindAllStringSubmatch(s, -1)
	if matches == nil {
		return nil
	}

	style := MustStyle("", "")
	for _, m := range matches {
		key := strings.ToLower(m[1])
		switch key {
		case "fg":
			if err := style.SetForeground(strings.ToLower(m[2])); err != nil {
				panic(err)
			}
		case "bg":
			if err := style.SetBackground(strings.ToLower(m[2])); err != nil {
				panic(err)
			}
		case "href":
			style.SetHref(unescapeHref(m[2]))
		case "options":
			for opt := range strings.FieldsFuncSeq(strings.ToLower(m[2]), func(r rune) bool { return r == ',' || r == ';' }) {
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
		prefix = phpSubstr(text, 0, i) + "\n"
		text = phpSubstrFrom(text, i)
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

// phpSubstr is substr($s, 0, $length) for a non-negative start.
func phpSubstr(s string, start, length int) string {
	if start > len(s) {
		return ""
	}
	s = s[start:]
	if length < 0 {
		if -length >= len(s) {
			return ""
		}

		return s[:len(s)+length]
	}
	if length < len(s) {
		return s[:length]
	}

	return s
}

// phpSubstrFrom is substr($s, $start) for a non-negative start.
func phpSubstrFrom(s string, start int) string {
	if start >= len(s) {
		return ""
	}

	return s[start:]
}

// addLineBreaks ports the call to symfony/string's
// AbstractString::wordwrap($width, "\n", true) on the code points of text.
func addLineBreaks(text string, width int) string {
	if text == "" {
		return ""
	}

	// chars: one entry per code point (invalid bytes count as one each),
	// with "\n" entries between the original lines; mask mirrors it.
	chars := make([]string, 0, len(text))
	mask := make([]byte, 0, len(text))
	for i := 0; i < len(text); {
		if text[i] == '\n' {
			chars = append(chars, "\n")
			mask = append(mask, '#')
			i++

			continue
		}
		_, size := utf8.DecodeRuneInString(text[i:])
		ch := text[i : i+size]
		chars = append(chars, ch)
		if ch == " " {
			mask = append(mask, ' ')
		} else {
			mask = append(mask, '?')
		}
		i += size
	}

	wrapped := phpWordwrap(mask, width, '#')

	var b strings.Builder
	b.Grow(len(text) + len(wrapped) - len(mask))
	j := 0
	i := -1
	for bpos := 0; bpos < len(wrapped); bpos++ {
		if wrapped[bpos] != '#' {
			continue
		}
		for i++; i < bpos; i++ {
			if j < len(chars) {
				b.WriteString(chars[j])
			}
			j++
		}
		if j < len(chars) && (chars[j] == "\n" || chars[j] == " ") {
			j++
		}
		b.WriteByte('\n')
	}
	for ; j < len(chars); j++ {
		b.WriteString(chars[j])
	}

	return b.String()
}

// phpWordwrap ports php-src's wordwrap($text, $width, $brk, true) for a
// single-byte break (the general "multiple character break or forced cut"
// path).
func phpWordwrap(text []byte, width int, brk byte) []byte {
	n := len(text)
	if n == 0 {
		return text
	}

	out := make([]byte, 0, n+n/max(width, 1)+1)
	laststart, lastspace := 0, 0
	current := 0
	for ; current < n; current++ {
		c := text[current]
		switch {
		case c == brk && current+1 < n:
			out = append(out, text[laststart:current+1]...)
			laststart = current + 1
			lastspace = current + 1
		case c == ' ':
			if current-laststart >= width {
				out = append(out, text[laststart:current]...)
				out = append(out, brk)
				laststart = current + 1
			}
			lastspace = current
		case current-laststart >= width && laststart >= lastspace:
			out = append(out, text[laststart:current]...)
			out = append(out, brk)
			laststart = current
			lastspace = current
		case current-laststart >= width && laststart < lastspace:
			out = append(out, text[laststart:lastspace]...)
			out = append(out, brk)
			lastspace++
			laststart = lastspace
		}
	}
	if laststart != current {
		out = append(out, text[laststart:current]...)
	}

	return out
}

// NullOutputFormatter formats nothing.
type NullOutputFormatter struct {
	style *OutputFormatterStyle
}

// SetDecorated implements Formatter.
func (*NullOutputFormatter) SetDecorated(bool) {}

// IsDecorated implements Formatter.
func (*NullOutputFormatter) IsDecorated() bool { return false }

// SetStyle implements Formatter.
func (*NullOutputFormatter) SetStyle(string, Style) {}

// HasStyle implements Formatter.
func (*NullOutputFormatter) HasStyle(string) bool { return false }

// Style implements Formatter; it always returns a NullOutputFormatterStyle
// stand-in (an empty style).
func (f *NullOutputFormatter) Style(string) (Style, error) {
	if f.style == nil {
		f.style = MustStyle("", "")
	}

	return f.style, nil
}

// Format implements Formatter; PHP returns null, written as "".
func (*NullOutputFormatter) Format(string) string { return "" }
