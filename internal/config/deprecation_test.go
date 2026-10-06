package config

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// bin-compat "symlink" raises Config.php:478's deprecation (Composer
// 2.10.3 on PHP 8.4.25, ErrorHandler::register() with a verbose
// BufferIO) and is still returned. The notice's format, the site and the
// stack trace it shows at -v are free (docs/PORTING.md "The contract").
func TestConfig_BinCompatSymlinkDeprecation(t *testing.T) {
	util.ResetErrorHandler()
	t.Cleanup(util.ResetErrorHandler)

	out, err := io.NewBufferIO("", console.VerbosityVerbose, nil)
	if err != nil {
		t.Fatal(err)
	}

	util.RegisterErrorHandler(out)

	config := New(false, "")
	merge(t, config, cfg(t, `{"bin-compat": "symlink"}`), SourceUnknown)
	assertSame(t, "symlink", mustGet(t, config, "bin-compat", 0))

	want := `config.bin-compat "symlink" is deprecated since Composer 2.2, use auto, full (for Windows compatibility) or proxy instead.`
	if got := php.NormalizeEOL(out.Output()); !strings.Contains(got, want) {
		t.Errorf("got %q\nwant a notice %q", got, want)
	}
}
