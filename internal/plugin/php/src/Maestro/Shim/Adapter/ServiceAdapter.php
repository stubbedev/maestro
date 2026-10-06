<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim\Adapter;

use Composer\Filter\PlatformRequirementFilter\IgnoreAllPlatformRequirementFilter;
use Composer\Filter\PlatformRequirementFilter\IgnoreListPlatformRequirementFilter;
use Composer\Filter\PlatformRequirementFilter\IgnoreNothingPlatformRequirementFilter;
use Composer\Filter\PlatformRequirementFilter\PlatformRequirementFilterFactory;
use Composer\Filter\PlatformRequirementFilter\PlatformRequirementFilterInterface;
use Maestro\Shim\MirrorAdapter;
use Maestro\Shim\Remote;

/**
 * maestro's services whose PHP objects hold Composer's protected or
 * private properties (docs/PLUGINS.md §5.12's parity list: the running
 * Composer\Installer's settings, EventDispatcher::$runScripts,
 * Config::$baseDir): an instance of the service's class built without its
 * constructor, as any service, with the properties written in the scope
 * of the class declaring them, so array casts, Closure::bind and
 * reflection read them as in Composer. A platform requirement filter
 * travels as its description ("\0filter": true, false or the ignore list).
 */
final class ServiceAdapter implements MirrorAdapter
{
    const BASE = 'Maestro\Shim\Service';

    /** @var array<string, string> "class::property" => declaring class */
    private static $scopes = [];

    public function base(): string
    {
        return self::BASE;
    }

    public function create(int $handle, string $class, array $snapshot)
    {
        $object = (new \ReflectionClass($class))->newInstanceWithoutConstructor();
        $this->apply($object, $snapshot);
        // A clone remembers which of maestro's objects it copies
        // (Remote::self).
        for ($c = $class; is_string($c); $c = get_parent_class($c)) {
            if (property_exists($c, 'maestroOrigin')) {
                Remote::fill($object, $c, ['maestroOrigin' => $handle]);
                break;
            }
        }

        return $object;
    }

    public function snapshot($object): array
    {
        return [];
    }

    public function fields($object, array $names): array
    {
        $out = [];
        foreach ($names as $name) {
            $value = Remote::read($object, self::scope($object, $name), [$name])[$name];
            if ($value instanceof PlatformRequirementFilterInterface) {
                $value = ["\0filter" => self::describeFilter($value)];
            }
            $out[$name] = $value;
        }

        return $out;
    }

    public function apply($object, array $fields): void
    {
        $byScope = [];
        foreach ($fields as $name => $value) {
            if (is_array($value) && array_key_exists("\0filter", $value)) {
                $value = PlatformRequirementFilterFactory::fromBoolOrList($value["\0filter"]);
            } elseif (is_array($value) && array_key_exists("\0idmap", $value)) {
                // objects keyed by spl_object_id, as Composer keys them
                $map = [];
                foreach ($value["\0idmap"] as $item) {
                    $map[spl_object_id($item)] = $item;
                }
                $value = $map;
            }
            $byScope[self::scope($object, $name)][$name] = $value;
        }
        foreach ($byScope as $scope => $properties) {
            Remote::fill($object, $scope, $properties);
        }
    }

    /**
     * The class declaring a property of an object's class (a private
     * property must be written in its own class's scope).
     *
     * @param object $object
     */
    private static function scope($object, string $name): string
    {
        $class = get_class($object);
        $key = $class.'::'.$name;
        if (!isset(self::$scopes[$key])) {
            $scope = $class;
            for ($c = $class; is_string($c); $c = get_parent_class($c)) {
                if (property_exists($c, $name)) {
                    $scope = (new \ReflectionProperty($c, $name))->getDeclaringClass()->getName();
                    break;
                }
            }
            self::$scopes[$key] = $scope;
        }

        return self::$scopes[$key];
    }

    /**
     * A platform requirement filter as maestro builds it again: true
     * (ignore all), false (ignore nothing), the list of an ignore list, or
     * the object itself.
     *
     * @return bool|list<string>|PlatformRequirementFilterInterface
     */
    public static function describeFilter(PlatformRequirementFilterInterface $filter)
    {
        if ($filter instanceof IgnoreAllPlatformRequirementFilter) {
            return true;
        }
        if ($filter instanceof IgnoreNothingPlatformRequirementFilter) {
            return false;
        }
        if ($filter instanceof IgnoreListPlatformRequirementFilter) {
            return Remote::read($filter, IgnoreListPlatformRequirementFilter::class, ['reqList'])['reqList'];
        }

        return $filter;
    }
}
