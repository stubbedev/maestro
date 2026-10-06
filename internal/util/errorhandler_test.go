package util

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
)

// TestTriggerDeprecation checks which deprecation notices ErrorHandler
// shows, as Composer 2.10.3 on PHP 8.4.25 does: none before
// ErrorHandler::register(), then at normal verbosity the first one and a
// hint that more were hidden, at -v all of them. The notices' format, their
// sites and the stack traces shown at -v are free (docs/PORTING.md "The
// contract").
func TestTriggerDeprecation(t *testing.T) {
	t.Cleanup(ResetErrorHandler)

	const hidden = "More deprecation notices were hidden, run again with `-v` to show them."

	for _, c := range []struct {
		verbosity int
		shown     []string
		notShown  []string
	}{
		{console.VerbosityNormal, []string{"first", hidden}, []string{"unseen", "second", "third"}},
		{console.VerbosityVerbose, []string{"first", "second", "third"}, []string{"unseen", hidden}},
	} {
		ResetErrorHandler()

		// before register($io), notices are not shown
		TriggerDeprecation("unseen")

		out, err := io.NewBufferIO("", c.verbosity, nil)
		if err != nil {
			t.Fatal(err)
		}

		RegisterErrorHandler(out)

		TriggerDeprecation("first")
		TriggerDeprecation("second")
		TriggerDeprecation("third")

		got := php.NormalizeEOL(out.Output())
		for _, want := range c.shown {
			if !strings.Contains(got, want) {
				t.Errorf("verbosity %d: %q not shown:\n%s", c.verbosity, want, got)
			}
		}
		for _, notWant := range c.notShown {
			if strings.Contains(got, notWant) {
				t.Errorf("verbosity %d: %q shown:\n%s", c.verbosity, notWant, got)
			}
		}
	}
}

// Notices are rendered by internal/ui, without Composer's source paths or
// PHP stacks.
func TestTriggerDeprecationRendering(t *testing.T) {
	t.Cleanup(ResetErrorHandler)
	ResetErrorHandler()

	out, err := io.NewBufferIO("", console.VerbosityNormal, nil)
	if err != nil {
		t.Fatal(err)
	}
	RegisterErrorHandler(out)
	TriggerDeprecation("an old API")
	RaiseDeprecation("another old API")

	want := "Deprecated: an old API\nNote: More deprecation notices were hidden, run again with `-v` to show them.\n"
	if got := php.NormalizeEOL(out.Output()); got != want {
		t.Errorf("output %q, want %q", got, want)
	}
}
