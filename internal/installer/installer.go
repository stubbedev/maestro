// Ports src/Composer/Installer/InstallerInterface.php and
// BinaryPresenceInterface.php, plus the narrow interfaces of the
// collaborators the installers take.

package installer

import (
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver/operation"
)

// Installer is InstallerInterface. prev is nil for null; methods returning
// a promise return nil for PHP's null, and an error for a synchronous
// throw. Implementations must be comparable (pointers), as
// InstallationManager::removeInstaller finds them by identity.
type Installer interface {
	// Supports is supports($packageType).
	Supports(packageType string) (bool, error)
	// IsInstalled is isInstalled($repo, $package).
	IsInstalled(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (bool, error)
	// Download is download($package, $prevPackage).
	Download(p, prev pkg.PackageInterface) (*Promise, error)
	// Prepare is prepare($type, $package, $prevPackage); typ is install,
	// update or uninstall.
	Prepare(typ operation.Type, p, prev pkg.PackageInterface) (*Promise, error)
	// Install is install($repo, $package).
	Install(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (*Promise, error)
	// Update is update($repo, $initial, $target).
	Update(repo repository.InstalledRepositoryInterface, initial, target pkg.PackageInterface) (*Promise, error)
	// Uninstall is uninstall($repo, $package).
	Uninstall(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (*Promise, error)
	// Cleanup is cleanup($type, $package, $prevPackage).
	Cleanup(typ operation.Type, p, prev pkg.PackageInterface) (*Promise, error)
	// InstallPath is getInstallPath($package); ok false is null (nothing
	// installed on disk).
	InstallPath(p pkg.PackageInterface) (path string, ok bool, err error)
}

// BinaryPresence is BinaryPresenceInterface.
type BinaryPresence interface {
	// EnsureBinariesPresence is ensureBinariesPresence($package).
	EnsureBinariesPresence(p pkg.PackageInterface) error
}

// Virtuals are the methods of LibraryInstaller (and the installers built on
// it) that a PHP subclass may override and that the class calls on itself
// (docs/PLUGINS.md §5.6): every internal call to one of them goes through
// the Virtuals the installer was given (SetVirtuals), the installer itself
// by default. A plugin's installer proxy implements them by calling into
// PHP for the overridden methods and back into the Go base for the others.
type Virtuals interface {
	Supports(packageType string) (bool, error)
	IsInstalled(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (bool, error)
	InstallPath(p pkg.PackageInterface) (path string, ok bool, err error)
	// PackageBasePath is getPackageBasePath($package).
	PackageBasePath(p pkg.PackageInterface) (string, error)
	// InstallCode is installCode($package).
	InstallCode(p pkg.PackageInterface) (*Promise, error)
	// UpdateCode is updateCode($initial, $target).
	UpdateCode(initial, target pkg.PackageInterface) (*Promise, error)
	// RemoveCode is removeCode($package).
	RemoveCode(p pkg.PackageInterface) (*Promise, error)
	EnsureBinariesPresence(p pkg.PackageInterface) error
}

// PartialComposer is the Composer\PartialComposer the installers read:
// its Config.
type PartialComposer interface {
	Config() *config.Config
}

// Composer is a fully loaded Composer\Composer (`instanceof Composer`), which
// gives LibraryInstaller its download manager.
type Composer interface {
	PartialComposer
	DownloadManager() *downloader.DownloadManager
}

// PluginComposer is the Composer PluginInstaller reads its plugin manager
// from (getPluginManager()). It is read when needed, as Composer sets the
// plugin manager after creating the installers.
type PluginComposer interface {
	Composer
	InstallerPluginManager() PluginManager
}

// PluginManager is the part of Composer\Plugin\PluginManager PluginInstaller
// uses; internal/plugin implements it.
type PluginManager interface {
	// RegisterPackage is registerPackage($package, $failOnMissingClasses,
	// $isGlobalPlugin).
	RegisterPackage(p pkg.PackageInterface, failOnMissingClasses, isGlobalPlugin bool) error
	// DeactivatePackage is deactivatePackage($package).
	DeactivatePackage(p pkg.PackageInterface) error
	// UninstallPackage is uninstallPackage($package).
	UninstallPackage(p pkg.PackageInterface) error
	// DisablePlugins is disablePlugins().
	DisablePlugins()
	// ArePluginsDisabled is arePluginsDisabled($type): "local" or
	// "global".
	ArePluginsDisabled(typ string) bool
	// IsPluginAllowed is isPluginAllowed($package, $isGlobalPlugin,
	// $optional): it may prompt, and fails for a plugin that is not
	// allowed.
	IsPluginAllowed(packageName string, isGlobalPlugin, optional bool) (bool, error)
}

// DownloadManager is the part of Composer\Downloader\DownloadManager the
// installers use; *downloader.DownloadManager implements it.
type DownloadManager interface {
	Download(p pkg.PackageInterface, targetDir string, prev pkg.PackageInterface) (*downloader.Promise, error)
	Prepare(typ operation.Type, p pkg.PackageInterface, targetDir string, prev pkg.PackageInterface) (*downloader.Promise, error)
	Install(p pkg.PackageInterface, targetDir string) (*downloader.Promise, error)
	Update(initial, target pkg.PackageInterface, targetDir string) (*downloader.Promise, error)
	Remove(p pkg.PackageInterface, targetDir string) (*downloader.Promise, error)
	Cleanup(typ operation.Type, p pkg.PackageInterface, targetDir string, prev pkg.PackageInterface) (*downloader.Promise, error)
}

// EventDispatcher is the part of Composer\EventDispatcher\EventDispatcher
// InstallationManager uses; *eventdispatcher.EventDispatcher implements
// it.
type EventDispatcher interface {
	DispatchPackageEvent(eventName string, devMode bool, localRepo pkg.Repository, operations []eventdispatcher.Operation, operation eventdispatcher.Operation) (int, error)
}

var (
	_ DownloadManager = (*downloader.DownloadManager)(nil)
	_ EventDispatcher = (*eventdispatcher.EventDispatcher)(nil)
)
