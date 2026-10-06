<?php

/*
 * maestro's plugin shim: Composer\Repository\CompositeRepository,
 * reimplemented with Composer 2.10.3's behaviour (docs/PLUGINS.md §4.6):
 * a composite created in PHP asks its repositories (maestro's proxies or
 * PHP's own); one of maestro's is a proxy (repo.*).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Repository;

use Composer\IO\IOInterface;
use Composer\Package\BasePackage;
use Composer\Package\PackageInterface;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class CompositeRepository implements RepositoryInterface
{
    private $repositories;

    public function __construct(array $repositories)
    {
        $this->repositories = [];
        foreach ($repositories as $repository) {
            $this->addRepository($repository);
        }
    }

    public function getRepoName(): string
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.getRepoName', [$this]);
        }

        $names = [];
        foreach ($this->repositories as $repository) {
            $names[] = $repository->getRepoName();
        }

        return 'composite repo ('.implode(', ', $names).')';
    }

    public function getRepositories(): array
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.getRepositories', [$this]);
        }

        return $this->repositories;
    }

    public function hasPackage(PackageInterface $package): bool
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.hasPackage', [$this, $package]);
        }

        foreach ($this->repositories as $repository) {
            if ($repository->hasPackage($package)) {
                return true;
            }
        }

        return false;
    }

    public function findPackage($name, $constraint): ?BasePackage
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.findPackage', [$this, $name, $constraint]);
        }

        foreach ($this->repositories as $repository) {
            $package = $repository->findPackage($name, $constraint);
            if ($package !== null) {
                return $package;
            }
        }

        return null;
    }

    public function findPackages($name, $constraint = null): array
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.findPackages', [$this, $name, $constraint]);
        }

        $lists = [];
        foreach ($this->repositories as $repository) {
            $lists[] = $repository->findPackages($name, $constraint);
        }

        return $lists !== [] ? array_merge(...$lists) : [];
    }

    public function loadPackages(array $packageNameMap, array $acceptableStabilities, array $stabilityFlags, array $alreadyLoaded = []): array
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.loadPackages', [$this, $packageNameMap, $acceptableStabilities, $stabilityFlags, $alreadyLoaded]);
        }

        $packages = [];
        $namesFound = [];
        foreach ($this->repositories as $repository) {
            $result = $repository->loadPackages($packageNameMap, $acceptableStabilities, $stabilityFlags, $alreadyLoaded);
            $packages[] = $result['packages'];
            $namesFound[] = $result['namesFound'];
        }

        return [
            'packages' => $packages !== [] ? array_merge(...$packages) : [],
            'namesFound' => $namesFound !== [] ? array_unique(array_merge(...$namesFound)) : [],
        ];
    }

    public function search(string $query, int $mode = 0, ?string $type = null, ?IOInterface $io = null): array
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.search', [$this, $query, $mode, $type]);
        }

        $matches = [];
        foreach ($this->repositories as $repository) {
            $results = $repository->search($query, $mode, $type);
            if ($io !== null) {
                $io->writeError('Searched '.$repository->getRepoName().', found <info>'.count($results).'</info> result(s)', true, IOInterface::VERY_VERBOSE);
            }
            $matches[] = $results;
        }

        return count($matches) > 0 ? array_merge(...$matches) : [];
    }

    public function getPackages(): array
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.getPackages', [$this]);
        }

        $lists = [];
        foreach ($this->repositories as $repository) {
            $lists[] = $repository->getPackages();
        }

        return $lists !== [] ? array_merge(...$lists) : [];
    }

    public function getProviders($packageName): array
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.getProviders', [$this, $packageName]);
        }

        $lists = [];
        foreach ($this->repositories as $repository) {
            $lists[] = $repository->getProviders($packageName);
        }

        return $lists !== [] ? array_merge(...$lists) : [];
    }

    public function removePackage(PackageInterface $package): void
    {
        if (Remote::owned($this)) {
            Rpc::call('repo.removePackage', [$this, $package]);

            return;
        }

        foreach ($this->repositories as $repository) {
            if ($repository instanceof WritableRepositoryInterface) {
                $repository->removePackage($package);
            }
        }
    }

    public function count(): int
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.count', [$this]);
        }

        $total = 0;
        foreach ($this->repositories as $repository) {
            $total += $repository->count();
        }

        return $total;
    }

    public function addRepository(RepositoryInterface $repository): void
    {
        if (Remote::owned($this)) {
            Rpc::call('repo.addRepository', [$this, $repository]);

            return;
        }

        if ($repository instanceof self) {
            foreach ($repository->getRepositories() as $inner) {
                $this->addRepository($inner);
            }

            return;
        }
        $this->repositories[] = $repository;
    }
}
