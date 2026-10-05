<?php

/*
 * maestro's plugin shim: Composer\Installer\PackageEvent (docs/PLUGINS.md
 * §4.4). See Composer\EventDispatcher\Event: getOperations() returns the
 * same operation mirrors for every event of one installation.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Installer;

class PackageEvent extends \Composer\EventDispatcher\Event
{
    private $composer;
    private $io;
    private $devMode;
    private $localRepo;
    private $operations;
    private $operation;

    public function __construct(string $eventName, \Composer\Composer $composer, \Composer\IO\IOInterface $io, bool $devMode, \Composer\Repository\RepositoryInterface $localRepo, array $operations, \Composer\DependencyResolver\Operation\OperationInterface $operation)
    {
        parent::__construct($eventName);
        $this->composer = $composer;
        $this->io = $io;
        $this->devMode = $devMode;
        $this->localRepo = $localRepo;
        $this->operations = $operations;
        $this->operation = $operation;
    }

    public function getComposer(): \Composer\Composer
    {
        return $this->composer;
    }

    public function getIO(): \Composer\IO\IOInterface
    {
        return $this->io;
    }

    public function getLocalRepo(): \Composer\Repository\RepositoryInterface
    {
        return $this->localRepo;
    }

    public function getOperation(): \Composer\DependencyResolver\Operation\OperationInterface
    {
        return $this->operation;
    }

    public function getOperations(): array
    {
        return $this->operations;
    }

    public function isDevMode(): bool
    {
        return $this->devMode;
    }
}
