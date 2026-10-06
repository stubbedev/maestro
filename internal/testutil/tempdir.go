package testutil

import (
	"path/filepath"
	"testing"
)

// RealTempDir is tb.TempDir() with symlinks resolved. On macOS the
// temporary directory is under /var, a symlink to /private/var, and code
// that realpath()s a path (as Composer does in many places) reports the
// /private form: tests comparing such paths need the resolved one.
func RealTempDir(tb testing.TB) string {
	tb.Helper()

	dir, err := filepath.EvalSymlinks(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}

	return dir
}
