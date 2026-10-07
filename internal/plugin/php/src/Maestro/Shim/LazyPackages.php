<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.3, "Lazy snapshot tiers"). Not
 * part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * Packages maestro sent with their core fields only (id, names, versions,
 * type, stability): the package lists of PRE_POOL_CREATE and of
 * transactions, which can hold thousands. The getters of the other fields
 * call load() first, which fetches the rest from maestro (pkg.load) the
 * first time.
 */
final class LazyPackages
{
    /**
     * How many packages one pkg.load fetches at most: the one a getter
     * needs and the pending ones that arrived first. Code that reads a
     * field of one package of a list mostly reads it of the others too,
     * in order, so those are the next ones.
     */
    const BATCH = 64;

    /**
     * The pending packages, in the order they arrived (handles are never
     * released, so an id stays its object's).
     *
     * @var array<int, object> spl_object_id => package
     */
    public static $pending = [];

    /**
     * Fetches the fields of a package sent with its core fields only, and
     * of the pending packages that arrived first, up to BATCH in all.
     *
     * @param object $package
     */
    public static function load($package): void
    {
        $id = spl_object_id($package);
        if (!isset(self::$pending[$id])) {
            return;
        }

        $batch = [$package];
        unset(self::$pending[$id]);
        foreach (self::$pending as $other => $object) {
            if (count($batch) === self::BATCH) {
                break;
            }
            $batch[] = $object;
            unset(self::$pending[$other]);
        }

        $fields = Rpc::call('pkg.load', $batch);
        foreach ($batch as $i => $object) {
            $adapter = Mirrors::adapterOf($object);
            if ($adapter !== null && isset($fields[$i]) && is_array($fields[$i])) {
                $adapter->apply($object, $fields[$i]);
            }
        }
    }
}
