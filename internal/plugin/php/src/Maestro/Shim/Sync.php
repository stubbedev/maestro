<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * The sync engine's PHP half (docs/PLUGINS.md §5.3, §6.3): every message
 * carries what changed on its sender's side since the last message, so
 * the receiver sees it before it runs again. That is the environment, the
 * working directory, Composer's statics and the dirty fields of mirrors.
 */
final class Sync
{
    /** @var array<string, string> the environment both sides last agreed on */
    private static $env = [];

    /** @var string|false the working directory both sides last agreed on */
    private static $cwd = false;

    /**
     * Composer's process-wide statics: Composer::$runningCommand,
     * Composer::$runningOperation and ProcessExecutor::$timeout, with
     * PHP's initial values.
     *
     * @var array<string, mixed>
     */
    private static $statics = ['runningCommand' => null, 'runningOperation' => null, 'processTimeout' => 300];

    /** @var array<string, mixed> the statics both sides last agreed on */
    private static $sentStatics = [];

    /** @var array<string, mixed>|null what outgoing() would record */
    private static $sending;

    /**
     * Takes the current state as the agreed one (before the first
     * message).
     */
    public static function init(): void
    {
        self::$env = self::environ();
        self::$cwd = getcwd();
        self::$sentStatics = self::$statics;
    }

    /**
     * @return mixed
     */
    public static function getStatic(string $name)
    {
        return self::$statics[$name];
    }

    /**
     * @param mixed $value
     */
    public static function setStatic(string $name, $value): void
    {
        self::$statics[$name] = $value;
    }

    /**
     * The sync block of the next message to maestro, or null when nothing
     * changed. The values are not yet passed through Codec::encode().
     * Nothing counts as agreed until commit() (the message was sent).
     *
     * @return array<string, mixed>|null
     */
    public static function outgoing(): ?array
    {
        $s = [];
        $sending = [];

        $env = self::environ();
        if ($env !== self::$env) {
            $set = [];
            $unset = [];
            foreach ($env as $name => $value) {
                if (!array_key_exists($name, self::$env) || self::$env[$name] !== $value) {
                    $set[(string) $name] = $value;
                }
            }
            foreach (self::$env as $name => $_) {
                if (!array_key_exists($name, $env)) {
                    $unset[] = (string) $name;
                }
            }
            $sending['env'] = $env;
            if ($set !== []) {
                $s['env']['set'] = $set;
            }
            if ($unset !== []) {
                $s['env']['unset'] = $unset;
            }
        }

        $cwd = getcwd();
        if ($cwd !== false && $cwd !== self::$cwd) {
            $s['cwd'] = $cwd;
            $sending['cwd'] = $cwd;
        }

        foreach (self::$statics as $name => $value) {
            if ($value !== self::$sentStatics[$name]) {
                $s['st'][$name] = $value;
                $sending['st'][$name] = $value;
            }
        }

        $o = Mirrors::outgoing();
        if ($o !== []) {
            $s['o'] = $o;
        }

        self::$sending = $sending;

        return $s === [] ? null : $s;
    }

    /**
     * The message carrying outgoing()'s block was sent.
     */
    public static function commit(): void
    {
        $sending = self::$sending;
        self::$sending = null;
        if (isset($sending['env'])) {
            self::$env = $sending['env'];
        }
        if (isset($sending['cwd'])) {
            self::$cwd = $sending['cwd'];
        }
        if (isset($sending['st'])) {
            self::$sentStatics = $sending['st'] + self::$sentStatics;
        }
        Mirrors::commit();
    }

    /**
     * Applies a decoded sync block from maestro, before the rest of its
     * message is processed.
     *
     * @param array<string, mixed> $s
     */
    public static function apply(array $s): void
    {
        if (isset($s['reg'])) {
            foreach ($s['reg'] as $reg) {
                Handles::rebind((int) $reg['tmp'], (int) $reg['h']);
            }
        }

        if (isset($s['env'])) {
            if (isset($s['env']['set'])) {
                foreach ($s['env']['set'] as $name => $value) {
                    // As Platform::putEnv does.
                    putenv($name.'='.$value);
                    $_SERVER[$name] = $_ENV[$name] = $value;
                }
            }
            if (isset($s['env']['unset'])) {
                foreach ($s['env']['unset'] as $name) {
                    // As Platform::clearEnv does.
                    putenv((string) $name);
                    unset($_SERVER[$name], $_ENV[$name]);
                }
            }
            self::$env = self::environ();
        }

        if (isset($s['cwd'])) {
            @chdir($s['cwd']);
            self::$cwd = getcwd();
        }

        if (isset($s['st'])) {
            foreach ($s['st'] as $name => $value) {
                self::$statics[$name] = $value;
                self::$sentStatics[$name] = $value;
            }
        }

        if (isset($s['o'])) {
            Mirrors::apply($s['o']);
        }
    }

    /**
     * @return array<string, string>
     */
    private static function environ(): array
    {
        $env = getenv();

        return is_array($env) ? $env : [];
    }
}
