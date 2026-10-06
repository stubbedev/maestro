package composer

import (
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/installer"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// defaultInstallationManager ports createInstallationManager.
func defaultInstallationManager(loop *http.Loop, out io.IO, dispatcher *eventdispatcher.EventDispatcher) (InstallationManager, error) {
	var ed installer.EventDispatcher
	if dispatcher != nil {
		ed = dispatcher
	}

	return installer.NewManager(loop, out, ed), nil
}

// defaultInstallers ports createDefaultInstallers: the library, plugin and
// metapackage installers.
func defaultInstallers(im InstallationManager, c *PartialComposer, full *Composer, out io.IO, process *util.ProcessExecutor) error {
	manager, ok := im.(*installer.Manager)
	if !ok {
		return nil
	}

	cfg := c.Config()
	get := func(key string) (string, error) {
		leave := phperr.Enter(`Composer\Config->get`, "Factory.php", 587)
		v, err := cfg.Get(key, 0)
		leave()

		// $composer->getConfig()->get(...) at Factory.php:587
		return php.ToString(v), phperr.Call(err, `Composer\Config->get`, "Factory.php", 587)
	}
	binDir, err := get("bin-dir")
	if err != nil {
		return err
	}
	binCompat, err := get("bin-compat")
	if err != nil {
		return err
	}
	vendorDir, err := get("vendor-dir")
	if err != nil {
		return err
	}

	fs := util.NewFilesystem(process)
	binaryInstaller := installer.NewBinaryInstaller(out, php.RtrimSet(binDir, "/"), binCompat, fs, pkg.Str(php.RtrimSet(vendorDir, "/")))

	var composer installer.PartialComposer = c
	if full != nil {
		composer = full
	}
	library, err := installer.NewLibraryInstaller(out, composer, pkg.NullString{}, fs, binaryInstaller)
	if err != nil {
		return err
	}
	manager.AddInstaller(library)
	plugin, err := installer.NewPluginInstaller(out, composer, fs, binaryInstaller)
	if err != nil {
		return err
	}
	manager.AddInstaller(plugin)
	manager.AddInstaller(installer.NewMetapackageInstaller(out))

	return nil
}

// InstallerPluginManager implements installer.PluginComposer: the plugin
// manager PluginInstaller reads when it needs it.
func (c *Composer) InstallerPluginManager() installer.PluginManager {
	if c.pluginManager == nil {
		return nil
	}

	return c.pluginManager
}

var (
	_ installer.PluginComposer  = (*Composer)(nil)
	_ installer.PartialComposer = (*PartialComposer)(nil)
)
