// The PHP exception classes Composer\Util throws, as Go errors carrying the
// exact message, so callers can tell them apart with errors.As as Composer
// tells them apart with catch.

package util

import (
	"errors"

	"github.com/stubbedev/maestro/internal/php"
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
