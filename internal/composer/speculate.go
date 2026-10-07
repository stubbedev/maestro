// Ports nothing: the work an install from the lock starts while the lock
// is verified: the dist downloads, the installer's autoload scan ahead of
// the dump and the local repository's write built ahead (deliberate
// deviation 3, speed).

package composer

import (
	"slices"

	"github.com/stubbedev/maestro/internal/autoload"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
)

// autoloadSpeculator is an AutoloadGenerator that can run the class map
// scan of the dump ahead (GeneratorAdapter).
type autoloadSpeculator interface {
	SpeculateAutoloads(config ConfigReader, localRepo repository.InstalledRepositoryInterface, root pkg.RootPackageInterface, im InstallationManager, scanPsrPackages bool)
	DiscardAutoloadSpeculation()
}

// dispatchChecker is an EventDispatcher that tells whether an event would
// reach a listener (eventdispatcher.EventDispatcher).
type dispatchChecker interface {
	WillDispatchTo(event eventdispatcher.Event) bool
}

// speculativeEvents are the events dispatched between doInstall's lock
// verification and the autoload dump's scan when no package operation
// runs: a listener could change the files the scan reads.
var speculativeEvents = []string{eventdispatcher.PrePoolCreate, eventdispatcher.PreOperationsExec, autoload.PreAutoloadDump}

// writePreparer is a local repository that can build its write ahead
// (repository.FilesystemRepository).
type writePreparer interface {
	PrepareWrite(devMode bool, im repository.InstallationManager, devPackageNames []string)
	DiscardPreparedWrite()
}

// aheadWork is what an install from the lock started while the lock is
// verified (startAhead): the dist transfers and store trees of its
// operations and, when it asks for none, the class map scan of the
// autoload dump that follows and the local repository's final write.
// doInstall defers Discard right after starting it, which ends what
// nobody took on every way out of doInstall; the code that takes each
// part checks it was made of what it would make it of. The scan is the
// one part that outlives doInstall: keepAutoloads hands it over to the
// dump once the install ran no operation. A nil *aheadWork started
// nothing.
type aheadWork struct {
	// dists ends the transfers and trees no download took, nil without.
	dists     func()
	autoloads autoloadSpeculator // nil without
	write     writePreparer      // nil without
}

// startAhead starts the work ahead of an install from the lock whose
// local repository transaction is transaction (nil when it could not be
// computed).
func (i *Installer) startAhead(localRepo repository.InstalledRepositoryInterface, transaction *resolver.Transaction) *aheadWork {
	noOperations := transaction != nil && len(transaction.Operations()) == 0
	a := &aheadWork{}
	a.dists = i.prefetchDists(transaction)
	a.autoloads = i.speculateAutoloads(localRepo, noOperations)
	a.write = i.prepareLocalRepoWrite(localRepo, noOperations)

	return a
}

// discardNoOperations drops what was started for an install that runs no
// operation: operations change the files the scan read and the packages
// the write was made of.
func (a *aheadWork) discardNoOperations() {
	if a == nil {
		return
	}
	if a.autoloads != nil {
		a.autoloads.DiscardAutoloadSpeculation()
		a.autoloads = nil
	}
	if a.write != nil {
		a.write.DiscardPreparedWrite()
		a.write = nil
	}
}

// discardAutoloadSpeculation drops a scan started ahead that the autoload
// dump did not take.
func (i *Installer) discardAutoloadSpeculation() {
	if s, ok := i.autoloadGenerator.(autoloadSpeculator); ok {
		s.DiscardAutoloadSpeculation()
	}
}

// keepAutoloads leaves the scan for the autoload dump that follows the
// install to take (Run drops it when the dump does not).
func (a *aheadWork) keepAutoloads() {
	if a != nil {
		a.autoloads = nil
	}
}

// Discard ends what was started that nobody took, but a kept scan.
func (a *aheadWork) Discard() {
	if a == nil {
		return
	}
	if a.dists != nil {
		a.dists()
		a.dists = nil
	}
	a.discardNoOperations()
}

// listenedTo reports whether a listener waits on one of events, true when
// the event dispatcher cannot tell.
func (i *Installer) listenedTo(events []string) bool {
	if i.eventDispatcher == nil {
		return true
	}
	checker, ok := i.eventDispatcher.(dispatchChecker)
	if !ok {
		return true
	}
	for _, name := range events {
		if checker.WillDispatchTo(eventdispatcher.NewEvent(name, nil, nil)) {
			return true
		}
	}

	return false
}

// prepareLocalRepoWrite has the local repository build, while the lock
// file is verified, the write that ends a no-op install (the installation
// manager writes it after running no operation), when nothing between
// them can change what it writes: the lock file asks for no package
// operation and no listener waits on the events in between. The
// repository builds the write again when what it is made of changed after
// all (repository.FilesystemRepository.PrepareWrite). It returns the
// repository building it, nil without one.
func (i *Installer) prepareLocalRepoWrite(localRepo repository.InstalledRepositoryInterface, noOperations bool) writePreparer {
	if !i.install || !i.executeOperations || i.dryRun || i.downloadOnly {
		return nil
	}
	r, ok := localRepo.(writePreparer)
	if !ok || i.listenedTo(speculativeEvents[:2]) || !noOperations {
		return nil
	}
	devPackageNames, err := i.locker.DevPackageNames()
	if err != nil {
		return nil
	}
	r.PrepareWrite(i.devMode, i.installationManager, devPackageNames)

	return r
}

// speculateAutoloads starts the class map scan of the autoload dump that
// follows a no-op install while the lock file is verified (which waits on
// the filter list's request), when nothing between them can change the
// files: the lock file asks for no package operation and no listener
// waits on the events in between. It returns the generator running the
// scan, nil without one.
func (i *Installer) speculateAutoloads(localRepo repository.InstalledRepositoryInterface, noOperations bool) autoloadSpeculator {
	if !i.dumpAutoloader || !i.install || !i.executeOperations || i.dryRun {
		return nil
	}
	s, ok := i.autoloadGenerator.(autoloadSpeculator)
	if !ok || i.listenedTo(speculativeEvents) || !noOperations {
		return nil
	}
	s.SpeculateAutoloads(i.config, localRepo, i.pkg, i.installationManager, i.optimizeAutoloader || i.classMapAuthoritative)

	return s
}

// localRepoTransaction is the transaction from a local repository to a
// lock's, computed once before the lock is verified for the work started
// ahead and for doInstall after the verification, which takes it while
// the repositories hold the packages it was computed of, in the state
// they were in (a listener of the verification's events can change them
// in place).
type localRepoTransaction struct {
	locked repository.RepositoryInterface
	local  repository.InstalledRepositoryInterface

	transaction *resolver.Transaction
	err         error
	state       packagesState
}

// newLocalRepoTransaction is resolver.NewLocalRepoTransaction(locked,
// local), kept for take.
func newLocalRepoTransaction(locked repository.RepositoryInterface, local repository.InstalledRepositoryInterface) *localRepoTransaction {
	t := &localRepoTransaction{locked: locked, local: local}
	present, err := local.Packages()
	if err != nil {
		t.err = err

		return t
	}
	result, err := locked.Packages()
	if err != nil {
		t.err = err

		return t
	}
	t.transaction = resolver.NewTransaction(present, result)
	t.state = packagesStateOf(present, result)

	return t
}

// take is resolver.NewLocalRepoTransaction(locked, local) now: the
// transaction computed before when the repositories' packages are as
// they were then.
func (t *localRepoTransaction) take() (*resolver.Transaction, error) {
	if t.err == nil {
		present, presentErr := t.local.Packages()
		result, resultErr := t.locked.Packages()
		if presentErr == nil && resultErr == nil && t.state.same(packagesStateOf(present, result)) {
			return t.transaction, nil
		}
	}

	return resolver.NewLocalRepoTransaction(t.locked, t.local)
}

// packagesState is which packages lists hold, at which revisions.
type packagesState struct {
	packages []pkg.PackageInterface
	revs     []uint64
	lens     []int
}

func packagesStateOf(lists ...[]pkg.PackageInterface) packagesState {
	var s packagesState
	for _, list := range lists {
		s.lens = append(s.lens, len(list))
		for _, p := range list {
			s.packages = append(s.packages, p)
			s.revs = append(s.revs, p.Rev())
		}
	}

	return s
}

func (a packagesState) same(b packagesState) bool {
	return slices.Equal(a.lens, b.lens) && slices.Equal(a.packages, b.packages) && slices.Equal(a.revs, b.revs)
}
