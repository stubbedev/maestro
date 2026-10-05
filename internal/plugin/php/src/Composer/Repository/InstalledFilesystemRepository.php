<?php

/*
 * maestro's plugin shim: Composer\Repository\InstalledFilesystemRepository
 * (docs/PLUGINS.md §4.6), the local repository: a proxy of maestro's (see
 * ArrayRepository).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Repository;

use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class InstalledFilesystemRepository extends FilesystemRepository implements InstalledRepositoryInterface
{
    public function getRepoName()
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.getRepoName', [$this]);
        }

        return 'installed '.parent::getRepoName();
    }

    public function isFresh()
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.isFresh', [$this]);
        }
        Remote::unsupported(static::class, 'isFresh');
    }
}
