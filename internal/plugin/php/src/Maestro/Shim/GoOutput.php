<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * One of maestro's outputs that is not its console (a buffer, a test's
 * stream), as PHP writes to it: Symfony formats and filters each message
 * here, and maestro writes the result.
 */
class GoOutput extends \Symfony\Component\Console\Output\Output
{
    protected function doWrite(string $message, bool $newline)
    {
        Rpc::call('output.write', [$this, $message, $newline]);
    }
}
