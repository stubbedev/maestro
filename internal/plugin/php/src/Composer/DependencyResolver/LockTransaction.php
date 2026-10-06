<?php

/*
 * maestro's plugin shim: Composer\DependencyResolver\LockTransaction
 * (docs/PLUGINS.md §4.7, §5.12): the solver's result, maestro's. It crosses
 * with Composer's protected properties filled, as Transaction does, its
 * own included ($presentMap by spl_object_id, $unlockableMap by package
 * id, $resultPackages' all, non-dev and dev lists), kept current when
 * maestro changes them. Creating one, or setting its result packages,
 * takes the solver's Pool and Decisions, which are maestro's internals and
 * never cross (Pool objects in PHP are copies): those stay unsupported.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\DependencyResolver;

use Composer\Package\AliasPackage;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class LockTransaction extends \Composer\DependencyResolver\Transaction
{
    protected $presentMap;
    protected $resultPackages;
    protected $unlockableMap;

    public function __construct(\Composer\DependencyResolver\Pool $pool, array $presentMap, array $unlockableMap, \Composer\DependencyResolver\Decisions $decisions)
    {
        Remote::unsupported(self::class, '__construct');
    }

    public function getAliases(array $aliases): array
    {
        $usedAliases = [];

        foreach ($this->resultPackages['all'] as $package) {
            if ($package instanceof AliasPackage) {
                foreach ($aliases as $index => $alias) {
                    if ($alias['package'] === $package->getName()) {
                        $usedAliases[] = $alias;
                        unset($aliases[$index]);
                    }
                }
            }
        }

        usort($usedAliases, static function ($a, $b): int {
            return strcmp($a['package'], $b['package']);
        });

        return $usedAliases;
    }

    public function getNewLockPackages(bool $devMode, bool $updateMirrors = false): array
    {
        return Rpc::call('transaction.getNewLockPackages', [$this, $devMode, $updateMirrors]);
    }

    public function setNonDevPackages(\Composer\DependencyResolver\LockTransaction $extractionResult): void
    {
        Rpc::call('transaction.setNonDevPackages', [$this, $extractionResult]);
    }

    public function setResultPackages(\Composer\DependencyResolver\Pool $pool, \Composer\DependencyResolver\Decisions $decisions): void
    {
        Remote::unsupported(self::class, 'setResultPackages');
    }
}
