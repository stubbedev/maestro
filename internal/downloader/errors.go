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

// PHPClass implements util.PHPClasser.
func (e *FilesystemError) PHPClass() (string, int) {
	return `Composer\Downloader\FilesystemException`, e.Code
}

// Unwrap returns the previous exception.
func (e *FilesystemError) Unwrap() error { return e.Previous }

// PHPPrevious implements phperr.Chained.
func (e *FilesystemError) PHPPrevious() error { return e.Previous }

// isIrrecoverable is $e instanceof IrrecoverableDownloadException.
func isIrrecoverable(err error) bool {
	var e *util.IrrecoverableDownloadError

	return errors.As(err, &e)
}

// PHPClassOf forwards to util.PHPClassOf, which owns it.
func PHPClassOf(err error) (string, int) { return util.PHPClassOf(err) }
