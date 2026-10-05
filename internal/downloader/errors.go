// Ports src/Composer/Downloader/FilesystemException.php. TransportException
// and MaxFileSizeExceededException are util.TransportError and
// util.MaxFileSizeExceededError (Composer\Util's HTTP layer throws them).

package downloader

import (
	"errors"

	"github.com/stubbedev/maestro/internal/util"
)

// FilesystemError is Composer\Downloader\FilesystemException.
type FilesystemError struct {
	Message  string
	Code     int
	Previous error
}

// NewFilesystemError is new FilesystemException($message, $code,
// $previous): the message gets Composer's "Filesystem exception: " prefix.
func NewFilesystemError(message string, code int, previous error) *FilesystemError {
	return &FilesystemError{Message: "Filesystem exception: \n" + message, Code: code, Previous: previous}
}

func (e *FilesystemError) Error() string { return e.Message }

// Unwrap returns the previous exception.
func (e *FilesystemError) Unwrap() error { return e.Previous }

// isIrrecoverable is $e instanceof IrrecoverableDownloadException.
func isIrrecoverable(err error) bool {
	var e *util.IrrecoverableDownloadError

	return errors.As(err, &e)
}

// PHPClassOf names err's PHP exception class and code, as get_class($e) and
// $e->getCode() show them.
func PHPClassOf(err error) (string, int) {
	var (
		maxSize    *util.MaxFileSizeExceededError
		transport  *util.TransportError
		irrecov    *util.IrrecoverableDownloadError
		unexpected *util.UnexpectedValueError
		invalid    *util.InvalidArgumentError
		logic      *util.LogicError
		fsErr      *FilesystemError
		errExc     *util.ErrorException
	)

	switch {
	case errors.As(err, &maxSize):
		return `Composer\Downloader\MaxFileSizeExceededException`, maxSize.Code
	case errors.As(err, &transport):
		return `Composer\Downloader\TransportException`, transport.Code
	case errors.As(err, &irrecov):
		return `Composer\Exception\IrrecoverableDownloadException`, 0
	case errors.As(err, &unexpected):
		return "UnexpectedValueException", 0
	case errors.As(err, &invalid):
		return "InvalidArgumentException", 0
	case errors.As(err, &logic):
		return "LogicException", 0
	case errors.As(err, &fsErr):
		return `Composer\Downloader\FilesystemException`, fsErr.Code
	case errors.As(err, &errExc):
		return "ErrorException", 0
	}

	return "RuntimeException", 0
}
