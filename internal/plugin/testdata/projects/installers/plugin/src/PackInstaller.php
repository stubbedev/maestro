<?php

namespace MaestroTest\Installers;

use Composer\Installer\MetapackageInstaller;
use Composer\IO\IOInterface;
use Composer\Package\PackageInterface;
use Composer\Repository\InstalledRepositoryInterface;

class PackInstaller extends MetapackageInstaller
{
    private $output;

    public function __construct(IOInterface $io)
    {
        parent::__construct($io);
        $this->output = $io;
    }

    public function supports(string $packageType)
    {
        return $packageType === 'pack-thing';
    }

    public function install(InstalledRepositoryInterface $repo, PackageInterface $package)
    {
        return parent::install($repo, $package)->then(function () use ($package) {
            $this->output->write('pack installed '.$package->getName().' path='.var_export($this->getInstallPath($package), true));
        });
    }
}
