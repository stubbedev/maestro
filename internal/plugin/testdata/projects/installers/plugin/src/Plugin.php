<?php

namespace MaestroTest\Installers;

use Composer\Autoload\ClassMapGenerator;
use Composer\Composer;
use Composer\Installer\BinaryInstaller;
use Composer\IO\IOInterface;
use Composer\Plugin\PluginInterface;

class Plugin implements PluginInterface
{
    private $custom;

    public function activate(Composer $composer, IOInterface $io)
    {
        $im = $composer->getInstallationManager();
        $this->custom = new CustomInstaller($io, $composer, 'custom-thing');
        $im->addInstaller($this->custom);
        $io->write('getInstaller is the object: '.var_export($im->getInstaller('custom-thing') === $this->custom, true));
        $im->removeInstaller($this->custom);
        $io->write('after removeInstaller: '.get_class($im->getInstaller('custom-thing')));
        $io->write('plugin installer: '.get_class($im->getInstaller('composer-plugin')).' same='.var_export($im->getInstaller('composer-plugin') === $im->getInstaller('composer-installer'), true));
        $im->addInstaller($this->custom);
        $im->addInstaller(new IfaceInstaller());
        $im->addInstaller(new PackInstaller($io));
        $im->addInstaller(new FailingInstaller($io, $composer, 'failing-thing'));

        $io->write('PearInstaller exists: '.var_export(class_exists('Composer\Installer\PearInstaller'), true));
        $map = ClassMapGenerator::createMap(__DIR__);
        ksort($map);
        $io->write('classmap: '.implode(',', array_keys($map)));
        $io->write('binary caller: '.BinaryInstaller::determineBinaryCaller(__FILE__));
        $io->write('custom install path: '.$im->getInstallPath($composer->getRepositoryManager()->getLocalRepository()->findPackage('maestro-test/installers-plugin', '*')));
    }

    public function deactivate(Composer $composer, IOInterface $io)
    {
        $composer->getInstallationManager()->removeInstaller($this->custom);
    }

    public function uninstall(Composer $composer, IOInterface $io)
    {
    }
}
