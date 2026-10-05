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
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Repository\\RepositoryManager::__construct() in plugins yet');
    }

    public function addRepository(\Composer\Repository\RepositoryInterface $repository): void
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Repository\\RepositoryManager::addRepository() in plugins yet');
    }

    public function createRepository(string $type, array $config, ?string $name = null): \Composer\Repository\RepositoryInterface
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Repository\\RepositoryManager::createRepository() in plugins yet');
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
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Repository\\RepositoryManager::getHttpDownloader() in plugins yet');
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
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Repository\\RepositoryManager::prependRepository() in plugins yet');
    }

    public function setLocalRepository(\Composer\Repository\InstalledRepositoryInterface $repository): void
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Repository\\RepositoryManager::setLocalRepository() in plugins yet');
    }

    public function setRepositoryClass(string $type, $class): void
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Repository\\RepositoryManager::setRepositoryClass() in plugins yet');
    }
}
