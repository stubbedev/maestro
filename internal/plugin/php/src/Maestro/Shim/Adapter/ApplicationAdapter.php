<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim\Adapter;

use Composer\Console\Application;
use Maestro\Shim\MirrorAdapter;

/**
 * maestro's running Application as PHP sees it (docs/PLUGINS.md §5.11): a
 * Composer\Console\Application whose Symfony part is constructed as
 * Composer's was (name, version) and whose Composer methods are maestro's.
 */
final class ApplicationAdapter implements MirrorAdapter
{
    const BASE = 'Maestro\Shim\Mirror\Application';

    public function base(): string
    {
        return self::BASE;
    }

    public function create(int $handle, string $class, array $snapshot)
    {
        $app = (new \ReflectionClass(Application::class))->newInstanceWithoutConstructor();
        $construct = function (string $name, string $version): void {
            \Symfony\Component\Console\Application::__construct($name, $version);
        };
        \Closure::bind($construct, $app, \Symfony\Component\Console\Application::class)((string) $snapshot['name'], (string) $snapshot['version']);

        return $app;
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
