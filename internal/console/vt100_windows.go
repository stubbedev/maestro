//go:build windows

// Ports the sapi_windows_vt100_support() checks of StreamOutput.php and
// Terminal.php.

package console

import (
	"os"

	"golang.org/x/sys/windows"
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
