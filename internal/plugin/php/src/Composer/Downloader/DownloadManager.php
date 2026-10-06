<?php

/*
 * maestro's plugin shim: Composer\Downloader\DownloadManager
 * (docs/PLUGINS.md §4.8): a proxy of maestro's (dm.*). Its promises settle
 * with what maestro's do (download(): the downloaded file's path), while
 * maestro's event loop runs (Loop::wait()).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Downloader;

use Composer\IO\IOInterface;
use Composer\Package\PackageInterface;
use Composer\Util\Filesystem;
use Maestro\Shim\Promises;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;
use React\Promise\PromiseInterface;

class DownloadManager
{
    public function __construct(IOInterface $io, bool $preferSource = false, ?Filesystem $filesystem = null)
    {
        Remote::unsupported(self::class, '__construct');
    }

    public function setPreferSource(bool $preferSource): self
    {
        Rpc::call('dm.setPreferSource', [$this, $preferSource]);

        return $this;
    }

    public function setPreferDist(bool $preferDist): self
    {
        Rpc::call('dm.setPreferDist', [$this, $preferDist]);

        return $this;
    }

    public function setPreferences(array $preferences): self
    {
        Rpc::call('dm.setPreferences', [$this, $preferences]);

        return $this;
    }

    public function setSourceFallback(bool $sourceFallback): self
    {
        Rpc::call('dm.setSourceFallback', [$this, $sourceFallback]);

        return $this;
    }

    public function setDownloader(string $type, DownloaderInterface $downloader): self
    {
        Rpc::call('dm.setDownloader', [$this, $type, $downloader]);

        return $this;
    }

    public function getDownloader(string $type): DownloaderInterface
    {
        return Rpc::call('dm.getDownloader', [$this, $type]);
    }

    public function getDownloaderForPackage(PackageInterface $package): ?DownloaderInterface
    {
        return Rpc::call('dm.getDownloaderForPackage', [$this, $package]);
    }

    public function getDownloaderType(DownloaderInterface $downloader): string
    {
        return Rpc::call('dm.getDownloaderType', [$this, $downloader]);
    }

    public function download(PackageInterface $package, string $targetDir, ?PackageInterface $prevPackage = null): PromiseInterface
    {
        return Promises::fromMaestro(Rpc::call('dm.download', [$this, $package, $targetDir, $prevPackage]));
    }

    public function prepare(string $type, PackageInterface $package, string $targetDir, ?PackageInterface $prevPackage = null): PromiseInterface
    {
        return Promises::fromMaestro(Rpc::call('dm.prepare', [$this, $type, $package, $targetDir, $prevPackage]));
    }

    public function install(PackageInterface $package, string $targetDir): PromiseInterface
    {
        return Promises::fromMaestro(Rpc::call('dm.install', [$this, $package, $targetDir]));
    }

    public function update(PackageInterface $initial, PackageInterface $target, string $targetDir): PromiseInterface
    {
        return Promises::fromMaestro(Rpc::call('dm.update', [$this, $initial, $target, $targetDir]));
    }

    public function remove(PackageInterface $package, string $targetDir): PromiseInterface
    {
        return Promises::fromMaestro(Rpc::call('dm.remove', [$this, $package, $targetDir]));
    }

    public function cleanup(string $type, PackageInterface $package, string $targetDir, ?PackageInterface $prevPackage = null): PromiseInterface
    {
        return Promises::fromMaestro(Rpc::call('dm.cleanup', [$this, $type, $package, $targetDir, $prevPackage]));
    }

    protected function resolvePackageInstallPreference(PackageInterface $package): string
    {
        Remote::unsupported(self::class, 'resolvePackageInstallPreference');
    }
}
