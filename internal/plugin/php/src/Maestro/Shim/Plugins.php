<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

use Composer\Autoload\ClassLoader;
use Composer\Installer\InstallerInterface;
use Composer\Plugin\PluginManager;
use Composer\Pcre\Preg;

/**
 * The mechanism of plugin loading (docs/PLUGINS.md §5.4, D8): maestro's
 * plugin manager decides what loads; these handlers do in PHP what the end
 * of PluginManager::registerPackage() and its deactivation do.
 */
final class Plugins
{
    /** @var int PluginManager::$classCounter, process-wide */
    private static $classCounter = 0;

    public static function register(): void
    {
        Server::register('plugin.load', [self::class, 'load']);
        Server::register('plugin.deactivate', [self::class, 'deactivate']);
        Server::register('plugin.uninstall', [self::class, 'uninstall']);
        Mirrors::register(new Adapter\PluginManagerAdapter());
    }

    /**
     * `plugin.load`: registers the plugin package's class loader, requires
     * its `files`, then instantiates and activates each class (or adds a
     * legacy installer).
     *
     * @param array<string, mixed> $a
     * @return array<string, mixed>
     */
    public static function load(array $a): array
    {
        /** @var PluginManager $pm */
        $pm = $a['pm'];
        $package = $a['package'];
        $isGlobalPlugin = (bool) $a['isGlobal'];
        $loaderData = $a['loader'];

        $classLoader = new ClassLoader($loaderData['vendorDir']);
        foreach ($loaderData['psr0'] as $namespace => $path) {
            $classLoader->add($namespace, $path);
        }
        foreach ($loaderData['psr4'] as $namespace => $path) {
            $classLoader->addPsr4($namespace, $path);
        }
        $classLoader->addClassMap($loaderData['classmap']);
        $classLoader->register(false);

        // composerRequire() is defined in AutoloadGenerator's file, which
        // Composer has loaded by now.
        class_exists(\Composer\Autoload\AutoloadGenerator::class);
        foreach ($a['files'] as $fileIdentifier => $file) {
            // exclude laminas/laminas-zendframework-bridge:src/autoload.php as it breaks Composer in some conditions
            // see https://github.com/composer/composer/issues/10349 and https://github.com/composer/composer/issues/10401
            // this hack can be removed once this deprecated package stop being installed
            if ($fileIdentifier === '7e9bd612cc444b3eed788ebbe46263a0') {
                continue;
            }
            \Composer\Autoload\composerRequire((string) $fileIdentifier, $file);
        }

        $registered = [];
        $composer = Remote::read($pm, PluginManager::class, ['composer'])['composer'];
        $io = Remote::read($pm, PluginManager::class, ['io'])['io'];
        $globally = $isGlobalPlugin || $a['runningInGlobalDir'];

        foreach ($a['classes'] as $class) {
            // PluginManager.php declares strict_types: class_exists() refuses
            // an extra.class that is not a string, which this file (written
            // for PHP 7) would coerce
            if (!is_string($class)) {
                throw new \TypeError('class_exists(): Argument #1 ($class) must be of type string, '.self::zvalValueName($class).' given');
            }
            if (class_exists($class, false)) {
                $class = trim($class, '\\');
                $path = $classLoader->findFile($class);
                $code = file_get_contents($path);
                $separatorPos = strrpos($class, '\\');
                $className = $class;
                if ($separatorPos) {
                    $className = substr($class, $separatorPos + 1);
                }
                $code = Preg::replace('{^((?:(?:final|readonly)\s+)*(?:\s*))class\s+('.preg_quote($className).')}mi', '$1class $2_composer_tmp'.self::$classCounter, $code, 1);
                $code = strtr($code, [
                    '__FILE__' => var_export($path, true),
                    '__DIR__' => var_export(dirname($path), true),
                    '__CLASS__' => var_export($class, true),
                ]);
                $code = Preg::replace('/^\s*<\?(php)?/i', '', $code, 1);
                eval($code); // @line src/Composer/Plugin/PluginManager.php:305
                $class .= '_composer_tmp'.self::$classCounter;
                self::$classCounter++;
            }

            if ($a['legacyInstaller']) {
                if (!is_a($class, 'Composer\Installer\InstallerInterface', true)) {
                    throw new \RuntimeException('Could not activate plugin "'.$package->getName().'" as "'.$class.'" does not implement Composer\Installer\InstallerInterface');
                }
                $io->writeError('<warning>Loading "'.$package->getName() . '" '.($globally ? '(installed globally) ' : '').'which is a legacy composer-installer built for Composer 1.x, it is likely to cause issues as you are running Composer 2.x.</warning>');
                $installer = new $class($io, $composer);
                $composer->getInstallationManager()->addInstaller($installer);
                $registered[] = $installer;
            } elseif (class_exists($class)) {
                if (!is_a($class, 'Composer\Plugin\PluginInterface', true)) {
                    throw new \RuntimeException('Could not activate plugin "'.$package->getName().'" as "'.$class.'" does not implement Composer\Plugin\PluginInterface');
                }
                $plugin = new $class();
                $pm->addPlugin($plugin, $isGlobalPlugin, $package);
                $registered[] = $plugin;
            } elseif ($a['failOnMissing']) {
                throw new \UnexpectedValueException('Plugin '.$package->getName().' could not be initialized, class not found: '.$class);
            }
        }

        return ['registered' => $registered];
    }

    /**
     * PHP 8.3+'s name of a value's type in a TypeError: true, false, null,
     * int, float, array or the class.
     *
     * @param mixed $value
     */
    private static function zvalValueName($value): string
    {
        if (is_bool($value)) {
            return $value ? 'true' : 'false';
        }
        if (is_object($value)) {
            return get_class($value);
        }

        return ['NULL' => 'null', 'integer' => 'int', 'double' => 'float'][gettype($value)] ?? gettype($value);
    }

    /**
     * `plugin.deactivate`: deactivatePackage()'s PHP part.
     *
     * @param array<string, mixed> $a
     */
    public static function deactivate(array $a): void
    {
        foreach ($a['objects'] as $plugin) {
            if ($plugin instanceof InstallerInterface) {
                self::removeInstaller($a['pm'], $plugin);
            } else {
                $a['pm']->removePlugin($plugin);
            }
        }
    }

    /**
     * `plugin.uninstall`: uninstallPackage()'s PHP part.
     *
     * @param array<string, mixed> $a
     */
    public static function uninstall(array $a): void
    {
        foreach ($a['objects'] as $plugin) {
            if ($plugin instanceof InstallerInterface) {
                self::removeInstaller($a['pm'], $plugin);
            } else {
                $a['pm']->removePlugin($plugin);
                $a['pm']->uninstallPlugin($plugin);
            }
        }
    }

    /**
     * `$this->composer->getInstallationManager()->removeInstaller($plugin)`
     * of a legacy installer.
     */
    private static function removeInstaller(PluginManager $pm, InstallerInterface $installer): void
    {
        $composer = Remote::read($pm, PluginManager::class, ['composer'])['composer'];
        $composer->getInstallationManager()->removeInstaller($installer);
    }
}
