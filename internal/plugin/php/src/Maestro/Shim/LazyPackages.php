<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.3, "Lazy snapshot tiers"). Not
 * part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * Packages maestro sent with their core fields only (id, names, versions,
 * type, stability): the package lists of PRE_POOL_CREATE, which can hold
 * thousands. The getters of the other fields call load() first, which
 * fetches the rest from maestro (pkg.load) the first time.
 */
final class LazyPackages
{
    /** @var array<int, true> spl_object_id => pending */
    public static $pending = [];

    /**
     * Fetches the fields of a package sent with its core fields only.
     *
     * @param object $package
     */
    public static function load($package): void
    {
        $id = spl_object_id($package);
        if (!isset(self::$pending[$id])) {
            return;
        }
        unset(self::$pending[$id]);

        $fields = Rpc::call('pkg.load', [$package]);
        $adapter = Mirrors::adapterOf($package);
        if ($adapter !== null && is_array($fields)) {
            $adapter->apply($package, $fields);
        }
    }
}
