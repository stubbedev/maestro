// Ports src/Composer/Downloader/DownloaderInterface.php,
// ChangeReportInterface.php, DvcsDownloaderInterface.php and
// VcsCapableDownloaderInterface.php, plus the narrow interfaces of the
// collaborators the downloaders take.

package downloader

import (
	"os"

	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// Promise is what the downloaders return: React's PromiseInterface. Download
// promises resolve to the downloaded file's path (or "" for null), the others
// to "".
type Promise = util.Promise[string]

// Downloader is DownloaderInterface. prev is nil for null. A returned error
// is an exception thrown synchronously; the promise is never nil without
// one.
type Downloader interface {
	// PHPClass is get_class($downloader).
	php.Classer
	// InstallationSource is getInstallationSource().
	InstallationSource() pkg.InstallationSource
	Download(p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*Promise, error)
	Prepare(typ operation.Type, p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*Promise, error)
	Install(p pkg.PackageInterface, path string) (*Promise, error)
	Update(initial, target pkg.PackageInterface, path string) (*Promise, error)
	Remove(p pkg.PackageInterface, path string) (*Promise, error)
	Cleanup(typ operation.Type, p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*Promise, error)
}

// ChangeReporter is ChangeReportInterface.
type ChangeReporter interface {
	// LocalChanges is getLocalChanges(): the changes, or null for none.
	LocalChanges(p pkg.PackageInterface, path string) (pkg.NullString, error)
}

// AsyncChangeReporter is a ChangeReporter that can compare in the
// background: LocalChangesAsync starts what LocalChanges does and returns
// what waits for its result.
type AsyncChangeReporter interface {
	LocalChangesAsync(p pkg.PackageInterface, path string) func() (pkg.NullString, error)
}

// DvcsDownloader is DvcsDownloaderInterface.
type DvcsDownloader interface {
	// UnpushedChanges is getUnpushedChanges().
	UnpushedChanges(p pkg.PackageInterface, path string) (pkg.NullString, error)
}

// VcsCapableDownloader is VcsCapableDownloaderInterface.
type VcsCapableDownloader interface {
	// VcsReference is getVcsReference().
	VcsReference(p pkg.PackageInterface, path string) (pkg.NullString, error)
}

// Config is the part of Composer\Config the downloaders read; internal/config
// is adapted to it as to io.Config.
type Config interface {
	Get(key string) any
}

// HTTPDownloader is the part of Composer\Util\HttpDownloader the downloaders
// use. *http.HttpDownloader implements it.
type HTTPDownloader interface {
	AddCopy(url, to string, options *php.Array) (*util.Promise[*http.Response], error)
	Wait()
}

// Cache is the part of Composer\Cache the downloaders use, the files cache
// (cache-files-dir). *cache.Cache implements it.
type Cache interface {
	IsEnabled() bool
	IsReadOnly() bool
	Sha1(file string) (string, bool, error)
	CopyTo(file, target string) (bool, error)
	// Open is maestro's copyTo without the copy (cache.Cache.Open): the
	// cached file, open, or nil when missing.
	Open(file string) (*os.File, error)
	CopyFrom(file, source string) (bool, error)
	Remove(file string) (bool, error)
	GcIsNecessary() bool
	Gc(ttl int, maxSize int64) (bool, error)
}

// EventDispatcher is the part of Composer\EventDispatcher\EventDispatcher
// the downloaders use, to fire PRE_FILE_DOWNLOAD and POST_FILE_DOWNLOAD
// (eventdispatcher.PreFileDownloadEvent, PostFileDownloadEvent).
//
// FileDownloader dispatches on the calling goroutine for the first attempt
// of a download (PRE_FILE_DOWNLOAD), and from promise callbacks for
// retries, mirror fallbacks and POST_FILE_DOWNLOAD, as Composer does; those
// run on the goroutine driving the loop (util.Scheduler), so every
// dispatch happens on the main flow.
type EventDispatcher interface {
	Dispatch(eventName string, event eventdispatcher.Event) (int, error)
}

var (
	_ HTTPDownloader  = (*http.HttpDownloader)(nil)
	_ EventDispatcher = (*eventdispatcher.EventDispatcher)(nil)
)
