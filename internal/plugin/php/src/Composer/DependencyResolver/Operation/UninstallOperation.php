<?php

/*
 * maestro's plugin shim: Composer\DependencyResolver\Operation\UninstallOperation,
 * reimplemented with Composer 2.10.3's behaviour (docs/PLUGINS.md §4.7).
 * maestro's operations are data mirrors (Maestro\Shim\Adapter\
 * OperationAdapter); one created in PHP is plain PHP.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\DependencyResolver\Operation;

class UninstallOperation extends \Composer\DependencyResolver\Operation\SolverOperation
{
    protected const TYPE = 'uninstall';

    protected $package;

    public function __construct(\Composer\Package\PackageInterface $package)
    {
        $this->package = $package;
    }

    public static function format(\Composer\Package\PackageInterface $package, bool $lock = false): string
    {
        return 'Removing <info>'.$package->getPrettyName().'</info> (<comment>'.$package->getFullPrettyVersion().'</comment>)';
    }

    public function getPackage(): \Composer\Package\PackageInterface
    {
        return $this->package;
    }

    public function show($lock): string
    {
        return self::format($this->package, $lock);
    }
}
