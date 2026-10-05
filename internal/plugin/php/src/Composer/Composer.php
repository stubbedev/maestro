<?php

/*
 * maestro's plugin shim: Composer\Composer (docs/PLUGINS.md §4.2). See
 * PartialComposer. The running command and operation are Composer's
 * statics, kept in step with maestro by the sync engine.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer;

use Composer\Pcre\Preg;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;
use Maestro\Shim\Sync;

class Composer extends PartialComposer
{
    public const VERSION = '2.10.3';
    public const BRANCH_ALIAS_VERSION = '';
    public const RELEASE_DATE = '2026-08-27 13:34:23';
    public const SOURCE_VERSION = '';
    public const RUNTIME_API_VERSION = '2.2.2';

    private $locker;
    private $downloadManager;
    private $pluginManager;
    private $autoloadGenerator;
    private $archiveManager;

    public static function getVersion(): string
    {
        // no replacement done, this must be a source checkout
        if (self::VERSION === '@package_version'.'@') {
            return self::SOURCE_VERSION;
        }

        // we have a branch alias and version is a commit id, this must be a snapshot build
        if (self::BRANCH_ALIAS_VERSION !== '' && Preg::isMatch('{^[a-f0-9]{40}$}', self::VERSION)) {
            return self::BRANCH_ALIAS_VERSION.'+'.self::VERSION;
        }

        return self::VERSION;
    }

    public static function setRunningCommand(?string $command): void
    {
        Sync::setStatic('runningCommand', ($command === null || $command === '') ? null : $command);
        // a new outer command resets any operation left over from a prior context
        Sync::setStatic('runningOperation', null);
    }

    public static function getRunningCommand(): ?string
    {
        return Sync::getStatic('runningCommand');
    }

    public static function setRunningOperation(?string $operation): void
    {
        Sync::setStatic('runningOperation', ($operation === null || $operation === '') ? null : $operation);
    }

    public static function getRunningOperation(): ?string
    {
        return Sync::getStatic('runningOperation');
    }

    public function setLocker(\Composer\Package\Locker $locker): void
    {
        if (Remote::owned($this)) {
            Rpc::call('composer.setLocker', [$this, $locker]);

            return;
        }
        $this->locker = $locker;
    }

    public function getLocker(): \Composer\Package\Locker
    {
        if (Remote::owned($this)) {
            return Rpc::call('composer.getLocker', [$this]);
        }

        return $this->locker;
    }

    public function setDownloadManager(\Composer\Downloader\DownloadManager $manager): void
    {
        if (Remote::owned($this)) {
            Rpc::call('composer.setDownloadManager', [$this, $manager]);

            return;
        }
        $this->downloadManager = $manager;
    }

    public function getDownloadManager(): \Composer\Downloader\DownloadManager
    {
        if (Remote::owned($this)) {
            return Rpc::call('composer.getDownloadManager', [$this]);
        }

        return $this->downloadManager;
    }

    public function setPluginManager(\Composer\Plugin\PluginManager $manager): void
    {
        if (Remote::owned($this)) {
            Rpc::call('composer.setPluginManager', [$this, $manager]);

            return;
        }
        $this->pluginManager = $manager;
    }

    public function getPluginManager(): \Composer\Plugin\PluginManager
    {
        if (Remote::owned($this)) {
            return Rpc::call('composer.getPluginManager', [$this]);
        }

        return $this->pluginManager;
    }

    public function setAutoloadGenerator(\Composer\Autoload\AutoloadGenerator $autoloadGenerator): void
    {
        if (Remote::owned($this)) {
            Rpc::call('composer.setAutoloadGenerator', [$this, $autoloadGenerator]);

            return;
        }
        $this->autoloadGenerator = $autoloadGenerator;
    }

    public function getAutoloadGenerator(): \Composer\Autoload\AutoloadGenerator
    {
        if (Remote::owned($this)) {
            return Rpc::call('composer.getAutoloadGenerator', [$this]);
        }

        return $this->autoloadGenerator;
    }

    public function setArchiveManager(\Composer\Package\Archiver\ArchiveManager $manager): void
    {
        if (Remote::owned($this)) {
            Rpc::call('composer.setArchiveManager', [$this, $manager]);

            return;
        }
        $this->archiveManager = $manager;
    }

    public function getArchiveManager(): \Composer\Package\Archiver\ArchiveManager
    {
        if (Remote::owned($this)) {
            return Rpc::call('composer.getArchiveManager', [$this]);
        }

        return $this->archiveManager;
    }
}
