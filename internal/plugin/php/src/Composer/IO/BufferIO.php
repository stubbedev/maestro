<?php

/*
 * maestro's plugin shim: Composer\IO\BufferIO (docs/PLUGINS.md §4.3): the
 * mirror of a maestro buffer IO (see ConsoleIO), or, created in PHP,
 * Composer's BufferIO on the vendored StreamOutput(php://memory).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\IO;

use Composer\Pcre\Preg;
use Symfony\Component\Console\Helper\HelperSet;
use Symfony\Component\Console\Helper\QuestionHelper;
use Symfony\Component\Console\Input\StreamableInputInterface;
use Symfony\Component\Console\Input\StringInput;
use Symfony\Component\Console\Output\StreamOutput;

class BufferIO extends \Composer\IO\ConsoleIO
{
    public function __construct(string $input = '', int $verbosity = \Symfony\Component\Console\Output\StreamOutput::VERBOSITY_NORMAL, ?\Symfony\Component\Console\Formatter\OutputFormatterInterface $formatter = null)
    {
        $input = new StringInput($input);
        $input->setInteractive(false);

        $stream = fopen('php://memory', 'rw');
        if ($stream === false) {
            throw new \RuntimeException('Unable to open memory output stream');
        }
        $output = new StreamOutput($stream, $verbosity, $formatter !== null ? $formatter->isDecorated() : false, $formatter);

        parent::__construct($input, $output, new HelperSet([
            new QuestionHelper(),
        ]));
    }

    public function getOutput(): string
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            return \Maestro\Shim\Rpc::call('io.getOutput', [$this]);
        }

        assert($this->output instanceof StreamOutput);
        fseek($this->output->getStream(), 0);

        $output = (string) stream_get_contents($this->output->getStream());

        $output = Preg::replaceCallback("{(?<=^|\n|\x08)(.+?)(\x08+)}", static function ($matches): string {
            $pre = strip_tags($matches[1]);

            if (strlen($pre) === strlen($matches[2])) {
                return '';
            }

            // TODO reverse parse the string, skipping span tags and \033\[([0-9;]+)m(.*?)\033\[0m style blobs
            return rtrim($matches[1])."\n";
        }, $output);

        return $output;
    }

    public function setUserInputs(array $inputs): void
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Rpc::call('io.setUserInputs', [$this, $inputs]);

            return;
        }

        if (!$this->input instanceof StreamableInputInterface) {
            throw new \RuntimeException('Setting the user inputs requires at least the version 3.2 of the symfony/console component.');
        }

        $this->input->setStream($this->createStream($inputs));
        $this->input->setInteractive(true);
    }

    /**
     * @param string[] $inputs
     *
     * @return resource stream
     */
    private function createStream(array $inputs)
    {
        $stream = fopen('php://memory', 'r+');
        if ($stream === false) {
            throw new \RuntimeException('Unable to open memory output stream');
        }

        foreach ($inputs as $input) {
            fwrite($stream, $input.PHP_EOL);
        }

        rewind($stream);

        return $stream;
    }
}
