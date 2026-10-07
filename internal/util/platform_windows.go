//go:build windows

package util

// The posix user lookups Composer falls back on do not exist on Windows.
func currentUID() string { return "" }

func currentEUID() string { return "" }
