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
     * The packages waiting for each group.
     *
     * @var array<string, array<int, true>> group => spl_object_id => true
     */
    private static $waiting = ['links' => [], 'extra' => [], 'rest' => []];

    /**
     * The packages that waited for each group, in the order they started
     * to: from $head on, each one waiting is there (handles are never
     * released, so an id stays its object's). A queue rather than the
     * order of $waiting: walking a PHP array from its start passes every
     * entry removed before, over and over.
     *
     * @var array<string, list<object>>
     */
    private static $queue = ['links' => [], 'extra' => [], 'rest' => []];

    /** @var array<string, int> */
    private static $head = ['links' => 0, 'extra' => 0, 'rest' => 0];

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
            if (!isset(self::$waiting[$group][$id])) {
                self::$waiting[$group][$id] = true;
                self::$queue[$group][] = $package;
            }
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
            unset(self::$waiting[$group][$id]);
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
        if (!isset(self::$waiting[$group][$id])) {
            return;
        }

        $batch = [$package];
        unset(self::$waiting[$group][$id]);
        $queue = &self::$queue[$group];
        $i = self::$head[$group];
        for ($n = count($queue); $i < $n && count($batch) < self::BATCH; $i++) {
            $other = spl_object_id($queue[$i]);
            if (isset(self::$waiting[$group][$other])) {
                $batch[] = $queue[$i];
                unset(self::$waiting[$group][$other]);
            }
        }
        if ($i * 2 > $n) {
            // the queue is mostly behind the head
            $queue = array_slice($queue, $i);
            $i = 0;
        }
        self::$head[$group] = $i;
        unset($queue);

        $fields = Rpc::call('pkg.load', array_merge([$group], $batch));
        foreach ($batch as $i => $object) {
            $adapter = Mirrors::adapterOf($object);
            if ($adapter instanceof Adapter\PackageAdapter && isset($fields[$i]) && is_array($fields[$i])) {
                $adapter->fill($object, $fields[$i]);
            }
        }
    }
}
