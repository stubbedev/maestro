// The collaborators of src/Composer/Autoload/AutoloadGenerator.php that
// live in packages above this one, as the narrow interfaces it uses.

package autoload

import (
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

// Config is the part of Composer\Config the generator reads (*config.Config
// is one).
type Config interface {
	Get(key string, flags int) (any, error)
}

// InstalledRepository is the part of Composer\Repository\
// InstalledRepositoryInterface the generator uses.
type InstalledRepository interface {
	// DevPackageNames ports getDevPackageNames.
	DevPackageNames() []string
	// CanonicalPackages ports getCanonicalPackages.
	CanonicalPackages() []pkg.PackageInterface
}

// InstallationManager is the part of Composer\Installer\InstallationManager
// the generator uses.
type InstallationManager interface {
	// InstallPath ports getInstallPath: ok is false for null (a
	// metapackage, which is not installed anywhere).
	InstallPath(p pkg.PackageInterface) (path string, ok bool, err error)
}

// EventDispatcher is the part of Composer\EventDispatcher\EventDispatcher
// the generator uses.
type EventDispatcher interface {
	// DispatchScript ports dispatchScript($eventName, $devMode,
	// $additionalArgs, $flags).
	DispatchScript(eventName string, devMode bool, additionalArgs []string, flags *php.Array) (int, error)
}

// Locker is the part of Composer\Package\Locker the suffix logic uses.
type Locker interface {
	IsLocked() (bool, error)
	LockData() (*php.Array, error)
}

// The script events the generator dispatches (Composer\Script\ScriptEvents).
const (
	PreAutoloadDump  = "pre-autoload-dump"
	PostAutoloadDump = "post-autoload-dump"
)
