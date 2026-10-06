<?php

/*
 * maestro's plugin shim: Composer\Repository\FilesystemRepository
 * (docs/PLUGINS.md §4.6). maestro's local repositories are proxies of
 * maestro's (see ArrayRepository); one created in PHP is maestro's too,
 * reading and writing its file as Composer's does.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Repository;

use Composer\Installer\InstallationManager;
use Composer\Json\JsonFile;
use Composer\Package\RootPackageInterface;
use Composer\Util\Filesystem;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class FilesystemRepository extends WritableArrayRepository
{
    protected $file;

    public function __construct(JsonFile $repositoryFile, bool $dumpVersions = false, ?RootPackageInterface $rootPackage = null, ?Filesystem $filesystem = null)
    {
        $this->file = $repositoryFile;
        $io = Remote::read($repositoryFile, JsonFile::class, ['io'])['io'];
        Rpc::call('repo.newFilesystem', [$this, $repositoryFile->getPath(), $io, $dumpVersions, $rootPackage, $this instanceof InstalledRepositoryInterface]);
    }

    public function getDevMode()
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.getDevMode', [$this]);
        }
        Remote::unsupported(static::class, 'getDevMode');
    }

    protected function initialize()
    {
        Remote::unsupported(static::class, 'initialize');
    }

    public function reload()
    {
        if (Remote::owned($this)) {
            Rpc::call('repo.reload', [$this]);

            return;
        }
        Remote::unsupported(static::class, 'reload');
    }

    public function write(bool $devMode, InstallationManager $installationManager)
    {
        if (Remote::owned($this)) {
            Rpc::call('repo.write', [$this, $devMode, $installationManager]);

            return;
        }
        Remote::unsupported(static::class, 'write');
    }

    public static function safelyLoadInstalledVersions(string $path): bool
    {
        // maestro validates and reads the file as Composer does; the data
        // is null when the file is not a valid installed.php.
        $data = Rpc::call('repo.safelyLoadInstalledVersions', [$path]);
        if ($data === null) {
            return false;
        }
        \Composer\InstalledVersions::reload($data);

        return true;
    }
}
