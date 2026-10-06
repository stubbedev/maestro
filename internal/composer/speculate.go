// Ports nothing: the installer's autoload scan ahead of the dump
// (deliberate deviation 3, speed).

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
	if !ok || i.eventDispatcher == nil {
		return nil
	}
	checker, ok := i.eventDispatcher.(dispatchChecker)
	if !ok {
		return nil
	}
	for _, name := range speculativeEvents {
		if checker.WillDispatchTo(eventdispatcher.NewEvent(name, nil, nil)) {
			return nil
		}
	}
	transaction, err := resolver.NewLocalRepoTransaction(lockedRepository, localRepo)
	if err != nil || len(transaction.Operations()) != 0 {
		return nil
	}
	s.SpeculateAutoloads(i.config, localRepo, i.pkg, i.installationManager, i.optimizeAutoloader || i.classMapAuthoritative)

	return s
}
