<?php

/*
 * maestro's plugin shim: Composer\Repository\LockArrayRepository,
 * reimplemented with Composer 2.10.3's behaviour (docs/PLUGINS.md §4.6).
 * See ArrayRepository.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Repository;

use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class LockArrayRepository extends ArrayRepository
{
    use CanonicalPackagesTrait;

    public function getRepoName(): string
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.getRepoName', [$this]);
        }

        return 'lock repo';
    }
}
