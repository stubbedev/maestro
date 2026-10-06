<?php

/*
 * maestro's plugin shim: Composer\Package\Locker (docs/PLUGINS.md §4.5),
 * a service proxy of maestro's (locker.*): the lock data is maestro's, in
 * memory. One created in PHP is maestro's too.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Package;

class Locker
{
    /** @var \Composer\Json\JsonFile|null the lock file (built from maestro's path for maestro's lockers) */
    private $lockFile;
    public function __construct(\Composer\IO\IOInterface $io, \Composer\Json\JsonFile $lockFile, \Composer\Installer\InstallationManager $installationManager, string $composerFileContents, ?\Composer\Util\ProcessExecutor $process = null)
    {
        $this->lockFile = $lockFile;
        $io = \Maestro\Shim\Remote::read($lockFile, \Composer\Json\JsonFile::class, ['io'])['io'];
        \Maestro\Shim\Rpc::call('locker.new', [$this, $io, $lockFile->getPath(), $installationManager, $composerFileContents, $process]);
    }

    public function getAliases(): array
    {
        return \Maestro\Shim\Rpc::call('locker.getAliases', [$this]);
    }

    public static function getContentHash(string $composerFileContents): string
    {
        return \Maestro\Shim\Rpc::call('locker.getContentHash', [$composerFileContents]);
    }

    public function getDevPackageNames(): array
    {
        return \Maestro\Shim\Rpc::call('locker.getDevPackageNames', [$this]);
    }

    public function getJsonFile(): \Composer\Json\JsonFile
    {
        if ($this->lockFile === null) {
            $this->lockFile = new \Composer\Json\JsonFile(\Maestro\Shim\Rpc::call('locker.getJsonFile', [$this]));
        }

        return $this->lockFile;
    }

    public function getLockData(): array
    {
        return \Maestro\Shim\Rpc::call('locker.getLockData', [$this]);
    }

    public function getLockedRepository(bool $withDevReqs = false): \Composer\Repository\LockArrayRepository
    {
        return \Maestro\Shim\Rpc::call('locker.getLockedRepository', [$this, $withDevReqs]);
    }

    public function getMinimumStability(): string
    {
        return \Maestro\Shim\Rpc::call('locker.getMinimumStability', [$this]);
    }

    public function getMissingRequirementInfo(\Composer\Package\RootPackageInterface $package, bool $includeDev): array
    {
        return \Maestro\Shim\Rpc::call('locker.getMissingRequirementInfo', [$this, $package, $includeDev]);
    }

    public function getPlatformOverrides(): array
    {
        return \Maestro\Shim\Rpc::call('locker.getPlatformOverrides', [$this]);
    }

    public function getPlatformRequirements(bool $withDevReqs = false): array
    {
        return \Maestro\Shim\Rpc::call('locker.getPlatformRequirements', [$this, $withDevReqs]);
    }

    public function getPluginApi()
    {
        return \Maestro\Shim\Rpc::call('locker.getPluginApi', [$this]);
    }

    public function getPreferLowest(): ?bool
    {
        return \Maestro\Shim\Rpc::call('locker.getPreferLowest', [$this]);
    }

    public function getPreferStable(): ?bool
    {
        return \Maestro\Shim\Rpc::call('locker.getPreferStable', [$this]);
    }

    public function getStabilityFlags(): array
    {
        return \Maestro\Shim\Rpc::call('locker.getStabilityFlags', [$this]);
    }

    public function isFresh(): bool
    {
        return \Maestro\Shim\Rpc::call('locker.isFresh', [$this]);
    }

    public function isLocked(): bool
    {
        return \Maestro\Shim\Rpc::call('locker.isLocked', [$this]);
    }

    public function setLockData(array $packages, ?array $devPackages, array $platformReqs, array $platformDevReqs, array $aliases, string $minimumStability, array $stabilityFlags, bool $preferStable, bool $preferLowest, array $platformOverrides, bool $write = true): bool
    {
        return \Maestro\Shim\Rpc::call('locker.setLockData', [$this, array_values($packages), $devPackages === null ? null : array_values($devPackages), $platformReqs, $platformDevReqs, $aliases, $minimumStability, $stabilityFlags, $preferStable, $preferLowest, $platformOverrides, $write]);
    }

    public function updateHash(\Composer\Json\JsonFile $composerJson, ?callable $dataProcessor = null): void
    {
        \Maestro\Shim\Rpc::call('locker.updateHash', [$this, $composerJson->getPath(), $dataProcessor]);
    }
}
