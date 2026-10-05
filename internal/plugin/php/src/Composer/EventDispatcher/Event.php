<?php

/*
 * maestro's plugin shim: Composer\EventDispatcher\Event (docs/PLUGINS.md
 * §4.4). maestro's events are data mirrors (Maestro\Shim\Adapter\
 * EventAdapter); stopPropagation() on one stops maestro's dispatch. An event
 * created in PHP (a plugin's subclass) is plain PHP until it is dispatched.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\EventDispatcher;

class Event
{
    private $propagationStopped = false;

    protected $args;
    protected $flags;
    protected $name;

    public function __construct(string $name, array $args = [], array $flags = [])
    {
        $this->name = $name;
        $this->args = $args;
        $this->flags = $flags;
    }

    public function getArguments(): array
    {
        return $this->args;
    }

    public function getFlags(): array
    {
        return $this->flags;
    }

    public function getName(): string
    {
        return $this->name;
    }

    public function isPropagationStopped(): bool
    {
        return $this->propagationStopped;
    }

    public function stopPropagation(): void
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Rpc::call('event.stopPropagation', [$this]);

            return;
        }
        $this->propagationStopped = true;
    }
}
