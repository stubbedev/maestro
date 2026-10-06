<?php

/*
 * maestro's plugin shim: Composer\Installer\ProjectInstaller
 * (docs/PLUGINS.md §4.7, §5.6): its peer is a Go ProjectInstaller.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Installer;

use Maestro\Shim\Installers;

class ProjectInstaller implements \Composer\Installer\InstallerInterface
{
    private $installPath;
    private $downloadManager;
    private $filesystem;

    public function __construct(string $installPath, \Composer\Downloader\DownloadManager $dm, \Composer\Util\Filesystem $fs)
    {
        $this->installPath = rtrim(strtr($installPath, '\\', '/'), '/').'/';
        $this->downloadManager = $dm;
        $this->filesystem = $fs;

        Installers::create($this, [$installPath, $dm]);
    }

    public function cleanup($type, \Composer\Package\PackageInterface $package, ?\Composer\Package\PackageInterface $prevPackage = null): ?\React\Promise\PromiseInterface
    {
        return Installers::base($this, self::class, __FUNCTION__, [$type, $package, $prevPackage]);
    }

    public function download(\Composer\Package\PackageInterface $package, ?\Composer\Package\PackageInterface $prevPackage = null): ?\React\Promise\PromiseInterface
    {
        return Installers::base($this, self::class, __FUNCTION__, [$package, $prevPackage]);
    }

    public function getInstallPath(\Composer\Package\PackageInterface $package): string
    {
        return Installers::base($this, self::class, __FUNCTION__, [$package]);
    }

    public function install(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\Package\PackageInterface $package): ?\React\Promise\PromiseInterface
    {
        return Installers::base($this, self::class, __FUNCTION__, [$repo, $package]);
    }

    public function isInstalled(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\Package\PackageInterface $package): bool
    {
        return Installers::base($this, self::class, __FUNCTION__, [$repo, $package]);
    }

    public function prepare($type, \Composer\Package\PackageInterface $package, ?\Composer\Package\PackageInterface $prevPackage = null): ?\React\Promise\PromiseInterface
    {
        return Installers::base($this, self::class, __FUNCTION__, [$type, $package, $prevPackage]);
    }

    public function supports(string $packageType): bool
    {
        return Installers::base($this, self::class, __FUNCTION__, [$packageType]);
    }

    public function uninstall(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\Package\PackageInterface $package): ?\React\Promise\PromiseInterface
    {
        return Installers::base($this, self::class, __FUNCTION__, [$repo, $package]);
    }

    public function update(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\Package\PackageInterface $initial, \Composer\Package\PackageInterface $target): ?\React\Promise\PromiseInterface
    {
        return Installers::base($this, self::class, __FUNCTION__, [$repo, $initial, $target]);
    }
}
