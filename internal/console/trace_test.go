package console

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
)

// The "Exception trace:" block names the throw site and the frames
// recorded with phperr.Call by their absolute paths under phperr.Root, as
// PHP's getFile() and getTrace() give them; Application.Call adds the
// frames of the console's own calls.
func TestRenderThrowableTrace(t *testing.T) {
	phperr.SetRoot("phar:///opt/maestro")
	t.Cleanup(func() { phperr.SetRoot("") })
	t.Setenv("COLUMNS", "80")

	app := newTestApp()
	_, err := app.Call(phperr.Frame{Function: `Symfony\Component\Console\Application->doRunCommand`, File: applicationPHP, Line: 301}, func() (int, error) {
		e := newError(KindRuntime, "ArgvInput.php", 220, `The "--nope" option does not exist.`)

		return 0, phperr.Call(e, `Symfony\Component\Console\Input\ArgvInput->addLongOption`, "ArgvInput.php", 149)
	})
	if len(app.Stack()) != 0 {
		t.Errorf("Stack() after Call = %v, want none", app.Stack())
	}

	var buf bytes.Buffer
	decorated := false
	app.RenderThrowable(err, NewStreamOutput(&buf, VerbosityVerbose, &decorated, nil))

	want := "Exception trace:\n" +
		"  at phar:///opt/maestro/vendor/symfony/console/Input/ArgvInput.php:220\n" +
		" Symfony\\Component\\Console\\Input\\ArgvInput->addLongOption() at phar:///opt/maestro/vendor/symfony/console/Input/ArgvInput.php:149\n" +
		" Symfony\\Component\\Console\\Application->doRunCommand() at phar:///opt/maestro/vendor/symfony/console/Application.php:301\n"
	if !strings.Contains(php.NormalizeEOL(buf.String()), want) {
		t.Errorf("rendered\n%s\nwant it to contain\n%s", buf.String(), want)
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
