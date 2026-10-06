<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.12). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * The frames of the methods Symfony's Console declares that Composer's
 * stack holds while a command runs: Application::run() and doRun()
 * (parent::run() and parent::doRun() of Composer's), doRunCommand() and
 * Command::run(). maestro enters a
 * frame by calling the method itself on its object (Frames), whose code
 * must resume the rest of the call at once (Frames::resumes()); the shim's
 * Composer classes do so, but the bundled Symfony Console is Composer's
 * vendor/ copy, line for line, which traces name as Composer's files.
 *
 * So the files stay as they are, and their methods resume where PHP
 * compiles them: the two files are required through a `file` stream
 * wrapper (this class) that hands PHP their code with the call of
 * Frames::resumes() added after the opening brace of each method, on the
 * brace's line. Their path (__FILE__, what traces name), lines and code
 * are otherwise the files', and the wrapper is PHP's own again as soon as
 * the file is open: no other file goes through it.
 */
final class SymfonyHooks
{
    /** The methods that resume a frame, by class. */
    private const METHODS = [
        'Symfony\\Component\\Console\\Application' => ['run', 'doRun', 'doRunCommand'],
        'Symfony\\Component\\Console\\Command\\Command' => ['run'],
    ];

    /** @var array<string, string> the code to hand PHP, by path */
    private static $pending = [];

    /** @var resource|null set by PHP for a stream wrapper */
    public $context;

    /** @var string */
    private $code = '';

    /** @var int */
    private $pos = 0;

    /** @var string */
    private $path = '';

    /**
     * Requires the file of $class, with the hooks when it is one of the
     * Console's classes they go in. Returns false for any other class.
     */
    public static function load(string $class, string $file): bool
    {
        if (!isset(self::METHODS[$class])) {
            return false;
        }
        $code = @file_get_contents($file);
        $hooked = is_string($code) ? self::hook($code, self::METHODS[$class]) : null;
        if ($hooked === null) {
            // not the file the shim bundles: as it is
            self::requireFile($file);

            return true;
        }

        self::$pending = [$file => $hooked];
        $real = realpath($file);
        if (is_string($real)) {
            self::$pending[$real] = $hooked;
        }
        stream_wrapper_unregister('file');
        stream_wrapper_register('file', self::class);
        try {
            self::requireFile($file);
        } finally {
            self::$pending = [];
            @stream_wrapper_restore('file');
        }

        return true;
    }

    /**
     * $code with the hook after the opening brace of each method, or null
     * when one of them is not found as Symfony declares it (the brace on
     * the line after the signature).
     *
     * @param list<string> $methods
     */
    public static function hook(string $code, array $methods): ?string
    {
        $lines = explode("\n", $code);
        foreach ($methods as $method) {
            $found = false;
            foreach ($lines as $i => $line) {
                if (preg_match('{^\s*(?:public|protected|private)\s+function\s+'.$method.'\(}', $line) && isset($lines[$i + 1]) && trim($lines[$i + 1]) === '{') {
                    $lines[$i + 1] = rtrim($lines[$i + 1]).' if (\\Maestro\\Shim\\Frames::resumes($this, __FUNCTION__)) { return 0; }';
                    $found = true;

                    break;
                }
            }
            if (!$found) {
                return null;
            }
        }

        return implode("\n", $lines);
    }

    /**
     * Requires a file without exposing the loader's locals to it.
     */
    private static function requireFile(string $file): void
    {
        require $file;
    }

    /**
     * @param string $path
     * @param string $mode
     * @param int $options
     * @param string|null $openedPath
     */
    public function stream_open($path, $mode, $options, &$openedPath): bool
    {
        // PHP's wrapper is back before anything else opens a file
        @stream_wrapper_restore('file');
        if (!isset(self::$pending[$path])) {
            return false;
        }
        $this->code = self::$pending[$path];
        $this->path = $path;
        $this->pos = 0;
        if (($options & STREAM_USE_PATH) !== 0) {
            $openedPath = $path;
        }

        return true;
    }

    /**
     * @param int $count
     * @return string
     */
    public function stream_read($count)
    {
        $chunk = (string) substr($this->code, $this->pos, $count);
        $this->pos += strlen($chunk);

        return $chunk;
    }

    public function stream_eof(): bool
    {
        return $this->pos >= strlen($this->code);
    }

    public function stream_tell(): int
    {
        return $this->pos;
    }

    /**
     * @param int $offset
     * @param int $whence
     */
    public function stream_seek($offset, $whence): bool
    {
        $length = strlen($this->code);
        switch ($whence) {
            case SEEK_SET:
                $pos = $offset;
                break;
            case SEEK_CUR:
                $pos = $this->pos + $offset;
                break;
            case SEEK_END:
                $pos = $length + $offset;
                break;
            default:
                return false;
        }
        if ($pos < 0) {
            return false;
        }
        $this->pos = $pos;

        return true;
    }

    /**
     * @return array<int|string, int>|false
     */
    public function stream_stat()
    {
        $stat = @stat($this->path);
        if (!is_array($stat)) {
            return false;
        }
        $stat[7] = $stat['size'] = strlen($this->code);

        return $stat;
    }

    /**
     * @param int $option
     * @param int $arg1
     * @param int|null $arg2
     */
    public function stream_set_option($option, $arg1, $arg2): bool
    {
        return false;
    }

    public function stream_close(): void
    {
        $this->code = '';
    }

    /**
     * Before the file is open (PHP resolving the path), with PHP's wrapper.
     *
     * @param string $path
     * @param int $flags
     * @return array<int|string, int>|false
     */
    public function url_stat($path, $flags)
    {
        @stream_wrapper_restore('file');
        try {
            if (($flags & STREAM_URL_STAT_LINK) !== 0) {
                $stat = @lstat($path);
            } else {
                $stat = @stat($path);
            }
        } finally {
            if (self::$pending !== []) {
                stream_wrapper_unregister('file');
                stream_wrapper_register('file', self::class);
            }
        }

        return $stat;
    }
}
