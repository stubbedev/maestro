// The downloaders' exceptions. TransportException and
// MaxFileSizeExceededException are util.TransportError and
// util.MaxFileSizeExceededError (Composer\Util's HTTP layer throws them).
// FilesystemException has no type: only SelfUpdateCommand throws it, and
// maestro's self-update gives its command.Error that class.

package downloader

import (
	"errors"

	"github.com/stubbedev/maestro/internal/util"
)

// isIrrecoverable is $e instanceof IrrecoverableDownloadException.
func isIrrecoverable(err error) bool {
	var e *util.IrrecoverableDownloadError

	return errors.As(err, &e)
}

// PHPClassOf forwards to util.PHPClassOf, which owns it.
func PHPClassOf(err error) (string, int) { return util.PHPClassOf(err) }
