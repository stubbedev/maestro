<?php

/*
 * maestro's plugin shim: Composer\Package\Dumper\ArrayDumper
 * (docs/PLUGINS.md §4.5): maestro's dumper (dumper.dump).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Package\Dumper;

class ArrayDumper
{
    public function dump(\Composer\Package\PackageInterface $package): array
    {
        return \Maestro\Shim\Rpc::call('dumper.dump', [$package]);
    }
}
