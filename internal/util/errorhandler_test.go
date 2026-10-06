package util

import (
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/phperr"
)

// TestTriggerDeprecation checks ErrorHandler's output against Composer
// 2.10.3 on PHP 8.4.25: ErrorHandler::register() with a BufferIO
// of each verbosity, then three trigger_error(..., E_USER_DEPRECATED) calls
// (PHP names the absolute path of the calling file and lists its whole
// call stack; here the site's basename and only that frame).
func TestTriggerDeprecation(t *testing.T) {
	t.Cleanup(ResetErrorHandler)

	for _, c := range []struct {
		verbosity int
		want      string
	}{
		{console.VerbosityNormal, "<warning>Deprecation Notice: first in eh.php:21</warning>\n" +
			"<warning>More deprecation notices were hidden, run again with `-v` to show them.</warning>\n"},
		{console.VerbosityVerbose, "<warning>Deprecation Notice: first in eh.php:21</warning>\n" +
			"<warning>Stack trace:</warning>\n" +
			"<warning> eh.php:21</warning>\n" +
			"<warning>Deprecation Notice: second in eh.php:22</warning>\n" +
			"<warning>Stack trace:</warning>\n" +
			"<warning> eh.php:22</warning>\n" +
			"<warning>Deprecation Notice: third in eh.php:23</warning>\n" +
			"<warning>Stack trace:</warning>\n" +
			"<warning> eh.php:23</warning>\n"},
	} {
		ResetErrorHandler()

		// before register($io), notices are not shown
		TriggerDeprecation("unseen", phperr.At("eh.php", 1))

		out, err := io.NewBufferIO("", c.verbosity, nil)
		if err != nil {
			t.Fatal(err)
		}

		RegisterErrorHandler(out)

		TriggerDeprecation("first", phperr.At("eh.php", 21))
		TriggerDeprecation("second", phperr.At("eh.php", 22))
		TriggerDeprecation("third", phperr.At("eh.php", 23))

		if got := out.Output(); got != c.want {
			t.Errorf("verbosity %d:\n got %q\nwant %q", c.verbosity, got, c.want)
		}
	}
}
