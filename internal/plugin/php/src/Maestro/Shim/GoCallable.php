<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * A function of maestro's as a PHP callable (a validator maestro passes to
 * an IO created in PHP): calling it calls maestro (`callable.go`), whose
 * errors are thrown here.
 */
final class GoCallable
{
    /**
     * @param mixed ...$args
     * @return mixed
     */
    public function __invoke(...$args)
    {
        return Rpc::call('callable.go', [$this, $args]);
    }
}
