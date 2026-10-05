// The PHP exceptions Composer's commands and Application throw, as errors
// that carry their class and throw site so the Application renders them as
// Symfony does ("In ConfigCommand.php line 214:").

package command

import (
	"errors"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/json/jsonlint"
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
// is get_class($e), File the basename of the PHP source that throws it and
// Line its line there (both shown when it is rendered), Code getCode().
// It unwraps to the util error of the same class (util.RuntimeError,
// util.InvalidArgumentError, ...), so errors.As works as `catch` would.
type Error struct {
	Class   string
	Message string
	File    string
	Line    int
	Code    int
	Prev    error
}

// NewError returns an Error.
func NewError(class, file string, line int, message string) *Error {
	return &Error{Class: class, Message: message, File: file, Line: line}
}

func (e *Error) Error() string { return e.Message }

// Unwrap returns the util error matching the class, for errors.As.
func (e *Error) Unwrap() []error {
	errs := make([]error, 0, 2)
	switch e.Class {
	case ClassRuntime:
		errs = append(errs, &util.RuntimeError{Message: e.Message})
	case ClassInvalidArgument:
		errs = append(errs, &util.InvalidArgumentError{Message: e.Message})
	case ClassLogic:
		errs = append(errs, &util.LogicError{Message: e.Message})
	case ClassUnexpectedValue:
		errs = append(errs, &util.UnexpectedValueError{Message: e.Message})
	}
	if e.Prev != nil {
		errs = append(errs, e.Prev)
	}

	return errs
}

// ThrowableClass implements console.Throwable.
func (e *Error) ThrowableClass() string { return e.Class }

// ThrowableFile implements console.Throwable.
func (e *Error) ThrowableFile() string { return e.File }

// ThrowableLine implements console.Throwable.
func (e *Error) ThrowableLine() int { return e.Line }

// ThrowableCode implements console.Throwable.
func (e *Error) ThrowableCode() int { return e.Code }

// ThrowablePrevious implements console.Throwable.
func (e *Error) ThrowablePrevious() error { return e.Prev }

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

// throwable is how the Application presents an error that is not a
// console.Throwable to Symfony's rendering: its PHP class (util.PHPClassOf)
// and code. The throw site is unknown ("In n/a line n/a:") unless the error
// implements console.Throwable itself, which lower packages may do.
type throwable struct {
	err   error
	class string
	code  int
}

func (t *throwable) Error() string           { return t.err.Error() }
func (t *throwable) Unwrap() error           { return t.err }
func (t *throwable) ThrowableClass() string  { return t.class }
func (*throwable) ThrowableFile() string     { return "" }
func (*throwable) ThrowableLine() int        { return 0 }
func (t *throwable) ThrowableCode() int      { return t.code }
func (*throwable) ThrowablePrevious() error  { return nil }
func (t *throwable) PHPClass() (string, int) { return t.class, t.code }

// asThrowable gives err the PHP exception details Symfony's renderer and
// exit code use. code overrides the exception code when >= 0.
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
	if site, ok := knownThrowSite(err); ok {
		site.code = c
		site.err = err

		return site
	}

	return &throwable{err: err, class: class, code: c}
}

// sitedThrowable is a throwable whose PHP throw site is known.
type sitedThrowable struct {
	throwable
	file string
	line int
}

func newSited(class, file string, line int) *sitedThrowable {
	s := &sitedThrowable{file: file, line: line}
	s.class = class

	return s
}

func (s *sitedThrowable) ThrowableFile() string { return s.file }
func (s *sitedThrowable) ThrowableLine() int    { return s.line }

// knownThrowSite gives the class and throw site of the errors of lower
// packages that commonly reach the user, which carry no site themselves:
// invalid and schema-violating JSON files.
func knownThrowSite(err error) (*sitedThrowable, bool) {
	if ve, ok := errors.AsType[*json.ValidationError](err); ok && ve == err { //nolint:errorlint // the object itself
		if ve.Errors == nil {
			// Factory::createComposer rethrows with the errors in the message
			return newSited(`Composer\Json\JsonValidationException`, "Factory.php", 317), true
		}

		return newSited(`Composer\Json\JsonValidationException`, "JsonFile.php", 265), true
	}
	if pe, ok := err.(*jsonlint.ParsingError); ok { //nolint:errorlint // the object itself
		switch {
		case strings.HasPrefix(pe.Message, "The input does not contain valid JSON"):
			return newSited(`Seld\JsonLint\ParsingException`, "JsonFile.php", 393), true
		case strings.Contains(pe.Message, "\n"):
			return newSited(`Seld\JsonLint\ParsingException`, "JsonFile.php", 398), true
		default:
			return newSited(`Seld\JsonLint\ParsingException`, "Response.php", 98), true
		}
	}

	return nil, false
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
