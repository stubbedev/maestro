// Ports ConsoleIO::sanitize() and ConsoleIO::ensureValidUtf8() of
// src/Composer/IO/ConsoleIO.php (Composer). console has the implementation,
// which the OSC encoders of that release use without importing internal/io.

package io

import "github.com/stubbedev/maestro/internal/console"

// Sanitize removes ANSI escape sequences and control characters from a
// message (ConsoleIO::sanitize). With allowNewlines, "\n" and "\r\n" are
// kept; otherwise every character in \x01-\x1A is removed. Invalid UTF-8 is
// first replaced like mb_convert_encoding($s, 'UTF-8', 'UTF-8').
func Sanitize(message string, allowNewlines bool) string {
	return console.Sanitize(message, allowNewlines)
}

// SanitizeMessages sanitizes each message (sanitize() of an array).
func SanitizeMessages(messages []string, allowNewlines bool) []string {
	return console.SanitizeMessages(messages, allowNewlines)
}
