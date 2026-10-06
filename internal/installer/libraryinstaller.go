// Ports src/Composer/Installer/LibraryInstaller.php.

package installer

import (
	"os"
	"strings"

	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
)

// LibraryInstaller ports Composer\Installer\LibraryInstaller: packages
// installed into vendor/<name> through the download manager, with their
// binaries.
//
// Its overridable methods are called through Virtuals (SetVirtuals), so a
// PHP subclass's getInstallPath (composer/installers) is honoured by the
// inherited install/update/uninstall.
type LibraryInstaller struct {
	composer        PartialComposer
	vendorDir       string
	downloadManager DownloadManager
	io              mio.IO
	// typ is null for the default installer, which supports every type.
	typ             pkg.NullString
	filesystem      *util.Filesystem
	binaryInstaller Binaries
	virt            Virtuals

	// realVendorDir is what InitializeVendorDir's realpath() last gave,
	// realVendorInfo that directory's lstat().
	realVendorDir  string
	realVendorInfo os.FileInfo
}

var (
	_ Installer      = (*LibraryInstaller)(nil)
	_ BinaryPresence = (*LibraryInstaller)(nil)
	_ Virtuals       = (*LibraryInstaller)(nil)
)

// NewLibraryInstaller is new LibraryInstaller($io, $composer, $type,
// $filesystem, $binaryInstaller): typ null supports every package type; a
// nil filesystem or binaryInstaller is a new one. The download manager is
// the composer's when it is a full Composer.
func NewLibraryInstaller(io mio.IO, composer PartialComposer, typ pkg.NullString, filesystem *util.Filesystem, binaryInstaller Binaries) (*LibraryInstaller, error) {
	l := &LibraryInstaller{composer: composer, io: io, typ: typ}
	l.virt = l

	if c, ok := composer.(Composer); ok {
		if dm := c.DownloadManager(); dm != nil {
			l.downloadManager = dm
		}
	}

	if filesystem == nil {
		filesystem = util.NewFilesystem(nil)
	}

	l.filesystem = filesystem

	cfg := composer.Config()

	vendorDir, err := cfg.Get("vendor-dir", 0)
	if err != nil {
		return nil, err
	}

	l.vendorDir = strings.TrimRight(php.ToString(vendorDir), "/")

	if binaryInstaller == nil {
		binDir, err := cfg.Get("bin-dir", 0)
		if err != nil {
			return nil, err
		}

		binCompat, err := cfg.Get("bin-compat", 0)
		if err != nil {
			return nil, err
		}

		binaryInstaller = NewBinaryInstaller(io, strings.TrimRight(php.ToString(binDir), "/"), php.ToString(binCompat), l.filesystem, pkg.Str(l.vendorDir))
	}

	l.binaryInstaller = binaryInstaller

	return l, nil
}

// SetVirtuals sets the receiver of the installer's calls to its own
// overridable methods (a PHP subclass's proxy); nil restores the
// installer itself.
func (l *LibraryInstaller) SetVirtuals(v Virtuals) {
	if v == nil {
		v = l
	}

	l.virt = v
}

// Composer is the $composer property.
func (l *LibraryInstaller) Composer() PartialComposer { return l.composer }

// VendorDir is the $vendorDir property.
func (l *LibraryInstaller) VendorDir() string { return l.vendorDir }

// Type is the $type property.
func (l *LibraryInstaller) Type() pkg.NullString { return l.typ }

// Filesystem is the $filesystem property.
func (l *LibraryInstaller) Filesystem() *util.Filesystem { return l.filesystem }

// BinaryInstaller is the $binaryInstaller property.
func (l *LibraryInstaller) BinaryInstaller() Binaries { return l.binaryInstaller }

// IO is the $io property.
func (l *LibraryInstaller) IO() mio.IO { return l.io }

// Supports is supports().
func (l *LibraryInstaller) Supports(packageType string) (bool, error) {
	return !l.typ.Valid || packageType == l.typ.S, nil
}

// IsInstalled is isInstalled().
func (l *LibraryInstaller) IsInstalled(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (bool, error) {
	has, err := repo.HasPackage(p)
	if err != nil || !has {
		return false, err
	}

	installPath, _, err := l.virt.InstallPath(p)
	if err != nil {
		return false, err
	}

	if util.IsReadable(installPath) {
		return true, nil
	}

	if util.IsWindows() && util.IsJunction(installPath) {
		return true, nil
	}

	if isLink(installPath) {
		if _, ok := util.RealpathOK(installPath); !ok {
			return false, nil
		}

		return true, nil
	}

	return false, nil
}

// Download is download().
func (l *LibraryInstaller) Download(p, prev pkg.PackageInterface) (*Promise, error) {
	downloadPath, dm, err := l.prepareCall(p)
	if err != nil {
		return nil, err
	}

	promise, err := dm.Download(p, downloadPath, prev)

	return wrap(util.CallSync(promise, err, `Composer\Downloader\DownloadManager->download`, "LibraryInstaller.php", 111))
}

// Prepare is prepare().
func (l *LibraryInstaller) Prepare(typ string, p, prev pkg.PackageInterface) (*Promise, error) {
	downloadPath, dm, err := l.prepareCall(p)
	if err != nil {
		return nil, err
	}

	return wrap(dm.Prepare(typ, p, downloadPath, prev))
}

// Cleanup is cleanup().
func (l *LibraryInstaller) Cleanup(typ string, p, prev pkg.PackageInterface) (*Promise, error) {
	downloadPath, dm, err := l.prepareCall(p)
	if err != nil {
		return nil, err
	}

	return wrap(dm.Cleanup(typ, p, downloadPath, prev))
}

// prepareCall is the common start of download/prepare/cleanup:
// initializeVendorDir(), getInstallPath() and getDownloadManager().
func (l *LibraryInstaller) prepareCall(p pkg.PackageInterface) (string, DownloadManager, error) {
	if err := l.InitializeVendorDir(); err != nil {
		return "", nil, err
	}

	downloadPath, _, err := l.virt.InstallPath(p)
	if err != nil {
		return "", nil, err
	}

	dm, err := l.DownloadManager()
	if err != nil {
		return "", nil, err
	}

	return downloadPath, dm, nil
}

// Install is install().
func (l *LibraryInstaller) Install(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (*Promise, error) {
	if err := l.InitializeVendorDir(); err != nil {
		return nil, err
	}

	downloadPath, _, err := l.virt.InstallPath(p)
	if err != nil {
		return nil, err
	}

	// remove the binaries if it appears the package files are missing
	if !util.IsReadable(downloadPath) {
		has, err := repo.HasPackage(p)
		if err != nil {
			return nil, err
		}

		if has {
			if err := l.binaryInstaller.RemoveBinaries(p); err != nil {
				return nil, err
			}
		}
	}

	promise, err := l.virt.InstallCode(p)
	if err != nil {
		return nil, err
	}

	installPath, _, err := l.virt.InstallPath(p)
	if err != nil {
		return nil, err
	}

	binaryInstaller := l.binaryInstaller

	return Then(promise, func() (*Promise, error) {
		if err := binaryInstaller.InstallBinaries(p, installPath, true); err != nil {
			return nil, err
		}

		return nil, addIfMissing(repo, p)
	}, nil), nil
}

// addIfMissing is `if (!$repo->hasPackage($package)) $repo->addPackage(clone
// $package)`.
func addIfMissing(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) error {
	has, err := repo.HasPackage(p)
	if err != nil || has {
		return err
	}

	return repo.AddPackage(pkg.Clone(p))
}

// requireInstalled is the "Package is not installed" check of update and
// uninstall, thrown at site.
func requireInstalled(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) error {
	has, err := repo.HasPackage(p)
	if err != nil {
		return err
	}

	if !has {
		return &util.InvalidArgumentError{Message: "Package is not installed: " + p.String()}
	}

	return nil
}

// Update is update().
func (l *LibraryInstaller) Update(repo repository.InstalledRepositoryInterface, initial, target pkg.PackageInterface) (*Promise, error) {
	if err := requireInstalled(repo, initial); err != nil {
		return nil, err
	}

	if err := l.InitializeVendorDir(); err != nil {
		return nil, err
	}

	if err := l.binaryInstaller.RemoveBinaries(initial); err != nil {
		return nil, err
	}

	promise, err := l.virt.UpdateCode(initial, target)
	if err != nil {
		return nil, err
	}

	installPath, _, err := l.virt.InstallPath(target)
	if err != nil {
		return nil, err
	}

	binaryInstaller := l.binaryInstaller

	return Then(promise, func() (*Promise, error) {
		if err := binaryInstaller.InstallBinaries(target, installPath, true); err != nil {
			return nil, err
		}

		if err := repo.RemovePackage(initial); err != nil {
			return nil, err
		}

		return nil, addIfMissing(repo, target)
	}, nil), nil
}

// Uninstall is uninstall().
func (l *LibraryInstaller) Uninstall(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (*Promise, error) {
	if err := requireInstalled(repo, p); err != nil {
		return nil, err
	}

	promise, err := l.virt.RemoveCode(p)
	if err != nil {
		return nil, err
	}

	binaryInstaller := l.binaryInstaller

	downloadPath, err := l.virt.PackageBasePath(p)
	if err != nil {
		return nil, err
	}

	return Then(promise, func() (*Promise, error) {
		if err := binaryInstaller.RemoveBinaries(p); err != nil {
			return nil, err
		}

		if err := repo.RemovePackage(p); err != nil {
			return nil, err
		}

		if strings.Index(p.Name(), "/") > 0 {
			packageVendorDir := util.Dirname(downloadPath)
			if isDir(packageVendorDir) {
				if empty, _ := util.IsDirEmpty(packageVendorDir); empty {
					_ = os.Remove(packageVendorDir)
				}
			}
		}

		return nil, nil
	}, nil), nil
}

// InstallPath is getInstallPath(); it is never null.
func (l *LibraryInstaller) InstallPath(p pkg.PackageInterface) (string, bool, error) {
	if err := l.InitializeVendorDir(); err != nil {
		return "", false, err
	}

	basePath := p.PrettyName()
	if php.ToBool(l.vendorDir) {
		basePath = l.vendorDir + "/" + basePath
	}

	if targetDir := p.TargetDir(); targetDir.Valid && php.ToBool(targetDir.S) {
		return basePath + "/" + targetDir.S, true, nil
	}

	return basePath, true, nil
}

// EnsureBinariesPresence is ensureBinariesPresence(): it makes sure the
// binaries of an installed package are installed.
func (l *LibraryInstaller) EnsureBinariesPresence(p pkg.PackageInterface) error {
	installPath, _, err := l.virt.InstallPath(p)
	if err != nil {
		return err
	}

	return l.binaryInstaller.InstallBinaries(p, installPath, false)
}

// PackageBasePath is getPackageBasePath(): the install path without the
// target-dir (installer plugins override getInstallPath but rarely this).
func (l *LibraryInstaller) PackageBasePath(p pkg.PackageInterface) (string, error) {
	installPath, _, err := l.virt.InstallPath(p)
	if err != nil {
		return "", err
	}

	targetDir := p.TargetDir()
	if !targetDir.Valid || !php.ToBool(targetDir.S) {
		return installPath, nil
	}

	re, err := php.Compile("{/*" + strings.ReplaceAll(php.PregQuote(targetDir.S, ""), "/", "/+") + "/?$}")
	if err != nil {
		return "", err
	}

	out, _, err := re.Replace(installPath, "", -1)

	return out, err
}

// InstallCode is installCode().
func (l *LibraryInstaller) InstallCode(p pkg.PackageInterface) (*Promise, error) {
	downloadPath, _, err := l.virt.InstallPath(p)
	if err != nil {
		return nil, err
	}

	dm, err := l.DownloadManager()
	if err != nil {
		return nil, err
	}

	return wrap(dm.Install(p, downloadPath))
}

// UpdateCode is updateCode().
func (l *LibraryInstaller) UpdateCode(initial, target pkg.PackageInterface) (*Promise, error) {
	initialDownloadPath, _, err := l.virt.InstallPath(initial)
	if err != nil {
		return nil, err
	}

	targetDownloadPath, _, err := l.virt.InstallPath(target)
	if err != nil {
		return nil, err
	}

	if targetDownloadPath != initialDownloadPath {
		// if the target and initial dirs intersect, we force a remove +
		// install to avoid the rename wiping the target dir as part of the
		// initial dir cleanup
		if strings.HasPrefix(initialDownloadPath, targetDownloadPath) || strings.HasPrefix(targetDownloadPath, initialDownloadPath) {
			promise, err := l.virt.RemoveCode(initial)
			if err != nil {
				return nil, err
			}

			return Then(promise, func() (*Promise, error) {
				promise, err := l.virt.InstallCode(target)
				if err != nil || promise != nil {
					return promise, err
				}

				return Resolved(), nil
			}, nil), nil
		}

		if err := l.filesystem.Rename(initialDownloadPath, targetDownloadPath); err != nil {
			return nil, err
		}
	}

	dm, err := l.DownloadManager()
	if err != nil {
		return nil, err
	}

	return wrap(dm.Update(initial, target, targetDownloadPath))
}

// RemoveCode is removeCode().
func (l *LibraryInstaller) RemoveCode(p pkg.PackageInterface) (*Promise, error) {
	downloadPath, err := l.virt.PackageBasePath(p)
	if err != nil {
		return nil, err
	}

	dm, err := l.DownloadManager()
	if err != nil {
		return nil, err
	}

	return wrap(dm.Remove(p, downloadPath))
}

// InitializeVendorDir is initializeVendorDir(): it creates the vendor dir
// and resolves it.
func (l *LibraryInstaller) InitializeVendorDir() error {
	if err := util.EnsureDirectoryExists(l.vendorDir); err != nil {
		return err
	}

	// realpath() of the directory resolved last time is that directory
	// again while it is still the same directory: its path is not walked
	// again for every package (deliberate deviation 3)
	if l.vendorDir != "" && l.vendorDir == l.realVendorDir {
		if fi, err := os.Lstat(l.vendorDir); err == nil && fi.IsDir() && os.SameFile(fi, l.realVendorInfo) {
			return nil
		}
	}

	l.vendorDir = realpathOrFalse(l.vendorDir)
	l.realVendorDir, l.realVendorInfo = l.vendorDir, nil

	if l.vendorDir != "" {
		l.realVendorInfo, _ = os.Lstat(l.vendorDir)
	}

	return nil
}

// DownloadManager is getDownloadManager(): it fails for an installer of a
// PartialComposer.
func (l *LibraryInstaller) DownloadManager() (DownloadManager, error) {
	if l.downloadManager == nil {
		return nil, &util.LogicError{Message: `Composer\Installer\LibraryInstaller should be initialized with a fully loaded Composer instance to be able to install/... packages`}
	}

	return l.downloadManager, nil
}
