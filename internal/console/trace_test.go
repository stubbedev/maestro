package console

import (
	"bytes"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// tracedException is an exception PHP code threw (a plugin's): it knows
// its trace.
type tracedException struct {
	testException
	trace []TraceFrame
}

func (e *tracedException) ThrowableTrace() []TraceFrame { return e.trace }

// At -v an error's debugging details are the Go types and PHP classes of
// its chain and the exit code, and, for an exception PHP code threw, its
// trace.
func TestRenderThrowableDetails(t *testing.T) {
	app := newTestApp()
	_, err := app.Call(`Symfony\Component\Console\Application->doRunCommand`, func() (int, error) {
		return 0, newError(KindRuntime, `The "--nope" option does not exist.`)
	})

	render := func(err error) string {
		var buf bytes.Buffer
		decorated := false
		app.RenderThrowable(err, NewStreamOutput(&buf, VerbosityVerbose, &decorated, nil))

		return php.NormalizeEOL(buf.String())
	}

	want := "\nError: The \"--nope\" option does not exist.\n" +
		"  Debug: *console.Error [Symfony\\Component\\Console\\Exception\\RuntimeException]\n" +
		"         exit code 1\n"
	if got := render(err); got != want {
		t.Errorf("rendered\n%s\nwant\n%s", got, want)
	}

	plugin := &tracedException{
		class: `Acme\PluginException`, message: "the plugin failed", code: 3,
		trace: []TraceFrame{
			{Class: `Acme\Plugin`, Type: "->", Function: "onInstall", File: "/p/vendor/acme/plugin/src/Plugin.php", Line: 42},
			{Function: "{closure}"},
		},
	}
	want = "\nError: the plugin failed\n" +
		"  Debug: *console.tracedException [Acme\\PluginException, code 3]\n" +
		"           Acme\\Plugin->onInstall() at /p/vendor/acme/plugin/src/Plugin.php:42\n" +
		"           {closure}()\n" +
		"         exit code 3\n"
	if got := render(plugin); got != want {
		t.Errorf("rendered\n%s\nwant\n%s", got, want)
	}
}
