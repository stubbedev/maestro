<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * A stream writing to one of maestro's outputs (the maestro-output://
 * wrapper), for what writes to a stream itself: the ConsoleSectionOutput
 * of GoConsoleOutput::section().
 */
final class OutputStream
{
    private const SCHEME = 'maestro-output';

    /** @var array<string, GoOutput> by spl_object_id */
    private static $outputs = [];

    /** @var resource|null set by PHP for stream wrappers */
    public $context;

    /** @var GoOutput|null */
    private $output;

    /**
     * A writable stream whose data goes to $output.
     *
     * @return resource
     */
    public static function open(GoOutput $output)
    {
        if (!in_array(self::SCHEME, stream_get_wrappers(), true)) {
            stream_wrapper_register(self::SCHEME, self::class);
        }
        $id = (string) spl_object_id($output);
        self::$outputs[$id] = $output;
        $stream = fopen(self::SCHEME.'://'.$id, 'w');
        if ($stream === false) {
            throw new ProtocolException('maestro shim: cannot open a stream to maestro\'s output');
        }

        return $stream;
    }

    /**
     * @param string $path
     * @param string $mode
     * @param int $options
     * @param string|null $openedPath
     */
    public function stream_open($path, $mode, $options, &$openedPath): bool
    {
        $id = (string) substr((string) $path, strlen(self::SCHEME.'://'));
        if (!isset(self::$outputs[$id])) {
            return false;
        }
        $this->output = self::$outputs[$id];

        return true;
    }

    /**
     * @param string $data
     */
    public function stream_write($data): int
    {
        // Formatted already: maestro writes it as is.
        Rpc::call('output.write', [$this->output, $data, false]);

        return strlen($data);
    }

    public function stream_flush(): bool
    {
        return true;
    }

    public function stream_eof(): bool
    {
        return false;
    }

    public function stream_close(): void
    {
        $this->output = null;
    }
}
