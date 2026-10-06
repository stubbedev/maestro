<?php

/*
 * maestro's plugin shim: Composer\Plugin\PluginManager (docs/PLUGINS.md
 * §5.4). The policy (which packages load, in which order, allow-plugins and
 * its prompt, the plugin API check, the autoload plan) is maestro's
 * (pm.*); the mechanism (class loading, instantiation, activate(),
 * addSubscriber()) is here and in Maestro\Shim\Plugins. The instances of
 * maestro's plugin managers mirror its composer, io, globalComposer and
 * disablePlugins properties; getPlugins() and the capabilities are PHP.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Plugin;

class PluginManager
{
    private $runningInGlobalDir = false;

    private function isRunningInGlobalDir(): bool
    {
        return $this->runningInGlobalDir;
    }

    protected $composer;
    protected $disablePlugins = false;
    protected $globalComposer;
    protected $io;
    protected $plugins = [];
    protected $registeredPlugins = [];
    protected $versionParser;

    public function __construct(\Composer\IO\IOInterface $io, \Composer\Composer $composer, ?\Composer\PartialComposer $globalComposer = null, $disablePlugins = false)
    {
        // maestro's manager (pm.new), with its allow-plugins rules, whose
        // mirror this object is from now on (its properties come with the
        // reply); loading plugins into it is maestro's policy and this
        // process's mechanism, as for the managers Factory creates.
        $this->versionParser = new \Composer\Package\Version\VersionParser();
        \Maestro\Shim\Rpc::call('pm.new', [$this, $io, $composer, $globalComposer, $disablePlugins]);
    }

    public function addPlugin(\Composer\Plugin\PluginInterface $plugin, bool $isGlobalPlugin = false, ?\Composer\Package\PackageInterface $sourcePackage = null): void
    {
        if ($this->arePluginsDisabled($isGlobalPlugin ? 'global' : 'local')) {
            return;
        }

        if ($sourcePackage === null) {
            trigger_error('Calling PluginManager::addPlugin without $sourcePackage is deprecated, if you are using this please get in touch with us to explain the use case', E_USER_DEPRECATED);
        } elseif (!$this->isPluginAllowed($sourcePackage->getName(), $isGlobalPlugin, true === ($sourcePackage->getExtra()['plugin-optional'] ?? false))) {
            $this->io->writeError('Skipped loading "'.get_class($plugin).' from '.$sourcePackage->getName() . '" '.($isGlobalPlugin || $this->isRunningInGlobalDir() ? '(installed globally) ' : '').' as it is not in config.allow-plugins', true, \Composer\IO\IOInterface::DEBUG);

            return;
        }

        $details = [];
        if ($sourcePackage) {
            $details[] = 'from '.$sourcePackage->getName();
        }
        if ($isGlobalPlugin || $this->isRunningInGlobalDir()) {
            $details[] = 'installed globally';
        }
        $this->io->writeError('Loading plugin '.get_class($plugin).($details ? ' ('.implode(', ', $details).')' : ''), true, \Composer\IO\IOInterface::DEBUG);
        $this->plugins[] = $plugin;
        $plugin->activate($this->composer, $this->io); // @line 435

        if ($plugin instanceof \Composer\EventDispatcher\EventSubscriberInterface) {
            $this->composer->getEventDispatcher()->addSubscriber($plugin); // @line 438
        }
    }

    public function arePluginsDisabled($type)
    {
        return $this->disablePlugins === true || $this->disablePlugins === $type;
    }

    public function deactivateInstalledPlugins(): void
    {
        \Maestro\Shim\Rpc::call('pm.deactivateInstalledPlugins', [$this]);
    }

    public function deactivatePackage(\Composer\Package\PackageInterface $package): void
    {
        \Maestro\Shim\Rpc::call('pm.deactivatePackage', [$this, $package]);
    }

    public function disablePlugins(): void
    {
        \Maestro\Shim\Rpc::call('pm.disablePlugins', [$this]);
    }

    protected function getCapabilityImplementationClassName(\Composer\Plugin\PluginInterface $plugin, string $capability): ?string
    {
        if (!($plugin instanceof Capable)) {
            return null;
        }

        $capabilities = (array) $plugin->getCapabilities();

        if (!empty($capabilities[$capability]) && is_string($capabilities[$capability]) && trim($capabilities[$capability])) {
            return trim($capabilities[$capability]);
        }

        if (
            array_key_exists($capability, $capabilities)
            && (empty($capabilities[$capability]) || !is_string($capabilities[$capability]) || !trim($capabilities[$capability]))
        ) {
            throw new \UnexpectedValueException('Plugin '.get_class($plugin).' provided invalid capability class name(s), got '.var_export($capabilities[$capability], true));
        }

        return null;
    }

    public function getGlobalComposer(): ?\Composer\PartialComposer
    {
        return $this->globalComposer;
    }

    protected function getPluginApiVersion(): string
    {
        return PluginInterface::PLUGIN_API_VERSION;
    }

    public function getPluginCapabilities($capabilityClassName, array $ctorArgs = []): array
    {
        $capabilities = [];
        foreach ($this->getPlugins() as $plugin) {
            $capability = $this->getPluginCapability($plugin, $capabilityClassName, $ctorArgs);
            if (null !== $capability) {
                $capabilities[] = $capability;
            }
        }

        return $capabilities;
    }

    public function getPluginCapability(\Composer\Plugin\PluginInterface $plugin, $capabilityClassName, array $ctorArgs = []): ?\Composer\Plugin\Capability\Capability
    {
        if ($capabilityClass = $this->getCapabilityImplementationClassName($plugin, $capabilityClassName)) {
            if (!class_exists($capabilityClass)) {
                throw new \RuntimeException("Cannot instantiate Capability, as class $capabilityClass from plugin ".get_class($plugin)." does not exist.");
            }

            $ctorArgs['plugin'] = $plugin;
            $capabilityObj = new $capabilityClass($ctorArgs);

            // FIXME these could use is_a and do the check *before* instantiating once drop support for php<5.3.9
            if (!$capabilityObj instanceof \Composer\Plugin\Capability\Capability || !$capabilityObj instanceof $capabilityClassName) {
                throw new \RuntimeException(
                    'Class ' . $capabilityClass . ' must implement both Composer\Plugin\Capability\Capability and '. $capabilityClassName . '.'
                );
            }

            return $capabilityObj;
        }

        return null;
    }

    public function getPlugins(): array
    {
        return $this->plugins;
    }

    public function getRegisteredPlugins(): array
    {
        return \Maestro\Shim\Rpc::call('pm.getRegisteredPlugins', [$this]);
    }

    public function isPluginAllowed(string $package, bool $isGlobalPlugin, bool $optional = false, bool $prompt = true): bool
    {
        return \Maestro\Shim\Rpc::call('pm.isPluginAllowed', [$this, $package, $isGlobalPlugin, $optional, $prompt]);
    }

    public function loadInstalledPlugins(): void
    {
        if (\Maestro\Shim\Frames::resumes($this, __FUNCTION__)) {
            return;
        }
        \Maestro\Shim\Rpc::call('pm.loadInstalledPlugins', [$this]);
    }

    public function registerPackage(\Composer\Package\PackageInterface $package, bool $failOnMissingClasses = false, bool $isGlobalPlugin = false): void
    {
        if (\Maestro\Shim\Frames::resumes($this, __FUNCTION__)) {
            return;
        }
        \Maestro\Shim\Rpc::call('pm.registerPackage', [$this, $package, $failOnMissingClasses, $isGlobalPlugin]);
    }

    /**
     * maestro's loading of a repository's plugins, as a frame of
     * Composer's stack only (docs/PLUGINS.md §5.12).
     */
    private function loadRepository(\Composer\Repository\RepositoryInterface $repo, bool $isGlobalRepo, ?\Composer\Package\RootPackageInterface $rootPackage = null): void
    {
        \Maestro\Shim\Frames::resumes($this, __FUNCTION__);
    }

    public function removePlugin(\Composer\Plugin\PluginInterface $plugin): void
    {
        $index = array_search($plugin, $this->plugins, true);
        if ($index === false) {
            return;
        }

        $this->io->writeError('Unloading plugin '.get_class($plugin), true, \Composer\IO\IOInterface::DEBUG);
        unset($this->plugins[$index]);
        $plugin->deactivate($this->composer, $this->io); // @line 460

        $this->composer->getEventDispatcher()->removeListener($plugin); // @line 462
    }

    public function setRunningInGlobalDir(bool $runningInGlobalDir): void
    {
        \Maestro\Shim\Rpc::call('pm.setRunningInGlobalDir', [$this, $runningInGlobalDir]);
    }

    public function uninstallPackage(\Composer\Package\PackageInterface $package): void
    {
        \Maestro\Shim\Rpc::call('pm.uninstallPackage', [$this, $package]);
    }

    public function uninstallPlugin(\Composer\Plugin\PluginInterface $plugin): void
    {
        $this->io->writeError('Uninstalling plugin '.get_class($plugin), true, \Composer\IO\IOInterface::DEBUG);
        $plugin->uninstall($this->composer, $this->io); // @line 477
    }
}
