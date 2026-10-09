//go:build !unix && !windows

package fsstate

import "os"

// Known reports whether files have IDs here.
func Known() bool { return false }

// Stat: no IDs here.
func Stat(string) (ID, bool) { return ID{}, false }

// Lstat: no IDs here.
func Lstat(string) (ID, bool) { return ID{}, false }

// Fstat: no IDs here.
func Fstat(*os.File) (ID, bool) { return ID{}, false }
