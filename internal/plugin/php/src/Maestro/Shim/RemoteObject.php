<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * A Go-owned object of a class the shim has no proxy or mirror for. It
 * only keeps the object's identity: passed back to maestro, it is the
 * same Go object.
 */
final class RemoteObject
{
    /** @var int */
    private $handle;

    /** @var string */
    private $class;

    public function __construct(int $handle, string $class)
    {
        $this->handle = $handle;
        $this->class = $class;
    }

    public function getMaestroHandle(): int
    {
        return $this->handle;
    }

    /**
     * The PHP class maestro gave the object.
     */
    public function getMaestroClass(): string
    {
        return $this->class;
    }
}
