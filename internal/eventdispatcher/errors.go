// Ports src/Composer/EventDispatcher/ScriptExecutionException.php and the
// other exceptions EventDispatcher throws.

package eventdispatcher

import "github.com/stubbedev/maestro/internal/phperr"

// The PHP file the dispatcher's exceptions are thrown from, for the "In
// EventDispatcher.php line N:" rendering.
const dispatcherFile = "EventDispatcher.php"

// ScriptExecutionError is Composer\EventDispatcher\ScriptExecutionException,
// thrown when a script exits with a non-zero code. Code is that exit code,
// which Application::doRun returns.
type ScriptExecutionError struct {
	Message string
	Code    int
	// File and Line are the throw site ("" and 0 when created elsewhere,
	// as by a plugin).
	File string
	Line int
	phperr.Frames
}

func (e *ScriptExecutionError) Error() string { return e.Message }

// ThrowableClass implements console.Throwable.
func (*ScriptExecutionError) ThrowableClass() string {
	return `Composer\EventDispatcher\ScriptExecutionException`
}

// ThrowableCode implements console.Throwable.
func (e *ScriptExecutionError) ThrowableCode() int { return e.Code }

// ThrowablePrevious implements console.Throwable.
func (*ScriptExecutionError) ThrowablePrevious() error { return nil }

// Error is any other exception the dispatcher throws (RuntimeException,
// LogicException, PHP's \Error), with its PHP class and throw site.
type Error struct {
	Class   string
	Message string
	Line    int
	phperr.Frames
}

func (e *Error) Error() string { return e.Message }

// ThrowableClass implements console.Throwable.
func (e *Error) ThrowableClass() string { return e.Class }

// ThrowableCode implements console.Throwable.
func (*Error) ThrowableCode() int { return 0 }

// ThrowablePrevious implements console.Throwable.
func (*Error) ThrowablePrevious() error { return nil }

// IsPHPError implements PHPError for \Error classes.
func (e *Error) IsPHPError() bool { return e.Class == "Error" }

// PHPError is implemented by errors that stand for a PHP \Error (TypeError,
// a call to an undefined method, ...) rather than an \Exception. The
// dispatcher's `catch (\Exception $e)` blocks do not see those.
type PHPError interface {
	error
	IsPHPError() bool
}

func isPHPError(err error) bool {
	e, ok := err.(PHPError) //nolint:errorlint // catch inspects the thrown object itself.

	return ok && e.IsPHPError()
}

func runtimeError(line int, message string) *Error {
	return &Error{Class: "RuntimeException", Message: message, Line: line}
}
