<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim\Adapter;

use Maestro\Shim\Definitions;
use Maestro\Shim\MirrorAdapter;
use Maestro\Shim\Remote;
use Symfony\Component\Console\Command\Command;

/**
 * maestro's commands as PHP sees them (docs/PLUGINS.md §4.11): an instance
 * of the command's class (a Composer command, a Symfony one) with the
 * properties of Symfony's Command, so find(), all(), getName(),
 * getDefinition() and instanceof answer as in Composer.
 */
final class CommandAdapter implements MirrorAdapter
{
    const BASE = 'Maestro\Shim\Mirror\Command';

    public function base(): string
    {
        return self::BASE;
    }

    public function create(int $handle, string $class, array $snapshot)
    {
        if (!class_exists($class) || !is_a($class, Command::class, true) || (new \ReflectionClass($class))->isAbstract()) {
            $class = Command::class;
        }
        $command = (new \ReflectionClass($class))->newInstanceWithoutConstructor();
        Remote::fill($command, Command::class, [
            'name' => $snapshot['name'],
            'aliases' => $snapshot['aliases'],
            'description' => $snapshot['description'],
            'help' => $snapshot['help'],
            'hidden' => $snapshot['hidden'],
            'usages' => $snapshot['usages'],
            'definition' => Definitions::build($snapshot['definition']),
        ]);

        return $command;
    }

    public function snapshot($object): array
    {
        return [];
    }

    public function fields($object, array $names): array
    {
        return [];
    }

    public function apply($object, array $fields): void
    {
    }
}
