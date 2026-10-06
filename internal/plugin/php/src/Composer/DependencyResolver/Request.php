<?php

/*
 * maestro's plugin shim: Composer\DependencyResolver\Request
 * (docs/PLUGINS.md §4.12): the request of PRE_POOL_CREATE, a proxy of
 * maestro's (request.*). Creating one in PHP is not supported.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\DependencyResolver;

use Composer\Package\BasePackage;
use Composer\Package\PackageInterface;
use Composer\Repository\LockArrayRepository;
use Composer\Semver\Constraint\ConstraintInterface;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class Request
{
    public const UPDATE_ONLY_LISTED = 0;
    public const UPDATE_LISTED_WITH_TRANSITIVE_DEPS_NO_ROOT_REQUIRE = 1;
    public const UPDATE_LISTED_WITH_TRANSITIVE_DEPS = 2;

    protected $lockedRepository;
    protected $requires = [];
    protected $fixedPackages = [];
    protected $lockedPackages = [];
    protected $fixedLockedPackages = [];
    protected $updateAllowList = [];
    protected $updateAllowTransitiveDependencies = false;

    public function __construct(?LockArrayRepository $lockedRepository = null)
    {
        Remote::unsupported(self::class, '__construct');
    }

    public function requireName(string $packageName, ?ConstraintInterface $constraint = null): void
    {
        Rpc::call('request.requireName', [$this, $packageName, $constraint]);
    }

    public function fixPackage(BasePackage $package): void
    {
        Rpc::call('request.fixPackage', [$this, $package]);
    }

    public function lockPackage(BasePackage $package): void
    {
        Rpc::call('request.lockPackage', [$this, $package]);
    }

    public function fixLockedPackage(BasePackage $package): void
    {
        Rpc::call('request.fixLockedPackage', [$this, $package]);
    }

    public function unlockPackage(BasePackage $package): void
    {
        Rpc::call('request.unlockPackage', [$this, $package]);
    }

    public function setUpdateAllowList(array $updateAllowList, $updateAllowTransitiveDependencies): void
    {
        Rpc::call('request.setUpdateAllowList', [$this, $updateAllowList, $updateAllowTransitiveDependencies]);
    }

    public function getUpdateAllowList(): array
    {
        return Rpc::call('request.getUpdateAllowList', [$this]);
    }

    public function getUpdateAllowTransitiveDependencies(): bool
    {
        return Rpc::call('request.getUpdateAllowTransitiveDependencies', [$this]);
    }

    public function getUpdateAllowTransitiveRootDependencies(): bool
    {
        return Rpc::call('request.getUpdateAllowTransitiveRootDependencies', [$this]);
    }

    public function getRequires(): array
    {
        return Rpc::call('request.getRequires', [$this]);
    }

    public function getFixedPackages(): array
    {
        return Rpc::call('request.getFixedPackages', [$this]);
    }

    public function isFixedPackage(BasePackage $package): bool
    {
        return Rpc::call('request.isFixedPackage', [$this, $package]);
    }

    public function getLockedPackages(): array
    {
        return Rpc::call('request.getLockedPackages', [$this]);
    }

    public function isLockedPackage(PackageInterface $package): bool
    {
        return Rpc::call('request.isLockedPackage', [$this, $package]);
    }

    public function getFixedOrLockedPackages(): array
    {
        return Rpc::call('request.getFixedOrLockedPackages', [$this]);
    }

    public function getPresentMap(bool $packageIds = false): array
    {
        return Rpc::call('request.getPresentMap', [$this, $packageIds]);
    }

    public function getFixedPackagesMap(): array
    {
        return Rpc::call('request.getFixedPackagesMap', [$this]);
    }

    public function getLockedRepository(): ?LockArrayRepository
    {
        return Rpc::call('request.getLockedRepository', [$this]);
    }

    public function restrictPackages(array $names): void
    {
        Rpc::call('request.restrictPackages', [$this, $names]);
    }

    public function getRestrictedPackages(): ?array
    {
        return Rpc::call('request.getRestrictedPackages', [$this]);
    }
}
