package console

import (
	"bytes"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
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
// trace; the PHP call stack of maestro's ports (phperr.Call's frames, the
// throw site) is not shown.
func TestRenderThrowableDetails(t *testing.T) {
	app := newTestApp()
	_, err := app.Call(phperr.Frame{Function: `Symfony\Component\Console\Application->doRunCommand`, File: applicationPHP, Line: 301}, func() (int, error) {
		e := newError(KindRuntime, "ArgvInput.php", 220, `The "--nope" option does not exist.`)

		return 0, phperr.Call(e, `Symfony\Component\Console\Input\ArgvInput->addLongOption`, "ArgvInput.php", 149)
	})
	if len(app.Stack()) != 0 {
		t.Errorf("Stack() after Call = %v, want none", app.Stack())
	}

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

// Stack lists the calls in progress innermost first, above the run()
// callers.
func TestApplicationStack(t *testing.T) {
	app := newTestApp()
	outer := phperr.Frame{Function: "outer", File: "A.php", Line: 1}
	inner := phperr.Frame{Function: "inner", File: "B.php", Line: 2}
	caller := phperr.Frame{Function: "run", File: "bin/composer", Line: 113}
	app.SetRunCallers(caller)

	var stack []phperr.Frame
	_, _ = app.Call(outer, func() (int, error) {
		return app.Call(inner, func() (int, error) {
			stack = app.Stack()

			return 0, nil
		})
	})
	if len(stack) != 3 || stack[0] != inner || stack[1] != outer || stack[2] != caller {
		t.Errorf("Stack() = %v, want [inner outer run]", stack)
	}
}
