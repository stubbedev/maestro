// The PHP exceptions Composer's commands and Application throw, as errors
// that carry their class (which plugins see).

package command

import (
	"github.com/stubbedev/maestro/internal/phperr"
)

// PHP exception classes used by the commands.
const (
	ClassRuntime         = "RuntimeException"
	ClassInvalidArgument = "InvalidArgumentException"
	ClassLogic           = "LogicException"
	ClassUnexpectedValue = "UnexpectedValueException"
)

// Error is a PHP exception thrown by a command or the Application: Class
// is get_class($e), Code getCode(), Prev getPrevious() (phperr.Chained,
// which `catch` does not look at). Its catches test its class with
// phperr.InstanceOf.
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

// PHPClass implements phperr.Exception.
func (e *Error) PHPClass() string { return e.Class }

// PHPCode implements phperr.Coded.
func (e *Error) PHPCode() int { return e.Code }

// PHPPrevious implements phperr.Chained.
func (e *Error) PHPPrevious() error { return e.Prev }

// ExitCoder is implemented by errors that stand for the end of the
// process with a status rather than an exception: PHP's exit($code) in
// plugin code (the plugin runtime's PHPExit) or a fatal error. The
// Application never renders them; Run returns ExitCode() and cmd/maestro
// exits with it.
type ExitCoder interface {
	error
	ExitCode() int
}

// uncaughtExitCode is the exit status of PHP's uncaught fatal error.
const uncaughtExitCode = 255

// uncaught is a PHP \Error that escapes all of Composer's handling: thrown
// in doRun before its try, or raised again by hintCommonErrors (whose
// catch only takes \Exception). Symfony's run() does not catch \Error
// either, so PHP ends with "Fatal error: Uncaught ..." and exit status
// 255. maestro renders it as it renders any error, with that status.
func uncaught(err error) error { return withExitCode(err, uncaughtExitCode) }

// withExitCode is err with the exception code code: the TransportException
// code Application::doRun sets through reflection for parent::run() to
// exit with, or the status of an uncaught \Error. err itself when that is
// its code already.
func withExitCode(err error, code int) error {
	if _, c := phperr.ClassOf(err); c == code {
		return err
	}

	return &codeOverride{err: err, code: code}
}

// codeOverride is an exception whose code was replaced; it is the
// exception err stands for in every other way (its previous exception
// too: phperr.PreviousOf looks through a wrapper with err's message).
type codeOverride struct {
	err  error
	code int
}

func (c *codeOverride) Error() string { return c.err.Error() }

func (c *codeOverride) Unwrap() error { return c.err }

// PHPClass implements phperr.Exception.
func (c *codeOverride) PHPClass() string { return phperr.Class(c.err) }

// PHPCode implements phperr.Coded.
func (c *codeOverride) PHPCode() int { return c.code }

// InstanceOf is err's: a code changes no class.
func (c *codeOverride) InstanceOf(class string) bool { return phperr.InstanceOf(c.err, class) }
