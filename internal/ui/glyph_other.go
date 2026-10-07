//go:build !windows

package ui

// consoleIsUTF8 is unknown outside Windows: the locale says.
func consoleIsUTF8() (ok, known bool) { return false, false }
