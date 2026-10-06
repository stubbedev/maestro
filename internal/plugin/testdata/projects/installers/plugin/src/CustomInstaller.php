<?php

namespace MaestroTest\Installers;

use Composer\Installer\LibraryInstaller;
use Composer\Package\PackageInterface;
use Composer\Repository\InstalledRepositoryInterface;
use React\Promise\PromiseInterface;

class CustomInstaller extends LibraryInstaller
{
    public function getInstallPath(PackageInterface $package)
    {
        return 'custom/'.explode('/', $package->getName())[1];
    }

    protected function installCode(PackageInterface $package)
    {
        $this->io->write('installCode '.$package->getName().' at '.$this->getPackageBasePath($package));

        return parent::installCode($package);
    }

    public function install(InstalledRepositoryInterface $repo, PackageInterface $package)
    {
        $promise = parent::install($repo, $package);
        $this->io->write('parent::install() gave a promise: '.var_export($promise instanceof PromiseInterface, true));

        return $promise->then(function () use ($repo, $package) {
            $this->io->write('installed '.$package->getName().' in repo='.var_export($repo->hasPackage($package), true).' file='.var_export(is_file('custom/custom-a/file.php'), true).' vendorDir resolved='.var_export($this->vendorDir === realpath($this->vendorDir), true));
        });
    }

    public function uninstall(InstalledRepositoryInterface $repo, PackageInterface $package)
    {
        return parent::uninstall($repo, $package)->then(function () use ($package) {
            $this->io->write('uninstalled '.$package->getName().' file='.var_export(is_file('custom/custom-a/file.php'), true));
        });
    }
}
