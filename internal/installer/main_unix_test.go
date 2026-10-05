//go:build unix

package installer

import (
	"os"
	"syscall"
	"testing"
)

// TestMain runs the tests with the umask the oracle scripts use, so that
// file modes compare equal.
func TestMain(m *testing.M) {
	syscall.Umask(0o022)

	os.Exit(m.Run())
}
