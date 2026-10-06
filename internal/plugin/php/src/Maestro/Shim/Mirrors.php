<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * Data mirrors (docs/PLUGINS.md §5.3): PHP objects holding a copy of a
 * Go-owned object's fields. Mirror setters call touch(); at every message
 * to maestro the dirty fields go up in the sync block ("o"), and the
 * fields maestro changed come down the same way and are written in place,
 * so object identity is kept.
 */
final class Mirrors
{
    /** @var array<string, MirrorAdapter> by base class */
    private static $adapters = [];

    /** @var array<string, MirrorAdapter|null> by concrete class */
    private static $byClass = [];

    /**
     * The adapters of mirrors built by an adapter whose base is not a
     * class (console objects: vendored Symfony classes), by
     * spl_object_id.
     *
     * @var array<int, MirrorAdapter>
     */
    private static $objectAdapters = [];

    /**
     * Mirrors whose adapter finds their changes itself (PolledMirrorAdapter),
     * by spl_object_id.
     *
     * @var array<int, object>
     */
    private static $polled = [];

    /** @var array<int, array{0: object, 1: array<string, true>}> by spl_object_id */
    private static $dirty = [];

    /** @var array<int, int> handle => revision */
    private static $revs = [];

    /** @var array<int, int> the revisions of the message being encoded */
    private static $sendingRevs = [];

    public static function register(MirrorAdapter $adapter): void
    {
        self::$adapters[$adapter->base()] = $adapter;
        self::$byClass = [];
    }

    /**
     * The adapter of an object's class or its nearest registered parent.
     *
     * @param object|string $objectOrClass
     */
    public static function adapterOf($objectOrClass): ?MirrorAdapter
    {
        if (is_object($objectOrClass) && isset(self::$objectAdapters[spl_object_id($objectOrClass)])) {
            return self::$objectAdapters[spl_object_id($objectOrClass)];
        }
        $class = is_object($objectOrClass) ? get_class($objectOrClass) : $objectOrClass;
        if (!array_key_exists($class, self::$byClass)) {
            $found = null;
            for ($c = $class; is_string($c); $c = get_parent_class($c)) {
                if (isset(self::$adapters[$c])) {
                    $found = self::$adapters[$c];
                    break;
                }
            }
            self::$byClass[$class] = $found;
        }

        return self::$byClass[$class];
    }

    /**
     * Builds the mirror of a Go-owned object maestro sent for the first
     * time. An object that is not a mirror (a service maestro owns: the
     * Config, a manager, a repository) becomes an instance of its class
     * built without its constructor, whose methods call maestro; null when
     * the class cannot be instantiated.
     *
     * @param mixed $snapshot
     * @return object|null
     */
    public static function create(int $handle, string $class, ?string $base, $snapshot)
    {
        $adapter = null;
        if ($base !== null && isset(self::$adapters[$base])) {
            $adapter = self::$adapters[$base];
        } elseif ($base !== null && isset(self::$adapters[$class])) {
            $adapter = self::$adapters[$class];
        }
        if ($adapter === null) {
            return self::service($class);
        }

        $object = $adapter->create($handle, $class, is_array($snapshot) ? $snapshot : []);
        self::$revs[$handle] = 0;
        if (!class_exists($adapter->base(), false) && !interface_exists($adapter->base(), false)) {
            self::$objectAdapters[spl_object_id($object)] = $adapter;
        }
        if ($adapter instanceof PolledMirrorAdapter) {
            self::$polled[spl_object_id($object)] = $object;
        }

        return $object;
    }

    /**
     * An instance of a service class, without its constructor; null when
     * there is no such concrete class.
     *
     * @return object|null
     */
    private static function service(string $class)
    {
        if (!class_exists($class)) {
            return null;
        }
        $r = new \ReflectionClass($class);
        if ($r->isAbstract() || $r->isInterface()) {
            return null;
        }

        return $r->newInstanceWithoutConstructor();
    }

    /**
     * Makes a PHP object of a family whose adapter's base is not a class
     * (a vendored Symfony input) a mirror of that family: maestro adopts it
     * when it first crosses (docs/PLUGINS.md §5.3, PHP-born data).
     *
     * @param object $object
     */
    public static function adopt($object, MirrorAdapter $adapter): void
    {
        self::$objectAdapters[spl_object_id($object)] = $adapter;
        if ($adapter instanceof PolledMirrorAdapter) {
            self::$polled[spl_object_id($object)] = $object;
        }
    }

    /**
     * Records that a mirror setter changed a field.
     *
     * @param object $object
     */
    public static function touch($object, string $field): void
    {
        $id = spl_object_id($object);
        if (!isset(self::$dirty[$id])) {
            self::$dirty[$id] = [$object, []];
        }
        self::$dirty[$id][1][$field] = true;
    }

    /**
     * Forgets the dirty fields of an object whose full snapshot is being
     * sent.
     *
     * @param object $object
     */
    public static function clean($object): void
    {
        unset(self::$dirty[spl_object_id($object)]);
    }

    /**
     * A PHP-born mirror maestro registered as handle $h.
     *
     * @param object $object
     */
    public static function registered($object, int $h): void
    {
        self::$revs[$h] = 0;
    }

    /**
     * The "o" entries of the next message to maestro: the dirty fields of
     * Go-owned mirrors. Objects maestro does not own are dropped: their
     * full snapshot travels when they first cross. Nothing is forgotten
     * until commit().
     *
     * @return list<array<string, mixed>>
     */
    public static function outgoing(): array
    {
        self::$sendingRevs = [];
        if (self::$dirty === [] && self::$polled === []) {
            return [];
        }

        $out = [];
        foreach (self::$polled as $object) {
            $adapter = self::adapterOf($object);
            $h = Handles::lookup($object);
            if (!$adapter instanceof PolledMirrorAdapter || $h === null || $h <= 0) {
                continue;
            }
            $fields = $adapter->poll($object);
            if ($fields === null || $fields === []) {
                continue;
            }
            $rev = (isset(self::$revs[$h]) ? self::$revs[$h] : 0) + 1;
            self::$sendingRevs[$h] = $rev;
            $out[] = ['h' => $h, 'r' => $rev, 'f' => $fields];
        }
        foreach (self::$dirty as $entry) {
            $object = $entry[0];
            $h = Handles::lookup($object);
            $adapter = self::adapterOf($object);
            if ($h === null || $h <= 0 || $adapter === null) {
                continue;
            }
            $rev = (isset(self::$revs[$h]) ? self::$revs[$h] : 0) + 1;
            self::$sendingRevs[$h] = $rev;
            $out[] = ['h' => $h, 'r' => $rev, 'f' => $adapter->fields($object, array_keys($entry[1]))];
        }

        return $out;
    }

    /**
     * The message carrying outgoing()'s entries was sent.
     */
    public static function commit(): void
    {
        self::$revs = self::$sendingRevs + self::$revs;
        self::$sendingRevs = [];
        self::$dirty = [];
    }

    /**
     * Applies the "o" entries maestro sent: changed fields ("f") or a full
     * snapshot ("full").
     *
     * @param list<array<string, mixed>> $updates
     */
    public static function apply(array $updates): void
    {
        foreach ($updates as $update) {
            $h = (int) $update['h'];
            $object = Handles::get($h);
            $adapter = self::adapterOf($object);
            if ($adapter === null) {
                throw new ProtocolException('maestro shim: handle '.$h.' is not a mirror');
            }
            $adapter->apply($object, isset($update['full']) ? $update['full'] : $update['f']);
            self::$revs[$h] = (int) $update['r'];
        }
    }
}
