<?php

/*
 * maestro's plugin shim: Composer\Plugin\CommandEvent (docs/PLUGINS.md
 * §4.4), a mirror of maestro's. The input and output mirrors come with the
 * command support (phase 4).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Plugin;

class CommandEvent extends \Composer\EventDispatcher\Event
{
    private $commandName;
    private $input;
    private $output;

    public function __construct(string $name, string $commandName, \Symfony\Component\Console\Input\InputInterface $input, \Symfony\Component\Console\Output\OutputInterface $output, array $args = [], array $flags = [])
    {
        parent::__construct($name, $args, $flags);
        $this->commandName = $commandName;
        $this->input = $input;
        $this->output = $output;
    }

    public function getCommandName(): string
    {
        return $this->commandName;
    }

    public function getInput(): \Symfony\Component\Console\Input\InputInterface
    {
        if ($this->input === null) {
            \Maestro\Shim\Remote::unsupported(self::class, 'getInput');
        }

        return $this->input;
    }

    public function getOutput(): \Symfony\Component\Console\Output\OutputInterface
    {
        if ($this->output === null) {
            \Maestro\Shim\Remote::unsupported(self::class, 'getOutput');
        }

        return $this->output;
    }
}
