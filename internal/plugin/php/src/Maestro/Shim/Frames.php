<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.12). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * Backtrace frames (docs/PLUGINS.md §5.12): the objects that would be on
 * Composer's PHP call stack when maestro runs plugin code (the
 * Application with its input and output, the running command, the
 * Composer\Installer during run()). A call carrying `frames` runs its
 * handler inside one trampoline per frame, a closure bound to the frame's
 * object and called with its arguments, so that debug_backtrace() shows
 * `object` and `args` as Composer's frames do (symfony/flex,
 * php-http/discovery and symfony/thanks look for them).
 */
final class Frames
{
    /**
     * Runs $fn inside the frames, outermost first.
     *
     * @param list<array{0: object, 1?: list<mixed>}> $frames
     * @param callable(): mixed $fn
     * @return mixed
     */
    public static function run(array $frames, callable $fn)
    {
        $result = self::enter(array_values($frames), 0, $fn);
        self::reportAddedCommands($frames);

        return $result;
    }

    /**
     * @param list<array{0: object, 1?: list<mixed>}> $frames
     * @param callable(): mixed $fn
     * @return mixed
     */
    public static function enter(array $frames, int $i, callable $fn)
    {
        if (!isset($frames[$i])) {
            return $fn();
        }
        $object = $frames[$i][0];
        $args = isset($frames[$i][1]) && is_array($frames[$i][1]) ? array_values($frames[$i][1]) : [];
        if (!is_object($object)) {
            return self::enter($frames, $i + 1, $fn);
        }

        $trampoline = function (...$args) use ($frames, $i, $fn) {
            return Frames::enter($frames, $i + 1, $fn);
        };
        $trampoline = \Closure::bind($trampoline, $object, get_class($object));

        return $trampoline(...$args);
    }

    /**
     * Commands plugin code added to maestro's running Application with
     * Symfony's add() (flex and thanks do it from activate()): maestro's
     * Application registers them right away, as Composer's has them.
     *
     * @param list<array{0: object, 1?: list<mixed>}> $frames
     */
    private static function reportAddedCommands(array $frames): void
    {
        foreach ($frames as $frame) {
            $app = $frame[0];
            if ($app instanceof \Composer\Console\Application && Remote::owned($app)) {
                $added = Console::addedCommands($app);
                if ($added !== []) {
                    Rpc::call('app.added', [$app, $added]);
                }
            }
        }
    }
}
