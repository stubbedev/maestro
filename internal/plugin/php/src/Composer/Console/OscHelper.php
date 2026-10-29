<?php declare(strict_types=1);

/*
 * maestro's plugin shim: Composer\Console\OscHelper (docs/PLUGINS.md
 * §4.3). Writes Operating System Commands without formatting or adding a
 * newline; a ConsoleIO created in PHP uses it, maestro's own writes them
 * from Go.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Console;

use Composer\Pcre\Preg;
use Symfony\Component\Console\Helper\Helper;
use Symfony\Component\Console\Output\OutputInterface;
use Symfony\Component\Console\Output\StreamOutput;

final class OscHelper extends Helper
{
    public function getName(): string
    {
        return 'osc';
    }

    public function write(OutputInterface $output, string $command, string $payload): void
    {
        if (Preg::isMatch('{[\x00-\x1f\x7f-\x9f]}u', $command) || Preg::isMatch('{[\x00-\x1f\x7f-\x9f]}u', $payload)) {
            throw new \InvalidArgumentException('OSC commands and payloads must not contain control characters.');
        }

        if (!$this->supportsOutput($output)) {
            return;
        }

        $output->write("\033]".$command.';'.$payload."\033\\", false, OutputInterface::OUTPUT_RAW);
    }

    public function supportsOutput(OutputInterface $output): bool
    {
        if (!$output->isDecorated() || $output->getVerbosity() <= OutputInterface::VERBOSITY_QUIET) {
            return false;
        }

        // Forced ANSI decoration does not make a redirected stream a terminal.
        return !$output instanceof StreamOutput || $this->isTerminal($output->getStream());
    }

    /** @param resource $stream */
    private function isTerminal($stream): bool
    {
        // Platform's MSYS shortcut cannot establish whether this stream is redirected.
        if (function_exists('stream_isatty')) {
            return stream_isatty($stream);
        }
        if (function_exists('posix_isatty')) {
            return posix_isatty($stream);
        }

        $stat = @fstat($stream);

        return $stat !== false && ($stat['mode'] & 0170000) === 0020000;
    }
}
