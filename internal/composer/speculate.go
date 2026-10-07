// Ports nothing: the installer's autoload scan ahead of the dump, and
// the local repository's write built ahead (deliberate deviation 3,
// speed).

package composer

import (
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

// noOperations reports whether the lock file asks for no package
// operation on localRepo.
func noOperations(lockedRepository *repository.LockArrayRepository, localRepo repository.InstalledRepositoryInterface) bool {
	transaction, err := resolver.NewLocalRepoTransaction(lockedRepository, localRepo)

	return err == nil && len(transaction.Operations()) == 0
}

// prepareLocalRepoWrite has the local repository build, while the lock
// file is verified, the write that ends a no-op install (the installation
// manager writes it after running no operation), when nothing between
// them can change what it writes: the lock file asks for no package
// operation and no listener waits on the events in between. The
// repository builds the write again when what it is made of changed after
// all (repository.FilesystemRepository.PrepareWrite).
func (i *Installer) prepareLocalRepoWrite(lockedRepository *repository.LockArrayRepository, localRepo repository.InstalledRepositoryInterface) {
	if !i.install || !i.executeOperations || i.dryRun || i.downloadOnly {
		return
	}
	r, ok := localRepo.(writePreparer)
	if !ok || i.listenedTo(speculativeEvents[:2]) || !noOperations(lockedRepository, localRepo) {
		return
	}
	devPackageNames, err := i.locker.DevPackageNames()
	if err != nil {
		return
	}
	r.PrepareWrite(i.devMode, i.installationManager, devPackageNames)
}

// speculateAutoloads starts the class map scan of the autoload dump that
// follows a no-op install while the lock file is verified (which waits on
// the filter list's request), when nothing between them can change the
// files: the lock file asks for no package operation and no listener
// waits on the events in between. It returns the generator running the
// scan, nil without one; doInstall discards the scan if operations run
// after all.
func (i *Installer) speculateAutoloads(lockedRepository *repository.LockArrayRepository, localRepo repository.InstalledRepositoryInterface) autoloadSpeculator {
	if !i.dumpAutoloader || !i.install || !i.executeOperations || i.dryRun {
		return nil
	}
	s, ok := i.autoloadGenerator.(autoloadSpeculator)
	if !ok || i.listenedTo(speculativeEvents) || !noOperations(lockedRepository, localRepo) {
		return nil
	}
	s.SpeculateAutoloads(i.config, localRepo, i.pkg, i.installationManager, i.optimizeAutoloader || i.classMapAuthoritative)

	return s
}
