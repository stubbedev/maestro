<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * Reads and writes the fields of one family of data mirrors (packages,
 * events, operations, ...; docs/PLUGINS.md §5.3) from outside the class,
 * so the mirror classes keep exactly Composer's members. Field names are
 * the PHP property names.
 */
interface MirrorAdapter
{
    /**
     * The shim base class of the family ("Composer\Package\Package"): what
     * maestro builds a PHP-born object of a subclass as.
     */
    public function base(): string;

    /**
     * Builds the PHP object of a Go-owned mirror of class $class from its
     * snapshot. $class may be a subclass of base() (a user subclass seen
     * from Go as its base).
     *
     * @param array<string, mixed> $snapshot
     * @return object
     */
    public function create(int $handle, string $class, array $snapshot);

    /**
     * Every field of the object.
     *
     * @param object $object
     * @return array<string, mixed>
     */
    public function snapshot($object): array;

    /**
     * The named fields of the object.
     *
     * @param object $object
     * @param list<string> $names
     * @return array<string, mixed>
     */
    public function fields($object, array $names): array;

    /**
     * Writes fields maestro changed into the object, in place.
     *
     * @param object $object
     * @param array<string, mixed> $fields
     */
    public function apply($object, array $fields): void;
}
