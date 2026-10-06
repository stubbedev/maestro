package loader_test

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/util"
)

// The reserved script names raise ArrayLoader.php:271's deprecation, in
// the order Composer 2.10.3 checks them (PHP 8.4.25: ArrayLoader::load of
// scripts {"php", "foo", "composer": [], "putenv": null} under
// ErrorHandler::register(new BufferIO())): the first is shown, and that
// more were hidden. The notices' format and site are free (docs/PORTING.md
// "The contract").
func TestArrayLoader_ReservedScriptNames(t *testing.T) {
	util.ResetErrorHandler()
	t.Cleanup(util.ResetErrorHandler)

	out, err := io.NewBufferIO("", 0, nil)
	if err != nil {
		t.Fatal(err)
	}

	util.RegisterErrorHandler(out)

	config := php.ArrayOf("name", "a/b", "version", "1.0.0", "scripts", php.ArrayOf("php", "x", "foo", "y", "composer", php.NewArray(), "putenv", nil))
	mustLoad(t, loader.NewArrayLoader(nil, false), config)

	got := php.NormalizeEOL(out.Output())
	for _, want := range []string{
		"The `composer` script name is reserved for internal use, please avoid defining it",
		"More deprecation notices were hidden, run again with `-v` to show them.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("got %q\nwant %q", got, want)
		}
	}
	if strings.Contains(got, "The `php` script name") {
		t.Errorf("got %q\nwant only the first notice", got)
	}
}
