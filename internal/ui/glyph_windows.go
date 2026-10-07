//go:build windows

package ui

import "golang.org/x/sys/windows"

// consoleIsUTF8 is whether the console's output code page is UTF-8; known
// is false without a console.
func consoleIsUTF8() (ok, known bool) {
	cp, err := windows.GetConsoleOutputCP()
	if err != nil || cp == 0 {
		return false, false
	}

	return cp == 65001, true // CP_UTF8
}
