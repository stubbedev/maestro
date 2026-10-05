// The parts of Composer\PartialComposer and Composer\Composer the
// dispatcher reads. internal/composer implements them; they are interfaces
// here because that package imports this one.

package eventdispatcher

import (
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

// PartialComposer is Composer\PartialComposer as far as the dispatcher
// uses it: the root package (scripts, binaries) and the config (bin-dir).
type PartialComposer interface {
	Package() pkg.RootPackageInterface
	Config() *config.Config
}

// Composer is a fully loaded Composer\Composer (`instanceof Composer`).
// ScriptAutoloader gives makeAutoloader access to the local repository,
// autoload generator and installation manager the Composer instance holds
// at the time of the call.
type Composer interface {
	PartialComposer
	ScriptAutoloader() ScriptAutoloader
}

// ScriptAutoloader is what EventDispatcher::makeAutoloader asks of
// Composer's services before PHP code runs in a listener.
type ScriptAutoloader interface {
	// CanonicalLocalPackages is
	// getRepositoryManager()->getLocalRepository()->getCanonicalPackages().
	CanonicalLocalPackages() ([]pkg.PackageInterface, error)
	// SetDevMode is getAutoloadGenerator()->setDevMode().
	SetDevMode(devMode bool)
	// CreateLoader is, on the autoload generator,
	// createLoader(parseAutoloads(buildPackageMap(getInstallationManager(),
	// root, packages), root), config vendor-dir): the class loader for the
	// root package and packages, including the generator's own output
	// (classmap scan warnings).
	CreateLoader(packages []pkg.PackageInterface) (*LoaderContents, error)
}

// LoaderContents is the Composer\Autoload\ClassLoader createLoader returns,
// as data the plugin runtime rebuilds in PHP with the same calls in the
// same order: add() for each PSR-0 prefix, addPsr4() for each PSR-4 prefix,
// then addClassMap().
type LoaderContents struct {
	// VendorDir is the ClassLoader's $vendorDir.
	VendorDir string
	// Psr0 maps a namespace prefix to its path or list of paths; nil when
	// the autoloads have no psr-0 key.
	Psr0 *php.Array
	// Psr4 maps a namespace prefix to its path or list of paths; nil when
	// the autoloads have no psr-4 key.
	Psr4 *php.Array
	// ClassMap maps a class to its file; nil when the autoloads have no
	// classmap key (addClassMap is then not called).
	ClassMap *php.Array
}
