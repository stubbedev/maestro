//go:build windows

package util

import "io"

// builtinCommand is nil: on Windows every command runs through cmd.exe.
func builtinCommand([]string, string) func(stderr io.Writer) int { return nil }
