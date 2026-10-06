<?php

/*
 * maestro's plugin shim: Composer\DependencyResolver\Transaction
 * (docs/PLUGINS.md §4.7, §5.12). maestro's transactions (the
 * PRE_OPERATIONS_EXEC event's) cross with Composer's protected properties
 * filled ($operations, $presentPackages, $resultPackageMap, keyed by
 * spl_object_id as Composer keys it, $resultPackagesByName with the keys
 * uasort() left), so symfony/flex's Closure::bind
 * into the class reads them; `new Transaction($present, $result)` computes
 * the operations with maestro's algorithm (transaction.new) and keeps
 * Composer's properties.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\DependencyResolver;

use Composer\Package\AliasPackage;
use Composer\Package\PackageInterface;
use Maestro\Shim\Rpc;

class Transaction
{
    protected $operations;
    protected $presentPackages;
    protected $resultPackageMap;
    protected $resultPackagesByName = [];

    public function __construct(array $presentPackages, array $resultPackages)
    {
        $this->presentPackages = $presentPackages;
        $this->setResultPackageMaps($resultPackages);
        $this->operations = Rpc::call('transaction.new', [$this, array_values($presentPackages), array_values($resultPackages)]);
    }

    public function getOperations(): array
    {
        if ($this->operations === null) {
            $this->operations = Rpc::call('transaction.getOperations', [$this]);
        }

        return $this->operations;
    }

    /**
     * @param PackageInterface[] $resultPackages
     */
    private function setResultPackageMaps(array $resultPackages): void
    {
        $packageSort = static function (PackageInterface $a, PackageInterface $b): int {
            // sort alias packages by the same name behind their non alias version
            if ($a->getName() === $b->getName()) {
                if ($a instanceof AliasPackage !== $b instanceof AliasPackage) {
                    return $a instanceof AliasPackage ? -1 : 1;
                }

                // if names are the same, compare version, e.g. to sort aliases reliably, actual order does not matter
                return strcmp($b->getVersion(), $a->getVersion());
            }

            return strcmp($b->getName(), $a->getName());
        };

        $this->resultPackageMap = [];
        foreach ($resultPackages as $package) {
            $this->resultPackageMap[spl_object_id($package)] = $package;
            foreach ($package->getNames() as $name) {
                $this->resultPackagesByName[$name][] = $package;
            }
        }

        uasort($this->resultPackageMap, $packageSort);
        foreach ($this->resultPackagesByName as $name => $packages) {
            uasort($this->resultPackagesByName[$name], $packageSort);
        }
    }

    protected function calculateOperations(): array
    {
        // maestro's algorithm on maestro's transaction (every Transaction
        // is one, new Transaction() included): new operation objects, as
        // Composer's.
        return Rpc::call('transaction.calculateOperations', [$this]);
    }

    protected function getProvidersInResult(\Composer\Package\Link $link): array
    {
        if (!isset($this->resultPackagesByName[$link->getTarget()])) {
            return [];
        }

        return $this->resultPackagesByName[$link->getTarget()];
    }

    protected function getRootPackages(): array
    {
        $roots = $this->resultPackageMap;

        foreach ($this->resultPackageMap as $packageHash => $package) {
            if (!isset($roots[$packageHash])) {
                continue;
            }

            foreach ($package->getRequires() as $link) {
                $possibleRequires = $this->getProvidersInResult($link);

                foreach ($possibleRequires as $require) {
                    if ($require !== $package) {
                        unset($roots[spl_object_id($require)]);
                    }
                }
            }
        }

        return $roots;
    }
}
