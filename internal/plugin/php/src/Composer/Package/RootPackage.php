<?php

/*
 * maestro's plugin shim: Composer\Package\RootPackage, reimplemented with
 * Composer 2.10.3's behaviour (docs/PLUGINS.md §4.5). See BasePackage for
 * how a Go-owned package mirrors maestro's.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Package;

use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class RootPackage extends CompletePackage implements RootPackageInterface
{
    public const DEFAULT_PRETTY_VERSION = '1.0.0+no-version-set';

    protected $minimumStability = 'stable';
    protected $preferStable = false;
    protected $stabilityFlags = [];
    protected $config = [];
    protected $references = [];
    protected $aliases = [];

    public function setMinimumStability(string $minimumStability): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setMinimumStability', [$this, $minimumStability]);

            return;
        }
        $this->minimumStability = $minimumStability;
    }

    public function setStabilityFlags(array $stabilityFlags): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setStabilityFlags', [$this, $stabilityFlags]);

            return;
        }
        $this->stabilityFlags = $stabilityFlags;
    }

    public function setPreferStable(bool $preferStable): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setPreferStable', [$this, $preferStable]);

            return;
        }
        $this->preferStable = $preferStable;
    }

    public function setConfig(array $config): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setConfig', [$this, $config]);

            return;
        }
        $this->config = $config;
    }

    public function setReferences(array $references): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setReferences', [$this, $references]);

            return;
        }
        $this->references = $references;
    }

    public function setAliases(array $aliases): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setAliases', [$this, $aliases]);

            return;
        }
        $this->aliases = $aliases;
    }

    public function getMinimumStability(): string
    {
        return $this->minimumStability;
    }

    public function getStabilityFlags(): array
    {
        return $this->stabilityFlags;
    }

    public function getPreferStable(): bool
    {
        return $this->preferStable;
    }

    public function getConfig(): array
    {
        return $this->config;
    }

    public function getReferences(): array
    {
        return $this->references;
    }

    public function getAliases(): array
    {
        return $this->aliases;
    }
}
