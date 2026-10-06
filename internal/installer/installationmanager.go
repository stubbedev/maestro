// Ports src/Composer/Installer/InstallationManager.php.

package installer

import (
	"errors"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// Manager ports Composer\Installer\InstallationManager: it picks the
// installer of each package type and executes the solver's operations.
//
// Every installer call happens on the goroutine calling the manager, in
// Composer's order, so installers backed by PHP (plugins) need no special
// treatment; only the downloaders' own work (downloads, extraction into the
// store, removals) runs in parallel. See Promise for how callbacks are
// ordered.
type Manager struct {
	installers []Installer
	cache      map[string]Installer
	// notifiable holds the packages to notify about, by notification URL
	// then package name, in insertion order.
	notifiable      []*notifyGroup
	loop            *http.Loop
	io              mio.IO
	eventDispatcher EventDispatcher
	outputProgress  bool
	metadata        *downloader.Metadata
	// flushWrites flushes the repository writes deferred while a batch is
	// waited on (deferWrites), nil outside that time.
	flushWrites func() error
}

var _ repository.InstallationManager = (*Manager)(nil)

// NewManager is new InstallationManager($loop, $io, $eventDispatcher); a
// nil dispatcher is null. The loop may be nil in tests that execute
// nothing asynchronous.
func NewManager(loop *http.Loop, io mio.IO, eventDispatcher EventDispatcher) *Manager {
	return &Manager{loop: loop, io: io, eventDispatcher: eventDispatcher, cache: map[string]Installer{}}
}

// SetDownloadMetadata sets the FileDownloader::$downloadMetadata the
// downloaders fill (downloader.Deps.Metadata), which reset() clears and
// notifyInstalls() reports.
func (m *Manager) SetDownloadMetadata(metadata *downloader.Metadata) {
	m.metadata = metadata
}

// Reset is reset().
func (m *Manager) Reset() {
	m.notifiable = nil

	if m.metadata != nil {
		m.metadata.Reset()
	}
}

// AddInstaller is addInstaller(): the installer takes precedence over the
// ones added before.
func (m *Manager) AddInstaller(installer Installer) {
	m.installers = append([]Installer{installer}, m.installers...)
	m.cache = map[string]Installer{}
}

// RemoveInstaller is removeInstaller().
func (m *Manager) RemoveInstaller(installer Installer) {
	for i, inst := range m.installers {
		if inst == installer {
			m.installers = append(m.installers[:i:i], m.installers[i+1:]...)
			m.cache = map[string]Installer{}

			return
		}
	}
}

// Installers returns the installers, most recently added first.
func (m *Manager) Installers() []Installer {
	return append([]Installer(nil), m.installers...)
}

// pluginsDisabler is `instanceof PluginInstaller`.
type pluginsDisabler interface {
	DisablePlugins() error
}

// DisablePlugins is disablePlugins(): no plugin is instantiated from here
// on.
func (m *Manager) DisablePlugins() error {
	for _, installer := range m.installers {
		if pi, ok := installer.(pluginsDisabler); ok {
			if err := pi.DisablePlugins(); err != nil {
				return err
			}
		}
	}

	return nil
}

// Installer is getInstaller(): the installer for a package type, or an
// InvalidArgumentException when none supports it.
func (m *Manager) Installer(typ string) (Installer, error) {
	typ = php.Strtolower(typ)

	if installer, ok := m.cache[typ]; ok {
		return installer, nil
	}

	for _, installer := range m.installers {
		ok, err := installer.Supports(typ)
		if err != nil {
			return nil, err
		}

		if ok {
			m.cache[typ] = installer

			return installer, nil
		}
	}

	return nil, &util.InvalidArgumentError{Site: phperr.At("InstallationManager.php", 133), Message: "Unknown installer type: " + typ}
}

// IsPackageInstalled is isPackageInstalled().
func (m *Manager) IsPackageInstalled(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (bool, error) {
	if alias, ok := p.(pkg.Alias); ok {
		has, err := repo.HasPackage(p)
		if err != nil || !has {
			return false, err
		}

		return m.IsPackageInstalled(repo, alias.AliasOf())
	}

	installer, err := m.Installer(p.Type())
	if err != nil {
		return false, err
	}

	return installer.IsInstalled(repo, p)
}

// EnsureBinariesPresence is ensureBinariesPresence(): it installs the
// binaries of a package if its installer handles binaries.
func (m *Manager) EnsureBinariesPresence(p pkg.PackageInterface) error {
	installer, err := m.Installer(p.Type())
	if err != nil {
		// no installer found for the current package type (@see
		// `getInstaller()`)
		if _, ok := errors.AsType[*util.InvalidArgumentError](err); ok {
			return nil
		}

		return err
	}

	// if the given installer support installing binaries
	if bp, ok := installer.(BinaryPresence); ok {
		return bp.EnsureBinariesPresence(p)
	}

	return nil
}

// indexedOp is an operation with its index in the operations passed to
// Execute (PHP keeps the keys through the batches).
type indexedOp struct {
	index int
	op    operation.Operation
}

// cleanups are execute()'s $cleanupPromises: the cleanup step of every
// operation whose download started, by operation index, in order.
type cleanups struct {
	mu    sync.Mutex
	order []int
	fns   map[int]func() (*Promise, error)
}

func (c *cleanups) set(index int, fn func() (*Promise, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.fns[index]; !ok {
		c.order = append(c.order, index)
	}

	c.fns[index] = fn
}

func (c *cleanups) get(index int) func() (*Promise, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.fns[index]
}

func (c *cleanups) all() []func() (*Promise, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]func() (*Promise, error), len(c.order))
	for i, index := range c.order {
		out[i] = c.fns[index]
	}

	return out
}

// Execute is execute(): it downloads and executes the operations on repo,
// in batches so that plugins modifying downloads, and plugins and
// installers in general, are active before the packages after them are
// handled. On failure the cleanup step of every started operation runs.
func (m *Manager) Execute(repo repository.InstalledRepositoryInterface, operations []operation.Operation, devMode, runScripts, downloadOnly bool) error {
	cl := &cleanups{fns: map[int]func() (*Promise, error){}}

	stop := m.handleSignals(cl)

	err := m.executeBatches(repo, operations, cl, devMode, runScripts, downloadOnly)

	stop()

	if err != nil {
		return err
	}

	if downloadOnly {
		return nil
	}

	// do a last write so that we write the repository even if nothing
	// changed as that can trigger an update of some files like
	// InstalledVersions.php if running a new composer version
	return repo.Write(devMode, m)
}

// handleSignals is the SignalHandler execute() installs: on SIGINT,
// SIGTERM or SIGHUP the cleanup steps run and the process exits with the
// signal. PHP runs the handler on its one thread between two statements;
// here it interrupts the goroutine driving the loop (at its next wait, or
// when Execute ends), so that the cleanup's callbacks run where all others
// do. It returns the unregister function.
func (m *Manager) handleSignals(cl *cleanups) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, util.HandledSignals()...)

	handle := func(sig os.Signal) {
		m.io.WriteError("Received "+util.SignalName(sig)+", aborting", true, mio.Debug)
		if m.flushWrites != nil {
			_ = m.flushWrites()
		}
		_ = m.runCleanup(cl)
		util.ExitWithSignal(sig)
	}

	done := make(chan struct{})

	var watching sync.WaitGroup

	watching.Go(func() {
		select {
		case sig := <-ch:
			if m.loop == nil {
				handle(sig)

				return
			}

			m.loop.HttpDownloader().Scheduler().Interrupt(func() { handle(sig) })
		case <-done:
		}
	})

	return func() {
		signal.Stop(ch)
		close(done)
		watching.Wait()

		// a signal received since the last wait
		if m.loop != nil {
			m.loop.HttpDownloader().Scheduler().RunInterrupts()
		}
	}
}

func (m *Manager) executeBatches(repo repository.InstalledRepositoryInterface, operations []operation.Operation, cl *cleanups, devMode, runScripts, downloadOnly bool) error {
	allOperations := make([]eventdispatcher.Operation, len(operations))
	for i, op := range operations {
		allOperations[i] = op
	}

	// execute operations in batches to make sure download-modifying-plugins
	// are installed before the other packages get downloaded
	var (
		batches [][]indexedOp
		batch   []indexedOp
	)

	for index, op := range operations {
		if p := installedOrUpdatedPackage(op); p != nil && p.Type() == "composer-plugin" {
			if v, ok := p.Extra().Get("plugin-modifies-downloads"); ok && v == true {
				if len(batch) > 0 {
					batches = append(batches, batch)
				}

				batches = append(batches, []indexedOp{{index, op}})
				batch = nil

				continue
			}
		}

		batch = append(batch, indexedOp{index, op})
	}

	if len(batch) > 0 {
		batches = append(batches, batch)
	}

	for _, b := range batches {
		if err := m.downloadAndExecuteBatch(repo, b, cl, devMode, runScripts, downloadOnly, allOperations); err != nil {
			if isException(err) {
				if cerr := m.runCleanup(cl); cerr != nil {
					return cerr
				}
			}

			return err
		}
	}

	return nil
}

// installedOrUpdatedPackage is the package an install or update
// operation installs, nil for other operations.
func installedOrUpdatedPackage(op operation.Operation) pkg.PackageInterface {
	switch o := op.(type) {
	case *operation.UpdateOperation:
		return o.TargetPackage()
	case *operation.InstallOperation:
		return o.Package()
	}

	return nil
}

// operationPackages returns the package an operation is about and, for an
// update, the initial package.
func operationPackages(op operation.Operation) (p, initial pkg.PackageInterface) {
	switch o := op.(type) {
	case *operation.UpdateOperation:
		return o.TargetPackage(), o.InitialPackage()
	case *operation.InstallOperation:
		return o.Package(), nil
	case *operation.UninstallOperation:
		return o.Package(), nil
	case *operation.MarkAliasInstalledOperation:
		return o.Package(), nil
	case *operation.MarkAliasUninstalledOperation:
		return o.Package(), nil
	}

	return nil, nil
}

// executesCode reports whether an operation type changes files (alias
// operations do not).
func executesCode(opType string) bool {
	return opType == operation.TypeUpdate || opType == operation.TypeInstall || opType == operation.TypeUninstall
}

func (m *Manager) downloadAndExecuteBatch(repo repository.InstalledRepositoryInterface, operations []indexedOp, cl *cleanups, devMode, runScripts, downloadOnly bool, allOperations []eventdispatcher.Operation) error {
	var promises []*Promise

	for _, iop := range operations {
		opType := iop.op.OperationType()

		// ignoring alias ops as they don't need to execute anything at this
		// stage
		if !executesCode(opType) {
			continue
		}

		p, initial := operationPackages(iop.op)

		installer, err := m.Installer(p.Type())
		if err != nil {
			return err
		}

		cl.set(iop.index, func() (*Promise, error) {
			// avoid calling cleanup if the download was not even
			// initialized for a package as without installation source
			// configured nothing will work
			if !p.InstallationSource().Valid {
				return Resolved(), nil
			}

			return installer.Cleanup(opType, p, initial)
		})

		if opType != operation.TypeUninstall {
			promise, err := installer.Download(p, initial)
			if err != nil {
				return err
			}

			if promise != nil {
				promises = append(promises, promise)
			}
		}
	}

	// execute all downloads first
	if len(promises) > 0 {
		if err := m.waitOnPromises(promises); err != nil {
			return err
		}
	}

	if downloadOnly {
		return m.runCleanup(cl)
	}

	// execute operations in batches to make sure every plugin is installed
	// in the right order and activated before the packages depending on it
	// are installed
	var (
		batches [][]indexedOp
		batch   []indexedOp
	)

	for _, iop := range operations {
		if p := installedOrUpdatedPackage(iop.op); p != nil && (p.Type() == "composer-plugin" || p.Type() == "composer-installer") {
			if len(batch) > 0 {
				batches = append(batches, batch)
			}

			batches = append(batches, []indexedOp{iop})
			batch = nil

			continue
		}

		batch = append(batch, iop)
	}

	if len(batch) > 0 {
		batches = append(batches, batch)
	}

	for _, b := range batches {
		if err := m.executeBatch(repo, b, cl, devMode, runScripts, allOperations); err != nil {
			return err
		}
	}

	return nil
}

// package event names by operation type.
var (
	prePackageEvents = map[string]string{
		operation.TypeInstall:   eventdispatcher.PrePackageInstall,
		operation.TypeUpdate:    eventdispatcher.PrePackageUpdate,
		operation.TypeUninstall: eventdispatcher.PrePackageUninstall,
	}
	postPackageEvents = map[string]string{
		operation.TypeInstall:   eventdispatcher.PostPackageInstall,
		operation.TypeUpdate:    eventdispatcher.PostPackageUpdate,
		operation.TypeUninstall: eventdispatcher.PostPackageUninstall,
	}
)

func (m *Manager) executeBatch(repo repository.InstalledRepositoryInterface, operations []indexedOp, cl *cleanups, devMode, runScripts bool, allOperations []eventdispatcher.Operation) error {
	var (
		promises          []*Promise
		postExecCallbacks []func() error
	)

	for _, iop := range operations {
		op := iop.op
		opType := op.OperationType()

		// ignoring alias ops as they don't need to execute anything
		if !executesCode(opType) {
			// output alias ops in debug verbosity as they have no output
			// otherwise
			if m.io.IsDebug() {
				show, err := op.Show(false)
				if err != nil {
					return err
				}

				m.io.WriteError("  - "+show, true, mio.Normal)
			}

			if _, err := m.executeOperation(repo, op); err != nil {
				return err
			}

			continue
		}

		p, initial := operationPackages(op)

		installer, err := m.Installer(p.Type())
		if err != nil {
			return err
		}

		if runScripts && m.eventDispatcher != nil {
			if _, err := m.eventDispatcher.DispatchPackageEvent(prePackageEvents[opType], devMode, repo, allOperations, op); err != nil {
				return err
			}
		}

		promise, err := installer.Prepare(opType, p, initial)
		if err != nil {
			return err
		}

		cleanup := cl.get(iop.index)

		promise = Then(promise, func() (*Promise, error) {
			return m.executeOperation(repo, op)
		}, nil)
		promise = Then(promise, cleanup, nil)
		promise = Then(promise, func() (*Promise, error) {
			return nil, repo.Write(devMode, m)
		}, func(e error) (*Promise, error) {
			m.io.WriteError("    <error>"+php.Ucfirst(opType)+" of "+p.PrettyName()+" failed</error>", true, mio.Normal)

			return nil, e
		})

		if runScripts && m.eventDispatcher != nil {
			dispatcher := m.eventDispatcher
			eventName := postPackageEvents[opType]

			postExecCallbacks = append(postExecCallbacks, func() error {
				_, err := dispatcher.DispatchPackageEvent(eventName, devMode, repo, allOperations, op)

				return err
			})
		}

		promises = append(promises, promise)
	}

	// execute all prepare => installs/updates/removes => cleanup steps
	if len(promises) > 0 {
		flush := m.deferWrites(repo, operations)
		err := m.waitOnPromises(promises)
		if ferr := flush(); err == nil {
			err = ferr
		}
		if err != nil {
			return err
		}
	}

	util.WorkaroundFilesystemIssues()

	for _, cb := range postExecCallbacks {
		if err := cb(); err != nil {
			return err
		}
	}

	return nil
}

// executeOperation is `$this->{$opType}($repo, $operation)`.
func (m *Manager) executeOperation(repo repository.InstalledRepositoryInterface, op operation.Operation) (*Promise, error) {
	switch o := op.(type) {
	case *operation.InstallOperation:
		return m.Install(repo, o)
	case *operation.UpdateOperation:
		return m.Update(repo, o)
	case *operation.UninstallOperation:
		return m.Uninstall(repo, o)
	case *operation.MarkAliasInstalledOperation:
		return nil, m.MarkAliasInstalled(repo, o)
	case *operation.MarkAliasUninstalledOperation:
		return nil, m.MarkAliasUninstalled(repo, o)
	}

	return nil, &util.LogicError{Message: "Unknown operation type: " + op.OperationType()}
}

// progressIO is `instanceof ConsoleIO` (BufferIO included), which gives
// the progress bar.
type progressIO interface {
	ProgressBar(maxSteps int) *console.ProgressBar
}

// waitOnPromises is waitOnPromises(): it waits for the promises with a
// progress bar when the output allows one.
func (m *Manager) waitOnPromises(promises []*Promise) error {
	var progress *console.ProgressBar

	if cio, ok := m.io.(progressIO); ok && m.outputProgress && !m.io.IsDebug() && len(promises) > 1 {
		if ci, _ := util.GetEnv("CI"); !php.ToBool(ci) {
			progress = cio.ProgressBar(0)
		}
	}

	// Loop::wait throws the first rejection, which skips the clean-up of
	// the progress bar below (the exception's rendering ends its line)
	if err := wait(m.loop, promises, progress); err != nil {
		return err
	}

	if progress != nil {
		progress.Clear()
		// ProgressBar in non-decorated output does not output a final
		// line-break and clear() does nothing
		if !m.io.IsDecorated() {
			m.io.WriteError("", true, mio.Normal)
		}
	}

	return nil
}

// Download is download(), which, as in Composer, runs the installer's
// install cleanup.
func (m *Manager) Download(p pkg.PackageInterface) (*Promise, error) {
	installer, err := m.Installer(p.Type())
	if err != nil {
		return nil, err
	}

	return installer.Cleanup("install", p, nil)
}

// Install is install(): it executes an install operation.
func (m *Manager) Install(repo repository.InstalledRepositoryInterface, op *operation.InstallOperation) (*Promise, error) {
	p := op.Package()

	installer, err := m.Installer(p.Type())
	if err != nil {
		return nil, err
	}

	promise, err := installer.Install(repo, p)
	if err != nil {
		return nil, err
	}

	m.markForNotification(p)

	return promise, nil
}

// Update is update(): it executes an update operation; a package whose
// type changed is uninstalled by the old type's installer and installed by
// the new one's.
func (m *Manager) Update(repo repository.InstalledRepositoryInterface, op *operation.UpdateOperation) (*Promise, error) {
	initial := op.InitialPackage()
	target := op.TargetPackage()

	initialType := initial.Type()
	targetType := target.Type()

	if initialType == targetType {
		installer, err := m.Installer(initialType)
		if err != nil {
			return nil, err
		}

		promise, err := installer.Update(repo, initial, target)
		if err != nil {
			return nil, err
		}

		m.markForNotification(target)

		return promise, nil
	}

	initialInstaller, err := m.Installer(initialType)
	if err != nil {
		return nil, err
	}

	promise, err := initialInstaller.Uninstall(repo, initial)
	if err != nil {
		return nil, err
	}

	installer, err := m.Installer(targetType)
	if err != nil {
		return nil, err
	}

	return Then(promise, func() (*Promise, error) {
		promise, err := installer.Install(repo, target)
		if err != nil || promise != nil {
			return promise, err
		}

		return Resolved(), nil
	}, nil), nil
}

// Uninstall is uninstall(): it executes an uninstall operation.
func (m *Manager) Uninstall(repo repository.InstalledRepositoryInterface, op *operation.UninstallOperation) (*Promise, error) {
	p := op.Package()

	installer, err := m.Installer(p.Type())
	if err != nil {
		return nil, err
	}

	return installer.Uninstall(repo, p)
}

// MarkAliasInstalled is markAliasInstalled().
func (m *Manager) MarkAliasInstalled(repo repository.InstalledRepositoryInterface, op *operation.MarkAliasInstalledOperation) error {
	p := op.Package()

	has, err := repo.HasPackage(p)
	if err != nil || has {
		return err
	}

	return repo.AddPackage(pkg.Clone(p))
}

// MarkAliasUninstalled is markAliasUninstalled().
func (m *Manager) MarkAliasUninstalled(repo repository.InstalledRepositoryInterface, op *operation.MarkAliasUninstalledOperation) error {
	return repo.RemovePackage(op.Package())
}

// InstallPath is getInstallPath(): the absolute path a package is
// installed to, without trailing slash; ok false is null (nothing on disk).
func (m *Manager) InstallPath(p pkg.PackageInterface) (string, bool, error) {
	installer, err := m.Installer(p.Type())
	if err != nil {
		return "", false, err
	}

	return installer.InstallPath(p)
}

// SetOutputProgress is setOutputProgress().
func (m *Manager) SetOutputProgress(outputProgress bool) {
	m.outputProgress = outputProgress
}

// NotifyInstalls is notifyInstalls(): it reports the installed packages to
// their repositories' notification URLs. Failures are ignored.
func (m *Manager) NotifyInstalls(mio.IO) {
	if m.loop != nil {
		var promises []*Promise
		err := m.sendNotifications(func(url string, opts *php.Array) error {
			promise, err := m.loop.HttpDownloader().Add(url, opts)
			if err == nil {
				promises = append(promises, Of(promise))
			}

			return err
		})
		if err == nil {
			_ = wait(m.loop, promises, nil)
		}
	}

	m.Reset()
}

// NotifyInstallsAsync is NotifyInstalls returning before the requests
// complete, with the function waiting for them (deliberate deviation 3,
// speed): Composer waits for its install notifications right away, but
// their responses are ignored and, below -vvv, nothing about them is
// printed, so the caller can go on and wait at its end. The requests run
// as synchronous requests on their own goroutine, so that waiting for them
// runs nothing else of the loop.
func (m *Manager) NotifyInstallsAsync(mio.IO) (waitFor func()) {
	if m.loop == nil {
		m.Reset()

		return func() {}
	}

	// Loop::wait() also finishes all the other work in progress, which
	// must be done here as in Composer
	_ = wait(m.loop, nil, nil)

	type request struct {
		url  string
		opts *php.Array
	}
	var requests []request
	_ = m.sendNotifications(func(url string, opts *php.Array) error {
		requests = append(requests, request{url, opts})

		return nil
	})

	m.Reset()

	var done sync.WaitGroup
	downloader := m.loop.HttpDownloader()
	for _, r := range requests {
		done.Go(func() { _, _ = downloader.Get(r.url, r.opts) })
	}

	return done.Wait
}

// sendNotifications builds notifyInstalls()' requests and hands each to
// send, stopping at the first error, as Composer's add() throwing does.
func (m *Manager) sendNotifications(send func(url string, opts *php.Array) error) error {
	for _, group := range m.notifiable {
		repoURL := group.url
		packages := group.packages

		// non-batch API, deprecated
		if strings.Contains(repoURL, "%package%") {
			for _, p := range packages {
				url := strings.ReplaceAll(repoURL, "%package%", p.PrettyName())

				header := php.NewArray()
				header.Append("Content-type: application/x-www-form-urlencoded")

				httpOpts := php.NewArray()
				httpOpts.Set("method", "POST")
				httpOpts.Set("header", header)
				httpOpts.Set("content", php.HTTPBuildQuery("version", p.PrettyVersion(), "version_normalized", p.Version()))
				httpOpts.Set("timeout", 3)

				opts := php.NewArray()
				opts.Set("retry-auth-failure", false)
				opts.Set("http", httpOpts)

				if err := send(url, opts); err != nil {
					return err
				}
			}

			continue
		}

		downloads := php.NewArray()

		for _, p := range packages {
			notification := php.NewArray()
			notification.Set("name", p.PrettyName())
			notification.Set("version", p.Version())

			if strings.Contains(repoURL, "packagist.org/") {
				downloaded := any(false)

				if m.metadata != nil {
					if size, ok := m.metadata.Get(p.Name()); ok {
						downloaded = size
					}
				}

				notification.Set("downloaded", downloaded)
			}

			downloads.Append(notification)
		}

		postData := php.NewArray()
		postData.Set("downloads", downloads)

		content, err := php.JSONEncode(postData, 0)
		if err != nil {
			return err
		}

		header := php.NewArray()
		header.Append("Content-Type: application/json")

		httpOpts := php.NewArray()
		httpOpts.Set("method", "POST")
		httpOpts.Set("header", header)
		httpOpts.Set("content", content)
		httpOpts.Set("timeout", 6)

		opts := php.NewArray()
		opts.Set("retry-auth-failure", false)
		opts.Set("http", httpOpts)

		if err := send(repoURL, opts); err != nil {
			return err
		}
	}

	return nil
}

// notifyGroup is one entry of $notifiablePackages: the packages to
// report to a notification URL, by name in insertion order.
type notifyGroup struct {
	url      string
	packages []pkg.PackageInterface
}

// markForNotification is markForNotification():
// $notifiablePackages[$url][$name] = $package.
func (m *Manager) markForNotification(p pkg.PackageInterface) {
	url := p.NotificationURL()
	if !url.Valid {
		return
	}

	i := slices.IndexFunc(m.notifiable, func(g *notifyGroup) bool { return g.url == url.S })
	if i < 0 {
		m.notifiable = append(m.notifiable, &notifyGroup{url: url.S})
		i = len(m.notifiable) - 1
		// notifyInstalls will post to it: the connection is opened while
		// the installation goes on (deliberate deviation 3)
		if m.loop != nil {
			m.loop.HttpDownloader().Preconnect(url.S, nil)
		}
	}

	group := m.notifiable[i]

	if j := slices.IndexFunc(group.packages, func(q pkg.PackageInterface) bool { return q.Name() == p.Name() }); j >= 0 {
		group.packages[j] = p
	} else {
		group.packages = append(group.packages, p)
	}
}

// runCleanup is runCleanup(): it aborts the running jobs and runs every
// cleanup step. A cleanup that throws makes it fail with that error once
// all of them ran; rejected cleanups are ignored.
func (m *Manager) runCleanup(cl *cleanups) error {
	if m.loop != nil {
		m.loop.AbortJobs()
	}

	fns := cl.all()

	var (
		promises []*Promise
		firstErr error
	)

	for _, cleanup := range fns {
		promise, err := cleanup()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}

			continue
		}

		if promise != nil {
			promises = append(promises, promise)
		}
	}

	if len(fns) > 0 {
		_ = wait(m.loop, promises, nil)
	}

	return firstErr
}

// isException reports whether err is a PHP \Exception (what `catch
// (\Exception $e)` catches) rather than an \Error.
func isException(err error) bool {
	e, ok := err.(eventdispatcher.PHPError) //nolint:errorlint // catch inspects the thrown object itself.

	return !ok || !e.IsPHPError()
}
