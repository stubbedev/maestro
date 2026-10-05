// Ports src/Terminal.php (symfony/console).

package console

import (
	"os"
	"os/exec"
	"strconv"
	"sync"

	"golang.org/x/term"
)

// Terminal reports the terminal dimensions.
type Terminal struct{}

var terminalDims struct {
	once          sync.Once
	width, height int

	sttyOnce sync.Once
	stty     bool
}

// Width returns $COLUMNS when set (even if it is not a number), otherwise
// the detected width, falling back to 80.
func (Terminal) Width() int {
	if w, ok := os.LookupEnv("COLUMNS"); ok {
		return phpIntval(phpTrim(w))
	}
	terminalDims.once.Do(initDimensions)
	if terminalDims.width == 0 {
		return 80
	}

	return terminalDims.width
}

// Height returns $LINES when set, otherwise the detected height, falling
// back to 50.
func (Terminal) Height() int {
	if h, ok := os.LookupEnv("LINES"); ok {
		return phpIntval(phpTrim(h))
	}
	terminalDims.once.Do(initDimensions)
	if terminalDims.height == 0 {
		return 50
	}

	return terminalDims.height
}

// HasSttyAvailable ports Terminal::hasSttyAvailable(): `stty` prints its
// settings only when an stty binary exists and stdin is a terminal.
func HasSttyAvailable() bool {
	terminalDims.sttyOnce.Do(func() {
		if _, err := exec.LookPath("stty"); err != nil {
			return
		}
		terminalDims.stty = term.IsTerminal(int(os.Stdin.Fd()))
	})

	return terminalDims.stty
}

// initDimensions ports Terminal::initDimensions(). PHP parses the output of
// `stty -a | grep columns`, which reports the window size of the inherited
// stdin; reading that size with TIOCGWINSZ yields the same numbers without
// spawning a shell. On Windows the console screen buffer of stdout plays the
// part of `mode CON` (ANSICON is consulted first, like PHP).
func initDimensions() {
	if w, h, ok := ansiconDimensions(); ok {
		terminalDims.width, terminalDims.height = w, h

		return
	}
	if w, h, err := term.GetSize(int(stdDimensionsFd())); err == nil {
		terminalDims.width, terminalDims.height = w, h
	}
}

// parseANSICON parses "wxh (WxH)" or "wxh", returning [w, H] or [w, h].
func parseANSICON(s string) (int, int, bool) {
	s = phpTrim(s)
	num := func(i int) (int, int) {
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j == i {
			return 0, -1
		}
		n, _ := strconv.Atoi(s[i:j])

		return n, j
	}
	w, i := num(0)
	if i < 0 || i >= len(s) || s[i] != 'x' {
		return 0, 0, false
	}
	h, i := num(i + 1)
	if i < 0 {
		return 0, 0, false
	}
	if i == len(s) {
		return w, h, true
	}
	if s[i] != ' ' || i+1 >= len(s) || s[i+1] != '(' {
		return 0, 0, false
	}
	_, i = num(i + 2)
	if i < 0 || i >= len(s) || s[i] != 'x' {
		return 0, 0, false
	}
	hh, i := num(i + 1)
	if i < 0 || i != len(s)-1 || s[i] != ')' {
		return 0, 0, false
	}

	return w, hh, true
}
