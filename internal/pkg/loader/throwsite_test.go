package loader_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg/loader"
)

// The errors carry Composer's throw sites, which the console renders.
func TestArrayLoader_ThrowSites(t *testing.T) {
	l := loader.NewArrayLoader(nil, false)

	_, err := l.Load(php.ArrayOf("version", "1.0.0"), "")
	if !phperr.Is(err, "ArrayLoader.php", 112) {
		t.Errorf("no name: %v, want ArrayLoader.php:112", err)
	}

	_, err = l.Load(php.ArrayOf("name", "a/b"), "")
	if !phperr.Is(err, "ArrayLoader.php", 115) {
		t.Errorf("no version: %v, want ArrayLoader.php:115", err)
	}
}
