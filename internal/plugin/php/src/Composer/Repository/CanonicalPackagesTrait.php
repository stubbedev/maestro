<?php

/*
 * maestro's plugin shim: Composer\Repository\CanonicalPackagesTrait,
 * reimplemented with Composer 2.10.3's behaviour (docs/PLUGINS.md §4.6).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Repository;

use Composer\Package\AliasPackage;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

trait CanonicalPackagesTrait
{
    public function getCanonicalPackages()
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.getCanonicalPackages', [$this]);
        }

        $packages = $this->getPackages();

        // get at most one package of each name, preferring non-aliased ones
        $packagesByName = [];
        foreach ($packages as $package) {
            if (!isset($packagesByName[$package->getName()]) || $packagesByName[$package->getName()] instanceof AliasPackage) {
                $packagesByName[$package->getName()] = $package;
            }
        }

        $canonicalPackages = [];

        // unfold aliased packages
        foreach ($packagesByName as $package) {
            while ($package instanceof AliasPackage) {
                $package = $package->getAliasOf();
            }

            $canonicalPackages[] = $package;
        }

        return $canonicalPackages;
    }
}
