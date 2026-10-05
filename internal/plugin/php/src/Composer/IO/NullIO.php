<?php

/*
 * maestro's plugin shim: Composer\IO\NullIO (docs/PLUGINS.md §4.3): an IO
 * that does nothing, in PHP; given to maestro, it is maestro's null IO.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\IO;

class NullIO extends \Composer\IO\BaseIO
{
    public function ask($question, $default = null)
    {
        return $default;
    }

    public function askAndHideAnswer($question): ?string
    {
        return null;
    }

    public function askAndValidate($question, $validator, $attempts = null, $default = null)
    {
        return $default;
    }

    public function askConfirmation($question, $default = true): bool
    {
        return $default;
    }

    public function isDebug(): bool
    {
        return false;
    }

    public function isDecorated(): bool
    {
        return false;
    }

    public function isInteractive(): bool
    {
        return false;
    }

    public function isVerbose(): bool
    {
        return false;
    }

    public function isVeryVerbose(): bool
    {
        return false;
    }

    public function overwrite($messages, bool $newline = true, ?int $size = null, int $verbosity = self::NORMAL): void
    {

    }

    public function overwriteError($messages, bool $newline = true, ?int $size = null, int $verbosity = self::NORMAL): void
    {

    }

    public function select($question, $choices, $default, $attempts = false, $errorMessage = 'Value "%s" is invalid', $multiselect = false)
    {
        return $default;
    }

    public function write($messages, bool $newline = true, int $verbosity = self::NORMAL): void
    {

    }

    public function writeError($messages, bool $newline = true, int $verbosity = self::NORMAL): void
    {

    }
}
