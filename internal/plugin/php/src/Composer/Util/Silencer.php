<?php

/*
 * maestro's plugin shim: Composer\Util\Silencer, reimplemented with Composer
 * 2.10.3's behaviour (docs/PLUGINS.md §4.8).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Util;

class Silencer
{
    private static $stack = [];

    public static function call(callable $callable, ...$parameters)
    {
        try {
            self::suppress();
            $result = $callable(...$parameters);
            self::restore();

            return $result;
        } catch (\Exception $e) {
            self::restore();
            throw $e;
        }
    }

    public static function restore(): void
    {
        if (!empty(self::$stack)) {
            error_reporting(array_pop(self::$stack));
        }
    }

    public static function suppress(?int $mask = null): int
    {
        if (!isset($mask)) {
            $mask = E_WARNING | E_NOTICE | E_USER_WARNING | E_USER_NOTICE | E_DEPRECATED | E_USER_DEPRECATED;
        }
        $old = error_reporting();
        self::$stack[] = $old;
        error_reporting($old & ~$mask);

        return $old;
    }
}
