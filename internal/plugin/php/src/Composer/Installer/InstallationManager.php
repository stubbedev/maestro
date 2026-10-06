<?php

/*
 * maestro's plugin shim: Composer\Installer\InstallationManager
 * (docs/PLUGINS.md §4.7), a service proxy of maestro's (im.*).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Installer;

class InstallationManager
{
    public function __construct(\Composer\Util\Loop $loop, \Composer\IO\IOInterface $io, ?\Composer\EventDispatcher\EventDispatcher $eventDispatcher = null)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Installer\\InstallationManager::__construct() in plugins yet');
    }

    public function addInstaller(\Composer\Installer\InstallerInterface $installer): void
    {
        \Maestro\Shim\Rpc::call('im.addInstaller', [$this, $installer, \Maestro\Shim\Installers::describe($installer)]);
    }

    public function disablePlugins(): void
    {
        \Maestro\Shim\Rpc::call('im.disablePlugins', [$this]);
    }

    public function download(\Composer\Package\PackageInterface $package): ?\React\Promise\PromiseInterface
    {
        return \Maestro\Shim\Installers::promise(\Maestro\Shim\Rpc::call('im.download', [$this, $package]));
    }

    public function ensureBinariesPresence(\Composer\Package\PackageInterface $package): void
    {
        \Maestro\Shim\Rpc::call('im.ensureBinariesPresence', [$this, $package]);
    }

    public function execute(\Composer\Repository\InstalledRepositoryInterface $repo, array $operations, bool $devMode = true, bool $runScripts = true, bool $downloadOnly = false): void
    {
        \Maestro\Shim\Rpc::call('im.execute', [$this, $repo, $operations, $devMode, $runScripts, $downloadOnly]);
    }

    public function getInstallPath(\Composer\Package\PackageInterface $package): ?string
    {
        return \Maestro\Shim\Rpc::call('im.getInstallPath', [$this, $package]);
    }

    public function getInstaller(string $type): \Composer\Installer\InstallerInterface
    {
        return \Maestro\Shim\Rpc::call('im.getInstaller', [$this, $type]);
    }

    public function install(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\DependencyResolver\Operation\InstallOperation $operation): ?\React\Promise\PromiseInterface
    {
        return \Maestro\Shim\Installers::promise(\Maestro\Shim\Rpc::call('im.install', [$this, $repo, $operation]));
    }

    public function isPackageInstalled(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\Package\PackageInterface $package): bool
    {
        return \Maestro\Shim\Rpc::call('im.isPackageInstalled', [$this, $repo, $package]);
    }

    public function markAliasInstalled(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\DependencyResolver\Operation\MarkAliasInstalledOperation $operation): void
    {
        \Maestro\Shim\Rpc::call('im.markAliasInstalled', [$this, $repo, $operation]);
    }

    public function markAliasUninstalled(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\DependencyResolver\Operation\MarkAliasUninstalledOperation $operation): void
    {
        \Maestro\Shim\Rpc::call('im.markAliasUninstalled', [$this, $repo, $operation]);
    }

    public function notifyInstalls(\Composer\IO\IOInterface $io): void
    {
        \Maestro\Shim\Rpc::call('im.notifyInstalls', [$this, $io]);
    }

    public function removeInstaller(\Composer\Installer\InstallerInterface $installer): void
    {
        \Maestro\Shim\Rpc::call('im.removeInstaller', [$this, $installer]);
    }

    public function reset(): void
    {
        \Maestro\Shim\Rpc::call('im.reset', [$this]);
    }

    public function setOutputProgress(bool $outputProgress): void
    {
        \Maestro\Shim\Rpc::call('im.setOutputProgress', [$this, $outputProgress]);
    }

    public function uninstall(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\DependencyResolver\Operation\UninstallOperation $operation): ?\React\Promise\PromiseInterface
    {
        return \Maestro\Shim\Installers::promise(\Maestro\Shim\Rpc::call('im.uninstall', [$this, $repo, $operation]));
    }

    public function update(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\DependencyResolver\Operation\UpdateOperation $operation): ?\React\Promise\PromiseInterface
    {
        return \Maestro\Shim\Installers::promise(\Maestro\Shim\Rpc::call('im.update', [$this, $repo, $operation]));
    }
}
