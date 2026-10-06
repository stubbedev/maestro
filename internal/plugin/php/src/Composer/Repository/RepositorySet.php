<?php

/*
 * maestro's plugin shim: Composer\Repository\RepositorySet
 * (docs/PLUGINS.md §4.6): a proxy of maestro's (reposet.*); one created in
 * PHP is maestro's too. Pools are not supported yet.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Repository;

use Composer\DependencyResolver\Pool;
use Composer\DependencyResolver\Request;
use Composer\EventDispatcher\EventDispatcher;
use Composer\IO\IOInterface;
use Composer\Semver\Constraint\ConstraintInterface;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class RepositorySet
{
    public const ALLOW_UNACCEPTABLE_STABILITIES = 1;
    public const ALLOW_SHADOWED_REPOSITORIES = 2;

    public function __construct(string $minimumStability = 'stable', array $stabilityFlags = [], array $rootAliases = [], array $rootReferences = [], array $rootRequires = [], array $temporaryConstraints = [])
    {
        Rpc::call('reposet.new', [$this, $minimumStability, $stabilityFlags, $rootAliases, $rootReferences, $rootRequires, $temporaryConstraints]);
    }

    public function allowInstalledRepositories(bool $allow = true): void
    {
        Rpc::call('reposet.allowInstalledRepositories', [$this, $allow]);
    }

    public function getRootRequires(): array
    {
        return Rpc::call('reposet.getRootRequires', [$this]);
    }

    public function getTemporaryConstraints(): array
    {
        return Rpc::call('reposet.getTemporaryConstraints', [$this]);
    }

    public function addRepository(RepositoryInterface $repo): void
    {
        Rpc::call('reposet.addRepository', [$this, $repo]);
    }

    public function findPackages(string $name, ?ConstraintInterface $constraint = null, int $flags = 0): array
    {
        return Rpc::call('reposet.findPackages', [$this, $name, $constraint, $flags]);
    }

    public function getProviders(string $packageName): array
    {
        return Rpc::call('reposet.getProviders', [$this, $packageName]);
    }

    public function isPackageAcceptable(array $names, string $stability): bool
    {
        return Rpc::call('reposet.isPackageAcceptable', [$this, $names, $stability]);
    }

    public function getSecurityAdvisories(array $packageNames, bool $allowPartialAdvisories, bool $ignoreUnreachable = false): array
    {
        Remote::unsupported(self::class, 'getSecurityAdvisories');
    }

    public function getMatchingSecurityAdvisories(array $packages, bool $allowPartialAdvisories = false, bool $ignoreUnreachable = false): array
    {
        Remote::unsupported(self::class, 'getMatchingSecurityAdvisories');
    }

    public function createPool(Request $request, IOInterface $io, ?EventDispatcher $eventDispatcher = null, ?\Composer\DependencyResolver\PoolOptimizer $poolOptimizer = null, array $ignoredTypes = [], ?array $allowedTypes = null, ?\Composer\DependencyResolver\SecurityAdvisoryPoolFilter $securityAdvisoryPoolFilter = null, ?\Composer\DependencyResolver\FilterListPoolFilter $filterListPoolFilter = null): Pool
    {
        Remote::unsupported(self::class, 'createPool');
    }

    public function createPoolWithAllPackages(): Pool
    {
        Remote::unsupported(self::class, 'createPoolWithAllPackages');
    }

    public function createPoolForPackage(string $packageName, ?LockArrayRepository $lockedRepo = null): Pool
    {
        Remote::unsupported(self::class, 'createPoolForPackage');
    }

    public function createPoolForPackages(array $packageNames, ?LockArrayRepository $lockedRepo = null): Pool
    {
        Remote::unsupported(self::class, 'createPoolForPackages');
    }
}
