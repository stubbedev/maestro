<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.10, §5.12). Not part of
 * Composer. Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * Exception traces as Composer's PHP stack would hold them (docs/PLUGINS.md
 * §5.12). PHP's own trace of an exception holds the shim's machinery
 * where Composer's stack has Composer's code: the RPC loop and handlers
 * below the plugin code maestro called, the shim's classes standing for
 * Composer's. maestro knows Composer's frames (internal/phperr records
 * them as an error goes up its ports), so a trace is the plugin's own
 * frames down to the call maestro made, completed by maestro:
 *
 * - an exception PHP code throws goes to maestro with its frames up to the
 *   first one the shim's machinery called, the boundary, whose location
 *   maestro sets to Composer's call (the "open" frame) before adding the
 *   frames of the calls the error goes up through;
 * - an exception coming from maestro (a Go error, or a PHP exception going
 *   back) gets those frames as its trace, followed by the PHP stack it is
 *   thrown into, cut at its own boundary in turn.
 *
 * Composer's files are named as maestro names them, under the phar
 * maestro's executable stands for (phperr.Root): the vendored libraries
 * are Composer's vendor/ files, line for line; the shim's Composer classes
 * name Composer's line where a call carries an `@line N` comment, and keep
 * their own location otherwise.
 */
final class Traces
{
    /** @var string Composer's root as maestro names it ("phar://<maestro>") */
    private static $root = '';

    /** @var string|null */
    private static $shim;

    /** @var array<string, list<string>> the lines of the shim's Composer classes read so far */
    private static $lines = [];

    /** @var \SplObjectStorage<\Throwable, int>|\WeakMap<\Throwable, int>|null */
    private static $settled;

    public static function setRoot(string $root): void
    {
        self::$root = $root;
    }

    /**
     * The trace maestro gets with an exception PHP code threw: its frames
     * up to and including the boundary, each named as Composer's stack
     * names it, the boundary flagged `open`. The frames maestro gave the
     * exception when it threw it into PHP (throwInto()) are kept as they
     * are: a frame there may name the shim's machinery (a call maestro did
     * not locate), which is no boundary.
     *
     * @return list<array<string, mixed>>
     */
    public static function toMaestro(\Throwable $e): array
    {
        $trace = $e->getTrace();
        $settled = isset(self::settled()[$e]) ? self::settled()[$e] : 0;
        $out = [];
        foreach (array_merge(array_slice($trace, 0, $settled), self::cut(array_slice($trace, $settled), false)) as $frame) {
            $out[] = [
                'file' => isset($frame['file']) ? $frame['file'] : '',
                'line' => isset($frame['line']) ? $frame['line'] : 0,
                'class' => isset($frame['class']) ? $frame['class'] : '',
                'type' => isset($frame['type']) ? $frame['type'] : '',
                'function' => isset($frame['function']) ? $frame['function'] : '',
                'open' => !empty($frame[self::OPEN]),
            ];
        }

        return $out;
    }

    /**
     * Sets the trace of an exception maestro throws into PHP: the frames
     * maestro gives (Composer's calls the error went up through, the
     * plugin frames of a PHP exception before them), then the PHP stack
     * of the code it is thrown into, outermost frames from the boundary
     * on left out.
     *
     * @param list<array<string, mixed>> $frames
     */
    public static function throwInto(\Throwable $e, array $frames): void
    {
        $trace = [];
        foreach ($frames as $frame) {
            $f = [];
            foreach (['file', 'line', 'class', 'type', 'function'] as $key) {
                if (isset($frame[$key]) && $frame[$key] !== '' && $frame[$key] !== 0) {
                    $f[$key] = $frame[$key];
                }
            }
            if (!isset($f['function'])) {
                $f['function'] = '';
            }
            $trace[] = $f;
        }
        self::settled()[$e] = count($trace);
        foreach (self::cut(debug_backtrace(0), true) as $frame) {
            unset($frame[self::OPEN]);
            $trace[] = $frame;
        }

        $property = new \ReflectionProperty($e instanceof \Exception ? \Exception::class : \Error::class, 'trace');
        if (PHP_VERSION_ID < 80100) {
            $property->setAccessible(true);
        }
        $property->setValue($e, $trace);
    }

    /**
     * The number of leading frames maestro gave each exception it threw
     * into PHP (a WeakMap where PHP has one).
     *
     * @return \SplObjectStorage<\Throwable, int>|\WeakMap<\Throwable, int>
     */
    private static function settled()
    {
        if (self::$settled === null) {
            self::$settled = class_exists('WeakMap') ? new \WeakMap() : new \SplObjectStorage();
        }

        return self::$settled;
    }

    /**
     * Composer's location of a file and line of the shim: the vendored
     * libraries' files are Composer's vendor/ files, the shim's Composer
     * classes Composer's sources where the line names Composer's (`@line
     * N`), and any line naming a file of Composer's (`@line path:N`, for
     * code eval()'d where Composer evals it) that file. Code eval()'d
     * ("<file>(<line>) : eval()'d code") keeps its lines, and is named
     * after the location of the eval().
     *
     * @return array{0: string, 1: int}
     */
    public static function composerLocation(string $file, int $line): array
    {
        if (preg_match('{^(.*)\((\d+)\) : eval\(\)\'d code$}', $file, $m)) {
            list($evalFile, $evalLine) = self::composerLocation($m[1], (int) $m[2]);

            return [$evalFile.'('.$evalLine.") : eval()'d code", $line];
        }
        $rel = self::relative($file);
        if (self::$root === '' || $rel === null) {
            return [$file, $line];
        }
        if (strpos($rel, 'lib/') === 0) {
            return [self::$root.'/vendor/'.substr($rel, 4), $line];
        }
        list($composerFile, $composerLine) = self::composerLine($file, $line);
        if ($composerFile !== null) {
            return [self::$root.'/'.$composerFile, $composerLine];
        }
        if ($composerLine !== null && (strpos($rel, 'src/Composer/') === 0 || strpos($rel, 'stubs/Composer/') === 0)) {
            return [self::$root.'/src/'.substr($rel, strpos($rel, 'Composer/')), $composerLine];
        }

        return [$file, $line];
    }

    /**
     * Composer's path of a shim Composer class's file, for the throw
     * sites the shim names with Exceptions::at().
     */
    public static function composerFile(string $file): string
    {
        $rel = self::relative($file);
        if (self::$root === '' || $rel === null) {
            return $file;
        }
        if (strpos($rel, 'src/Composer/') === 0 || strpos($rel, 'stubs/Composer/') === 0) {
            return self::$root.'/src/'.substr($rel, strpos($rel, 'Composer/'));
        }

        return $file;
    }

    private const OPEN = "\0maestroOpen";

    /**
     * The frames of a trace as Composer's stack has them: with $leading
     * (the stack a Go error is thrown into, which starts inside the RPC
     * loop), the leading frames of the shim's machinery dropped; the
     * frames up to the boundary, the first one the machinery called, which
     * is kept with OPEN set: Composer's call stands there.
     *
     * @param list<array<string, mixed>> $trace
     * @return list<array<string, mixed>>
     */
    private static function cut(array $trace, bool $leading): array
    {
        $out = [];
        $i = 0;
        $n = count($trace);
        while ($leading && $i < $n && self::machinery($trace[$i])) {
            $i++;
        }
        for (; $i < $n; $i++) {
            $frame = $trace[$i];
            $file = isset($frame['file']) ? (string) $frame['file'] : '';
            if ($file !== '' && self::internal($file)) {
                $frame[self::OPEN] = true;
                $out[] = $frame;

                break;
            }
            if ($file !== '') {
                list($frame['file'], $frame['line']) = self::composerLocation($file, isset($frame['line']) ? (int) $frame['line'] : 0);
            }
            $out[] = $frame;
        }

        return $out;
    }

    /**
     * Whether a frame is of the shim's own machinery: a function of
     * Maestro\Shim, or a frame called from it.
     *
     * @param array<string, mixed> $frame
     */
    private static function machinery(array $frame): bool
    {
        if (isset($frame['class']) && strpos((string) $frame['class'], 'Maestro\\Shim\\') === 0) {
            return true;
        }

        return isset($frame['file']) && self::internal((string) $frame['file']);
    }

    /**
     * Whether a file is the shim's machinery (its bootstrap and
     * Maestro\Shim), not code standing for Composer's.
     */
    private static function internal(string $file): bool
    {
        $rel = self::relative($file);
        if ($rel === null || substr($file, -strlen("eval()'d code")) === "eval()'d code") {
            return false;
        }

        return strpos($rel, 'src/Maestro/') === 0 || strpos($rel, '/') === false || strpos($rel, 'bin/') === 0;
    }

    /**
     * The location in Composer's sources a line of the shim stands for
     * (its `@line N` or `@line path:N` comment): the path relative to
     * Composer's root (null for the shim file's own) and the line, or
     * [null, null].
     *
     * @return array{0: ?string, 1: ?int}
     */
    private static function composerLine(string $file, int $line): array
    {
        if (!isset(self::$lines[$file])) {
            $lines = @file($file);
            self::$lines[$file] = is_array($lines) ? $lines : [];
        }
        if (isset(self::$lines[$file][$line - 1]) && preg_match('{// @line (?:(\S+):)?(\d+)}', self::$lines[$file][$line - 1], $m)) {
            return [$m[1] !== '' ? $m[1] : null, (int) $m[2]];
        }

        return [null, null];
    }

    /**
     * The path of a file of the shim relative to its root, with forward
     * slashes (PHP names files with backslashes on Windows), or null for
     * a file elsewhere.
     */
    private static function relative(string $file): ?string
    {
        $shim = self::shim();
        if (DIRECTORY_SEPARATOR === '\\') {
            $file = strtr($file, '\\', '/');
        }
        if (strpos($file, $shim) !== 0) {
            return null;
        }

        return substr($file, strlen($shim));
    }

    /** The shim's root directory, with forward slashes and a trailing one. */
    private static function shim(): string
    {
        if (self::$shim === null) {
            $shim = dirname(__DIR__, 3);
            self::$shim = (DIRECTORY_SEPARATOR === '\\' ? strtr($shim, '\\', '/') : $shim).'/';
        }

        return self::$shim;
    }
}
