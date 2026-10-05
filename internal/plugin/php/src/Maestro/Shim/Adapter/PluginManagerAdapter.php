<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim\Adapter;

use Composer\Plugin\PluginManager;
use Maestro\Shim\MirrorAdapter;
use Maestro\Shim\Remote;

/**
 * maestro's plugin managers as PHP mirrors them: the composer, io,
 * globalComposer, disablePlugins and runningInGlobalDir properties; the
 * plugin instances stay PHP's.
 */
final class PluginManagerAdapter implements MirrorAdapter
{
    public function base(): string
    {
        return PluginManager::class;
    }

    public function create(int $handle, string $class, array $snapshot)
    {
        $pm = (new \ReflectionClass(PluginManager::class))->newInstanceWithoutConstructor();
        $this->apply($pm, $snapshot);

        return $pm;
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
        Remote::fill($object, PluginManager::class, $fields);
    }
}
