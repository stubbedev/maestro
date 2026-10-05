//go:build windows

// Ports the sapi_windows_vt100_support() checks of StreamOutput.php and
// Terminal.php.

package console

import (
	"os"
	"strconv"

	"golang.org/x/sys/windows"

	"github.com/stubbedev/maestro/internal/php"
)

// windowsVT100Support reports whether the stream is a console with virtual
// terminal processing enabled (sapi_windows_vt100_support($stream)).
func windowsVT100Support(stream any) bool {
	f, ok := stream.(fdWriter)
	if !ok {
		return false
	}
	var mode uint32
	if err := windows.GetConsoleMode(windows.Handle(f.Fd()), &mode); err != nil {
		return false
	}

	return mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0
}

func ansiconDimensions() (int, int, bool) {
	if v, ok := os.LookupEnv("ANSICON"); ok {
		return parseANSICON(v)
	}

	return 0, 0, false
}

// stdDimensionsFd is the console handle `mode CON` describes.
func stdDimensionsFd() uintptr { return os.Stdout.Fd() }

// parseANSICON parses "wxh (WxH)" or "wxh", returning [w, H] or [w, h].
func parseANSICON(s string) (int, int, bool) {
	s = php.Trim(s)
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
