<?php

/*
 * maestro's plugin shim: Composer\IO\ConsoleIO (docs/PLUGINS.md §4.3, §5.9).
 * maestro's console IO crosses as an instance of this class: its flags
 * (verbosity, decoration, interactivity) are mirror fields, kept up to date
 * by the sync engine, and every other method is maestro's (io.*). An IO
 * created in PHP (a BufferIO, a plugin's own ConsoleIO) behaves as
 * Composer's, on its own input, output and helpers.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\IO;

use Composer\Question\StrictConfirmationQuestion;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;
use Symfony\Component\Console\Helper\ProgressBar;
use Symfony\Component\Console\Helper\Table;
use Symfony\Component\Console\Output\ConsoleOutputInterface;
use Symfony\Component\Console\Output\OutputInterface;
use Symfony\Component\Console\Question\ChoiceQuestion;
use Symfony\Component\Console\Question\Question;

class ConsoleIO extends \Composer\IO\BaseIO
{
    private $maestroState = [];

    protected $helperSet;
    protected $input;
    protected $lastMessage = '';
    protected $lastMessageErr = '';
    protected $output;

    private $sendTimestamps = false;
    private $startTime;
    private $verbosityMap;

    public function __construct(\Symfony\Component\Console\Input\InputInterface $input, \Symfony\Component\Console\Output\OutputInterface $output, \Symfony\Component\Console\Helper\HelperSet $helperSet)
    {
        $this->input = $input;
        $this->output = $output;
        $this->helperSet = $helperSet;
        $this->verbosityMap = [
            self::QUIET => OutputInterface::VERBOSITY_QUIET,
            self::NORMAL => OutputInterface::VERBOSITY_NORMAL,
            self::VERBOSE => OutputInterface::VERBOSITY_VERBOSE,
            self::VERY_VERBOSE => OutputInterface::VERBOSITY_VERY_VERBOSE,
            self::DEBUG => OutputInterface::VERBOSITY_DEBUG,
        ];
    }

    public function ask($question, $default = null)
    {
        if (Remote::owned($this)) {
            return Rpc::call('io.ask', [$this, $question, $default]);
        }

        /** @var \Symfony\Component\Console\Helper\QuestionHelper $helper */
        $helper = $this->helperSet->get('question');
        $question = new Question(self::sanitize($question), is_string($default) ? self::sanitize($default) : $default);

        return $helper->ask($this->input, $this->getErrorOutput(), $question);
    }

    public function askAndHideAnswer($question)
    {
        if (Remote::owned($this)) {
            return Rpc::call('io.askAndHideAnswer', [$this, $question]);
        }

        /** @var \Symfony\Component\Console\Helper\QuestionHelper $helper */
        $helper = $this->helperSet->get('question');
        $question = new Question(self::sanitize($question));
        $question->setHidden(true);

        return $helper->ask($this->input, $this->getErrorOutput(), $question);
    }

    public function askAndValidate($question, $validator, $attempts = null, $default = null)
    {
        if (Remote::owned($this)) {
            return Rpc::call('io.askAndValidate', [$this, $question, $validator, $attempts, $default]);
        }

        /** @var \Symfony\Component\Console\Helper\QuestionHelper $helper */
        $helper = $this->helperSet->get('question');
        $question = new Question(self::sanitize($question), is_string($default) ? self::sanitize($default) : $default);
        $question->setValidator($validator);
        $question->setMaxAttempts($attempts);

        return $helper->ask($this->input, $this->getErrorOutput(), $question);
    }

    public function askConfirmation($question, $default = true)
    {
        if (Remote::owned($this)) {
            return Rpc::call('io.askConfirmation', [$this, $question, $default]);
        }

        /** @var \Symfony\Component\Console\Helper\QuestionHelper $helper */
        $helper = $this->helperSet->get('question');
        $question = new StrictConfirmationQuestion(self::sanitize($question), is_string($default) ? self::sanitize($default) : $default);

        return $helper->ask($this->input, $this->getErrorOutput(), $question);
    }

    public function enableDebugging(float $startTime)
    {
        if (Remote::owned($this)) {
            // maestro's IO prefixes what it writes (`--profile`'s
            // "[%.1fMiB/%.2fs] ", with maestro's memory usage).
            Rpc::call('io.enableDebugging', [$this, $startTime]);

            return;
        }

        $this->startTime = $startTime;
    }

    public function enableTimestamps(string $format = \DATE_RFC3339_EXTENDED)
    {
        if (Remote::owned($this)) {
            // maestro formats as DateTime::format() in PHP's default
            // time zone.
            Rpc::call('io.enableTimestamps', [$this, $format, date_default_timezone_get()]);

            return;
        }

        $this->sendTimestamps = $format;
    }

    public function getProgressBar(int $max = 0)
    {
        if (Remote::owned($this) && $this->output === null) {
            Remote::unsupported(static::class, 'getProgressBar');
        }

        // On maestro's IO, its output's mirror (filled by IOAdapter).
        return new ProgressBar($this->getErrorOutput(), $max);
    }

    public function getTable(): \Symfony\Component\Console\Helper\Table
    {
        if (Remote::owned($this) && $this->output === null) {
            Remote::unsupported(static::class, 'getTable');
        }

        return new Table($this->output);
    }

    public function isDebug()
    {
        if (Remote::owned($this)) {
            return \Maestro\Shim\Adapter\IOAdapter::state($this, 'debug');
        }

        return $this->output->isDebug();
    }

    public function isDecorated()
    {
        if (Remote::owned($this)) {
            return \Maestro\Shim\Adapter\IOAdapter::state($this, 'decorated');
        }

        return $this->output->isDecorated();
    }

    public function isInteractive()
    {
        if (Remote::owned($this)) {
            return \Maestro\Shim\Adapter\IOAdapter::state($this, 'interactive');
        }

        return $this->input->isInteractive();
    }

    public function isVerbose()
    {
        if (Remote::owned($this)) {
            return \Maestro\Shim\Adapter\IOAdapter::state($this, 'verbose');
        }

        return $this->output->isVerbose();
    }

    public function isVeryVerbose()
    {
        if (Remote::owned($this)) {
            return \Maestro\Shim\Adapter\IOAdapter::state($this, 'veryVerbose');
        }

        return $this->output->isVeryVerbose();
    }

    public function overwrite($messages, bool $newline = true, ?int $size = null, int $verbosity = self::NORMAL)
    {
        if (Remote::owned($this)) {
            Rpc::call('io.overwrite', [$this, $messages, $newline, $size, $verbosity]);

            return;
        }

        $this->doOverwrite($messages, $newline, $size, false, $verbosity);
    }

    public function overwriteError($messages, bool $newline = true, ?int $size = null, int $verbosity = self::NORMAL)
    {
        if (Remote::owned($this)) {
            Rpc::call('io.overwriteError', [$this, $messages, $newline, $size, $verbosity]);

            return;
        }

        $this->doOverwrite($messages, $newline, $size, true, $verbosity);
    }

    public static function sanitize($messages, bool $allowNewlines = true)
    {
        return Rpc::call('io.sanitize', [$messages, $allowNewlines]);
    }

    public function select($question, $choices, $default, $attempts = false, $errorMessage = 'Value "%s" is invalid', $multiselect = false)
    {
        if (Remote::owned($this)) {
            return Rpc::call('io.select', [$this, $question, $choices, $default, $attempts, $errorMessage, $multiselect]);
        }

        /** @var \Symfony\Component\Console\Helper\QuestionHelper $helper */
        $helper = $this->helperSet->get('question');
        $question = new ChoiceQuestion(self::sanitize($question), self::sanitize($choices), is_string($default) ? self::sanitize($default) : $default);
        $question->setMaxAttempts($attempts ?: null); // IOInterface requires false, and Question requires null or int
        $question->setErrorMessage($errorMessage);
        $question->setMultiselect($multiselect);

        $result = $helper->ask($this->input, $this->getErrorOutput(), $question);

        $isAssoc = (bool) \count(array_filter(array_keys($choices), 'is_string'));
        if ($isAssoc) {
            return $result;
        }

        if (!is_array($result)) {
            return (string) array_search($result, $choices, true);
        }

        $results = [];
        foreach ($choices as $index => $choice) {
            if (in_array($choice, $result, true)) {
                $results[] = (string) $index;
            }
        }

        return $results;
    }

    public function write($messages, bool $newline = true, int $verbosity = self::NORMAL)
    {
        if (Remote::owned($this)) {
            Rpc::call('io.write', [$this, $messages, $newline, $verbosity]);

            return;
        }

        $messages = self::sanitize($messages);

        $this->doWrite($messages, $newline, false, $verbosity);
    }

    public function writeError($messages, bool $newline = true, int $verbosity = self::NORMAL)
    {
        if (Remote::owned($this)) {
            Rpc::call('io.writeError', [$this, $messages, $newline, $verbosity]);

            return;
        }

        $messages = self::sanitize($messages);

        $this->doWrite($messages, $newline, true, $verbosity);
    }

    public function writeErrorRaw($messages, bool $newline = true, int $verbosity = self::NORMAL)
    {
        if (Remote::owned($this)) {
            Rpc::call('io.writeErrorRaw', [$this, $messages, $newline, $verbosity]);

            return;
        }

        $this->doWrite($messages, $newline, true, $verbosity, true);
    }

    public function writeRaw($messages, bool $newline = true, int $verbosity = self::NORMAL)
    {
        if (Remote::owned($this)) {
            Rpc::call('io.writeRaw', [$this, $messages, $newline, $verbosity]);

            return;
        }

        $this->doWrite($messages, $newline, false, $verbosity, true);
    }

    /**
     * @param string[]|string $messages
     */
    private function doWrite($messages, bool $newline, bool $stderr, int $verbosity, bool $raw = false): void
    {
        $sfVerbosity = $this->verbosityMap[$verbosity];
        if ($sfVerbosity > $this->output->getVerbosity()) {
            return;
        }

        if ($raw) {
            $sfVerbosity |= OutputInterface::OUTPUT_RAW;
        }

        if (null !== $this->startTime) {
            $memoryUsage = memory_get_usage() / 1024 / 1024;
            $timeSpent = microtime(true) - $this->startTime;
            $messages = array_map(static function ($message) use ($memoryUsage, $timeSpent): string {
                return sprintf('[%.1fMiB/%.2fs] %s', $memoryUsage, $timeSpent, $message);
            }, (array) $messages);
        }

        if ($this->sendTimestamps !== false) {
            $messages = array_map(function ($message): string {
                return sprintf('[%s] %s', (new \DateTime())->format($this->sendTimestamps), $message);
            }, (array) $messages);
        }

        if (true === $stderr && $this->output instanceof ConsoleOutputInterface) {
            $this->output->getErrorOutput()->write($messages, $newline, $sfVerbosity);
            $this->lastMessageErr = implode($newline ? "\n" : '', (array) $messages);

            return;
        }

        $this->output->write($messages, $newline, $sfVerbosity);
        $this->lastMessage = implode($newline ? "\n" : '', (array) $messages);
    }

    /**
     * @param string[]|string $messages
     */
    private function doOverwrite($messages, bool $newline, ?int $size, bool $stderr, int $verbosity): void
    {
        // messages can be an array, let's convert it to string anyway
        $messages = implode($newline ? "\n" : '', (array) $messages);

        $decorated = $stderr ? $this->getErrorOutput()->isDecorated() : $this->output->isDecorated();

        // backspaces corrupt non-decorated output, so write a plain line instead
        if (!$decorated) {
            if ($messages !== '') {
                $this->doWrite($messages, true, $stderr, $verbosity);
            }
            if ($stderr) {
                $this->lastMessageErr = $messages;
            } else {
                $this->lastMessage = $messages;
            }

            return;
        }

        // since overwrite is supposed to overwrite last message...
        if (!isset($size)) {
            // removing possible formatting of lastMessage with strip_tags
            $size = strlen(strip_tags($stderr ? $this->lastMessageErr : $this->lastMessage));
        }
        // ...let's fill its length with backspaces
        $this->doWrite(str_repeat("\x08", $size), false, $stderr, $verbosity);

        // write the new message
        $this->doWrite($messages, false, $stderr, $verbosity);

        // In cmd.exe on Win8.1 (possibly 10?), the line can not be cleared, so we need to
        // track the length of previous output and fill it with spaces to make sure the line is cleared.
        // See https://github.com/composer/composer/pull/5836 for more details
        $fill = $size - strlen(strip_tags($messages));
        if ($fill > 0) {
            // whitespace whatever has left
            $this->doWrite(str_repeat(' ', $fill), false, $stderr, $verbosity);
            // move the cursor back
            $this->doWrite(str_repeat("\x08", $fill), false, $stderr, $verbosity);
        }

        if ($newline) {
            $this->doWrite('', true, $stderr, $verbosity);
        }

        if ($stderr) {
            $this->lastMessageErr = $messages;
        } else {
            $this->lastMessage = $messages;
        }
    }

    private function getErrorOutput(): OutputInterface
    {
        if ($this->output instanceof ConsoleOutputInterface) {
            return $this->output->getErrorOutput();
        }

        return $this->output;
    }
}
