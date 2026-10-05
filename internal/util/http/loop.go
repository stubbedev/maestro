// Ports src/Composer/Util/Loop.php, src/Composer/Util/SyncHelper.php and
// the HttpDownloader part of Factory (createHttpDownloader).

package http

import (
	"maps"
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
type Waitable = util.Waitable

// Loop ports Composer\Util\Loop: it drives the asynchronous HTTP requests
// and processes until a set of promises settled.
//
// The HttpDownloader and the ProcessExecutor share one util.Scheduler (the
// downloader's), which Wait drives on the calling goroutine: requests and
// processes run in parallel, but every callback runs on that goroutine, in
// the order the work started. Composer runs the callbacks in the order the
// work finishes; that order, the only nondeterministic one in Composer's
// loop, is replaced by the start order. See util.Scheduler.
type Loop struct {
	httpDownloader  *HttpDownloader
	processExecutor *util.ProcessExecutor

	mu              sync.Mutex
	currentPromises map[int][]Waitable
	waitIndex       int
}

// NewLoop is new Loop($httpDownloader, $processExecutor); processExecutor
// may be nil. The executor's async jobs complete through the downloader's
// scheduler from here on.
func NewLoop(httpDownloader *HttpDownloader, processExecutor *util.ProcessExecutor) *Loop {
	httpDownloader.EnableAsync()

	if processExecutor != nil {
		processExecutor.SetScheduler(httpDownloader.Scheduler())
		processExecutor.EnableAsync()
	}

	return &Loop{httpDownloader: httpDownloader, processExecutor: processExecutor, currentPromises: map[int][]Waitable{}}
}

// HttpDownloader is getHttpDownloader().
func (l *Loop) HttpDownloader() *HttpDownloader { return l.httpDownloader }

// ProcessExecutor is getProcessExecutor(); nil for null.
func (l *Loop) ProcessExecutor() *util.ProcessExecutor { return l.processExecutor }

// Wait is wait($promises, $progress): it runs the queued requests and
// processes, and their callbacks, until no job is left, and returns the
// first rejection of the promises (in the order they settled). progress
// may be nil. Only one goroutine may wait at a time; a callback may wait
// again (nested).
//
// A nil Loop drives the schedulers the promises wait on until they
// settled (tests building promises without a Loop).
func (l *Loop) Wait(promises []Waitable, progress *console.ProgressBar) error {
	if l == nil {
		return util.AwaitAll(promises)
	}

	uncaught := util.FirstRejection(promises)

	// keep track of every group of promises that is waited on, so
	// AbortJobs can cancel them all, even if Wait is called within a Wait
	l.mu.Lock()
	waitIndex := l.waitIndex
	l.waitIndex++
	l.currentPromises[waitIndex] = promises
	l.mu.Unlock()

	sched := l.httpDownloader.Scheduler()

	if progress != nil {
		progress.StartMax(l.countedJobs(sched))
	}

	var lastUpdate time.Time

	for {
		if progress != nil && time.Since(lastUpdate) > 100*time.Millisecond {
			lastUpdate = time.Now()
			progress.SetProgress(progress.MaxSteps() - l.countedJobs(sched))
		}

		if sched.Pending()+l.queuedJobs() == 0 {
			break
		}

		if !sched.RunReady() {
			wait := time.Duration(0)
			if progress != nil {
				wait = 100 * time.Millisecond
			}

			sched.WaitReady(wait, nil)
		}
	}

	// as we skip progress updates if they are too quick, make sure we do
	// one last one here at 100%
	if progress != nil {
		progress.Finish()
	}

	l.mu.Lock()
	delete(l.currentPromises, waitIndex)
	l.mu.Unlock()

	return uncaught()
}

// countedJobs is Composer's $httpDownloader->countActiveJobs() +
// $processExecutor->countActiveJobs(), which the progress bar shows: the
// counted work holding a scheduler ticket (running transfers and
// processes and the work standing for Composer's processes, completions
// not yet delivered) plus the requests and processes still queued, which
// take a ticket only when they start. Composer counts a job until its
// completion is processed, so a progress bar starts at the number of
// queued jobs. Background work (the package store) is waited for but not
// counted, as Composer has no such job.
func (l *Loop) countedJobs(sched *util.Scheduler) int {
	return sched.Counted() + l.queuedJobs()
}

// queuedJobs is the number of requests and processes waiting for a slot.
func (l *Loop) queuedJobs() int {
	n := l.httpDownloader.countQueued()
	if l.processExecutor != nil {
		n += l.processExecutor.CountQueuedJobs()
	}

	return n
}

// AbortJobs is abortJobs(): it cancels every promise being waited on.
func (l *Loop) AbortJobs() {
	l.mu.Lock()
	groups := make([][]Waitable, 0, len(l.currentPromises))

	// in the order the waits started, as PHP iterates the array
	for _, index := range slices.Sorted(maps.Keys(l.currentPromises)) {
		groups = append(groups, l.currentPromises[index])
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

// ResetCreateHttpDownloaderWarning forgets that the TLS warning was shown,
// as a fresh process would; for tests.
func ResetCreateHttpDownloaderWarning() {
	createHTTPDownloaderWarned.Store(false)
}

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
