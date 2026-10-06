//go:build !linux

// Ports nothing: git's version is kept across runs only on Linux (see
// UseVersionCache).

package vcs

import "time"

func gitBinaryKey(string, time.Time) string { return "" }
