// Ports src/Composer/Installer/ProjectInstaller.php.

package installer

import (
	"os"
	"strings"

	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
)

// ProjectInstaller ports Composer\Installer\ProjectInstaller: it installs a
// single package into a directory as the root project (create-project).
type ProjectInstaller struct {
	installPath     string
	downloadManager DownloadManager
	filesystem      *util.Filesystem
}

var _ Installer = (*ProjectInstaller)(nil)

// NewProjectInstaller is new ProjectInstaller($installPath, $dm, $fs).
func NewProjectInstaller(installPath string, dm DownloadManager, fs *util.Filesystem) *ProjectInstaller {
	return &ProjectInstaller{
		installPath:     strings.TrimRight(strings.ReplaceAll(installPath, `\`, "/"), "/") + "/",
		downloadManager: dm,
		filesystem:      fs,
	}
}

// Supports is supports(): every type.
func (*ProjectInstaller) Supports(string) (bool, error) { return true, nil }

// IsInstalled is isInstalled(): never.
func (*ProjectInstaller) IsInstalled(repository.InstalledRepositoryInterface, pkg.PackageInterface) (bool, error) {
	return false, nil
}

// Download is download(): the project directory must be empty.
func (i *ProjectInstaller) Download(p, prev pkg.PackageInterface) (*Promise, error) {
	installPath := i.installPath
	if fileExists(installPath) {
		empty, err := util.IsDirEmpty(installPath)
		if err != nil {
			return nil, err
		}

		if !empty {
			return nil, &util.InvalidArgumentError{Site: phperr.At("ProjectInstaller.php", 66), Message: "Project directory " + installPath + " is not empty."}
		}
	}

	if !isDir(installPath) {
		if err := os.MkdirAll(installPath, 0o777); err != nil {
			return nil, &util.ErrorException{Message: "mkdir(): " + util.Strerror(err), Site: phperr.At("ProjectInstaller.php", 69)}
		}
	}

	return wrap(i.downloadManager.Download(p, installPath, prev))
}

// Prepare is prepare().
func (i *ProjectInstaller) Prepare(typ string, p, prev pkg.PackageInterface) (*Promise, error) {
	return wrap(i.downloadManager.Prepare(typ, p, i.installPath, prev))
}

// Cleanup is cleanup().
func (i *ProjectInstaller) Cleanup(typ string, p, prev pkg.PackageInterface) (*Promise, error) {
	return wrap(i.downloadManager.Cleanup(typ, p, i.installPath, prev))
}

// Install is install().
func (i *ProjectInstaller) Install(_ repository.InstalledRepositoryInterface, p pkg.PackageInterface) (*Promise, error) {
	return wrap(i.downloadManager.Install(p, i.installPath))
}

// Update is update(): not supported.
func (*ProjectInstaller) Update(repository.InstalledRepositoryInterface, pkg.PackageInterface, pkg.PackageInterface) (*Promise, error) {
	return nil, &util.InvalidArgumentError{Message: "not supported", Site: phperr.At("ProjectInstaller.php", 104)}
}

// Uninstall is uninstall(): not supported.
func (*ProjectInstaller) Uninstall(repository.InstalledRepositoryInterface, pkg.PackageInterface) (*Promise, error) {
	return nil, &util.InvalidArgumentError{Message: "not supported", Site: phperr.At("ProjectInstaller.php", 112)}
}

// InstallPath is getInstallPath(): the configured install path.
func (i *ProjectInstaller) InstallPath(pkg.PackageInterface) (string, bool, error) {
	return i.installPath, true, nil
}
