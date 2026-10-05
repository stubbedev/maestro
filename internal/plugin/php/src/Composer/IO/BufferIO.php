<?php

/*
 * maestro's plugin shim: Composer\IO\BufferIO (docs/PLUGINS.md §4.3): the
 * mirror of a maestro buffer IO (see ConsoleIO). Creating one in PHP is not
 * supported yet.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\IO;

class BufferIO extends \Composer\IO\ConsoleIO
{
    public function __construct(string $input = '', int $verbosity = \Symfony\Component\Console\Output\StreamOutput::VERBOSITY_NORMAL, ?\Symfony\Component\Console\Formatter\OutputFormatterInterface $formatter = null)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\IO\\BufferIO::__construct() in plugins yet');
    }

    public function getOutput(): string
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(static::class, 'getOutput');
        }

        return \Maestro\Shim\Rpc::call('io.getOutput', [$this]);
    }

    public function setUserInputs(array $inputs): void
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(static::class, 'setUserInputs');
        }

        \Maestro\Shim\Rpc::call('io.setUserInputs', [$this, $inputs]);
    }
}
