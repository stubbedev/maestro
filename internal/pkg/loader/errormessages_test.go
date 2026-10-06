package loader_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg/loader"
)

// The errors carry Composer's messages (where they were thrown is free,
// docs/PORTING.md "The contract").
func TestArrayLoader_ErrorMessages(t *testing.T) {
	l := loader.NewArrayLoader(nil, false)

	_, err := l.Load(php.ArrayOf("version", "1.0.0"), "")
	if want := `Unknown package has no name defined ({"version":"1.0.0"}).`; err == nil || err.Error() != want {
		t.Errorf("no name: %v, want %q", err, want)
	}

	_, err = l.Load(php.ArrayOf("name", "a/b"), "")
	if want := "Package a/b has no version defined."; err == nil || err.Error() != want {
		t.Errorf("no version: %v, want %q", err, want)
	}
}
