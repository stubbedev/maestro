// Exceptions crossing the channel (docs/PLUGINS.md §5.10, D12) and the
// channel's own failures.

package rpc

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// PHPExit is the end of the PHP process while maestro needed it: exit()
// in plugin code, a fatal error (255, after PHP printed its message), a
// signal (128+signal) or the shim giving up. It reaches cmd/maestro
// without further output, and maestro exits with Code.
type PHPExit struct {
	Code int
}

func (e *PHPExit) Error() string { return "php exited with code " + strconv.Itoa(e.Code) }

// ExitCode is the status maestro exits with (internal/command's ExitCoder:
// the run ends with it and nothing is rendered).
func (e *PHPExit) ExitCode() int { return e.Code }

// ProtocolError is a message that breaks the IPC protocol. The PHP process
// is killed when one arrives; it is an internal error of maestro or the
// shim.
type ProtocolError struct {
	Message string
}

func (e *ProtocolError) Error() string { return "maestro: plugin runtime protocol error: " + e.Message }

// UnsupportedError is returned to PHP for a method maestro has no handler
// for; PHP throws it as Maestro\Shim\UnsupportedApiException.
type UnsupportedError struct {
	Method string
}

func (e *UnsupportedError) Error() string { return "maestro has no handler for " + e.Method }

// ThrowableClass implements console.Throwable.
func (*UnsupportedError) ThrowableClass() string { return `Maestro\Shim\UnsupportedApiException` }

// ThrowableFile implements console.Throwable.
func (*UnsupportedError) ThrowableFile() string { return "" }

// ThrowableLine implements console.Throwable.
func (*UnsupportedError) ThrowableLine() int { return 0 }

// ThrowableCode implements console.Throwable.
func (*UnsupportedError) ThrowableCode() int { return 0 }

// ThrowablePrevious implements console.Throwable.
func (*UnsupportedError) ThrowablePrevious() error { return nil }

// PHPException is an exception PHP code threw, as maestro sees it. H is
// the PHP object's handle: returned to PHP (unwrapped), the very same
// object is rethrown there.
type PHPException struct {
	// Class is get_class(); Classes adds its parents and interfaces.
	Class   string
	Classes []string
	Message string
	// Code is getCode() as an int (PHP allows other types).
	Code     int
	File     string
	Line     int
	Trace    []console.TraceFrame
	Previous *PHPException
	H        Handle
	// Extra holds the typed extras of known classes (docs/PLUGINS.md
	// §5.10), when any.
	Extra any

	// open is set while the last frame of Trace is the call the shim
	// made into the PHP code that threw (docs/PLUGINS.md §5.12): its
	// callee is PHP's, its location still the shim's until maestro
	// names Composer's call (LocateCall, or AddFrame of a frame for the
	// same callee).
	open bool
}

var (
	_ phperr.Traced  = (*PHPException)(nil)
	_ phperr.Locator = (*PHPException)(nil)
	_ phperr.Chained = (*PHPException)(nil)
)

// AddFrame implements phperr.Traced: the exception went up through the
// port of a PHP call, whose frame its trace gets as PHP's would. The first
// frame added names the call the boundary frame stands for when it is
// for the same callee (Symfony's Command::run() of a command written in
// PHP), and only locates it then.
func (e *PHPException) AddFrame(fr phperr.Frame) {
	if e.open {
		e.open = false
		last := &e.Trace[len(e.Trace)-1]
		if fr.Function == last.Class+last.Type+last.Function {
			last.File, last.Line = phperr.AbsPath(fr.File), fr.Line

			return
		}
	}
	e.Trace = append(e.Trace, traceFrame(fr))
}

// LocateCall implements phperr.Locator: the boundary frame's call is
// Composer's at file:line.
func (e *PHPException) LocateCall(file string, line int) bool {
	if !e.open {
		return false
	}
	e.open = false
	last := &e.Trace[len(e.Trace)-1]
	last.File, last.Line = phperr.AbsPath(file), line

	return true
}

// OpenFrame is the boundary frame while its location is not Composer's
// yet (LocateCall): the callee of the call maestro made into PHP code
// that the exception left through.
func (e *PHPException) OpenFrame() (console.TraceFrame, bool) {
	if !e.open {
		return console.TraceFrame{}, false
	}

	return e.Trace[len(e.Trace)-1], true
}

// PHPTrace implements phperr.Traced.
func (e *PHPException) PHPTrace() []phperr.Frame {
	out := make([]phperr.Frame, len(e.Trace))
	for i, f := range e.Trace {
		out[i] = phperr.Frame{Function: f.Class + f.Type + f.Function, File: f.File, Line: f.Line}
	}

	return out
}

// PHPPrevious implements phperr.Chained: maestro's frames go to the
// previous exceptions too, constructed further down the same stack.
func (e *PHPException) PHPPrevious() error {
	if e.Previous == nil {
		return nil
	}

	return e.Previous
}

// traceFrame is a frame maestro recorded as PHP's trace has it: the class,
// call type and function split, the file absolute (phperr.AbsPath).
func traceFrame(fr phperr.Frame) console.TraceFrame {
	tf := console.TraceFrame{Function: fr.Function, File: phperr.AbsPath(fr.File), Line: fr.Line}
	// the first "->" or "::" separates the class, unless it is inside a
	// closure's name ("{closure:Class::method():12}")
	i := -1
	for _, sep := range [...]string{"->", "::"} {
		if j := strings.Index(fr.Function, sep); j > 0 && (i < 0 || j < i) {
			i = j
		}
	}
	if i > 0 && !strings.Contains(fr.Function[:i], "{") {
		tf.Class, tf.Type, tf.Function = fr.Function[:i], fr.Function[i:i+2], fr.Function[i+2:]
	}

	return tf
}

func (e *PHPException) Error() string { return e.Message }

// InstanceOf is `$e instanceof $class` (exact names, as PHP reports them).
func (e *PHPException) InstanceOf(class string) bool { return slices.Contains(e.Classes, class) }

// ThrowableClass implements console.Throwable.
func (e *PHPException) ThrowableClass() string { return e.Class }

// ThrowableFile implements console.Throwable.
func (e *PHPException) ThrowableFile() string { return e.File }

// ThrowableLine implements console.Throwable.
func (e *PHPException) ThrowableLine() int { return e.Line }

// ThrowableCode implements console.Throwable.
func (e *PHPException) ThrowableCode() int { return e.Code }

// ThrowablePrevious implements console.Throwable.
func (e *PHPException) ThrowablePrevious() error {
	if e.Previous == nil {
		return nil
	}

	return e.Previous
}

// ThrowableTrace implements console.Tracer.
func (e *PHPException) ThrowableTrace() []console.TraceFrame { return e.Trace }

// IsPHPError implements eventdispatcher.PHPError: a \Error is not caught
// by `catch (\Exception)`.
func (e *PHPException) IsPHPError() bool { return e.InstanceOf("Error") }

// As makes errors.As see the Go error types the ports use for the PHP
// classes they catch (docs/PLUGINS.md §5.10). The result is a copy;
// returning e itself to PHP keeps the object's identity.
func (e *PHPException) As(target any) bool {
	switch t := target.(type) {
	case **eventdispatcher.ScriptExecutionError:
		if e.InstanceOf(`Composer\EventDispatcher\ScriptExecutionException`) {
			*t = &eventdispatcher.ScriptExecutionError{Message: e.Message, Code: e.Code, File: e.File, Line: e.Line}

			return true
		}
	case **util.TransportError:
		if e.InstanceOf(`Composer\Downloader\TransportException`) {
			*t = &util.TransportError{Message: e.Message, Code: e.Code}

			return true
		}
	case **util.RuntimeError:
		if e.InstanceOf("RuntimeException") {
			*t = &util.RuntimeError{Message: e.Message}

			return true
		}
	case **util.LogicError:
		if e.InstanceOf("LogicException") {
			*t = &util.LogicError{Message: e.Message}

			return true
		}
	case **util.InvalidArgumentError:
		if e.InstanceOf("InvalidArgumentException") {
			*t = &util.InvalidArgumentError{Message: e.Message}

			return true
		}
	case **util.UnexpectedValueError:
		if e.InstanceOf("UnexpectedValueException") {
			*t = &util.UnexpectedValueError{Message: e.Message}

			return true
		}
	case **util.ErrorException:
		if e.InstanceOf("ErrorException") {
			*t = &util.ErrorException{Message: e.Message}

			return true
		}
	}

	return false
}

// goErrorClass is the PHP class of an error maestro raises that does not
// name one itself (console.Throwable), for the Go error types of
// internal/util.
func goErrorClass(err error) (class string, code int, ok bool) {
	switch e := err.(type) { //nolint:errorlint // the error itself, not its chain: a wrapper adds to the message.
	case *util.RuntimeError, *util.LogicError, *util.InvalidArgumentError,
		*util.UnexpectedValueError, *util.ErrorException, *util.IOError:
		// their Class field names a library's subclass (symfony/process's
		// RuntimeException, ...), which PHPClassOf reads
		class, _ := util.PHPClassOf(e)

		return class, 0, true
	case *util.TransportError:
		return `Composer\Downloader\TransportException`, e.Code, true
	}

	return "", 0, false
}
