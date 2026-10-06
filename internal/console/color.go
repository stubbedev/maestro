// Ports src/Color.php (symfony/console).

package console

import (
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
)

var colorNames = []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white", "default"}

var colorCodes = map[string]int{
	"black": 0, "red": 1, "green": 2, "yellow": 3, "blue": 4,
	"magenta": 5, "cyan": 6, "white": 7, "default": 9,
}

var brightColorNames = []string{
	"gray", "bright-red", "bright-green", "bright-yellow",
	"bright-blue", "bright-magenta", "bright-cyan", "bright-white",
}

var brightColorCodes = map[string]int{
	"gray": 0, "bright-red": 1, "bright-green": 2, "bright-yellow": 3,
	"bright-blue": 4, "bright-magenta": 5, "bright-cyan": 6, "bright-white": 7,
}

type colorOption struct {
	name       string
	set, unset int
}

var availableOptions = [...]colorOption{
	{"bold", 1, 22},
	{"underscore", 4, 24},
	{"blink", 5, 25},
	{"reverse", 7, 27},
	{"conceal", 8, 28},
}

// Color renders ANSI set/unset sequences for a foreground, background and
// options combination.
type Color struct {
	// set and unset are the escape sequences, computed once: a Color is
	// immutable (styles build a new one on every change).
	set, unset string
}

// NewColor validates its arguments like the PHP constructor.
func NewColor(foreground, background string, options []string) (*Color, error) {
	fg, err := parseColor(foreground, false)
	if err != nil {
		return nil, err
	}
	bg, err := parseColor(background, true)
	if err != nil {
		return nil, err
	}

	// $this->options[$option] = ...: assigning an existing key keeps its
	// position, so options are deduplicated in first-seen order.
	var seen [len(availableOptions)]bool
	var opts [len(availableOptions)]colorOption
	n := 0
	for _, name := range options {
		i := slices.IndexFunc(availableOptions[:], func(o colorOption) bool { return o.name == name })
		if i < 0 {
			names := make([]string, len(availableOptions))
			for k, o := range availableOptions {
				names[k] = o.name
			}

			return nil, newError(KindInvalidArgument,
				`Invalid option specified: "%s". Expected one of (%s).`, name, strings.Join(names, ", "))
		}
		if !seen[i] {
			seen[i] = true
			opts[n] = availableOptions[i]
			n++
		}
	}

	return &Color{
		set:   sequence(fg, bg, opts[:n], false),
		unset: sequence(fg, bg, opts[:n], true),
	}, nil
}

// sequence builds Color::set() or Color::unset().
func sequence(fg, bg string, opts []colorOption, unset bool) string {
	if fg == "" && bg == "" && len(opts) == 0 {
		return ""
	}
	b := make([]byte, 0, 16)
	b = append(b, "\033["...)
	sep := func() {
		if len(b) > 2 {
			b = append(b, ';')
		}
	}
	if fg != "" {
		if unset {
			b = append(b, "39"...)
		} else {
			b = append(b, fg...)
		}
	}
	if bg != "" {
		sep()
		if unset {
			b = append(b, "49"...)
		} else {
			b = append(b, bg...)
		}
	}
	for _, o := range opts {
		sep()
		if unset {
			b = strconv.AppendInt(b, int64(o.unset), 10)
		} else {
			b = strconv.AppendInt(b, int64(o.set), 10)
		}
	}

	return string(append(b, 'm'))
}

// Apply wraps text in the set and unset sequences.
func (c *Color) Apply(text string) string {
	return c.set + text + c.unset
}

// Set returns the opening escape sequence.
func (c *Color) Set() string { return c.set }

// Unset returns the closing escape sequence.
func (c *Color) Unset() string { return c.unset }

func parseColor(color string, background bool) (string, error) {
	if color == "" {
		return "", nil
	}

	if color[0] == '#' {
		color = color[1:]
		if len(color) == 3 {
			color = string([]byte{color[0], color[0], color[1], color[1], color[2], color[2]})
		}
		if len(color) != 6 {
			return "", newError(KindInvalidArgument, `Invalid "%s" color.`, color)
		}

		prefix := "3"
		if background {
			prefix = "4"
		}

		return prefix + convertHexColorToAnsi(hexdec(color)), nil
	}

	if code, ok := colorCodes[color]; ok {
		if background {
			return "4" + strconv.Itoa(code), nil
		}

		return "3" + strconv.Itoa(code), nil
	}

	if code, ok := brightColorCodes[color]; ok {
		if background {
			return "10" + strconv.Itoa(code), nil
		}

		return "9" + strconv.Itoa(code), nil
	}

	all := make([]string, 0, len(colorNames)+len(brightColorNames))
	all = append(all, colorNames...)
	all = append(all, brightColorNames...)

	return "", newError(KindInvalidArgument,
		`Invalid "%s" color; expected one of (%s).`, color, strings.Join(all, ", "))
}

// hexdec ignores any non-hexadecimal characters, like PHP's hexdec().
func hexdec(s string) int {
	n := 0
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			n = n<<4 | int(c-'0')
		case c >= 'a' && c <= 'f':
			n = n<<4 | int(c-'a'+10)
		case c >= 'A' && c <= 'F':
			n = n<<4 | int(c-'A'+10)
		}
	}

	return n
}

func convertHexColorToAnsi(color int) string {
	r := (color >> 16) & 255
	g := (color >> 8) & 255
	b := color & 255

	if os.Getenv("COLORTERM") != "truecolor" {
		return strconv.Itoa(degradeHexColorToAnsi(r, g, b))
	}

	return "8;2;" + strconv.Itoa(r) + ";" + strconv.Itoa(g) + ";" + strconv.Itoa(b)
}

// degradeHexColorToAnsi ports the PHP method. Its saturation check compares
// an int with the float returned by round() using ===, which is never true,
// so it is omitted here.
func degradeHexColorToAnsi(r, g, b int) int {
	rr := int(math.Round(float64(r) / 255))
	gg := int(math.Round(float64(g) / 255))
	bb := int(math.Round(float64(b) / 255))

	return bb<<2 | gg<<1 | rr
}
