// Ports src/Formatter/OutputFormatterStyle.php, OutputFormatterStyleInterface.php
// and OutputFormatterStyleStack.php (symfony/console).

package console

import (
	"os"
	"slices"

	"github.com/stubbedev/maestro/internal/php"
)

// Style is OutputFormatterStyleInterface.
type Style interface {
	SetForeground(color string) error
	SetBackground(color string) error
	SetOption(option string) error
	UnsetOption(option string) error
	SetOptions(options []string) error
	Apply(text string) string
}

// OutputFormatterStyle is the default Style implementation.
type OutputFormatterStyle struct {
	color      *Color
	foreground string
	background string
	options    []string
	href       string
	hasHref    bool
	// handlesHrefGracefully is computed lazily from the environment.
	hrefChecked, handlesHrefGracefully bool
}

// NewOutputFormatterStyle mirrors new OutputFormatterStyle($fg, $bg, $options);
// an empty colour means none.
func NewOutputFormatterStyle(foreground, background string, options ...string) (*OutputFormatterStyle, error) {
	s := &OutputFormatterStyle{foreground: foreground, background: background, options: options}
	c, err := NewColor(foreground, background, options)
	if err != nil {
		return nil, err
	}
	s.color = c

	return s, nil
}

// MustStyle is NewOutputFormatterStyle for static, known-valid styles.
func MustStyle(foreground, background string, options ...string) *OutputFormatterStyle {
	s, err := NewOutputFormatterStyle(foreground, background, options...)
	if err != nil {
		panic(err)
	}

	return s
}

func (s *OutputFormatterStyle) rebuild() error {
	c, err := NewColor(s.foreground, s.background, s.options)
	if err != nil {
		return err
	}
	s.color = c

	return nil
}

// SetForeground implements Style. On error the colour is left as before, but
// like PHP the stored name is already updated.
func (s *OutputFormatterStyle) SetForeground(color string) error {
	s.foreground = color

	return s.rebuild()
}

// SetBackground implements Style.
func (s *OutputFormatterStyle) SetBackground(color string) error {
	s.background = color

	return s.rebuild()
}

// SetHref sets a terminal hyperlink target.
func (s *OutputFormatterStyle) SetHref(url string) {
	s.href = url
	s.hasHref = true
}

// SetOption implements Style.
func (s *OutputFormatterStyle) SetOption(option string) error {
	s.options = append(s.options, option)

	return s.rebuild()
}

// UnsetOption implements Style; it removes the first occurrence only.
func (s *OutputFormatterStyle) UnsetOption(option string) error {
	for i, o := range s.options {
		if o == option {
			s.options = append(s.options[:i:i], s.options[i+1:]...)

			break
		}
	}

	return s.rebuild()
}

// SetOptions implements Style.
func (s *OutputFormatterStyle) SetOptions(options []string) error {
	s.options = append([]string(nil), options...)

	return s.rebuild()
}

// Apply implements Style.
func (s *OutputFormatterStyle) Apply(text string) string {
	if !s.hrefChecked {
		s.hrefChecked = true
		konsole := os.Getenv("KONSOLE_VERSION")
		_, idea := os.LookupEnv("IDEA_INITIAL_DIRECTORY")
		s.handlesHrefGracefully = os.Getenv("TERMINAL_EMULATOR") != "JetBrains-JediTerm" &&
			(!php.Truthy(konsole) || phpIntval(konsole) > 201100) && !idea
	}

	if s.hasHref && s.handlesHrefGracefully {
		text = "\033]8;;" + s.href + "\033\\" + text + "\033]8;;\033\\"
	}

	return s.color.Apply(text)
}

func (s *OutputFormatterStyle) clone() *OutputFormatterStyle {
	c := *s
	c.options = append([]string(nil), s.options...)

	return &c
}

// NullOutputFormatterStyle is a style that changes nothing.
type NullOutputFormatterStyle struct{}

// SetForeground implements Style.
func (*NullOutputFormatterStyle) SetForeground(string) error { return nil }

// SetBackground implements Style.
func (*NullOutputFormatterStyle) SetBackground(string) error { return nil }

// SetOption implements Style.
func (*NullOutputFormatterStyle) SetOption(string) error { return nil }

// UnsetOption implements Style.
func (*NullOutputFormatterStyle) UnsetOption(string) error { return nil }

// SetOptions implements Style.
func (*NullOutputFormatterStyle) SetOptions([]string) error { return nil }

// Apply implements Style.
func (*NullOutputFormatterStyle) Apply(text string) string { return text }

// OutputFormatterStyleStack tracks the styles opened by nested tags.
type OutputFormatterStyleStack struct {
	styles     []Style
	emptyStyle Style
}

// NewOutputFormatterStyleStack uses an empty OutputFormatterStyle when
// emptyStyle is nil.
func NewOutputFormatterStyleStack(emptyStyle Style) *OutputFormatterStyleStack {
	if emptyStyle == nil {
		emptyStyle = MustStyle("", "")
	}

	return &OutputFormatterStyleStack{emptyStyle: emptyStyle}
}

// Reset empties the stack.
func (s *OutputFormatterStyleStack) Reset() { s.styles = s.styles[:0] }

// Push adds a style on top.
func (s *OutputFormatterStyleStack) Push(style Style) { s.styles = append(s.styles, style) }

// Pop removes the top style, or with a style, everything from the last
// stacked style rendering identically.
func (s *OutputFormatterStyleStack) Pop(style Style) (Style, error) {
	if len(s.styles) == 0 {
		return s.emptyStyle, nil
	}

	if style == nil {
		top := s.styles[len(s.styles)-1]
		s.styles = s.styles[:len(s.styles)-1]

		return top, nil
	}

	want := style.Apply("")
	for i, stacked := range slices.Backward(s.styles) {
		if stacked.Apply("") == want {
			s.styles = s.styles[:i]

			return stacked, nil
		}
	}

	return nil, newError(KindInvalidArgument, "Incorrectly nested style tag found.")
}

// Current returns the top style, or the empty style.
func (s *OutputFormatterStyleStack) Current() Style {
	if len(s.styles) == 0 {
		return s.emptyStyle
	}

	return s.styles[len(s.styles)-1]
}

// SetEmptyStyle replaces the style used when the stack is empty.
func (s *OutputFormatterStyleStack) SetEmptyStyle(style Style) { s.emptyStyle = style }

// EmptyStyle returns the style used when the stack is empty.
func (s *OutputFormatterStyleStack) EmptyStyle() Style { return s.emptyStyle }
