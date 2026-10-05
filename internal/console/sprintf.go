// PHP 8's sprintf() (internal/php's port of php_formatted_print()) as the
// console throws it, used for ProgressBar placeholders ("%percent:3s%"), Table
// formats, ChoiceQuestion error messages and the synopsis rendered after an
// exception.

package console

import (
	"errors"

	"github.com/stubbedev/maestro/internal/php"
)

// phpFormatError is the ValueError or ArgumentCountError sprintf() throws
// for a bad format.
type phpFormatError struct {
	class   string
	message string
}

func (e *phpFormatError) Error() string { return e.message }

// ThrowableClass implements Throwable.
func (e *phpFormatError) ThrowableClass() string { return e.class }

// ThrowableFile implements Throwable (the PHP caller's file is unknown).
func (e *phpFormatError) ThrowableFile() string { return "" }

// ThrowableLine implements Throwable.
func (e *phpFormatError) ThrowableLine() int { return 0 }

// ThrowableCode implements Throwable.
func (e *phpFormatError) ThrowableCode() int { return 0 }

// ThrowablePrevious implements Throwable.
func (e *phpFormatError) ThrowablePrevious() error { return nil }

func formatPanic(class, message string) { panic(&phpFormatError{class, message}) }

// phpSprintf formats args like PHP 8's sprintf(). A bad format panics with
// a *phpFormatError carrying PHP's ValueError or ArgumentCountError.
func phpSprintf(format string, args ...any) string {
	s, err := php.Sprintf(format, args...)
	if err != nil {
		if e, ok := errors.AsType[*php.EngineError](err); ok {
			formatPanic(e.Class, e.Message)
		}
		panic(err)
	}

	return s
}
