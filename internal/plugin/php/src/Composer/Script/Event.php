<?php

/*
 * maestro's plugin shim: Composer\Script\Event (docs/PLUGINS.md §4.4). See
 * Composer\EventDispatcher\Event.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Script;

class Event extends \Composer\EventDispatcher\Event
{
    private $composer;
    private $io;
    private $devMode;
    private $originatingEvent;

    private function calculateOriginatingEvent(\Composer\EventDispatcher\Event $event): \Composer\EventDispatcher\Event
    {
        if ($event instanceof Event && $event->getOriginatingEvent()) {
            return $this->calculateOriginatingEvent($event->getOriginatingEvent());
        }

        return $event;
    }

    public function __construct(string $name, \Composer\Composer $composer, \Composer\IO\IOInterface $io, bool $devMode = false, array $args = [], array $flags = [])
    {
        parent::__construct($name, $args, $flags);
        $this->composer = $composer;
        $this->io = $io;
        $this->devMode = $devMode;
    }

    public function getComposer(): \Composer\Composer
    {
        return $this->composer;
    }

    public function getIO(): \Composer\IO\IOInterface
    {
        return $this->io;
    }

    public function getOriginatingEvent(): ?\Composer\EventDispatcher\Event
    {
        return $this->originatingEvent;
    }

    public function isDevMode(): bool
    {
        return $this->devMode;
    }

    public function setOriginatingEvent(\Composer\EventDispatcher\Event $event): self
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Rpc::call('event.setOriginatingEvent', [$this, $event]);

            return $this;
        }
        $this->originatingEvent = $this->calculateOriginatingEvent($event);

        return $this;
    }
}
