<?php

/*
 * maestro's plugin shim: Composer\Cache (docs/PLUGINS.md §4.8): a proxy of
 * maestro's (cache.*). One created in PHP, or by a subclass's call to the
 * parent constructor, is maestro's too.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer;

use Composer\IO\IOInterface;
use Composer\Util\Filesystem;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class Cache
{
    public function __construct(IOInterface $io, string $cacheDir, string $allowlist = 'a-z0-9._', ?Filesystem $filesystem = null, bool $readOnly = false)
    {
        Rpc::call('cache.new', [$this, $io, $cacheDir, $allowlist, $readOnly]);
    }

    public function setReadOnly(bool $readOnly)
    {
        Rpc::call('cache.setReadOnly', [$this, $readOnly]);
    }

    public function isReadOnly()
    {
        return Rpc::call('cache.isReadOnly', [$this]);
    }

    public static function isUsable(string $path)
    {
        return Rpc::call('cache.isUsable', [$path]);
    }

    public function isEnabled()
    {
        return Rpc::call('cache.isEnabled', [$this]);
    }

    public function getRoot()
    {
        return Rpc::call('cache.getRoot', [$this]);
    }

    public function read(string $file)
    {
        return Rpc::call('cache.read', [$this, $file]);
    }

    public function write(string $file, string $contents)
    {
        return Rpc::call('cache.write', [$this, $file, $contents]);
    }

    public function copyFrom(string $file, string $source)
    {
        return Rpc::call('cache.copyFrom', [$this, $file, $source]);
    }

    public function copyTo(string $file, string $target)
    {
        return Rpc::call('cache.copyTo', [$this, $file, $target]);
    }

    public function gcIsNecessary()
    {
        return Rpc::call('cache.gcIsNecessary', [$this]);
    }

    public function remove(string $file)
    {
        return Rpc::call('cache.remove', [$this, $file]);
    }

    public function clear()
    {
        return Rpc::call('cache.clear', [$this]);
    }

    public function getAge(string $file)
    {
        return Rpc::call('cache.getAge', [$this, $file]);
    }

    public function gc(int $ttl, int $maxSize)
    {
        return Rpc::call('cache.gc', [$this, $ttl, $maxSize]);
    }

    public function gcVcsCache(int $ttl): bool
    {
        return Rpc::call('cache.gcVcsCache', [$this, $ttl]);
    }

    public function sha1(string $file)
    {
        return Rpc::call('cache.sha1', [$this, $file]);
    }

    public function sha256(string $file)
    {
        return Rpc::call('cache.sha256', [$this, $file]);
    }

    protected function getFinder()
    {
        Remote::unsupported(self::class, 'getFinder');
    }
}
