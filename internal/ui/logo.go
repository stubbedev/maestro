package ui

import (
	"fmt"
	"strings"
)

// wordmark is "MAESTRO" in ANSI Shadow block letters, for a console that
// shows Unicode.
const wordmark = `███╗   ███╗ █████╗ ███████╗███████╗████████╗██████╗  ██████╗
████╗ ████║██╔══██╗██╔════╝██╔════╝╚══██╔══╝██╔══██╗██╔═══██╗
██╔████╔██║███████║█████╗  ███████╗   ██║   ██████╔╝██║   ██║
██║╚██╔╝██║██╔══██║██╔══╝  ╚════██║   ██║   ██╔══██╗██║   ██║
██║ ╚═╝ ██║██║  ██║███████╗███████║   ██║   ██║  ██║╚██████╔╝
╚═╝     ╚═╝╚═╝  ╚═╝╚══════╝╚══════╝   ╚═╝   ╚═╝  ╚═╝ ╚═════╝`

// asciiWordmark is the wordmark for a console that cannot show Unicode.
const asciiWordmark = `                              __
   ____ ___  ____ ____  _____/ /__________
  / __ ` + "`" + `__ \/ __ ` + "`" + `/ _ \/ ___/ __/ ___/ __ \
 / / / / / / /_/ /  __(__  ) /_/ /  / /_/ /
/_/ /_/ /_/\__,_/\___/____/\__/_/   \____/`

// rgb is a 24-bit colour.
type rgb struct{ r, g, b int }

// The ends of the logo's left-to-right gradient, orange to yellow like
// Composer's logo. Where the console has no true colour, Symfony's
// formatter degrades each column to the nearest of the 8 ANSI colours: red
// to yellow, so the logo still fades from one to the other.
var (
	logoFrom = rgb{0xf9, 0x73, 0x16}
	logoTo   = rgb{0xfa, 0xcc, 0x15}
)

// Logo is maestro's wordmark as formatter markup, each column coloured
// along the gradient, ending in a newline; the formatter drops the colours
// on undecorated output.
func Logo() string { return gradient(LogoText(), logoFrom, logoTo) + "\n" }

// LogoText is the wordmark Logo colours, without markup or the final
// newline: block letters where the console shows Unicode, ASCII art where
// it does not.
func LogoText() string {
	if unicodeConsole() {
		return wordmark
	}

	return asciiWordmark
}

// gradient colours every non-blank column of the lines of s with the
// colour at that column of a ramp from `from` to `to` across the widest
// line, in Symfony "fg=#rrggbb" tags.
func gradient(s string, from, to rgb) string {
	lines := strings.Split(s, "\n")
	width := 0
	for _, line := range lines {
		width = max(width, len([]rune(line)))
	}

	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		for col, r := range []rune(line) {
			if r == ' ' {
				b.WriteRune(r)

				continue
			}
			c := lerp(from, to, col, width)
			fmt.Fprintf(&b, "<fg=#%02x%02x%02x>%c</>", c.r, c.g, c.b, r)
		}
	}

	return b.String()
}

// lerp is the colour at column col of a ramp of width columns.
func lerp(from, to rgb, col, width int) rgb {
	if width < 2 {
		return from
	}
	mix := func(a, b int) int { return a + (b-a)*col/(width-1) }

	return rgb{mix(from.r, to.r), mix(from.g, to.g), mix(from.b, to.b)}
}
