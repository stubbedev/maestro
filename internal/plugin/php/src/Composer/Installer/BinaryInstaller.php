<?php

/*
 * maestro's plugin shim: Composer\Installer\BinaryInstaller
 * (docs/PLUGINS.md §4.7): the constructor sets Composer's properties and
 * creates the maestro peer (a Go BinaryInstaller), which installs and
 * removes the binaries. A subclass overriding installBinaries() or
 * removeBinaries() is called in PHP by the LibraryInstaller peers it is
 * given to. The protected helpers are the peer's (a subclass's parent::
 * calls); the peer's installBinaries() does not call a subclass's
 * overrides of them (override installBinaries() itself for that).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Installer;

use Composer\Util\Filesystem;
use Maestro\Shim\Installers;

class BinaryInstaller
{
    protected $binCompat;
    protected $binDir;
    protected $filesystem;
    protected $io;
    private $vendorDir;

    public function __construct(\Composer\IO\IOInterface $io, string $binDir, string $binCompat, ?\Composer\Util\Filesystem $filesystem = null, ?string $vendorDir = null)
    {
        $this->binDir = $binDir;
        $this->binCompat = $binCompat;
        $this->io = $io;
        $this->filesystem = $filesystem ?: new Filesystem();
        $this->vendorDir = $vendorDir;

        Installers::createBinary($this, [$io, $binDir, $binCompat, $vendorDir]);
    }

    public static function determineBinaryCaller(string $bin): string
    {
        return \Maestro\Shim\Rpc::call('installer.determineBinaryCaller', [$bin]);
    }

    protected function generateUnixyProxyCode(string $bin, string $link): string
    {
        return Installers::binary($this, __FUNCTION__, [$bin, $link]);
    }

    protected function generateWindowsProxyCode(string $bin, string $link): string
    {
        return Installers::binary($this, __FUNCTION__, [$bin, $link]);
    }

    protected function getBinaries(\Composer\Package\PackageInterface $package): array
    {
        return $package->getBinaries();
    }

    protected function initializeBinDir(): void
    {
        $this->filesystem->ensureDirectoryExists($this->binDir);
        $this->binDir = realpath($this->binDir);
    }

    public function installBinaries(\Composer\Package\PackageInterface $package, string $installPath, bool $warnOnOverwrite = true): void
    {
        Installers::binary($this, __FUNCTION__, [$package, $installPath, $warnOnOverwrite]);
    }

    protected function installFullBinaries(string $binPath, string $link, string $bin, \Composer\Package\PackageInterface $package): void
    {
        Installers::binary($this, __FUNCTION__, [$binPath, $link, $bin, $package]);
    }

    protected function installUnixyProxyBinaries(string $binPath, string $link): void
    {
        Installers::binary($this, __FUNCTION__, [$binPath, $link]);
    }

    public static function isBinPathInsidePackage(string $installPath, string $binPath): bool
    {
        $realBinPath = realpath($binPath);
        $realInstallPath = realpath($installPath);

        // fail closed if either path cannot be resolved
        if (false === $realBinPath || false === $realInstallPath) {
            return false;
        }

        return strpos($realBinPath, $realInstallPath.DIRECTORY_SEPARATOR) === 0;
    }

    public function removeBinaries(\Composer\Package\PackageInterface $package): void
    {
        Installers::binary($this, __FUNCTION__, [$package]);
    }
}
