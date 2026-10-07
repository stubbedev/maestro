package composer

import (
	"github.com/stubbedev/maestro/internal/classmap"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/installer"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
	"github.com/stubbedev/maestro/internal/resolver/operation"
)

// InstallationManager is the part of Composer\Installer\InstallationManager
// the Composer object graph and the Installer use. internal/installer's
// manager implements it; tests substitute InstallationManagerMock.
type InstallationManager interface {
	repository.InstallationManager

	// IsPackageInstalled ports isPackageInstalled.
	IsPackageInstalled(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (bool, error)
	// Execute ports execute: the operations, in batches, as Composer runs
	// them.
	Execute(repo repository.InstalledRepositoryInterface, operations []operation.Operation, devMode, runScripts, downloadOnly bool) error
	// EnsureBinariesPresence ports ensureBinariesPresence.
	EnsureBinariesPresence(p pkg.PackageInterface) error
	// NotifyInstalls ports notifyInstalls.
	NotifyInstalls(out io.IO)
	// DisablePlugins ports disablePlugins.
	DisablePlugins() error
}

var _ InstallationManager = (*installer.Manager)(nil)

// PluginManager is the seam of Composer\Plugin\PluginManager: the plugin
// runtime (internal/plugin, docs/PLUGINS.md) implements it, and
// Factory.CreatePluginManager builds it. Without a plugin runtime the
// Factory installs NoPluginManager.
type PluginManager interface {
	installer.PluginManager

	// LoadInstalledPlugins ports loadInstalledPlugins.
	LoadInstalledPlugins() error
	// SetRunningInGlobalDir ports setRunningInGlobalDir.
	SetRunningInGlobalDir(runningInGlobalDir bool)
}

// NoPluginManager is the PluginManager of a process without a plugin
// runtime: it loads and registers nothing, and allows every plugin (the
// allow-plugins checks are the plugin runtime's).
type NoPluginManager struct {
	disabled DisablePlugins
}

// NewNoPluginManager returns a NoPluginManager with createComposer's
// $disablePlugins.
func NewNoPluginManager(disablePlugins DisablePlugins) *NoPluginManager {
	return &NoPluginManager{disabled: disablePlugins}
}

// LoadInstalledPlugins implements PluginManager.
func (*NoPluginManager) LoadInstalledPlugins() error { return nil }

// SetRunningInGlobalDir implements PluginManager.
func (*NoPluginManager) SetRunningInGlobalDir(bool) {}

// RegisterPackage implements installer.PluginManager.
func (*NoPluginManager) RegisterPackage(pkg.PackageInterface, bool, bool) error { return nil }

// DeactivatePackage implements installer.PluginManager.
func (*NoPluginManager) DeactivatePackage(pkg.PackageInterface) error { return nil }

// UninstallPackage implements installer.PluginManager.
func (*NoPluginManager) UninstallPackage(pkg.PackageInterface) error { return nil }

// DisablePlugins implements installer.PluginManager.
func (m *NoPluginManager) DisablePlugins() { m.disabled = PluginsDisabled }

// ArePluginsDisabled ports arePluginsDisabled($type).
func (m *NoPluginManager) ArePluginsDisabled(typ string) bool {
	return m.disabled == PluginsDisabled ||
		(typ == "local" && m.disabled == PluginsDisabledLocal) ||
		(typ == "global" && m.disabled == PluginsDisabledGlobal)
}

// IsPluginAllowed implements installer.PluginManager.
func (*NoPluginManager) IsPluginAllowed(string, bool, bool) (bool, error) { return true, nil }

// DisablePlugins is createComposer's $disablePlugins: bool|'local'|'global'.
type DisablePlugins int

// The DisablePlugins values.
const (
	PluginsEnabled DisablePlugins = iota
	PluginsDisabled
	PluginsDisabledLocal
	PluginsDisabledGlobal
)

// ConfigReader is the part of Composer\Config the Installer reads
// (*config.Config implements it).
type ConfigReader interface {
	Get(key string, flags int) (any, error)
}

// EventDispatcher is the part of Composer\EventDispatcher\EventDispatcher
// the Installer uses (*eventdispatcher.EventDispatcher implements it).
type EventDispatcher interface {
	resolver.EventDispatcher
	DispatchScript(eventName string, devMode bool, additionalArgs []string, flags *php.Array) (int, error)
	DispatchInstallerEvent(eventName string, devMode, executeOperations bool, transaction eventdispatcher.Transaction) (int, error)
}

// AutoloadGenerator is the part of Composer\Autoload\AutoloadGenerator the
// Installer uses (*autoload.Generator implements it).
type AutoloadGenerator interface {
	SetClassMapAuthoritative(classMapAuthoritative bool)
	SetApcu(apcu bool, apcuPrefix *string)
	SetRunScripts(runScripts bool)
	SetPlatformRequirementFilter(filter version.PlatformRequirementFilter)
	DumpAutoloads(config ConfigReader, localRepo repository.InstalledRepositoryInterface, root pkg.RootPackageInterface, im InstallationManager, targetDir string, scanPsrPackages bool, suffix string, locker AutoloadLocker) (*classmap.ClassMap, error)
}

// AutoloadLocker is the locker the autoload generator reads.
type AutoloadLocker interface {
	IsLocked() (bool, error)
	LockData() (*php.Array, error)
}
