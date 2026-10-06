<?php

namespace MaestroTest\Installers;

use Composer\Installer\InstallerInterface;
use Composer\Installer\NoopInstaller;
use Composer\Package\PackageInterface;
use Composer\Repository\InstalledRepositoryInterface;

/**
 * A direct InstallerInterface implementation: maestro calls every method
 * in PHP. It records packages through a NoopInstaller.
 */
class IfaceInstaller implements InstallerInterface
{
    private $noop;

    public function __construct()
    {
        $this->noop = new NoopInstaller();
    }

    public function supports(string $packageType)
    {
        return $packageType === 'iface-thing';
    }

    public function isInstalled(InstalledRepositoryInterface $repo, PackageInterface $package)
    {
        return $this->noop->isInstalled($repo, $package);
    }

    public function download(PackageInterface $package, ?PackageInterface $prevPackage = null)
    {
        return \React\Promise\resolve(null);
    }

    public function prepare($type, PackageInterface $package, ?PackageInterface $prevPackage = null)
    {
        return null;
    }

    public function install(InstalledRepositoryInterface $repo, PackageInterface $package)
    {
        @mkdir('iface');
        file_put_contents('iface/'.basename($package->getName()), $package->getPrettyVersion());

        return $this->noop->install($repo, $package);
    }

    public function update(InstalledRepositoryInterface $repo, PackageInterface $initial, PackageInterface $target)
    {
        return $this->noop->update($repo, $initial, $target);
    }

    public function uninstall(InstalledRepositoryInterface $repo, PackageInterface $package)
    {
        return $this->noop->uninstall($repo, $package);
    }

    public function cleanup($type, PackageInterface $package, ?PackageInterface $prevPackage = null)
    {
        return null;
    }

    public function getInstallPath(PackageInterface $package)
    {
        return 'iface/'.basename($package->getName());
    }
}
