//go:build !unix

package fsstate

import "os"

// Known reports whether files have IDs here: not on Windows, whose
// os.Stat describes no file index or change time.
func Known() bool { return false }

// Stat: no IDs here.
func Stat(string) (ID, bool) { return ID{}, false }

// Lstat: no IDs here.
func Lstat(string) (ID, bool) { return ID{}, false }

// Fstat: no IDs here.
func Fstat(*os.File) (ID, bool) { return ID{}, false }
