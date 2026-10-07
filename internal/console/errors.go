// Ports src/Exception/*.php (symfony/console).

package console

import (
	"fmt"
	"runtime"

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

// ClassCommandNotFound is the class of a command or namespace that
// cannot be found (KindCommandNotFound, KindNamespaceNotFound).
const ClassCommandNotFound = `Symfony\Component\Console\Exception\CommandNotFoundException`

var kindClass = [...]string{
	KindInvalidArgument:   `Symfony\Component\Console\Exception\InvalidArgumentException`,
	KindInvalidOption:     `Symfony\Component\Console\Exception\InvalidOptionException`,
	KindCommandNotFound:   ClassCommandNotFound,
	KindNamespaceNotFound: `Symfony\Component\Console\Exception\NamespaceNotFoundException`,
	KindLogic:             `Symfony\Component\Console\Exception\LogicException`,
	KindRuntime:           `Symfony\Component\Console\Exception\RuntimeException`,
	KindMissingInput:      `Symfony\Component\Console\Exception\MissingInputException`,
	KindSPLRuntime:        `RuntimeException`,
	KindSPLLogic:          `LogicException`,

	KindValueError:            `ValueError`,
	KindStringInvalidArgument: `Symfony\Component\String\Exception\InvalidArgumentException`,
}

// Error is a console exception.
type Error struct {
	Kind         Kind
	Message      string
	Alternatives []string // CommandNotFoundException::getAlternatives()
	Prev         error
}

// newError returns a console exception.
func newError(kind Kind, format string, args ...any) *Error {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}

	return &Error{Kind: kind, Message: msg}
}

func (e *Error) Error() string { return e.Message }

// PHPClass implements phperr.Exception.
func (e *Error) PHPClass() string { return kindClass[e.Kind] }

// PHPPrevious implements phperr.Chained.
func (e *Error) PHPPrevious() error { return e.Prev }

// IsConsoleException reports whether err stands for an exception
// implementing Symfony's ExceptionInterface.
func IsConsoleException(err error) bool { return phperr.InstanceOf(err, phperr.ClassConsoleException) }

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

// recoverThrowable converts a panic raised with an error, the way
// completion callbacks and sprintf throw, back into the error. Any other
// panic value, a Go runtime error included, is re-raised.
func recoverThrowable(r any) error {
	if err, ok := r.(error); ok {
		if _, isRuntime := err.(runtime.Error); !isRuntime { //nolint:errorlint // the panic value itself
			return err
		}
	}
	panic(r)
}
