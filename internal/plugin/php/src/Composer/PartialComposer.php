<?php

/*
 * maestro's plugin shim: Composer\PartialComposer (docs/PLUGINS.md §4.2).
 * maestro's Composer instances are service proxies: each getter asks
 * maestro for the current object (composer.get*), so a setter called
 * meanwhile (in PHP or in maestro) is seen. One created in PHP is a plain
 * holder, as Composer's is.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer;

use Composer\Package\RootPackageInterface;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class PartialComposer
{
    private $global = false;
    private $package;
    private $loop;
    private $repositoryManager;
    private $installationManager;
    private $config;
    private $eventDispatcher;

    public function setPackage(RootPackageInterface $package): void
    {
        if (Remote::owned($this)) {
            Rpc::call('composer.setPackage', [$this, $package]);

            return;
        }
        $this->package = $package;
    }

    public function getPackage(): \Composer\Package\RootPackageInterface
    {
        if (Remote::owned($this)) {
            return Rpc::call('composer.getPackage', [$this]);
        }

        return $this->package;
    }

    public function setConfig(Config $config): void
    {
        if (Remote::owned($this)) {
            Rpc::call('composer.setConfig', [$this, $config]);

            return;
        }
        $this->config = $config;
    }

    public function getConfig(): Config
    {
        if (Remote::owned($this)) {
            return Rpc::call('composer.getConfig', [$this]);
        }

        return $this->config;
    }

    public function setLoop(\Composer\Util\Loop $loop): void
    {
        if (Remote::owned($this)) {
            Rpc::call('composer.setLoop', [$this, $loop]);

            return;
        }
        $this->loop = $loop;
    }

    public function getLoop(): \Composer\Util\Loop
    {
        if (Remote::owned($this)) {
            return Rpc::call('composer.getLoop', [$this]);
        }

        return $this->loop;
    }

    public function setRepositoryManager(\Composer\Repository\RepositoryManager $manager): void
    {
        if (Remote::owned($this)) {
            Rpc::call('composer.setRepositoryManager', [$this, $manager]);

            return;
        }
        $this->repositoryManager = $manager;
    }

    public function getRepositoryManager(): \Composer\Repository\RepositoryManager
    {
        if (Remote::owned($this)) {
            return Rpc::call('composer.getRepositoryManager', [$this]);
        }

        return $this->repositoryManager;
    }

    public function setInstallationManager(\Composer\Installer\InstallationManager $manager): void
    {
        if (Remote::owned($this)) {
            Rpc::call('composer.setInstallationManager', [$this, $manager]);

            return;
        }
        $this->installationManager = $manager;
    }

    public function getInstallationManager(): \Composer\Installer\InstallationManager
    {
        if (Remote::owned($this)) {
            return Rpc::call('composer.getInstallationManager', [$this]);
        }

        return $this->installationManager;
    }

    public function setEventDispatcher(\Composer\EventDispatcher\EventDispatcher $eventDispatcher): void
    {
        if (Remote::owned($this)) {
            Rpc::call('composer.setEventDispatcher', [$this, $eventDispatcher]);

            return;
        }
        $this->eventDispatcher = $eventDispatcher;
    }

    public function getEventDispatcher(): \Composer\EventDispatcher\EventDispatcher
    {
        if (Remote::owned($this)) {
            return Rpc::call('composer.getEventDispatcher', [$this]);
        }

        return $this->eventDispatcher;
    }

    public function isGlobal(): bool
    {
        if (Remote::owned($this)) {
            return Rpc::call('composer.isGlobal', [$this]);
        }

        return $this->global;
    }

    public function setGlobal(): void
    {
        if (Remote::owned($this)) {
            Rpc::call('composer.setGlobal', [$this]);

            return;
        }
        $this->global = true;
    }
}
