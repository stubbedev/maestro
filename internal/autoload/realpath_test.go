package autoload

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/phperr"
)

// A path realpath() cannot resolve fails with the TypeError PHP throws for
// the false Filesystem then receives, naming the path rather than
// AutoloadGenerator's line.
func TestRealpathFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	_, err := realpath(missing)
	if err == nil {
		t.Fatal("no error")
	}
	if class, _ := phperr.ClassOf(err); class != "TypeError" {
		t.Errorf("class %s", class)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("message %q does not name %s", err.Error(), missing)
	}
}
