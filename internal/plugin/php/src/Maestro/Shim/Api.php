<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * Registers the Composer API's parts of the shim at boot: the mirror
 * families, the value tags and the methods maestro calls.
 */
final class Api
{
    public static function register(): void
    {
        Values::register();
        Mirrors::register(new Adapter\PackageAdapter());
        Mirrors::register(new Adapter\IOAdapter());
        Mirrors::register(new Adapter\EventAdapter());
        Mirrors::register(new Adapter\OperationAdapter());
        Mirrors::register(new Adapter\AuditConfigAdapter());
        Mirrors::register(new Adapter\ServiceAdapter());
        Dispatch::register();
        Plugins::register();
        Installers::register();
        Promises::register();
        Console::register();
        Server::register('iv.reload', [self::class, 'reloadInstalledVersions']);
    }

    /**
     * `iv.reload` (docs/PLUGINS.md §5.4): InstalledVersions::reload() with
     * the data Composer reloads it with; selfDir, when given, is the
     * vendor/composer directory FilesystemRepository::write() sets.
     *
     * @param array<string, mixed> $a
     */
    public static function reloadInstalledVersions(array $a): void
    {
        \Composer\InstalledVersions::reload($a['data']);
        if (isset($a['selfDir'])) {
            $set = function (string $selfDir): void {
                self::$selfDir = $selfDir;
                self::$installedIsLocalDir = true;
            };
            \Closure::bind($set, null, \Composer\InstalledVersions::class)($a['selfDir']);
        }
    }
}
