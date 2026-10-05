//go:build windows

package util

import "os"

func getwd() (string, error) {
	return os.Getwd()
}

// The posix user lookups Composer falls back on do not exist on Windows.
func currentUID() string { return "" }

func currentEUID() string { return "" }
