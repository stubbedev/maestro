// The PHP exception classes Composer\Util throws, as Go errors carrying the
// exact message, so callers can tell them apart with errors.As as Composer
// tells them apart with catch.

package util

import (
	"errors"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/semver"
)

// RuntimeError is PHP's \RuntimeException.
type RuntimeError struct{ Message string }

func (e *RuntimeError) Error() string { return e.Message }

// InvalidArgumentError is PHP's \InvalidArgumentException.
type InvalidArgumentError struct{ Message string }

func (e *InvalidArgumentError) Error() string { return e.Message }

// LogicError is PHP's \LogicException.
type LogicError struct{ Message string }

func (e *LogicError) Error() string { return e.Message }

// ErrorException is the \ErrorException Composer's ErrorHandler turns a PHP
// warning into, e.g. "copy(a): Failed to open stream: No such file or
// directory".
type ErrorException struct{ Message string }

func (e *ErrorException) Error() string { return e.Message }

// IOError is Symfony's Filesystem IOException, carrying the path involved.
type IOError struct {
	Message string
	Path    string
}

func (e *IOError) Error() string { return e.Message }

// UnexpectedValueError is PHP's \UnexpectedValueException.
type UnexpectedValueError struct{ Message string }

func (e *UnexpectedValueError) Error() string { return e.Message }

// SecurityError is Composer\Exception\SecurityException.
type SecurityError struct{ Message string }

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
		classer          PHPClasser
		errExc           *ErrorException
	)

	switch {
	case errors.As(err, &maxSize):
		return `Composer\Downloader\MaxFileSizeExceededException`, maxSize.Code
	case errors.As(err, &transport):
		return `Composer\Downloader\TransportException`, transport.Code
	case errors.As(err, &irrecov):
		return `Composer\Exception\IrrecoverableDownloadException`, 0
	case errors.As(err, &unexpected), errors.As(err, &semverUnexpected):
		return "UnexpectedValueException", 0
	case errors.As(err, &invalid), errors.As(err, &semverInvalid):
		return "InvalidArgumentException", 0
	case errors.As(err, &logic):
		return "LogicException", 0
	case errors.As(err, &classer):
		return classer.PHPClass()
	case errors.As(err, &errExc):
		return "ErrorException", 0
	}

	return "RuntimeException", 0
}
