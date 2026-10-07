// The PHP exceptions Composer's commands and Application throw, as errors
// that carry their class (which plugins see).

package command

import (
	"errors"
	"strconv"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// PHP exception classes used by the commands.
const (
	ClassRuntime         = "RuntimeException"
	ClassInvalidArgument = "InvalidArgumentException"
	ClassLogic           = "LogicException"
	ClassUnexpectedValue = "UnexpectedValueException"
)

// Error is a PHP exception thrown by a command or the Application: Class
// is get_class($e), Code getCode().
// It unwraps to the util error of the same class (util.RuntimeError,
// util.InvalidArgumentError, ...), so errors.As works as `catch` would;
// Prev is getPrevious() (phperr.Chained), which `catch` does not look at.
type Error struct {
	Class   string
	Message string
	Code    int
	Prev    error
}

// NewError returns an Error.
func NewError(class, message string) *Error {
	return &Error{Class: class, Message: message}
}

func (e *Error) Error() string { return e.Message }

// Unwrap returns the util error matching the class, for errors.As, or nil.
func (e *Error) Unwrap() error {
	switch e.Class {
	case ClassRuntime:
		return &util.RuntimeError{Message: e.Message}
	case ClassInvalidArgument:
		return &util.InvalidArgumentError{Message: e.Message}
	case ClassLogic:
		return &util.LogicError{Message: e.Message}
	case ClassUnexpectedValue:
		return &util.UnexpectedValueError{Message: e.Message}
	}

	return nil
}

// ThrowableClass implements console.Throwable.
func (e *Error) ThrowableClass() string { return e.Class }

// ThrowableCode implements console.Throwable.
func (e *Error) ThrowableCode() int { return e.Code }

// PHPPrevious implements phperr.Chained.
func (e *Error) PHPPrevious() error { return e.Prev }

// ThrowablePrevious implements console.Throwable.
func (e *Error) ThrowablePrevious() error {
	if e.Prev == nil {
		return nil
	}

	return asThrowable(e.Prev, -1)
}

// ExitCoder is implemented by errors that stand for the end of the
// process with a status rather than an exception: PHP's exit($code) in
// plugin code (the plugin runtime's PHPExit) or a fatal error. The
// Application never renders them; Run returns ExitCode() and cmd/maestro
// exits with it.
type ExitCoder interface {
	error
	ExitCode() int
}

// ExitError is an ExitCoder carrying Code.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return "exit " + strconv.Itoa(e.Code) }

// ExitCode implements ExitCoder.
func (e *ExitError) ExitCode() int { return e.Code }

// uncaughtExitCode is the exit status of PHP's uncaught fatal error.
const uncaughtExitCode = 255

// uncaught is a PHP \Error that escapes all of Composer's handling: thrown
// in doRun before its try, or raised again by hintCommonErrors (whose
// catch only takes \Exception). Symfony's run() does not catch \Error
// either, so PHP ends with "Fatal error: Uncaught ..." and exit status
// 255. maestro renders it as it renders any error (#13), with that status.
func uncaught(err error) error { return asThrowable(err, uncaughtExitCode) }

// throwable is how the Application presents an error that is not a
// console.Throwable to the console: its PHP class (util.PHPClassOf), code
// and previous exception (phperr.PreviousOf).
type throwable struct {
	err   error
	class string
	code  int
	prev  error
}

func (t *throwable) Error() string            { return t.err.Error() }
func (t *throwable) Unwrap() error            { return t.err }
func (t *throwable) ThrowableClass() string   { return t.class }
func (t *throwable) ThrowableCode() int       { return t.code }
func (t *throwable) ThrowablePrevious() error { return t.prev }
func (t *throwable) PHPClass() (string, int)  { return t.class, t.code }

// asThrowable gives err the PHP exception details the console's rendering
// and exit code use. code overrides the exception code when >= 0.
func asThrowable(err error, code int) error {
	if t, ok := err.(console.Throwable); ok { //nolint:errorlint // PHP inspects the exception object itself.
		if code < 0 || t.ThrowableCode() == code {
			return err
		}

		return &codeOverride{Throwable: t, code: code}
	}
	class, c := util.PHPClassOf(err)
	if code >= 0 {
		c = code
	}
	t := &throwable{err: err, class: class, code: c}
	if prev := phperr.PreviousOf(err); prev != nil {
		t.prev = asThrowable(prev, -1)
	}

	return t
}

// codeOverride is a Throwable whose code Application::doRun replaced (the
// TransportException code set through reflection).
type codeOverride struct {
	console.Throwable
	code int
}

func (c *codeOverride) ThrowableCode() int { return c.code }
func (c *codeOverride) Unwrap() error      { return c.Throwable }

// phpClass is get_class($e) for any error.
func phpClass(err error) string {
	if t, ok := err.(console.Throwable); ok { //nolint:errorlint // get_class of the object itself.
		return t.ThrowableClass()
	}
	class, _ := util.PHPClassOf(err)

	return class
}

// isInvalidArgument is `$e instanceof \InvalidArgumentException`.
func isInvalidArgument(err error) bool {
	if errors.Is(err, console.ErrInvalidArgument) {
		return true
	}
	if _, ok := errors.AsType[*util.InvalidArgumentError](err); ok {
		return true
	}
	if e, ok := errors.AsType[*Error](err); ok && e.Class == ClassInvalidArgument {
		return true
	}
	class, _ := util.PHPClassOf(err)

	return class == ClassInvalidArgument
}

// isPHPError is `$e instanceof \Error` for a class name: PHP's engine
// errors, which are not Exceptions.
func isPHPError(class string) bool {
	switch class {
	case "Error", "TypeError", "ValueError", "ArgumentCountError", "ArithmeticError", "DivisionByZeroError",
		"CompileError", "ParseError", "UnhandledMatchError", "AssertionError", "FiberError":
		return true
	}

	return false
}
