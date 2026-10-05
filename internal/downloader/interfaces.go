// Ports src/Composer/Downloader/DownloaderInterface.php,
// ChangeReportInterface.php, DvcsDownloaderInterface.php and
// VcsCapableDownloaderInterface.php, plus the narrow interfaces of the
// collaborators the downloaders take.

package downloader

import (
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
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
	// InstallationSource is getInstallationSource(): "dist" or "source".
	InstallationSource() string
	Download(p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*Promise, error)
	Prepare(typ string, p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*Promise, error)
	Install(p pkg.PackageInterface, path string) (*Promise, error)
	Update(initial, target pkg.PackageInterface, path string) (*Promise, error)
	Remove(p pkg.PackageInterface, path string) (*Promise, error)
	Cleanup(typ string, p pkg.PackageInterface, path string, prev pkg.PackageInterface) (*Promise, error)
}

// ChangeReporter is ChangeReportInterface.
type ChangeReporter interface {
	// LocalChanges is getLocalChanges(): the changes, or null for none.
	LocalChanges(p pkg.PackageInterface, path string) (pkg.NullString, error)
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

// Classer gives a downloader's PHP class name (get_class()), used in
// DownloadManager's messages and by the plugin mirrors. Downloaders that do
// not implement it are named by their Go type.
type Classer interface {
	Class() string
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
// of a download (PRE_FILE_DOWNLOAD), and from the download's goroutine for
// retries, mirror fallbacks and POST_FILE_DOWNLOAD, as Composer dispatches
// from promise callbacks inside the loop's wait. A dispatcher running PHP
// listeners must queue the calls made off the main flow and run them there
// (docs/PLUGINS.md §5.14).
type EventDispatcher interface {
	Dispatch(eventName string, event eventdispatcher.Event) (int, error)
}

var (
	_ HTTPDownloader  = (*http.HttpDownloader)(nil)
	_ EventDispatcher = (*eventdispatcher.EventDispatcher)(nil)
)
