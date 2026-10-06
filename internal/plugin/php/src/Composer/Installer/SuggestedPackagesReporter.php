<?php

/*
 * maestro's plugin shim: Composer\Installer\SuggestedPackagesReporter
 * (docs/PLUGINS.md §4.7): a proxy of maestro's reporter (suggested.*); one
 * created in PHP is maestro's too, so it can be given to an Installer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Installer;

use Composer\IO\IOInterface;
use Composer\Package\PackageInterface;
use Composer\Repository\InstalledRepository;
use Maestro\Shim\Rpc;

class SuggestedPackagesReporter
{
    public const MODE_LIST = 1;
    public const MODE_BY_PACKAGE = 2;
    public const MODE_BY_SUGGESTION = 4;

    protected $suggestedPackages = [];

    public function __construct(IOInterface $io)
    {
        Rpc::call('suggested.new', [$this, $io]);
    }

    public function getPackages(): array
    {
        return Rpc::call('suggested.getPackages', [$this]);
    }

    public function addPackage(string $source, string $target, string $reason): SuggestedPackagesReporter
    {
        Rpc::call('suggested.addPackage', [$this, $source, $target, $reason]);

        return $this;
    }

    public function addSuggestionsFromPackage(PackageInterface $package): SuggestedPackagesReporter
    {
        Rpc::call('suggested.addSuggestionsFromPackage', [$this, $package]);

        return $this;
    }

    public function output(int $mode, ?InstalledRepository $installedRepo = null, ?PackageInterface $onlyDependentsOf = null): void
    {
        Rpc::call('suggested.output', [$this, $mode, $installedRepo, $onlyDependentsOf]);
    }

    public function outputMinimalistic(?InstalledRepository $installedRepo = null, ?PackageInterface $onlyDependentsOf = null): void
    {
        Rpc::call('suggested.outputMinimalistic', [$this, $installedRepo, $onlyDependentsOf]);
    }
}
