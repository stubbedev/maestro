<?php

/*
 * maestro's plugin shim: Composer\Repository\WritableArrayRepository,
 * reimplemented with Composer 2.10.3's behaviour (docs/PLUGINS.md §4.6).
 * See ArrayRepository.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Repository;

use Composer\Installer\InstallationManager;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class WritableArrayRepository extends ArrayRepository implements WritableRepositoryInterface
{
    use CanonicalPackagesTrait;

    protected $devPackageNames = [];

    private $devMode = null;

    public function getDevMode()
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.getDevMode', [$this]);
        }

        return $this->devMode;
    }

    public function setDevPackageNames(array $devPackageNames)
    {
        if (Remote::owned($this)) {
            Rpc::call('repo.setDevPackageNames', [$this, $devPackageNames]);

            return;
        }
        $this->devPackageNames = $devPackageNames;
    }

    public function getDevPackageNames()
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.getDevPackageNames', [$this]);
        }

        return $this->devPackageNames;
    }

    public function write(bool $devMode, InstallationManager $installationManager)
    {
        if (Remote::owned($this)) {
            Rpc::call('repo.write', [$this, $devMode, $installationManager]);

            return;
        }
        $this->devMode = $devMode;
    }

    public function reload()
    {
        if (Remote::owned($this)) {
            Rpc::call('repo.reload', [$this]);

            return;
        }
        $this->devMode = null;
    }
}
