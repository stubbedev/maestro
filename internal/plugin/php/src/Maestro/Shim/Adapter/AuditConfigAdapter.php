<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim\Adapter;

use Composer\Advisory\AuditConfig;
use Maestro\Shim\PolledMirrorAdapter;

/**
 * AuditConfig as PHP mirrors it: its two public properties, which code
 * writes directly (no setters report the change, so they are polled).
 */
final class AuditConfigAdapter implements PolledMirrorAdapter
{
    /** @var array<int, array<string, mixed>> the state maestro has, by spl_object_id */
    private $known = [];

    public function base(): string
    {
        return AuditConfig::class;
    }

    public function create(int $handle, string $class, array $snapshot)
    {
        if (!class_exists($class) || !is_a($class, AuditConfig::class, true)) {
            $class = AuditConfig::class;
        }
        $config = (new \ReflectionClass($class))->newInstanceWithoutConstructor();
        $this->apply($config, $snapshot);

        return $config;
    }

    public function snapshot($object): array
    {
        $state = self::state($object);
        $this->known[spl_object_id($object)] = $state;

        return $state;
    }

    public function fields($object, array $names): array
    {
        return array_intersect_key(self::state($object), array_flip($names));
    }

    public function apply($object, array $fields): void
    {
        foreach (['audit', 'auditFormat'] as $name) {
            if (array_key_exists($name, $fields)) {
                $object->$name = $fields[$name];
            }
        }
        $this->known[spl_object_id($object)] = self::state($object);
    }

    public function poll($object): ?array
    {
        $id = spl_object_id($object);
        $state = self::state($object);
        $known = isset($this->known[$id]) ? $this->known[$id] : null;
        $this->known[$id] = $state;
        if ($known === null) {
            return null;
        }
        $changed = [];
        foreach ($state as $name => $value) {
            if ($value !== $known[$name]) {
                $changed[$name] = $value;
            }
        }

        return $changed === [] ? null : $changed;
    }

    /**
     * @return array{audit: mixed, auditFormat: mixed}
     */
    private static function state(AuditConfig $config): array
    {
        return ['audit' => $config->audit, 'auditFormat' => $config->auditFormat];
    }
}
