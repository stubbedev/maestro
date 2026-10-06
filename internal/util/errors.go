// The PHP exception classes Composer\Util throws, as Go errors carrying the
// exact message, so callers can tell them apart with errors.As as Composer
// tells them apart with catch.

package util

import (
	"errors"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/semver"
)

// RuntimeError is PHP's \RuntimeException, or a library's subclass of it
// (Class).
type RuntimeError struct {
	Message string
	// Prev is the $previous exception, rendered after this one (it is
	// not unwrapped: PHP's catch does not look at it).
	Prev error
	// Class is get_class($e) of a library's subclass of \RuntimeException
	// that adds nothing Composer catches (symfony/process's
	// Symfony\Component\Process\Exception\RuntimeException); empty for
	// \RuntimeException itself.
	Class string
}

func (e *RuntimeError) Error() string { return e.Message }

// PHPPrevious implements phperr.Chained.
func (e *RuntimeError) PHPPrevious() error { return e.Prev }

// InvalidArgumentError is PHP's \InvalidArgumentException, or a library's
// subclass of it (Class).
type InvalidArgumentError struct {
	Message string
	// Prev is the $previous exception, rendered after this one (it is
	// not unwrapped: PHP's catch does not look at it).
	Prev error
	// Class is get_class($e) of a library's subclass of
	// \InvalidArgumentException (symfony/finder's
	// DirectoryNotFoundException, symfony/process's
	// InvalidArgumentException); empty for \InvalidArgumentException
	// itself.
	Class string
}

func (e *InvalidArgumentError) Error() string { return e.Message }

// PHPPrevious implements phperr.Chained.
func (e *InvalidArgumentError) PHPPrevious() error { return e.Prev }

// LogicError is PHP's \LogicException, or a library's subclass of it
// (Class).
type LogicError struct {
	Message string
	// Prev is the $previous exception, rendered after this one (it is
	// not unwrapped: PHP's catch does not look at it).
	Prev error
	// Class is get_class($e) of a library's subclass of \LogicException
	// (symfony/process's Symfony\Component\Process\Exception\LogicException);
	// empty for \LogicException itself.
	Class string
}

func (e *LogicError) Error() string { return e.Message }

// PHPPrevious implements phperr.Chained.
func (e *LogicError) PHPPrevious() error { return e.Prev }

// ErrorException is the \ErrorException Composer's ErrorHandler turns a PHP
// warning into, e.g. "copy(a): Failed to open stream: No such file or
// directory".
type ErrorException struct {
	Message string
}

func (e *ErrorException) Error() string { return e.Message }

// IOError is symfony/filesystem's IOException, carrying the path involved,
// or its subclass FileNotFoundException (Class).
type IOError struct {
	Message string
	Path    string
	// Class is get_class($e) of a subclass of IOException
	// (ClassFileNotFound); empty for IOException itself.
	Class string
}

// The symfony/filesystem, symfony/finder and symfony/process exception
// classes maestro represents with util's generic error types (their Class).
const (
	ClassIOException       = `Symfony\Component\Filesystem\Exception\IOException`
	ClassFileNotFound      = `Symfony\Component\Filesystem\Exception\FileNotFoundException`
	ClassDirectoryNotFound = `Symfony\Component\Finder\Exception\DirectoryNotFoundException`
	ClassAccessDenied      = `Symfony\Component\Finder\Exception\AccessDeniedException`
	ClassProcessRuntime    = `Symfony\Component\Process\Exception\RuntimeException`
	ClassProcessLogic      = `Symfony\Component\Process\Exception\LogicException`
	ClassProcessInvalidArg = `Symfony\Component\Process\Exception\InvalidArgumentException`
)

func (e *IOError) Error() string { return e.Message }

// UnexpectedValueError is PHP's \UnexpectedValueException, or a library's
// subclass of it (Class).
type UnexpectedValueError struct {
	Message string
	// Prev is the $previous exception, rendered after this one (it is
	// not unwrapped: PHP's catch does not look at it).
	Prev error
	// Class is get_class($e) of a library's subclass of
	// \UnexpectedValueException (symfony/finder's AccessDeniedException);
	// empty for \UnexpectedValueException itself.
	Class string
}

func (e *UnexpectedValueError) Error() string { return e.Message }

// PHPPrevious implements phperr.Chained.
func (e *UnexpectedValueError) PHPPrevious() error { return e.Prev }

// SecurityError is Composer\Exception\SecurityException.
type SecurityError struct {
	Message string
}

func (e *SecurityError) Error() string { return e.Message }

// IsRuntimeException reports whether PHP would see err as a
// \RuntimeException (`catch (\RuntimeException $e)`): RuntimeError,
// TransportError (and MaxFileSizeExceededError), UnexpectedValueError,
// SecurityError, IOError, IrrecoverableDownloadError, the process
// failures and PCRE errors.
func IsRuntimeException(err error) bool {
	var (
		runtime    *RuntimeError
		transport  *TransportError
		unexpected *UnexpectedValueError
		security   *SecurityError
		ioErr      *IOError
		irrecov    *IrrecoverableDownloadError
		timedOut   *ProcessTimedOutError
		signaled   *ProcessSignaledError
		pcre       *php.PcreError
	)

	return errors.As(err, &runtime) || errors.As(err, &transport) || errors.As(err, &unexpected) ||
		errors.As(err, &security) || errors.As(err, &ioErr) || errors.As(err, &irrecov) ||
		errors.As(err, &timedOut) || errors.As(err, &signaled) || errors.As(err, &pcre)
}

// PHPClasser is implemented by error types of packages above util that
// stand for a PHP exception class PHPClassOf cannot know, such as
// Composer\Downloader\FilesystemException.
type PHPClasser interface {
	error
	// PHPClass returns get_class($e) and $e->getCode().
	PHPClass() (class string, code int)
}

// PHPClassOf names err's PHP exception class and code, as get_class($e) and
// $e->getCode() show them; errors it does not recognise are
// RuntimeException.
func PHPClassOf(err error) (string, int) {
	var (
		maxSize          *MaxFileSizeExceededError
		transport        *TransportError
		irrecov          *IrrecoverableDownloadError
		unexpected       *UnexpectedValueError
		semverUnexpected *semver.UnexpectedValueError
		invalid          *InvalidArgumentError
		semverInvalid    *semver.InvalidArgumentError
		logic            *LogicError
		runtime          *RuntimeError
		ioErr            *IOError
		classer          PHPClasser
		errExc           *ErrorException
		engine           *php.EngineError
	)

	switch {
	case errors.As(err, &maxSize):
		return `Composer\Downloader\MaxFileSizeExceededException`, maxSize.Code
	case errors.As(err, &transport):
		return `Composer\Downloader\TransportException`, transport.Code
	case errors.As(err, &irrecov):
		return `Composer\Exception\IrrecoverableDownloadException`, 0
	case errors.As(err, &unexpected):
		return orClass(unexpected.Class, "UnexpectedValueException"), 0
	case errors.As(err, &semverUnexpected):
		return "UnexpectedValueException", 0
	case errors.As(err, &invalid):
		return orClass(invalid.Class, "InvalidArgumentException"), 0
	case errors.As(err, &semverInvalid):
		return "InvalidArgumentException", 0
	case errors.As(err, &logic):
		return orClass(logic.Class, "LogicException"), 0
	case errors.As(err, &classer):
		return classer.PHPClass()
	case errors.As(err, &errExc):
		return "ErrorException", 0
	case errors.As(err, &engine):
		return engine.Class, 0
	case errors.As(err, &ioErr):
		return orClass(ioErr.Class, ClassIOException), 0
	case errors.As(err, &runtime):
		return orClass(runtime.Class, "RuntimeException"), 0
	}

	return "RuntimeException", 0
}

// orClass is class, or base when the error is of the base class itself.
func orClass(class, base string) string {
	if class != "" {
		return class
	}

	return base
}

// PHPClass implements PHPClasser: Composer\Exception\SecurityException.
func (*SecurityError) PHPClass() (string, int) { return `Composer\Exception\SecurityException`, 0 }
