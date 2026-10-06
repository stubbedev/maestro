<?php

/*
 * maestro's plugin shim: Composer\Installer\PluginInstaller
 * (docs/PLUGINS.md §4.7, §5.6). Its peer is a Go PluginInstaller, which
 * registers, deactivates and uninstalls plugins through maestro's plugin
 * manager, so a subclass's inherited install()/update()/uninstall() really
 * (de)activate the plugin.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Installer;

use Composer\Composer;
use Composer\Plugin\PluginManager;
use Maestro\Shim\Installers;

class PluginInstaller extends \Composer\Installer\LibraryInstaller
{
    public function __construct(\Composer\IO\IOInterface $io, \Composer\PartialComposer $composer, ?\Composer\Util\Filesystem $fs = null, ?\Composer\Installer\BinaryInstaller $binaryInstaller = null)
    {
        parent::__construct($io, $composer, 'composer-plugin', $fs, $binaryInstaller);
    }

    public function disablePlugins(): void
    {
        $this->getPluginManager()->disablePlugins();
    }

    public function download(\Composer\Package\PackageInterface $package, ?\Composer\Package\PackageInterface $prevPackage = null)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$package, $prevPackage]);
    }

    protected function getPluginManager(): \Composer\Plugin\PluginManager
    {
        assert($this->composer instanceof Composer, new \LogicException(self::class.' should be initialized with a fully loaded Composer instance.'));
        $pluginManager = $this->composer->getPluginManager();

        return $pluginManager;
    }

    public function install(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\Package\PackageInterface $package)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$repo, $package]);
    }

    public function prepare($type, \Composer\Package\PackageInterface $package, ?\Composer\Package\PackageInterface $prevPackage = null)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$type, $package, $prevPackage]);
    }

    public function supports(string $packageType)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$packageType]);
    }

    public function uninstall(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\Package\PackageInterface $package)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$repo, $package]);
    }

    public function update(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\Package\PackageInterface $initial, \Composer\Package\PackageInterface $target)
    {
        return Installers::base($this, self::class, __FUNCTION__, [$repo, $initial, $target]);
    }
}
