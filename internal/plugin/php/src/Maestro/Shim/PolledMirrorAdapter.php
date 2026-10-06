<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * The adapter of a mirror family whose objects have no setters that could
 * report their changes (vendored Symfony inputs and outputs): before every
 * message to maestro, Mirrors asks it what changed since the state maestro
 * last had.
 */
interface PolledMirrorAdapter extends MirrorAdapter
{
    /**
     * The fields of the object that changed since maestro last had them
     * (now remembered as maestro's), or null.
     *
     * @param object $object
     * @return array<string, mixed>|null
     */
    public function poll($object): ?array;
}
