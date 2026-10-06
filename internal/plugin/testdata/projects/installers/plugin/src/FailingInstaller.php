<?php

namespace MaestroTest\Installers;

use Composer\Installer\LibraryInstaller;
use Composer\Package\PackageInterface;

class FailingInstaller extends LibraryInstaller
{
    protected function installCode(PackageInterface $package)
    {
        $this->io->write('failing '.$package->getName());

        return \React\Promise\reject(new FailedException('could not install '.$package->getName()));
    }
}
