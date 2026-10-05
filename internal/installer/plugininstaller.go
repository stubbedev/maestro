// Ports src/Composer/Installer/PluginInstaller.php.

package installer

import (
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
)

// PluginInstaller ports Composer\Installer\PluginInstaller: a
// LibraryInstaller for composer-plugin and composer-installer packages that
// registers them with the plugin manager once installed.
type PluginInstaller struct {
	*LibraryInstaller
}

var _ Installer = (*PluginInstaller)(nil)

// NewPluginInstaller is new PluginInstaller($io, $composer, $fs,
// $binaryInstaller). The plugin manager is read from the composer
// (PluginComposer) when first needed.
func NewPluginInstaller(io mio.IO, composer PartialComposer, fs *util.Filesystem, binaryInstaller Binaries) (*PluginInstaller, error) {
	l, err := NewLibraryInstaller(io, composer, pkg.Str("composer-plugin"), fs, binaryInstaller)
	if err != nil {
		return nil, err
	}

	p := &PluginInstaller{LibraryInstaller: l}
	l.virt = p

	return p, nil
}

// Supports is supports().
func (*PluginInstaller) Supports(packageType string) (bool, error) {
	return packageType == "composer-plugin" || packageType == "composer-installer", nil
}

// DisablePlugins is disablePlugins().
func (i *PluginInstaller) DisablePlugins() error {
	pm, err := i.PluginManager()
	if err != nil {
		return err
	}

	pm.DisablePlugins()

	return nil
}

// Prepare is prepare(): it fails early if a plugin is not allowed.
func (i *PluginInstaller) Prepare(typ string, p, prev pkg.PackageInterface) (*Promise, error) {
	// fail install process early if it is going to fail due to a plugin
	// not being allowed
	if typ == "install" || typ == "update" {
		pm, err := i.PluginManager()
		if err != nil {
			return nil, err
		}

		if !pm.ArePluginsDisabled("local") {
			optional := false
			if v, ok := p.Extra().Get("plugin-optional"); ok {
				optional = v == true
			}

			if _, err := pm.IsPluginAllowed(p.Name(), false, optional); err != nil {
				return nil, err
			}
		}
	}

	return i.LibraryInstaller.Prepare(typ, p, prev)
}

// Download is download(): plugin packages need a class.
func (i *PluginInstaller) Download(p, prev pkg.PackageInterface) (*Promise, error) {
	class, _ := p.Extra().Get("class")
	if !php.ToBool(class) {
		return nil, &util.UnexpectedValueError{Site: phperr.At("PluginInstaller.php", 71), Message: "Error while installing " + p.PrettyName() + ", composer-plugin packages should have a class defined in their extra key to be usable."}
	}

	return i.LibraryInstaller.Download(p, prev)
}

// Install is install(): the package is registered once installed.
func (i *PluginInstaller) Install(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (*Promise, error) {
	promise, err := i.LibraryInstaller.Install(repo, p)
	if err != nil {
		return nil, err
	}

	return Then(promise, func() (*Promise, error) {
		err := func() error {
			util.WorkaroundFilesystemIssues()

			pm, err := i.PluginManager()
			if err != nil {
				return err
			}

			return pm.RegisterPackage(p, true, false)
		}()
		if err != nil && isException(err) {
			return nil, i.rollbackInstall(err, repo, p)
		}

		return nil, err
	}, nil), nil
}

// Update is update(): the old version is deactivated and the new one
// registered.
func (i *PluginInstaller) Update(repo repository.InstalledRepositoryInterface, initial, target pkg.PackageInterface) (*Promise, error) {
	promise, err := i.LibraryInstaller.Update(repo, initial, target)
	if err != nil {
		return nil, err
	}

	return Then(promise, func() (*Promise, error) {
		err := func() error {
			util.WorkaroundFilesystemIssues()

			pm, err := i.PluginManager()
			if err != nil {
				return err
			}

			if err := pm.DeactivatePackage(initial); err != nil {
				return err
			}

			return pm.RegisterPackage(target, true, false)
		}()
		if err != nil && isException(err) {
			return nil, i.rollbackInstall(err, repo, target)
		}

		return nil, err
	}, nil), nil
}

// Uninstall is uninstall().
func (i *PluginInstaller) Uninstall(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (*Promise, error) {
	pm, err := i.PluginManager()
	if err != nil {
		return nil, err
	}

	if err := pm.UninstallPackage(p); err != nil {
		return nil, err
	}

	return i.LibraryInstaller.Uninstall(repo, p)
}

// rollbackInstall is rollbackInstall(): it uninstalls the plugin whose
// initialization failed and returns the failure. As in Composer, the
// promise parent::uninstall() returns is dropped: its callbacks run while
// the loop finishes the removal.
func (i *PluginInstaller) rollbackInstall(e error, repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) error {
	i.io.WriteError("Plugin initialization failed ("+e.Error()+"), uninstalling plugin", true, mio.Normal)

	if _, err := i.LibraryInstaller.Uninstall(repo, p); err != nil {
		return err
	}

	return e
}

// PluginManager is getPluginManager(): the composer's plugin manager.
func (i *PluginInstaller) PluginManager() (PluginManager, error) {
	c, ok := i.composer.(PluginComposer)
	if !ok {
		return nil, &util.LogicError{Site: phperr.At("PluginInstaller.php", 134), Message: `Composer\Installer\PluginInstaller should be initialized with a fully loaded Composer instance.`}
	}

	return c.InstallerPluginManager(), nil
}
