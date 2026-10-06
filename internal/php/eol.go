// Ports the PHP_EOL constant (main/main.c, PHP_EOL in main/php.h).

package php

import (
	"runtime"
	"strings"
)

// EOL is PHP_EOL: "\r\n" on Windows and "\n" everywhere else. Ported code
// uses it exactly where Composer and Symfony Console use PHP_EOL (which
// includes every newline Output::writeln and StreamOutput::doWrite add),
// and a literal "\n" wherever they write "\n".
//
// It is a variable only so that tests can exercise the Windows line
// endings on every platform (see SetEOLForTest); nothing else assigns it.
var EOL = nativeEOL(runtime.GOOS)

// nativeEOL is PHP_EOL on goos.
func nativeEOL(goos string) string {
	if goos == "windows" {
		return "\r\n"
	}

	return "\n"
}

// NormalizeEOL is str_replace(PHP_EOL, "\n", $s): what SymfonyStyle does
// to its buffered output and Symfony's testers' getDisplay(true) do to
// what a command wrote. Tests use it to compare captured output with
// expectations written with "\n" on every platform.
func NormalizeEOL(s string) string {
	if EOL == "\n" {
		return s
	}

	return strings.ReplaceAll(s, EOL, "\n")
}

// cleanupT is the part of testing.TB SetEOLForTest needs; the php package
// does not import testing.
type cleanupT interface {
	Helper()
	Cleanup(func())
}

// SetEOLForTest makes EOL eol for the rest of the test t and restores it
// when t finishes. The test must not run in parallel with others reading
// EOL (do not call t.Parallel in it).
func SetEOLForTest(t cleanupT, eol string) {
	t.Helper()
	prev := EOL
	EOL = eol
	t.Cleanup(func() { EOL = prev })
}
