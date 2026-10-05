<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * The handle table (docs/PLUGINS.md §5.3): every object that crosses the
 * channel has one handle, positive for Go-owned objects (allocated by
 * maestro), negative for PHP-owned ones (allocated here). Handles are
 * never reused and objects never released, so a handle always yields the
 * same object and spl_object_id() values stay unique.
 */
final class Handles
{
    /** @var array<int, object> */
    private static $objects = [];

    /** @var array<int, int> spl_object_id => handle */
    private static $ids = [];

    /** @var int the last PHP handle allocated */
    private static $last = 0;

    /**
     * PHP handles whose class (and snapshot) maestro has been sent.
     *
     * @var array<int, true>
     */
    private static $sent = [];

    /**
     * Handles first sent in the message being encoded; they count as sent
     * once it is (commit()).
     *
     * @var array<int, true>
     */
    private static $sending = [];

    /**
     * The handle of a PHP object, allocating a PHP handle when it has none.
     */
    public static function handleOf($object): int
    {
        $id = spl_object_id($object);
        if (isset(self::$ids[$id])) {
            return self::$ids[$id];
        }

        $h = --self::$last;
        self::$objects[$h] = $object;
        self::$ids[$id] = $h;

        return $h;
    }

    /**
     * The handle of an object that has one, or null.
     */
    public static function lookup($object): ?int
    {
        $id = spl_object_id($object);

        return isset(self::$ids[$id]) ? self::$ids[$id] : null;
    }

    /**
     * Whether a handle is known.
     */
    public static function has(int $h): bool
    {
        return isset(self::$objects[$h]);
    }

    /**
     * The object of a known handle.
     *
     * @return object
     */
    public static function get(int $h)
    {
        if (!isset(self::$objects[$h])) {
            throw new ProtocolException('maestro shim: unknown handle '.$h);
        }

        return self::$objects[$h];
    }

    /**
     * The "\0o" tag of an object (docs/PLUGINS.md §6.4): its class, nearest
     * shim base and snapshot travel only the first time maestro sees the
     * handle.
     *
     * @return array<string, mixed>
     */
    public static function encodeObject($object): array
    {
        $h = self::handleOf($object);
        if ($h > 0 || isset(self::$sent[$h]) || isset(self::$sending[$h])) {
            return ["\0o" => $h];
        }
        self::$sending[$h] = true;

        $tag = ["\0o" => $h, 'c' => get_class($object)];
        $adapter = Mirrors::adapterOf($object);
        if ($adapter !== null) {
            $tag['base'] = $adapter->base();
            $tag['d'] = Codec::encode($adapter->snapshot($object));
            Mirrors::clean($object);
        }

        return $tag;
    }

    /**
     * The message being encoded was sent.
     */
    public static function commit(): void
    {
        self::$sent += self::$sending;
        self::$sending = [];
    }

    /**
     * The message being encoded was abandoned.
     */
    public static function rollback(): void
    {
        self::$sending = [];
    }

    /**
     * Resolves a "\0o" tag. A Go-owned handle seen for the first time
     * carries its class (and, for a mirror, its snapshot), from which the
     * PHP object is built.
     *
     * @return object
     */
    public static function decodeObject(array $tag)
    {
        $h = (int) $tag["\0o"];
        if (isset(self::$objects[$h])) {
            return self::$objects[$h];
        }
        if ($h <= 0 || !isset($tag['c'])) {
            throw new ProtocolException('maestro shim: unknown handle '.$h);
        }

        $class = (string) $tag['c'];
        $base = isset($tag['base']) ? (string) $tag['base'] : null;
        $snapshot = isset($tag['d']) ? Codec::decode($tag['d']) : null;

        $object = Mirrors::create($h, $class, $base, $snapshot);
        if ($object === null) {
            $object = new RemoteObject($h, $class);
        }
        self::$objects[$h] = $object;
        self::$ids[spl_object_id($object)] = $h;

        return $object;
    }

    /**
     * Rebinds a PHP-born object maestro registered (docs/PLUGINS.md §5.3,
     * the sync block's "reg"): from now on it is the Go-owned object h.
     */
    public static function rebind(int $tmp, int $h): void
    {
        $object = self::get($tmp);
        self::$objects[$h] = $object;
        self::$ids[spl_object_id($object)] = $h;
        Mirrors::registered($object, $h);
    }
}
