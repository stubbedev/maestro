<?php

/*
 * maestro's plugin shim: Composer\Installer\LibraryInstaller
 * (docs/PLUGINS.md §4.7, §5.6). The constructor sets Composer's protected
 * properties and creates the installer's maestro peer, a Go
 * LibraryInstaller; every other method runs the peer's implementation
 * (installer.base), which calls back into PHP for the methods a subclass
 * overrides.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Installer;

use Composer\Composer;
use Composer\Downloader\DownloadManager;
use Composer\IO\IOInterface;
use Composer\Package\PackageInterface;
use Composer\PartialComposer;
use Composer\Repository\InstalledRepositoryInterface;
use Composer\Util\Filesystem;
use Maestro\Shim\Installers;

class LibraryInstaller implements \Composer\Installer\BinaryPresenceInterface, \Composer\Installer\InstallerInterface
{
    protected $binaryInstaller;
    protected $composer;
    protected $downloadManager;
    protected $filesystem;
    protected $io;
    protected $type;
    protected $vendorDir;

    public function __construct(\Composer\IO\IOInterface $io, \Composer\PartialComposer $composer, ?string $type = 'library', ?\Composer\Util\Filesystem $filesystem = null, ?\Composer\Installer\BinaryInstaller $binaryInstaller = null)
    {
        $this->composer = $composer;
        $this->downloadManager = $composer instanceof Composer ? $composer->getDownloadManager() : null;
        $this->io = $io;
        $this->type = $type;

        $this->filesystem = $filesystem ?: new Filesystem();
        $this->vendorDir = rtrim($composer->getConfig()->get('vendor-dir'), '/');
        $this->binaryInstaller = $binaryInstaller ?: new BinaryInstaller($this->io, rtrim($composer->getConfig()->get('bin-dir'), '/'), $composer->getConfig()->get('bin-compat'), $this->filesystem, $this->vendorDir);

        Installers::create($this, [$io, $composer, $type, $this->binaryInstaller]);
    }

    public function cleanup($type, \Composer\Package\PackageInterface $package, ?\Composer\Package\PackageInterface $prevPackage = null)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$type, $package, $prevPackage]);
    }

    public function download(\Composer\Package\PackageInterface $package, ?\Composer\Package\PackageInterface $prevPackage = null)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$package, $prevPackage]);
    }

    public function ensureBinariesPresence(\Composer\Package\PackageInterface $package)
    {
        Installers::base($this, self::class, __FUNCTION__, [$package]);
    }

    protected function getDownloadManager(): \Composer\Downloader\DownloadManager
    {
        assert($this->downloadManager instanceof DownloadManager, new \LogicException(self::class.' should be initialized with a fully loaded Composer instance to be able to install/... packages'));

        return $this->downloadManager;
    }

    public function getInstallPath(\Composer\Package\PackageInterface $package)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$package]);
    }

    protected function getPackageBasePath(\Composer\Package\PackageInterface $package)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$package]);
    }

    protected function initializeVendorDir()
    {
        Installers::base($this, self::class, __FUNCTION__, []);
    }

    public function install(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\Package\PackageInterface $package)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$repo, $package]);
    }

    protected function installCode(\Composer\Package\PackageInterface $package)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$package]);
    }

    public function isInstalled(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\Package\PackageInterface $package)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$repo, $package]);
    }

    public function prepare($type, \Composer\Package\PackageInterface $package, ?\Composer\Package\PackageInterface $prevPackage = null)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$type, $package, $prevPackage]);
    }

    protected function removeCode(\Composer\Package\PackageInterface $package)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$package]);
    }

    public function supports(string $packageType)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$packageType]);
    }

    public function uninstall(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\Package\PackageInterface $package)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$repo, $package]);
    }

    public function update(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\Package\PackageInterface $initial, \Composer\Package\PackageInterface $target)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$repo, $initial, $target]);
    }

    protected function updateCode(\Composer\Package\PackageInterface $initial, \Composer\Package\PackageInterface $target)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$initial, $target]);
    }
}
