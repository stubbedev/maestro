// Ports the deprecation path of src/Composer/Util/ErrorHandler.php: how
// Composer reports the E_USER_DEPRECATED notices it raises with
// trigger_error() (its other levels become *ErrorException).

package util

import (
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/phperr"
)

// errorHandler is ErrorHandler's static state.
var errorHandler struct {
	mu sync.Mutex
	io io.IO
	// shown is $hasShownDeprecationNotice: 0, 1 once a notice was
	// printed, 2 once the "hidden" hint was printed. Composer has one
	// ErrorHandler, whose static the plugin runtime keeps PHP's in step
	// with (DeprecationNoticeShown): below -v, a notice plugin code
	// raises after one Composer raised is hidden, and the other way round.
	shown atomic.Int32
}

// The warnings internal/io raises are ErrorHandler's ErrorExceptions,
// none while Silencer::suppress() is in effect.
func init() {
	io.NewWarning = func(message string, site phperr.Site) error {
		if silenced.Load() > 0 {
			return nil
		}

		return &ErrorException{Message: message, Site: site}
	}
}

// silenced counts the Silencer::suppress() calls not restored yet.
var silenced atomic.Int32

// SilencerSuppress ports Silencer::suppress(): until SilencerRestore,
// error_reporting() leaves out warnings and notices, so ErrorHandler does
// not throw for them and the code goes on with the value PHP gives (null
// for a missing key). Only warnings raised through io.NewWarning honour
// it.
func SilencerSuppress() { silenced.Add(1) }

// SilencerRestore ports Silencer::restore().
func SilencerRestore() { silenced.Add(-1) }

// DeprecationNoticeShown is ErrorHandler::$hasShownDeprecationNotice.
func DeprecationNoticeShown() int { return int(errorHandler.shown.Load()) }

// SetDeprecationNoticeShown sets ErrorHandler::$hasShownDeprecationNotice
// (the plugin runtime, as PHP's ErrorHandler changed it).
func SetDeprecationNoticeShown(n int) { errorHandler.shown.Store(int32(min(max(n, 0), 2))) }

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

	errorHandler.io = nil
	errorHandler.shown.Store(0)
}

// TriggerDeprecation ports trigger_error($message, E_USER_DEPRECATED) as
// ErrorHandler::handle reports it: the first notice as "Deprecation
// Notice: <message> in <file>:<line>", later ones only in verbose mode
// (otherwise a single hint that more were hidden). site is the
// trigger_error call in Composer.
//
// PHP prints the absolute path of Composer's source file (phperr.AbsPath,
// as for exceptions' "at" lines) and, in verbose mode, its call stack
// (debug_backtrace): the site, then the calls in progress the ports
// recorded with phperr.Enter (a path whose ports do not record theirs
// lists fewer frames).
//
// The IO is called without a lock held: it may be one created in PHP,
// whose calls read the state (DeprecationNoticeShown) as they sync it.
func TriggerDeprecation(message string, site phperr.Site) {
	deprecation(message, site, true)
}

// RaiseDeprecation reports an E_DEPRECATED the engine raises at site
// ("Automatic conversion of false to array is deprecated") as
// ErrorHandler::handle does: as TriggerDeprecation, except that PHP calls
// handle() from the site itself, so the stack trace lists only the calls
// in progress.
func RaiseDeprecation(message string, site phperr.Site) {
	deprecation(message, site, false)
}

func deprecation(message string, site phperr.Site, triggered bool) {
	errorHandler.mu.Lock()
	out := errorHandler.io
	errorHandler.mu.Unlock()

	if out == nil {
		return
	}

	if shown := errorHandler.shown.Load(); shown > 0 && !out.IsVerbose() {
		if shown == 1 {
			out.WriteError("<warning>More deprecation notices were hidden, run again with `-v` to show them.</warning>", true, io.Normal)
			errorHandler.shown.Store(2)
		}

		return
	}

	errorHandler.shown.Store(1)

	where := phperr.AbsPath(site.File) + ":" + strconv.Itoa(site.Line)

	// outputWarning
	out.WriteError("<warning>Deprecation Notice: "+message+" in "+where+"</warning>", true, io.Normal)

	if out.IsVerbose() {
		out.WriteError("<warning>Stack trace:</warning>", true, io.Normal)
		// array_slice(debug_backtrace(), 2): outputWarning's and handle()'s
		// frames dropped, trigger_error() called at the site first, then
		// the calls in progress; frames without file and line are left out
		if triggered {
			out.WriteError("<warning> "+where+"</warning>", true, io.Normal)
		}
		for _, f := range phperr.Stack() {
			if f.File != "" && f.Line != 0 {
				out.WriteError("<warning> "+phperr.AbsPath(f.File)+":"+strconv.Itoa(f.Line)+"</warning>", true, io.Normal)
			}
		}
	}
}
