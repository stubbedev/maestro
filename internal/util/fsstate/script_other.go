//go:build !windows

package fsstate

// batchName: nothing but Windows runs batch files.
func batchName(string) bool { return false }
