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
// from /root/eh.php, the second inside f() called at line 30 of the
// same file (the "Stack trace:" lists the absolute paths of the
// trigger_error call and of the calls in progress).
func TestTriggerDeprecation(t *testing.T) {
	t.Cleanup(ResetErrorHandler)
	root := phperr.Root()
	phperr.SetRoot("/root")
	t.Cleanup(func() { phperr.SetRoot(root) })

	for _, c := range []struct {
		verbosity int
		want      string
	}{
		{console.VerbosityNormal, "<warning>Deprecation Notice: first in /root/eh.php:21</warning>\n" +
			"<warning>More deprecation notices were hidden, run again with `-v` to show them.</warning>\n"},
		{console.VerbosityVerbose, "<warning>Deprecation Notice: first in /root/eh.php:21</warning>\n" +
			"<warning>Stack trace:</warning>\n" +
			"<warning> /root/eh.php:21</warning>\n" +
			"<warning>Deprecation Notice: second in /root/eh.php:22</warning>\n" +
			"<warning>Stack trace:</warning>\n" +
			"<warning> /root/eh.php:22</warning>\n" +
			"<warning> /root/eh.php:30</warning>\n" +
			"<warning>Deprecation Notice: third in /root/eh.php:23</warning>\n" +
			"<warning>Stack trace:</warning>\n" +
			"<warning> /root/eh.php:23</warning>\n"},
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
		leave := phperr.Enter("f", "eh.php", 30)
		TriggerDeprecation("second", phperr.At("eh.php", 22))
		leave()
		TriggerDeprecation("third", phperr.At("eh.php", 23))

		if got := out.Output(); got != c.want {
			t.Errorf("verbosity %d:\n got %q\nwant %q", c.verbosity, got, c.want)
		}
	}
}
