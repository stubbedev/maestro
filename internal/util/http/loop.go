// Ports src/Composer/Util/Loop.php, src/Composer/Util/SyncHelper.php and
// the HttpDownloader part of Factory (createHttpDownloader).

package http

import (
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// Waitable is a promise Loop.Wait can wait for; every *util.Promise is
// one.
type Waitable interface {
	Done() <-chan struct{}
	Err() error
	Cancel()
}

// Loop ports Composer\Util\Loop: it drives the asynchronous HTTP requests
// and processes until a set of promises settled.
type Loop struct {
	httpDownloader  *HttpDownloader
	processExecutor *util.ProcessExecutor

	mu              sync.Mutex
	currentPromises map[int][]Waitable
	waitIndex       int
}

// NewLoop is new Loop($httpDownloader, $processExecutor); processExecutor
// may be nil.
func NewLoop(httpDownloader *HttpDownloader, processExecutor *util.ProcessExecutor) *Loop {
	httpDownloader.EnableAsync()

	if processExecutor != nil {
		processExecutor.EnableAsync()
	}

	return &Loop{httpDownloader: httpDownloader, processExecutor: processExecutor, currentPromises: map[int][]Waitable{}}
}

// HttpDownloader is getHttpDownloader().
func (l *Loop) HttpDownloader() *HttpDownloader { return l.httpDownloader }

// ProcessExecutor is getProcessExecutor(); nil for null.
func (l *Loop) ProcessExecutor() *util.ProcessExecutor { return l.processExecutor }

// Wait is wait($promises, $progress): it runs the queued requests and
// processes until none is left and the promises settled, and returns the
// first rejection. progress may be nil.
//
// Unlike PHP, then() callbacks run on their own goroutines, so Wait also
// waits for the promises themselves: a callback may still be adding work
// when the last job finished.
func (l *Loop) Wait(promises []Waitable, progress *console.ProgressBar) error {
	var (
		mu       sync.Mutex
		uncaught error
	)

	allDone := make(chan struct{})
	failed := make(chan struct{})
	remaining := atomic.Int64{}
	remaining.Store(int64(len(promises)))

	if len(promises) == 0 {
		close(allDone)
	}

	for _, p := range promises {
		go func() {
			<-p.Done()

			if err := p.Err(); err != nil {
				mu.Lock()
				if uncaught == nil {
					uncaught = err
					close(failed)
				}
				mu.Unlock()
			}

			if remaining.Add(-1) == 0 {
				close(allDone)
			}
		}()
	}

	// keep track of every group of promises that is waited on, so
	// AbortJobs can cancel them all, even if Wait is called within a Wait
	l.mu.Lock()
	waitIndex := l.waitIndex
	l.waitIndex++
	l.currentPromises[waitIndex] = promises
	l.mu.Unlock()

	if progress != nil {
		totalJobs := l.httpDownloader.countActiveJobs(-1, 0, nil)
		if l.processExecutor != nil {
			totalJobs += l.processExecutor.CountActiveJobs()
		}

		progress.StartMax(totalJobs)
	}

	var lastUpdate time.Time

	for {
		processes := 0
		if l.processExecutor != nil {
			processes = l.processExecutor.CountActiveJobs()
		}

		// Wait for transfers in short slices while processes run (they
		// have no channel to wake us) or the progress bar needs updates.
		wait := time.Second
		if processes > 0 {
			wait = time.Millisecond
		} else if progress != nil {
			wait = 100 * time.Millisecond
		}

		activeJobs := l.httpDownloader.countActiveJobs(-1, wait, allDone) + processes

		if progress != nil && time.Since(lastUpdate) > 100*time.Millisecond {
			lastUpdate = time.Now()
			progress.SetProgress(progress.MaxSteps() - activeJobs)
		}

		if activeJobs > 0 {
			if processes > 0 && l.httpDownloader.inFlight() == 0 {
				time.Sleep(time.Millisecond)
			}

			continue
		}

		select {
		case <-allDone:
		case <-failed:
		default:
			// no work left, but then() callbacks are still running and may
			// queue more
			select {
			case <-allDone:
			case <-failed:
			case <-l.httpDownloader.waitChange():
			case <-time.After(10 * time.Millisecond):
			}

			continue
		}

		break
	}

	// as we skip progress updates if they are too quick, make sure we do
	// one last one here at 100%
	if progress != nil {
		progress.Finish()
	}

	l.mu.Lock()
	delete(l.currentPromises, waitIndex)
	l.mu.Unlock()

	mu.Lock()
	defer mu.Unlock()

	return uncaught
}

// AbortJobs is abortJobs(): it cancels every promise being waited on.
func (l *Loop) AbortJobs() {
	l.mu.Lock()
	groups := make([][]Waitable, 0, len(l.currentPromises))

	for _, group := range l.currentPromises {
		groups = append(groups, group)
	}
	l.mu.Unlock()

	for _, group := range groups {
		for _, promise := range group {
			promise.Cancel()
		}
	}
}

// SyncDownloader is the part of Composer's DownloaderInterface (or
// DownloadManager) SyncHelper drives; P is the package type. A nil
// Waitable is PHP's null promise.
type SyncDownloader[P any] interface {
	Download(pkg P, path string, prevPackage P) (Waitable, error)
	Prepare(typ string, pkg P, path string, prevPackage P) (Waitable, error)
	Install(pkg P, path string) (Waitable, error)
	Update(pkg, prevPackage P, path string) (Waitable, error)
	Cleanup(typ string, pkg P, path string, prevPackage P) (Waitable, error)
}

// DownloadAndInstallPackageSync is SyncHelper::downloadAndInstallPackageSync:
// download, prepare, install (or update, when prevPackage is not nil) and
// clean up a package synchronously.
func DownloadAndInstallPackageSync[P any](loop *Loop, downloader SyncDownloader[P], path string, pkg, prevPackage P) error {
	hasPrev := any(prevPackage) != nil

	typ := "install"
	if hasPrev {
		typ = "update"
	}

	err := func() error {
		if err := loop.Await(downloader.Download(pkg, path, prevPackage)); err != nil {
			return err
		}

		if err := loop.Await(downloader.Prepare(typ, pkg, path, prevPackage)); err != nil {
			return err
		}

		if typ == "update" {
			return loop.Await(downloader.Update(pkg, prevPackage, path))
		}

		return loop.Await(downloader.Install(pkg, path))
	}()
	if err != nil {
		if cerr := loop.Await(downloader.Cleanup(typ, pkg, path, prevPackage)); cerr != nil {
			return cerr
		}

		return err
	}

	return loop.Await(downloader.Cleanup(typ, pkg, path, prevPackage))
}

// Await is SyncHelper::await($loop, $promise), taking the result of a call
// that returns a promise (nil for null) or fails synchronously.
func (l *Loop) Await(promise Waitable, err error) error {
	if err != nil {
		return err
	}

	if promise == nil {
		return nil
	}

	return l.Wait([]Waitable{promise}, nil)
}

// createHTTPDownloaderWarned is Factory::createHttpDownloader's static
// $warned: the TLS warning is shown once per process.
var createHTTPDownloaderWarned atomic.Bool

// CreateHttpDownloader is Factory::createHttpDownloader($io, $config,
// $options): a downloader using the configured CA file or path, warning
// when TLS is disabled.
func CreateHttpDownloader(ioi io.IO, config Config, options *php.Array, rt Runtime) (*HttpDownloader, error) {
	disableTLS := false

	// allow running the config command if disable-tls is in the arg list
	// (openssl, Composer's precondition, is always available)
	if args := os.Args; slices.Contains(args, "disable-tls") && (slices.Contains(args, "conf") || slices.Contains(args, "config")) {
		createHTTPDownloaderWarned.Store(true)
	} else if config.Get("disable-tls") == true {
		if !createHTTPDownloaderWarned.Swap(true) {
			ioi.WriteError("<warning>You are running Composer with SSL/TLS protection disabled.</warning>", true, io.Normal)
		}

		disableTLS = true
	}

	httpDownloaderOptions := php.NewArray()

	if !disableTLS {
		ssl := php.NewArray()

		for _, key := range [2]string{"cafile", "capath"} {
			if v := config.Get(key); v != nil && v != "" {
				ssl.Set(key, v)
			}
		}

		if ssl.Len() > 0 {
			httpDownloaderOptions.Set("ssl", ssl)
		}

		if options != nil {
			httpDownloaderOptions = php.ArrayReplaceRecursive(httpDownloaderOptions, options).Clone()
		}
	}

	httpDownloader, err := NewHttpDownloader(ioi, config, httpDownloaderOptions, disableTLS, rt)
	if err != nil {
		if te, ok := err.(*util.TransportError); ok && strings.Contains(te.Message, "cafile") { //nolint:errorlint // TransportException is thrown directly
			ioi.Write("<error>Unable to locate a valid CA certificate file. You must set a valid 'cafile' option.</error>", true, io.Normal)
			ioi.Write("<error>A valid CA certificate file is required for SSL/TLS protection.</error>", true, io.Normal)
			ioi.Write("<error>You can disable this error, at your own risk, by setting the 'disable-tls' option to true.</error>", true, io.Normal)
		}

		return nil, err
	}

	return httpDownloader, nil
}
