<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim\Adapter;

use Composer\EventDispatcher\Event;
use Composer\Installer\InstallerEvent;
use Composer\Installer\PackageEvent;
use Composer\Plugin\CommandEvent;
use Composer\Plugin\PreCommandRunEvent;
use Composer\Script\Event as ScriptEvent;
use Maestro\Shim\MirrorAdapter;
use Maestro\Shim\Remote;

/**
 * Events as PHP mirrors them (docs/PLUGINS.md §5.5 "Event objects"): the
 * fields of Event and of the class that declares the rest, by Composer's
 * property names.
 */
final class EventAdapter implements MirrorAdapter
{
    /** @var array<class-string, list<string>> the fields each class declares */
    const GROUPS = [
        Event::class => ['name', 'args', 'flags', 'propagationStopped'],
        ScriptEvent::class => ['composer', 'io', 'devMode', 'originatingEvent'],
        PackageEvent::class => ['composer', 'io', 'devMode', 'localRepo', 'operations', 'operation'],
        InstallerEvent::class => ['composer', 'io', 'devMode', 'executeOperations', 'transaction'],
        CommandEvent::class => ['commandName', 'input', 'output'],
        PreCommandRunEvent::class => ['input', 'command'],
    ];

    public function base(): string
    {
        return Event::class;
    }

    public function create(int $handle, string $class, array $snapshot)
    {
        if (!class_exists($class) || !is_a($class, Event::class, true)) {
            $class = Event::class;
        }
        $event = (new \ReflectionClass($class))->newInstanceWithoutConstructor();
        $this->apply($event, $snapshot);

        return $event;
    }

    public function snapshot($object): array
    {
        $out = [];
        foreach (self::GROUPS as $class => $names) {
            if ($object instanceof $class) {
                $out += Remote::read($object, $class, $names);
            }
        }
        $out['class'] = self::groupOf($object);

        return $out;
    }

    public function fields($object, array $names): array
    {
        return array_intersect_key($this->snapshot($object), array_flip($names));
    }

    public function apply($object, array $fields): void
    {
        foreach (self::GROUPS as $class => $names) {
            if ($object instanceof $class) {
                $values = array_intersect_key($fields, array_flip($names));
                if ($values !== []) {
                    Remote::fill($object, $class, $values);
                }
            }
        }
    }

    /**
     * The nearest Composer class of an event: what maestro treats a
     * plugin's subclass as.
     *
     * @param object $object
     */
    private static function groupOf($object): string
    {
        $group = Event::class;
        foreach (self::GROUPS as $class => $_) {
            if ($object instanceof $class) {
                $group = $class;
            }
        }

        return $group;
    }
}
