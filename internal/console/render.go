// How the Application shows an uncaught error. Symfony renders exceptions
// in boxes with their PHP classes, throw sites and call stacks; how errors
// look is maestro's own (docs/PORTING.md "The contract"), so the
// information goes to internal/ui, which renders it.

package console

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/ui"
)

// RenderThrowable shows err, an error that ended the run, on out (the
// error output): an empty line, then the Diagnostic of ErrorDiagnostic
// (with what the application adds, AppErrorPresenter) and, last of the
// debugging details, the exit code, written as it is at every verbosity,
// as Symfony writes its rendering.
func (a *Application) RenderThrowable(err error, out Output) {
	d := a.ErrorDiagnostic(err)
	if p, ok := a.self.(AppErrorPresenter); ok {
		p.PresentError(err, &d)
	}
	_, code, _ := throwableInfo(err)
	d.Details = append(d.Details, "exit code "+strconv.Itoa(exitCodeOf(code)))

	out.Write("", true, VerbosityQuiet)
	for _, l := range d.Lines(ui.Options{Decorated: out.IsDecorated(), Verbose: out.Verbosity() >= VerbosityVerbose, Width: a.terminal.Width()}) {
		out.Write(l, true, VerbosityQuiet|OutputRaw)
	}
}

// ErrorDiagnostic is what an uncaught error reports: its message, the
// messages of its previous errors (Throwable's, else phperr.PreviousOf, as
// Symfony follows getPrevious()), the running command's usage when the
// input was wrong (a console exception), and, for debugging, the Go types
// and PHP classes of the errors, the trace of an exception PHP code threw
// (a Tracer: plugin code).
func (a *Application) ErrorDiagnostic(err error) ui.Diagnostic {
	d := ui.Diagnostic{Kind: ui.Error}
	for i, e := 0, err; e != nil; i++ {
		class, code, prev := throwableInfo(e)
		message := php.Trim(e.Error())
		if message == "" {
			message = class + " (no message)"
		}
		if i == 0 {
			d.Message = message
		} else {
			d.Causes = append(d.Causes, message)
		}

		detail := errorTypes(e) + " [" + class
		if code != 0 {
			detail += ", code " + strconv.Itoa(code)
		}
		detail += "]"
		if i > 0 {
			detail = "caused by " + detail
		}
		d.Details = append(d.Details, detail)
		if tr, ok := e.(Tracer); ok {
			for _, f := range tr.ThrowableTrace() {
				d.Details = append(d.Details, "  "+traceFrame(f))
			}
		}

		e = prev
	}

	// the error shown, or one a wrapper with its message stands for
	if e, ok := errors.AsType[Alternativer](err); ok && php.Trim(e.Error()) == d.Message {
		d.TakeAlternatives(e.ErrorAlternatives())
	}

	if a.runningCommand != nil && isConsoleExceptionValue(err) {
		d.Usage = phpSprintf(a.runningCommand.Base().Synopsis(false), a.Name())
	}

	return d
}

// Alternativer is an error offering alternatives to what was typed (a
// command, namespace, option or package name), which its rendering shows
// as their own item (ui.Diagnostic.TakeAlternatives).
type Alternativer interface {
	error
	ErrorAlternatives() []string
}

// exitCodeOf is the exit code of an uncaught exception with code: the
// code when positive, else 1, at most 255.
func exitCodeOf(code int) int {
	if code <= 0 {
		return 1
	}

	return min(code, 255)
}

// errorTypes names the Go types of err and the errors it wraps
// (errors.Unwrap), outermost first.
func errorTypes(err error) string {
	var types []string
	for e := err; e != nil && len(types) < 8; e = unwrapOne(e) {
		types = append(types, fmt.Sprintf("%T", e))
	}

	return strings.Join(types, " > ")
}

func unwrapOne(err error) error {
	if u, ok := err.(interface{ Unwrap() error }); ok {
		return u.Unwrap()
	}

	return nil
}

// traceFrame is a frame of a PHP trace: "Class->function() at file:line".
func traceFrame(f TraceFrame) string {
	s := f.Class + f.Type + f.Function
	if f.Function != "" {
		s += "()"
	}
	if f.File != "" {
		if s != "" {
			s += " at "
		}
		s += f.File
		if f.Line != 0 {
			s += ":" + strconv.Itoa(f.Line)
		}
	}

	return s
}

// throwableInfo is the PHP exception details of err: its class, code
// (phperr.ClassOf) and previous exception (phperr.PreviousOf).
func throwableInfo(err error) (class string, code int, prev error) {
	class, code = phperr.ClassOf(err)

	return class, code, phperr.PreviousOf(err)
}

// isConsoleExceptionValue is "$e instanceof ExceptionInterface" for the
// object err itself, not what it wraps.
func isConsoleExceptionValue(err error) bool {
	e, ok := err.(phperr.Exception) //nolint:errorlint // instanceof applies to the object itself.

	return ok && phperr.InstanceOf(e, phperr.ClassConsoleException)
}
