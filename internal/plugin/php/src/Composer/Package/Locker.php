<?php

/*
 * maestro's plugin shim: Composer\Package\Locker (docs/PLUGINS.md §4.5),
 * a service proxy of maestro's (locker.*): the lock data is maestro's, in
 * memory.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Package;

class Locker
{
    public function __construct(\Composer\IO\IOInterface $io, \Composer\Json\JsonFile $lockFile, \Composer\Installer\InstallationManager $installationManager, string $composerFileContents, ?\Composer\Util\ProcessExecutor $process = null)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Package\\Locker::__construct() in plugins yet');
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
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Package\\Locker::getJsonFile() in plugins yet');
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
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Package\\Locker::setLockData() in plugins yet');
    }

    public function updateHash(\Composer\Json\JsonFile $composerJson, ?callable $dataProcessor = null): void
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Package\\Locker::updateHash() in plugins yet');
    }
}
