//go:build !windows

// Ports the sapi_windows_vt100_support() checks of StreamOutput.php and
// Terminal.php, which only apply on Windows.

package console

func windowsVT100Support(any) bool { return false }

// ansiconDimensions is only consulted on Windows.
func ansiconDimensions() (int, int, bool) { return 0, 0, false }

// stdDimensionsFd is the descriptor `stty -a` reads the window size from.
func stdDimensionsFd() uintptr { return 0 }
