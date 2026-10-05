<?php

/*
 * maestro's plugin shim: Composer\Repository\InstalledArrayRepository,
 * reimplemented with Composer 2.10.3's behaviour (docs/PLUGINS.md §4.6).
 * See ArrayRepository.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Repository;

use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class InstalledArrayRepository extends WritableArrayRepository implements InstalledRepositoryInterface
{
    public function getRepoName(): string
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.getRepoName', [$this]);
        }

        return 'installed '.parent::getRepoName();
    }

    public function isFresh(): bool
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.isFresh', [$this]);
        }

        // this is not a completely correct implementation but there is no way to
        // distinguish an empty repo and a newly created one given this is all in-memory
        return $this->count() === 0;
    }
}
