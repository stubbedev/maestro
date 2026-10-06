<?php

namespace MaestroTest\Legacy;

use Composer\Composer;
use Composer\Installer\LibraryInstaller;
use Composer\IO\IOInterface;
use Composer\Package\PackageInterface;

class Installer extends LibraryInstaller
{
    public function __construct(IOInterface $io, Composer $composer)
    {
        parent::__construct($io, $composer, 'legacy-thing');
    }

    public function getInstallPath(PackageInterface $package)
    {
        return 'legacy-installed/'.basename($package->getName());
    }
}
