<?php

/*
 * maestro's plugin shim: Composer\Installer\MetapackageInstaller
 * (docs/PLUGINS.md §4.7, §5.6): its peer is a Go MetapackageInstaller.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Installer;

use Maestro\Shim\Installers;

class MetapackageInstaller implements \Composer\Installer\InstallerInterface
{
    private $io;

    public function __construct(\Composer\IO\IOInterface $io)
    {
        $this->io = $io;

        Installers::create($this, [$io]);
    }

    public function cleanup($type, \Composer\Package\PackageInterface $package, ?\Composer\Package\PackageInterface $prevPackage = null)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$type, $package, $prevPackage]);
    }

    public function download(\Composer\Package\PackageInterface $package, ?\Composer\Package\PackageInterface $prevPackage = null)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$package, $prevPackage]);
    }

    public function getInstallPath(\Composer\Package\PackageInterface $package)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$package]);
    }

    public function install(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\Package\PackageInterface $package)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$repo, $package]);
    }

    public function isInstalled(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\Package\PackageInterface $package)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$repo, $package]);
    }

    public function prepare($type, \Composer\Package\PackageInterface $package, ?\Composer\Package\PackageInterface $prevPackage = null)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$type, $package, $prevPackage]);
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
}
