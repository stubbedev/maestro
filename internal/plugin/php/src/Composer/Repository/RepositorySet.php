<?php

/*
 * maestro's plugin shim: Composer\Repository\RepositorySet
 * (docs/PLUGINS.md §4.6): a proxy of maestro's (reposet.*); one created in
 * PHP is maestro's too. Its pools are PHP-local Pools of the packages
 * maestro's pool holds.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Repository;

use Composer\Advisory\PartialSecurityAdvisory;
use Composer\Advisory\SecurityAdvisory;
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
        return self::advisories(Rpc::call('reposet.getSecurityAdvisories', [$this, array_values($packageNames), $allowPartialAdvisories, $ignoreUnreachable]));
    }

    public function getMatchingSecurityAdvisories(array $packages, bool $allowPartialAdvisories = false, bool $ignoreUnreachable = false): array
    {
        return self::advisories(Rpc::call('reposet.getMatchingSecurityAdvisories', [$this, array_values($packages), $allowPartialAdvisories, $ignoreUnreachable]));
    }

    public function createPool(Request $request, IOInterface $io, ?EventDispatcher $eventDispatcher = null, ?\Composer\DependencyResolver\PoolOptimizer $poolOptimizer = null, array $ignoredTypes = [], ?array $allowedTypes = null, ?\Composer\DependencyResolver\SecurityAdvisoryPoolFilter $securityAdvisoryPoolFilter = null, ?\Composer\DependencyResolver\FilterListPoolFilter $filterListPoolFilter = null): Pool
    {
        if ($poolOptimizer !== null || $securityAdvisoryPoolFilter !== null || $filterListPoolFilter !== null) {
            Remote::unsupported(self::class, 'createPool');
        }

        return self::pool(Rpc::call('reposet.createPool', [$this, $request, $io, $eventDispatcher, $ignoredTypes, $allowedTypes]));
    }

    public function createPoolWithAllPackages(): Pool
    {
        return self::pool(Rpc::call('reposet.createPoolWithAllPackages', [$this]));
    }

    public function createPoolForPackage(string $packageName, ?LockArrayRepository $lockedRepo = null): Pool
    {
        return $this->createPoolForPackages([$packageName], $lockedRepo);
    }

    public function createPoolForPackages(array $packageNames, ?LockArrayRepository $lockedRepo = null): Pool
    {
        return self::pool(Rpc::call('reposet.createPoolForPackages', [$this, array_values($packageNames), $lockedRepo]));
    }

    /**
     * The Pool of maestro's pool (its packages, with maestro's ids, and
     * the versions it removed).
     *
     * @param array<string, mixed> $d
     */
    private static function pool(array $d): Pool
    {
        return new Pool($d['packages'], $d['unacceptable'], $d['removedVersions'], [], [], $d['abandonedRemovedVersions']);
    }

    /**
     * The result of getSecurityAdvisories(): maestro's advisories as
     * Composer's objects.
     *
     * @param array<string, mixed> $d
     * @return array{advisories: array<string, array<PartialSecurityAdvisory|SecurityAdvisory>>, unreachableRepos: array<string>}
     */
    private static function advisories(array $d): array
    {
        $out = [];
        foreach ($d['advisories'] as $name => $list) {
            foreach ($list as $a) {
                $class = isset($a['title']) ? SecurityAdvisory::class : PartialSecurityAdvisory::class;
                $advisory = (new \ReflectionClass($class))->newInstanceWithoutConstructor();
                $advisory->advisoryId = $a['advisoryId'];
                $advisory->packageName = $a['packageName'];
                $advisory->affectedVersions = $a['affectedVersions'];
                if (isset($a['title'])) {
                    $advisory->title = $a['title'];
                    $advisory->sources = $a['sources'];
                    $advisory->reportedAt = new \DateTimeImmutable($a['reportedAt']);
                    $advisory->cve = $a['cve'];
                    $advisory->link = $a['link'];
                    $advisory->severity = $a['severity'];
                }
                $out[$name][] = $advisory;
            }
        }

        return ['advisories' => $out, 'unreachableRepos' => $d['unreachableRepos']];
    }
}
