<?php

/*
 * maestro's plugin shim: Composer\IO\ConsoleIO (docs/PLUGINS.md §4.3, §5.9):
 * the mirror of maestro's console IO. Its flags (verbosity, decoration,
 * interactivity) are mirror fields, kept up to date by the sync engine;
 * every other method is maestro's (io.*). Creating one in PHP is not
 * supported yet.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\IO;

class ConsoleIO extends \Composer\IO\BaseIO
{
    private $maestroState = [];

    protected $helperSet;
    protected $input;
    protected $lastMessage = '';
    protected $lastMessageErr = '';
    protected $output;

    public function __construct(\Symfony\Component\Console\Input\InputInterface $input, \Symfony\Component\Console\Output\OutputInterface $output, \Symfony\Component\Console\Helper\HelperSet $helperSet)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\IO\\ConsoleIO::__construct() in plugins yet');
    }

    public function ask($question, $default = null)
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(static::class, 'ask');
        }

        return \Maestro\Shim\Rpc::call('io.ask', [$this, $question, $default]);
    }

    public function askAndHideAnswer($question)
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(static::class, 'askAndHideAnswer');
        }

        return \Maestro\Shim\Rpc::call('io.askAndHideAnswer', [$this, $question]);
    }

    public function askAndValidate($question, $validator, $attempts = null, $default = null)
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(static::class, 'askAndValidate');
        }

        return \Maestro\Shim\Rpc::call('io.askAndValidate', [$this, $question, $validator, $attempts, $default]);
    }

    public function askConfirmation($question, $default = true)
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(static::class, 'askConfirmation');
        }

        return \Maestro\Shim\Rpc::call('io.askConfirmation', [$this, $question, $default]);
    }

    public function enableDebugging(float $startTime)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\IO\\ConsoleIO::enableDebugging() in plugins yet');
    }

    public function enableTimestamps(string $format = \DATE_RFC3339_EXTENDED)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\IO\\ConsoleIO::enableTimestamps() in plugins yet');
    }

    public function getProgressBar(int $max = 0)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\IO\\ConsoleIO::getProgressBar() in plugins yet');
    }

    public function getTable(): \Symfony\Component\Console\Helper\Table
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\IO\\ConsoleIO::getTable() in plugins yet');
    }

    public function isDebug()
    {
        return \Maestro\Shim\Adapter\IOAdapter::state($this, 'debug');
    }

    public function isDecorated()
    {
        return \Maestro\Shim\Adapter\IOAdapter::state($this, 'decorated');
    }

    public function isInteractive()
    {
        return \Maestro\Shim\Adapter\IOAdapter::state($this, 'interactive');
    }

    public function isVerbose()
    {
        return \Maestro\Shim\Adapter\IOAdapter::state($this, 'verbose');
    }

    public function isVeryVerbose()
    {
        return \Maestro\Shim\Adapter\IOAdapter::state($this, 'veryVerbose');
    }

    public function overwrite($messages, bool $newline = true, ?int $size = null, int $verbosity = self::NORMAL)
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(static::class, 'overwrite');
        }

        \Maestro\Shim\Rpc::call('io.overwrite', [$this, $messages, $newline, $size, $verbosity]);
    }

    public function overwriteError($messages, bool $newline = true, ?int $size = null, int $verbosity = self::NORMAL)
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(static::class, 'overwriteError');
        }

        \Maestro\Shim\Rpc::call('io.overwriteError', [$this, $messages, $newline, $size, $verbosity]);
    }

    public static function sanitize($messages, bool $allowNewlines = true)
    {
        return \Maestro\Shim\Rpc::call('io.sanitize', [$messages, $allowNewlines]);
    }

    public function select($question, $choices, $default, $attempts = false, $errorMessage = 'Value "%s" is invalid', $multiselect = false)
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(static::class, 'select');
        }

        return \Maestro\Shim\Rpc::call('io.select', [$this, $question, $choices, $default, $attempts, $errorMessage, $multiselect]);
    }

    public function write($messages, bool $newline = true, int $verbosity = self::NORMAL)
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(static::class, 'write');
        }

        \Maestro\Shim\Rpc::call('io.write', [$this, $messages, $newline, $verbosity]);
    }

    public function writeError($messages, bool $newline = true, int $verbosity = self::NORMAL)
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(static::class, 'writeError');
        }

        \Maestro\Shim\Rpc::call('io.writeError', [$this, $messages, $newline, $verbosity]);
    }

    public function writeErrorRaw($messages, bool $newline = true, int $verbosity = self::NORMAL)
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(static::class, 'writeErrorRaw');
        }

        \Maestro\Shim\Rpc::call('io.writeErrorRaw', [$this, $messages, $newline, $verbosity]);
    }

    public function writeRaw($messages, bool $newline = true, int $verbosity = self::NORMAL)
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(static::class, 'writeRaw');
        }

        \Maestro\Shim\Rpc::call('io.writeRaw', [$this, $messages, $newline, $verbosity]);
    }
}
