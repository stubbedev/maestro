<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

use Symfony\Component\Console\Output\ConsoleOutputInterface;
use Symfony\Component\Console\Output\ConsoleSectionOutput;
use Symfony\Component\Console\Output\OutputInterface;

/**
 * A GoOutput with an error output (maestro's ConsoleOutputInterface).
 */
final class GoConsoleOutput extends GoOutput implements ConsoleOutputInterface
{
    /** @var OutputInterface|null */
    private $stderr;

    /** @var list<ConsoleSectionOutput> ConsoleOutput::$consoleSectionOutputs */
    private $sections = [];

    public function getErrorOutput()
    {
        if ($this->stderr === null) {
            $this->stderr = Rpc::call('output.errorOutput', [$this]);
        }

        return $this->stderr;
    }

    public function setErrorOutput(OutputInterface $error)
    {
        $this->stderr = $error;
    }

    /**
     * Symfony's ConsoleOutput::section(), on a stream writing to maestro's
     * output (Maestro\Shim\OutputStream).
     */
    public function section(): ConsoleSectionOutput
    {
        return new ConsoleSectionOutput(OutputStream::open($this), $this->sections, $this->getVerbosity(), $this->isDecorated(), $this->getFormatter());
    }
}
