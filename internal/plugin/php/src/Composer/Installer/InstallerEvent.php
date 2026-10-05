<?php

/*
 * maestro's plugin shim: Composer\Installer\InstallerEvent (docs/PLUGINS.md
 * §4.4). See Composer\EventDispatcher\Event.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Installer;

class InstallerEvent extends \Composer\EventDispatcher\Event
{
    private $composer;
    private $io;
    private $devMode;
    private $executeOperations;
    private $transaction;

    public function __construct(string $eventName, \Composer\Composer $composer, \Composer\IO\IOInterface $io, bool $devMode, bool $executeOperations, \Composer\DependencyResolver\Transaction $transaction)
    {
        parent::__construct($eventName);
        $this->composer = $composer;
        $this->io = $io;
        $this->devMode = $devMode;
        $this->executeOperations = $executeOperations;
        $this->transaction = $transaction;
    }

    public function getComposer(): \Composer\Composer
    {
        return $this->composer;
    }

    public function getIO(): \Composer\IO\IOInterface
    {
        return $this->io;
    }

    public function getTransaction(): ?\Composer\DependencyResolver\Transaction
    {
        return $this->transaction;
    }

    public function isDevMode(): bool
    {
        return $this->devMode;
    }

    public function isExecutingOperations(): bool
    {
        return $this->executeOperations;
    }
}
