<?php

/*
 * maestro's plugin shim: Composer\Repository\RepositoryManager
 * (docs/PLUGINS.md §4.6), a service proxy of maestro's (rm.*).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Repository;

class RepositoryManager
{
    public function __construct(\Composer\IO\IOInterface $io, \Composer\Config $config, \Composer\Util\HttpDownloader $httpDownloader, ?\Composer\EventDispatcher\EventDispatcher $eventDispatcher = null, ?\Composer\Util\ProcessExecutor $process = null)
    {
        // maestro's manager (rm.new), whose proxy this object is from now
        // on; its ProcessExecutor stands for $process when one is given.
        \Maestro\Shim\Rpc::call('rm.new', [$this, $io, $config, $httpDownloader, $eventDispatcher, $process]);
    }

    public function addRepository(\Composer\Repository\RepositoryInterface $repository): void
    {
        \Maestro\Shim\Rpc::call('rm.addRepository', [$this, $repository]);
    }

    public function createRepository(string $type, array $config, ?string $name = null): \Composer\Repository\RepositoryInterface
    {
        return \Maestro\Shim\Rpc::call('rm.createRepository', [$this, $type, $config, $name]);
    }

    public function findPackage(string $name, $constraint): ?\Composer\Package\PackageInterface
    {
        return \Maestro\Shim\Rpc::call('rm.findPackage', [$this, $name, $constraint]);
    }

    public function findPackages(string $name, $constraint): array
    {
        return \Maestro\Shim\Rpc::call('rm.findPackages', [$this, $name, $constraint]);
    }

    public function getHttpDownloader(): \Composer\Util\HttpDownloader
    {
        return \Maestro\Shim\Rpc::call('rm.getHttpDownloader', [$this]);
    }

    public function getLocalRepository(): \Composer\Repository\InstalledRepositoryInterface
    {
        return \Maestro\Shim\Rpc::call('rm.getLocalRepository', [$this]);
    }

    public function getRepositories(): array
    {
        return \Maestro\Shim\Rpc::call('rm.getRepositories', [$this]);
    }

    public function prependRepository(\Composer\Repository\RepositoryInterface $repository): void
    {
        \Maestro\Shim\Rpc::call('rm.prependRepository', [$this, $repository]);
    }

    public function setLocalRepository(\Composer\Repository\InstalledRepositoryInterface $repository): void
    {
        \Maestro\Shim\Rpc::call('rm.setLocalRepository', [$this, $repository]);
    }

    public function setRepositoryClass(string $type, $class): void
    {
        // A repository class written in PHP: maestro creates its
        // repositories here (object.new) and uses them through proxies.
        \Maestro\Shim\Rpc::call('rm.setRepositoryClass', [$this, $type, $class]);
    }
}
