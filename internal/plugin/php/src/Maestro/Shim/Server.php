<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * The methods maestro calls in PHP (docs/PLUGINS.md §6.5), by name. Each
 * handler takes the call's decoded params and returns its value; what it
 * throws goes back to maestro as an "err".
 */
final class Server
{
    /**
     * @var array<string, callable(mixed): mixed>
     */
    private static $handlers = [
        'boot' => [self::class, 'boot'],
        'shutdown' => [self::class, 'shutdown'],
        'ping' => [self::class, 'ping'],
    ];

    /**
     * Adds (or replaces) the handler of a method.
     *
     * @param callable(mixed): mixed $handler
     */
    public static function register(string $method, callable $handler): void
    {
        self::$handlers[$method] = $handler;
    }

    /**
     * @param mixed $args
     * @return mixed
     */
    public static function dispatch(string $method, $args)
    {
        if (!isset(self::$handlers[$method])) {
            throw new UnsupportedApiException('maestro shim: no handler for '.$method);
        }

        return (self::$handlers[$method])($args);
    }

    /**
     * `boot` (docs/PLUGINS.md §5.2 step 7): the process state of Composer's
     * process that PHP cannot know by itself. The working directory and
     * Composer's statics arrive in the sync block.
     *
     * @param array<string, mixed> $a
     */
    public static function boot(array $a): void
    {
        $argv = array_values($a['argv']);
        $_SERVER['argv'] = $argv;
        $_SERVER['argc'] = count($argv);
        $GLOBALS['argv'] = $argv;
        $GLOBALS['argc'] = count($argv);
        foreach ($a['server'] as $name => $value) {
            $_SERVER[$name] = $value;
        }

        // A self-test: the constants the shim pins equal maestro's.
        foreach (isset($a['composerVersion']) ? $a['composerVersion'] : [] as $constant => $want) {
            if (!defined($constant) || constant($constant) !== $want) {
                throw new \LogicException('maestro shim: '.$constant.' is '.var_export(defined($constant) ? constant($constant) : null, true).', maestro has '.var_export($want, true));
            }
        }

        // Step 8.
        \Composer\Util\ErrorHandler::register(isset($a['io']) ? $a['io'] : null);

        // InstalledVersions as Factory::createComposer() or the last
        // FilesystemRepository::write() loaded it before PHP started.
        if (isset($a['ivPending'])) {
            Api::reloadInstalledVersions($a['ivPending']);
        }

        // Files maestro has the shim load at boot (its tests' handlers).
        foreach (isset($a['require']) ? $a['require'] : [] as $file) {
            self::requireFile($file);
        }
    }

    /**
     * `shutdown`: the end of maestro's run. exit() runs the shutdown
     * functions and destructors, as at the end of Composer's process, and
     * gives maestro the process's final status.
     *
     * @param array<string, mixed> $a
     */
    public static function shutdown(array $a): void
    {
        Rpc::flushOutput();
        exit((int) $a['code']);
    }

    public static function ping(): void
    {
    }

    private static function requireFile(string $file): void
    {
        require $file;
    }
}
