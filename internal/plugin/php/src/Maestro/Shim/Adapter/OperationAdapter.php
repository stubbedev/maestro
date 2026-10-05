<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim\Adapter;

use Composer\DependencyResolver\Operation\SolverOperation;
use Composer\DependencyResolver\Operation\UpdateOperation;
use Maestro\Shim\MirrorAdapter;
use Maestro\Shim\Remote;

/**
 * Operations as PHP mirrors them (docs/PLUGINS.md §4.7): their packages,
 * by Composer's property names (package, or initialPackage and
 * targetPackage).
 */
final class OperationAdapter implements MirrorAdapter
{
    public function base(): string
    {
        return SolverOperation::class;
    }

    public function create(int $handle, string $class, array $snapshot)
    {
        if (!class_exists($class) || !is_a($class, SolverOperation::class, true)) {
            throw new \Maestro\Shim\ProtocolException('maestro shim: '.$class.' is not an operation');
        }
        $operation = (new \ReflectionClass($class))->newInstanceWithoutConstructor();
        $this->apply($operation, $snapshot);

        return $operation;
    }

    public function snapshot($object): array
    {
        $names = $object instanceof UpdateOperation ? ['initialPackage', 'targetPackage'] : ['package'];

        return Remote::read($object, SolverOperation::class, $names);
    }

    public function fields($object, array $names): array
    {
        return array_intersect_key($this->snapshot($object), array_flip($names));
    }

    public function apply($object, array $fields): void
    {
        Remote::fill($object, SolverOperation::class, $fields);
    }
}
