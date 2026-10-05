<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * Listeners PHP registers with maestro's dispatcher (docs/PLUGINS.md §5.5).
 * A string is a script; any other callable stays in PHP and maestro holds
 * it by handle: a closure or invokable object as itself, an array callable
 * as the CallableHolder of its value (equal arrays share one holder, as
 * removeListener() compares them with ===).
 */
final class Listeners
{
    /** @var array<string, CallableHolder> by callable key */
    private static $holders = [];

    /**
     * What maestro gets for addListener()'s $listener: the string itself,
     * or a descriptor of the callable.
     *
     * @param mixed $listener
     * @return string|array<string, mixed>
     */
    public static function describe($listener)
    {
        if (is_string($listener)) {
            return $listener;
        }

        if (is_array($listener) && count($listener) === 2 && isset($listener[0], $listener[1]) && (is_string($listener[0]) || is_object($listener[0])) && is_string($listener[1])) {
            $holder = self::holder($listener, true);

            return [
                'h' => $holder,
                'object' => is_object($listener[0]) ? $listener[0] : null,
                'class' => is_object($listener[0]) ? get_class($listener[0]) : $listener[0],
                'method' => $listener[1],
                'closure' => false,
            ];
        }

        if (is_object($listener)) {
            return [
                'h' => $listener,
                'object' => null,
                'class' => '',
                'method' => '',
                'closure' => $listener instanceof \Closure,
            ];
        }

        // Any other array: kept as is, as Composer keeps it.
        return [
            'h' => new CallableHolder($listener),
            'object' => null,
            'class' => '',
            'method' => '',
            'closure' => false,
        ];
    }

    /**
     * What maestro gets for removeListener()'s $listener.
     *
     * @param mixed $listener
     * @return array<string, mixed>
     */
    public static function match($listener): array
    {
        if (is_string($listener)) {
            return ['kind' => 'script', 'script' => $listener];
        }
        if (is_object($listener)) {
            return ['kind' => 'object', 'h' => $listener];
        }
        if (is_array($listener)) {
            $holder = self::holder($listener, false);
            if ($holder !== null) {
                return ['kind' => 'callable', 'h' => $holder];
            }
        }

        return ['kind' => 'none'];
    }

    /**
     * The callable a handle maestro passes stands for.
     *
     * @param object $h
     * @return mixed
     */
    public static function callable($h)
    {
        return $h instanceof CallableHolder ? $h->callable : $h;
    }

    /**
     * @param array<mixed> $callable
     */
    private static function holder(array $callable, bool $create): ?CallableHolder
    {
        if (count($callable) !== 2 || !isset($callable[0], $callable[1]) || !is_string($callable[1]) || (!is_string($callable[0]) && !is_object($callable[0]))) {
            return null;
        }
        $key = (is_object($callable[0]) ? 'o'.spl_object_id($callable[0]) : 'c'.$callable[0]).'::'.$callable[1];
        if (!isset(self::$holders[$key])) {
            if (!$create) {
                return null;
            }
            self::$holders[$key] = new CallableHolder($callable);
        }

        return self::$holders[$key];
    }
}
