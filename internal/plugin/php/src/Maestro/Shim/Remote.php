<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * Helpers of the shim classes whose instances may be Go-owned (service
 * proxies and data mirrors, docs/PLUGINS.md §5.3).
 */
final class Remote
{
    /**
     * Whether $object stands for a Go-owned object: its methods call
     * maestro.
     *
     * @param object $object
     */
    public static function owned($object): bool
    {
        $h = Handles::lookup($object);

        return $h !== null && $h > 0;
    }

    /**
     * The object a method of a maestro service runs on: $object itself,
     * or, for a PHP clone of maestro's object (whose private maestroOrigin
     * the clone copied), $object once maestro made it a copy of the
     * original's Go object (`object.clone`), as PHP's clone copies
     * Composer's object.
     *
     * @template T of object
     * @param T $object
     * @return T
     */
    public static function self($object)
    {
        if (Handles::lookup($object) !== null) {
            return $object;
        }
        $class = get_class($object);
        for ($c = $class; is_string($c); $c = get_parent_class($c)) {
            if (property_exists($c, 'maestroOrigin')) {
                $origin = self::read($object, $c, ['maestroOrigin'])['maestroOrigin'];
                if (is_int($origin) && Handles::has($origin)) {
                    Rpc::call('object.clone', [$object, Handles::get($origin)]);
                }
                break;
            }
        }

        return $object;
    }

    /**
     * Throws the UnsupportedApiException of a method the shim does not
     * implement (yet) for this kind of object.
     *
     * @return never
     */
    public static function unsupported(string $class, string $method): void
    {
        throw new UnsupportedApiException('maestro does not support '.$class.'::'.$method.'() in plugins yet');
    }

    /**
     * Writes Composer's protected properties into an object from outside
     * its class.
     *
     * @param object $object
     * @param array<string, mixed> $properties
     */
    public static function fill($object, string $scope, array $properties): void
    {
        $write = function (array $properties): void {
            foreach ($properties as $name => $value) {
                $this->$name = $value;
            }
        };
        \Closure::bind($write, $object, $scope)($properties);
    }

    /**
     * Reads properties of an object from outside its class.
     *
     * @param object $object
     * @param list<string> $names
     * @return array<string, mixed>
     */
    public static function read($object, string $scope, array $names): array
    {
        $read = function (array $names): array {
            $out = [];
            foreach ($names as $name) {
                $out[$name] = $this->$name;
            }

            return $out;
        };

        return \Closure::bind($read, $object, $scope)($names);
    }
}
