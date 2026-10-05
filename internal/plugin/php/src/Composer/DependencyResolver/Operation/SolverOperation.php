<?php

/*
 * maestro's plugin shim: Composer\DependencyResolver\Operation\SolverOperation,
 * reimplemented with Composer 2.10.3's behaviour (docs/PLUGINS.md §4.7).
 * maestro's operations are data mirrors (Maestro\Shim\Adapter\
 * OperationAdapter); one created in PHP is plain PHP.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\DependencyResolver\Operation;

abstract class SolverOperation implements \Composer\DependencyResolver\Operation\OperationInterface
{
    protected const TYPE = '';

    public function __toString(): string
    {
        return $this->show(false);
    }

    public function getOperationType(): string
    {
        return static::TYPE;
    }
}
