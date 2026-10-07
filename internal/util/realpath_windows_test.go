package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/testutil"
)

// PHP's realpath() follows junctions on Windows, as Composer relies on
// for path repositories (ShowCommand prints the source directory, not
// vendor/'s junction).
func TestRealpath_FollowsJunctions(t *testing.T) {
	dir := testutil.RealTempDir(t)
	target := filepath.Join(dir, "packages", "a")
	if err := os.MkdirAll(filepath.Join(target, "src"), 0o777); err != nil {
		t.Fatal(err)
	}

	vendor := filepath.Join(dir, "vendor", "acme")
	if err := os.MkdirAll(vendor, 0o777); err != nil {
		t.Fatal(err)
	}

	junction := filepath.Join(vendor, "a")
	if err := NewFilesystem(nil).Junction(target, junction); err != nil {
		t.Fatal(err)
	}

	if got, ok := php.Realpath(junction); !ok || got != target {
		t.Errorf("realpath(junction) = %q, %v, want %q", got, ok, target)
	}

	if got, ok := php.Realpath(filepath.Join(junction, "src")); !ok || got != filepath.Join(target, "src") {
		t.Errorf("realpath(junction/src) = %q, %v", got, ok)
	}

	// ".." applies after the junction is followed.
	if got, ok := php.Realpath(junction + `\src\..\..\a`); !ok || got != target {
		t.Errorf("realpath(junction/src/../../a) = %q, %v, want %q", got, ok, target)
	}

	if _, ok := php.Realpath(junction + `\missing\..`); ok {
		t.Error("realpath() of a path through a missing component succeeded")
	}

	// A junction to a junction.
	outer := filepath.Join(dir, "outer")
	if err := NewFilesystem(nil).Junction(vendor, outer); err != nil {
		t.Fatal(err)
	}

	if got, ok := php.Realpath(filepath.Join(outer, "a", "src")); !ok || got != filepath.Join(target, "src") {
		t.Errorf("realpath(outer/a/src) = %q, %v", got, ok)
	}
}
