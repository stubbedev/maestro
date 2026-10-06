// Ports the deprecation path of src/Composer/Util/ErrorHandler.php: how
// Composer reports the E_USER_DEPRECATED notices it raises with
// trigger_error() (its other levels become *ErrorException).

package util

import (
	"strconv"
	"sync"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
)

// errorHandler is ErrorHandler's static state.
var errorHandler struct {
	mu sync.Mutex
	io io.IO
	// shown is $hasShownDeprecationNotice: 0, 1 once a notice was
	// printed, 2 once the "hidden" hint was printed.
	shown int
}

// RegisterErrorHandler ports ErrorHandler::register($io): the IO
// deprecation notices are written to (Application::doRun registers its
// ConsoleIO; before that, as after bin/composer's register(), notices
// are not shown).
func RegisterErrorHandler(out io.IO) {
	errorHandler.mu.Lock()
	defer errorHandler.mu.Unlock()

	errorHandler.io = out
}

// ResetErrorHandler forgets the registered IO and the notices shown (a
// new PHP process); for tests.
func ResetErrorHandler() {
	errorHandler.mu.Lock()
	defer errorHandler.mu.Unlock()

	errorHandler.io, errorHandler.shown = nil, 0
}

// TriggerDeprecation ports trigger_error($message, E_USER_DEPRECATED) as
// ErrorHandler::handle reports it: the first notice as "Deprecation
// Notice: <message> in <file>:<line>", later ones only in verbose mode
// (otherwise a single hint that more were hidden). site is the
// trigger_error call in Composer.
//
// PHP prints the absolute path of Composer's source file and, in verbose
// mode, its whole call stack (debug_backtrace); maestro names the file by
// its basename, as the exception renderer does, and lists only that frame.
func TriggerDeprecation(message string, site phperr.Site) {
	errorHandler.mu.Lock()
	defer errorHandler.mu.Unlock()

	out := errorHandler.io
	if out == nil {
		return
	}

	if errorHandler.shown > 0 && !out.IsVerbose() {
		if errorHandler.shown == 1 {
			out.WriteError("<warning>More deprecation notices were hidden, run again with `-v` to show them.</warning>", true, io.Normal)
			errorHandler.shown = 2
		}

		return
	}

	errorHandler.shown = 1

	where := php.Basename(site.File, "") + ":" + strconv.Itoa(site.Line)

	// outputWarning
	out.WriteError("<warning>Deprecation Notice: "+message+" in "+where+"</warning>", true, io.Normal)

	if out.IsVerbose() {
		out.WriteError("<warning>Stack trace:</warning>", true, io.Normal)
		out.WriteError("<warning> "+where+"</warning>", true, io.Normal)
	}
}
