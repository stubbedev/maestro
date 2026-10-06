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

        $snapshot = Remote::read($object, SolverOperation::class, $names);
        // The Composer class a plugin's subclass extends (vaimo's
        // ResetOperation extends InstallOperation).
        for ($c = get_class($object); is_string($c); $c = get_parent_class($c)) {
            if (strpos($c, 'Composer\\DependencyResolver\\Operation\\') === 0) {
                $snapshot['composerClass'] = $c;
                break;
            }
        }

        return $snapshot;
    }

    public function fields($object, array $names): array
    {
        $snapshot = $this->snapshot($object);
        unset($snapshot['composerClass']);

        return array_intersect_key($snapshot, array_flip($names));
    }

    public function apply($object, array $fields): void
    {
        Remote::fill($object, SolverOperation::class, $fields);
    }
}
