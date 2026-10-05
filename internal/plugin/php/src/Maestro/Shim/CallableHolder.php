<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * Holds an array callable maestro keeps by handle (see Listeners).
 */
final class CallableHolder
{
    /** @var mixed */
    public $callable;

    /**
     * @param mixed $callable
     */
    public function __construct($callable)
    {
        $this->callable = $callable;
    }
}
