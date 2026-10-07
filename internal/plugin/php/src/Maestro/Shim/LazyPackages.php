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
     * arrived in the same message nearest to it, those after it first.
     * Code that reads a field of one package of a list mostly reads it of
     * the others too, in order, so those are the next ones; packages of
     * other lists (PRE_POOL_CREATE's thousands, when a transaction's are
     * read) are not.
     */
    const BATCH = 64;

    /** How far from the package load() looks for others, each way. */
    const REACH = 4 * self::BATCH;

    /**
     * The packages waiting for each group, by the position they arrived
     * at.
     *
     * @var array<string, array<int, object>> group => position => package
     */
    private static $queue = ['links' => [], 'extra' => [], 'rest' => []];

    /**
     * The position of each package waiting for a group.
     *
     * @var array<string, array<int, int>> group => spl_object_id => position
     */
    private static $waiting = ['links' => [], 'extra' => [], 'rest' => []];

    /**
     * The message each waiting package arrived in (Rpc::received).
     *
     * @var array<string, array<int, int>> group => position => message
     */
    private static $message = ['links' => [], 'extra' => [], 'rest' => []];

    /** @var array<string, int> the position the next package arrives at */
    private static $next = ['links' => 0, 'extra' => 0, 'rest' => 0];

    /** @var array<string, int> no package before it waits */
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
                $at = self::$next[$group]++;
                self::$waiting[$group][$id] = $at;
                self::$queue[$group][$at] = $package;
                self::$message[$group][$at] = Rpc::received();
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
            if (isset(self::$waiting[$group][$id])) {
                $at = self::$waiting[$group][$id];
                unset(self::$queue[$group][$at], self::$message[$group][$at], self::$waiting[$group][$id]);
            }
        }
    }

    /**
     * Fetches a group of fields of a package sent with its core fields
     * only, and of the packages still without it that arrived in the same
     * message nearest to it, up to BATCH in all.
     *
     * @param object $package
     */
    public static function load($package, string $group = 'rest'): void
    {
        $id = spl_object_id($package);
        if (!isset(self::$waiting[$group][$id])) {
            return;
        }

        $at = self::$waiting[$group][$id];
        $queue = &self::$queue[$group];
        $message = &self::$message[$group];
        $waiting = &self::$waiting[$group];
        $in = $message[$at];
        $batch = [$package];
        $take = function (int $i) use (&$queue, &$message, &$waiting, &$batch): void {
            $batch[] = $queue[$i];
            unset($waiting[spl_object_id($queue[$i])], $queue[$i], $message[$i]);
        };
        unset($queue[$at], $message[$at], $waiting[$id]);
        for ($i = $at + 1, $end = min(self::$next[$group], $at + 1 + self::REACH); $i < $end && count($batch) < self::BATCH; $i++) {
            if (isset($queue[$i])) {
                if ($message[$i] !== $in) {
                    break;
                }
                $take($i);
            }
        }
        $head = self::$head[$group];
        while ($head < $at && !isset($queue[$head])) {
            $head++;
        }
        self::$head[$group] = $head;
        for ($i = $at - 1, $end = max($head, $at - self::REACH); $i >= $end && count($batch) < self::BATCH; $i--) {
            if (isset($queue[$i])) {
                if ($message[$i] !== $in) {
                    break;
                }
                $take($i);
            }
        }
        unset($queue, $message, $waiting);

        $fields = Rpc::call('pkg.load', array_merge([$group], $batch));
        foreach ($batch as $i => $object) {
            $adapter = Mirrors::adapterOf($object);
            if ($adapter instanceof Adapter\PackageAdapter && isset($fields[$i]) && is_array($fields[$i])) {
                $adapter->fill($object, $fields[$i]);
            }
        }
    }
}
