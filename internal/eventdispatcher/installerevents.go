// Ports src/Composer/Installer/PackageEvent.php, PackageEvents.php,
// InstallerEvent.php and InstallerEvents.php.
//
// These live here rather than in internal/installer because EventDispatcher
// creates them (dispatchPackageEvent, dispatchInstallerEvent) and
// internal/installer imports this package. The operation and transaction
// types belong to internal/resolver, which is above this package, so they
// are held through the interfaces below.

package eventdispatcher

import (
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/pkg"
)

// The PackageEvents constants.
const (
	// PrePackageInstall occurs before a package is installed.
	PrePackageInstall = "pre-package-install"
	// PostPackageInstall occurs after a package is installed.
	PostPackageInstall = "post-package-install"
	// PrePackageUpdate occurs before a package is updated.
	PrePackageUpdate = "pre-package-update"
	// PostPackageUpdate occurs after a package is updated.
	PostPackageUpdate = "post-package-update"
	// PrePackageUninstall occurs before a package is removed.
	PrePackageUninstall = "pre-package-uninstall"
	// PostPackageUninstall occurs after a package is removed.
	PostPackageUninstall = "post-package-uninstall"
)

// PreOperationsExec is InstallerEvents::PRE_OPERATIONS_EXEC: it occurs
// before the operations of a transaction are executed.
const PreOperationsExec = "pre-operations-exec"

// Operation is Composer\DependencyResolver\Operation\OperationInterface;
// String is its __toString().
type Operation interface {
	String() string
}

// Transaction is a Composer\DependencyResolver\Transaction (an
// internal/resolver transaction; the dispatcher only carries it).
type Transaction any

// PackageEvent is Composer\Installer\PackageEvent.
type PackageEvent struct {
	BaseEvent
	composer   Composer
	io         io.IO
	devMode    bool
	localRepo  pkg.Repository
	operations []Operation
	operation  Operation
}

// NewPackageEvent is new PackageEvent($eventName, $composer, $io,
// $devMode, $localRepo, $operations, $operation).
func NewPackageEvent(name string, composer Composer, io io.IO, devMode bool, localRepo pkg.Repository, operations []Operation, operation Operation) *PackageEvent {
	e := &PackageEvent{composer: composer, io: io, devMode: devMode, localRepo: localRepo, operations: operations, operation: operation}
	e.init(name, nil, nil)

	return e
}

// Class implements Event.
func (*PackageEvent) Class() string { return `Composer\Installer\PackageEvent` }

// Composer is getComposer().
func (e *PackageEvent) Composer() Composer { return e.composer }

// IO is getIO().
func (e *PackageEvent) IO() io.IO { return e.io }

// IsDevMode is isDevMode().
func (e *PackageEvent) IsDevMode() bool { return e.devMode }

// LocalRepo is getLocalRepo(): the installed repository.
func (e *PackageEvent) LocalRepo() pkg.Repository { return e.localRepo }

// Operations is getOperations(): all operations of the run.
func (e *PackageEvent) Operations() []Operation { return e.operations }

// Operation is getOperation(): the operation this event is about.
func (e *PackageEvent) Operation() Operation { return e.operation }

// InstallerEvent is Composer\Installer\InstallerEvent.
type InstallerEvent struct {
	BaseEvent
	composer          Composer
	io                io.IO
	devMode           bool
	executeOperations bool
	transaction       Transaction
}

// NewInstallerEvent is new InstallerEvent($eventName, $composer, $io,
// $devMode, $executeOperations, $transaction).
func NewInstallerEvent(name string, composer Composer, io io.IO, devMode, executeOperations bool, transaction Transaction) *InstallerEvent {
	e := &InstallerEvent{composer: composer, io: io, devMode: devMode, executeOperations: executeOperations, transaction: transaction}
	e.init(name, nil, nil)

	return e
}

// Class implements Event.
func (*InstallerEvent) Class() string { return `Composer\Installer\InstallerEvent` }

// Composer is getComposer().
func (e *InstallerEvent) Composer() Composer { return e.composer }

// IO is getIO().
func (e *InstallerEvent) IO() io.IO { return e.io }

// IsDevMode is isDevMode().
func (e *InstallerEvent) IsDevMode() bool { return e.devMode }

// IsExecutingOperations is isExecutingOperations(): false in --dry-run.
func (e *InstallerEvent) IsExecutingOperations() bool { return e.executeOperations }

// Transaction is getTransaction().
func (e *InstallerEvent) Transaction() Transaction { return e.transaction }
