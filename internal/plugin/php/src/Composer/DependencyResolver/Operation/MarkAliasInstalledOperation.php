<?php

/*
 * maestro's plugin shim: Composer\DependencyResolver\Operation\MarkAliasInstalledOperation,
 * reimplemented with Composer 2.10.3's behaviour (docs/PLUGINS.md §4.7).
 * maestro's operations are data mirrors (Maestro\Shim\Adapter\
 * OperationAdapter); one created in PHP is plain PHP.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\DependencyResolver\Operation;

class MarkAliasInstalledOperation extends \Composer\DependencyResolver\Operation\SolverOperation
{
    protected const TYPE = 'markAliasInstalled';

    protected $package;

    public function __construct(\Composer\Package\AliasPackage $package)
    {
        $this->package = $package;
    }

    public function getPackage(): \Composer\Package\AliasPackage
    {
        return $this->package;
    }

    public function show($lock): string
    {
        return 'Marking <info>'.$this->package->getPrettyName().'</info> (<comment>'.$this->package->getFullPrettyVersion().'</comment>) as installed, alias of <info>'.$this->package->getAliasOf()->getPrettyName().'</info> (<comment>'.$this->package->getAliasOf()->getFullPrettyVersion().'</comment>)';
    }
}
