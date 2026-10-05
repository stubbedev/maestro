// Ports src/Composer/Installer/MetapackageInstaller.php and
// NoopInstaller.php.

package installer

import (
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver/operation"
)

// MetapackageInstaller ports Composer\Installer\MetapackageInstaller:
// metapackages have nothing on disk, they are only recorded in the
// repository.
type MetapackageInstaller struct {
	io mio.IO
}

var _ Installer = (*MetapackageInstaller)(nil)

// NewMetapackageInstaller is new MetapackageInstaller($io).
func NewMetapackageInstaller(io mio.IO) *MetapackageInstaller {
	return &MetapackageInstaller{io: io}
}

// Supports is supports().
func (*MetapackageInstaller) Supports(packageType string) (bool, error) {
	return packageType == "metapackage", nil
}

// IsInstalled is isInstalled().
func (*MetapackageInstaller) IsInstalled(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (bool, error) {
	return repo.HasPackage(p)
}

// Download is download(): a noop.
func (*MetapackageInstaller) Download(_, _ pkg.PackageInterface) (*Promise, error) {
	return Resolved(), nil
}

// Prepare is prepare(): a noop.
func (*MetapackageInstaller) Prepare(_ string, _, _ pkg.PackageInterface) (*Promise, error) {
	return Resolved(), nil
}

// Cleanup is cleanup(): a noop.
func (*MetapackageInstaller) Cleanup(_ string, _, _ pkg.PackageInterface) (*Promise, error) {
	return Resolved(), nil
}

// Install is install().
func (m *MetapackageInstaller) Install(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (*Promise, error) {
	m.io.WriteError("  - "+operation.FormatInstall(p, false), true, mio.Normal)

	if err := repo.AddPackage(pkg.Clone(p)); err != nil {
		return nil, err
	}

	return Resolved(), nil
}

// Update is update().
func (m *MetapackageInstaller) Update(repo repository.InstalledRepositoryInterface, initial, target pkg.PackageInterface) (*Promise, error) {
	if err := requireInstalled(phperr.At("MetapackageInstaller.php", 98), repo, initial); err != nil {
		return nil, err
	}

	line, err := operation.FormatUpdate(initial, target)
	if err != nil {
		return nil, err
	}

	m.io.WriteError("  - "+line, true, mio.Normal)

	if err := repo.RemovePackage(initial); err != nil {
		return nil, err
	}

	if err := repo.AddPackage(pkg.Clone(target)); err != nil {
		return nil, err
	}

	return Resolved(), nil
}

// Uninstall is uninstall().
func (m *MetapackageInstaller) Uninstall(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (*Promise, error) {
	if err := requireInstalled(phperr.At("MetapackageInstaller.php", 115), repo, p); err != nil {
		return nil, err
	}

	m.io.WriteError("  - "+operation.FormatUninstall(p), true, mio.Normal)

	if err := repo.RemovePackage(p); err != nil {
		return nil, err
	}

	return Resolved(), nil
}

// InstallPath is getInstallPath(): null.
func (*MetapackageInstaller) InstallPath(pkg.PackageInterface) (string, bool, error) {
	return "", false, nil
}

// NoopInstaller ports Composer\Installer\NoopInstaller: it installs
// nothing but marks packages installed in the repository (dry runs).
type NoopInstaller struct{}

var _ Installer = (*NoopInstaller)(nil)

// NewNoopInstaller is new NoopInstaller().
func NewNoopInstaller() *NoopInstaller { return &NoopInstaller{} }

// Supports is supports(): every type.
func (*NoopInstaller) Supports(string) (bool, error) { return true, nil }

// IsInstalled is isInstalled().
func (*NoopInstaller) IsInstalled(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (bool, error) {
	return repo.HasPackage(p)
}

// Download is download(): a noop.
func (*NoopInstaller) Download(_, _ pkg.PackageInterface) (*Promise, error) {
	return Resolved(), nil
}

// Prepare is prepare(): a noop.
func (*NoopInstaller) Prepare(_ string, _, _ pkg.PackageInterface) (*Promise, error) {
	return Resolved(), nil
}

// Cleanup is cleanup(): a noop.
func (*NoopInstaller) Cleanup(_ string, _, _ pkg.PackageInterface) (*Promise, error) {
	return Resolved(), nil
}

// Install is install().
func (*NoopInstaller) Install(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (*Promise, error) {
	if err := addIfMissing(repo, p); err != nil {
		return nil, err
	}

	return Resolved(), nil
}

// Update is update().
func (*NoopInstaller) Update(repo repository.InstalledRepositoryInterface, initial, target pkg.PackageInterface) (*Promise, error) {
	if err := requireInstalled(phperr.At("NoopInstaller.php", 85), repo, initial); err != nil {
		return nil, err
	}

	if err := repo.RemovePackage(initial); err != nil {
		return nil, err
	}

	if err := addIfMissing(repo, target); err != nil {
		return nil, err
	}

	return Resolved(), nil
}

// Uninstall is uninstall().
func (*NoopInstaller) Uninstall(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (*Promise, error) {
	if err := requireInstalled(phperr.At("NoopInstaller.php", 102), repo, p); err != nil {
		return nil, err
	}

	if err := repo.RemovePackage(p); err != nil {
		return nil, err
	}

	return Resolved(), nil
}

// InstallPath is getInstallPath(): the package name and target dir,
// relative.
func (*NoopInstaller) InstallPath(p pkg.PackageInterface) (string, bool, error) {
	if targetDir := p.TargetDir(); targetDir.Valid && php.ToBool(targetDir.S) {
		return p.PrettyName() + "/" + targetDir.S, true, nil
	}

	return p.PrettyName(), true, nil
}
