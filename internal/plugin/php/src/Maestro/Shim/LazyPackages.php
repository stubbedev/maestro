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
 * call load() first, which fetches the group of fields they read from
 * maestro (pkg.load) the first time: the links (requires, dev requires,
 * conflicts, provides, replaces), the extra, or the rest.
 */
final class LazyPackages
{
    /** The groups of fields fetched together. */
    const GROUPS = ['links', 'extra', 'rest'];

    /**
     * How many packages one pkg.load fetches a group for at most: the one
     * a getter needs and the packages still without that group that
     * arrived first. Code that reads a field of one package of a list
     * mostly reads it of the others too, in order, so those are the next
     * ones.
     */
    const BATCH = 64;

    /**
     * The packages still without each group, in the order they arrived
     * (handles are never released, so an id stays its object's).
     *
     * @var array<string, array<int, object>> group => spl_object_id => package
     */
    private static $pending = ['links' => [], 'extra' => [], 'rest' => []];

    /**
     * A package arrived with its core fields only: every group is to be
     * fetched.
     *
     * @param object $package
     */
    public static function pend($package): void
    {
        $id = spl_object_id($package);
        foreach (self::GROUPS as $group) {
            self::$pending[$group][$id] = $package;
        }
    }

    /**
     * A package arrived with all its fields.
     *
     * @param object $package
     */
    public static function settle($package): void
    {
        $id = spl_object_id($package);
        foreach (self::GROUPS as $group) {
            unset(self::$pending[$group][$id]);
        }
    }

    /**
     * Fetches a group of fields of a package sent with its core fields
     * only, and of the packages still without it that arrived first, up
     * to BATCH in all.
     *
     * @param object $package
     */
    public static function load($package, string $group = 'rest'): void
    {
        $id = spl_object_id($package);
        if (!isset(self::$pending[$group][$id])) {
            return;
        }

        $batch = [$package];
        unset(self::$pending[$group][$id]);
        foreach (self::$pending[$group] as $other => $object) {
            if (count($batch) === self::BATCH) {
                break;
            }
            $batch[] = $object;
            unset(self::$pending[$group][$other]);
        }

        $fields = Rpc::call('pkg.load', array_merge([$group], $batch));
        foreach ($batch as $i => $object) {
            $adapter = Mirrors::adapterOf($object);
            if ($adapter instanceof Adapter\PackageAdapter && isset($fields[$i]) && is_array($fields[$i])) {
                $adapter->fill($object, $fields[$i]);
            }
        }
    }
}
