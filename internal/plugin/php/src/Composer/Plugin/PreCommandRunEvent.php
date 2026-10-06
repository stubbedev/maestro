<?php

/*
 * maestro's plugin shim: Composer\Plugin\PreCommandRunEvent
 * (docs/PLUGINS.md §4.4), a mirror of maestro's, whose input is maestro's
 * (an input mirror: what listeners change in it reaches the command,
 * §5.9).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Plugin;

class PreCommandRunEvent extends \Composer\EventDispatcher\Event
{
    private $input;
    private $command;

    public function __construct(string $name, \Symfony\Component\Console\Input\InputInterface $input, string $command)
    {
        parent::__construct($name);
        $this->input = $input;
        $this->command = $command;
    }

    public function getCommand(): string
    {
        return $this->command;
    }

    public function getInput(): \Symfony\Component\Console\Input\InputInterface
    {
        if ($this->input === null) {
            // maestro's events always cross with their input.
            throw new \Maestro\Shim\ProtocolException('maestro shim: the event has no input');
        }

        return $this->input;
    }
}
