// The phases of Installer::run() for the plugin shim: what a subclass of
// Composer\Installer written in PHP calls on itself (parent::doInstall()).

package composer

import "github.com/stubbedev/maestro/internal/repository"

// DoUpdate is the protected doUpdate($localRepo, $doInstall): the update
// with the installer's current settings, without run()'s steps around it.
func (i *Installer) DoUpdate(localRepo repository.InstalledRepositoryInterface, doInstall bool) (int, error) {
	return i.doUpdate(localRepo, doInstall)
}

// DoInstall is the protected doInstall($localRepo, $alreadySolved).
func (i *Installer) DoInstall(localRepo repository.InstalledRepositoryInterface, alreadySolved bool) (int, error) {
	return i.doInstall(localRepo, alreadySolved)
}
