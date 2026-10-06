// Ports src/Exception/*.php (symfony/console).

package console

import (
	"errors"
	"fmt"

	"github.com/stubbedev/maestro/internal/phperr"
)

// Kind identifies the PHP exception class of an Error.
type Kind int

// The Symfony console exception classes, plus the SPL classes the console
// code throws directly.
const (
	KindInvalidArgument       Kind = iota // Symfony\Component\Console\Exception\InvalidArgumentException
	KindInvalidOption                     // ...\InvalidOptionException (extends InvalidArgumentException)
	KindCommandNotFound                   // ...\CommandNotFoundException (extends \InvalidArgumentException)
	KindNamespaceNotFound                 // ...\NamespaceNotFoundException (extends CommandNotFoundException)
	KindLogic                             // ...\LogicException
	KindRuntime                           // ...\RuntimeException
	KindMissingInput                      // ...\MissingInputException (extends RuntimeException)
	KindSPLRuntime                        // \RuntimeException (thrown as-is by CompleteCommand)
	KindSPLLogic                          // \LogicException (thrown as-is by CompletionInput)
	KindValueError                        // \ValueError (PHP engine argument validation)
	KindStringInvalidArgument             // Symfony\Component\String\Exception\InvalidArgumentException (symfony/string, via the wrapping formatter)
)

var kindClass = [...]string{
	KindInvalidArgument:   `Symfony\Component\Console\Exception\InvalidArgumentException`,
	KindInvalidOption:     `Symfony\Component\Console\Exception\InvalidOptionException`,
	KindCommandNotFound:   `Symfony\Component\Console\Exception\CommandNotFoundException`,
	KindNamespaceNotFound: `Symfony\Component\Console\Exception\NamespaceNotFoundException`,
	KindLogic:             `Symfony\Component\Console\Exception\LogicException`,
	KindRuntime:           `Symfony\Component\Console\Exception\RuntimeException`,
	KindMissingInput:      `Symfony\Component\Console\Exception\MissingInputException`,
	KindSPLRuntime:        `RuntimeException`,
	KindSPLLogic:          `LogicException`,

	KindValueError:            `ValueError`,
	KindStringInvalidArgument: `Symfony\Component\String\Exception\InvalidArgumentException`,
}

// Sentinels for errors.Is, following the PHP class hierarchy: a
// NamespaceNotFound error Is ErrCommandNotFound and ErrInvalidArgument, an
// InvalidOption error Is ErrInvalidArgument, a MissingInput error Is
// ErrRuntime. ErrConsole matches every error implementing Symfony's
// ExceptionInterface.
var (
	ErrConsole          = errors.New("console exception")
	ErrInvalidArgument  = errors.New("invalid argument")
	ErrCommandNotFound  = errors.New("command not found")
	ErrNamespaceMissing = errors.New("namespace not found")
	ErrInvalidOption    = errors.New("invalid option")
	ErrLogic            = errors.New("logic error")
	ErrRuntime          = errors.New("runtime error")
	ErrMissingInput     = errors.New("missing input")
)

// Error is a console exception. File and Line point at the PHP throw site
// (getFile(), getLine()).
type Error struct {
	Kind         Kind
	Message      string
	Alternatives []string // CommandNotFoundException::getAlternatives()
	File         string   // the PHP file that throws (as phperr.At takes it)
	Line         int
	Prev         error
	phperr.Frames
}

// newError returns the console exception thrown at file:line, a file of
// symfony/console (Application.php is its own, not Composer's).
func newError(kind Kind, file string, line int, format string, args ...any) *Error {
	if file == "Application.php" {
		file = "vendor/symfony/console/Application.php"
	}
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}

	return &Error{Kind: kind, Message: msg, File: file, Line: line}
}

func (e *Error) Error() string { return e.Message }

// Is implements the PHP class hierarchy for errors.Is.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrConsole:
		return e.Kind != KindSPLRuntime && e.Kind != KindSPLLogic && e.Kind != KindStringInvalidArgument && e.Kind != KindValueError
	case ErrInvalidArgument:
		return e.Kind == KindInvalidArgument || e.Kind == KindInvalidOption ||
			e.Kind == KindCommandNotFound || e.Kind == KindNamespaceNotFound || e.Kind == KindStringInvalidArgument
	case ErrCommandNotFound:
		return e.Kind == KindCommandNotFound || e.Kind == KindNamespaceNotFound
	case ErrNamespaceMissing:
		return e.Kind == KindNamespaceNotFound
	case ErrInvalidOption:
		return e.Kind == KindInvalidOption
	case ErrLogic:
		return e.Kind == KindLogic || e.Kind == KindSPLLogic
	case ErrRuntime:
		return e.Kind == KindRuntime || e.Kind == KindMissingInput || e.Kind == KindSPLRuntime
	case ErrMissingInput:
		return e.Kind == KindMissingInput
	}

	return false
}

func (e *Error) Unwrap() error { return e.Prev }

// ThrowableClass implements Throwable.
func (e *Error) ThrowableClass() string { return kindClass[e.Kind] }

// ThrowableCode implements Throwable.
func (e *Error) ThrowableCode() int { return 0 }

// ThrowablePrevious implements Throwable.
func (e *Error) ThrowablePrevious() error { return e.Prev }

// PHPPrevious implements phperr.Chained.
func (e *Error) PHPPrevious() error { return e.Prev }

// IsConsoleException reports whether err (or anything it wraps) implements
// Symfony's ExceptionInterface.
func IsConsoleException(err error) bool { return errors.Is(err, ErrConsole) }

// Throwable is implemented by errors that carry PHP exception details: the
// class name (get_debug_type), the exception code (the exit code of an uncaught one) and the previous
// exception. Plain Go errors are an "Exception" with code 0.
type Throwable interface {
	error
	ThrowableClass() string
	ThrowableCode() int
	ThrowablePrevious() error
}

// TraceFrame is one frame of a PHP exception trace.
type TraceFrame struct {
	Class, Type, Function, File string
	Line                        int
}

// Tracer is optionally implemented by Throwables PHP code threw (a
// plugin's), whose trace RenderThrowable shows at -v.
type Tracer interface {
	ThrowableTrace() []TraceFrame
}

// recoverThrowable converts a panic raised with an error value back into an
// error. Any other panic value is re-raised.
func recoverThrowable(r any) error {
	if err, ok := r.(error); ok {
		if _, ok := errors.AsType[Throwable](err); ok {
			return err
		}
	}
	panic(r)
}
