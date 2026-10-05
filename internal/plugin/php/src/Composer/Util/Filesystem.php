<?php

/*
 * maestro's plugin shim: Composer\Util\Filesystem (docs/PLUGINS.md §4.8):
 * every method is maestro's internal/util Filesystem (fs.*). Relative paths
 * resolve against the working directory both sides share (the sync engine
 * carries chdir()).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Util;

class Filesystem
{
    private $processExecutor;

    public function __construct(?\Composer\Util\ProcessExecutor $executor = null)
    {
        $this->processExecutor = $executor;
    }

    public function copy(string $source, string $target)
    {
        return \Maestro\Shim\Rpc::call('fs.copy', [$source, $target]);
    }

    public function copyThenRemove(string $source, string $target)
    {
        return \Maestro\Shim\Rpc::call('fs.copyThenRemove', [$source, $target]);
    }

    protected function directorySize(string $directory)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Util\\Filesystem::directorySize() in plugins yet');
    }

    public function emptyDirectory(string $dir, bool $ensureDirectoryExists = true)
    {
        return \Maestro\Shim\Rpc::call('fs.emptyDirectory', [$dir, $ensureDirectoryExists]);
    }

    public function ensureDirectoryExists(string $directory)
    {
        return \Maestro\Shim\Rpc::call('fs.ensureDirectoryExists', [$directory]);
    }

    public function filePutContentsIfModified(string $path, string $content)
    {
        return \Maestro\Shim\Rpc::call('fs.filePutContentsIfModified', [$path, $content]);
    }

    public function findShortestPath(string $from, string $to, bool $directories = false, bool $preferRelative = false)
    {
        return \Maestro\Shim\Rpc::call('fs.findShortestPath', [$from, $to, $directories, $preferRelative]);
    }

    public function findShortestPathCode(string $from, string $to, bool $directories = false, bool $staticCode = false, bool $preferRelative = false)
    {
        return \Maestro\Shim\Rpc::call('fs.findShortestPathCode', [$from, $to, $directories, $staticCode, $preferRelative]);
    }

    public static function getPlatformPath(string $path)
    {
        return \Maestro\Shim\Rpc::call('fs.getPlatformPath', [$path]);
    }

    protected function getProcess()
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Util\\Filesystem::getProcess() in plugins yet');
    }

    public function isAbsolutePath(string $path)
    {
        return \Maestro\Shim\Rpc::call('fs.isAbsolutePath', [$path]);
    }

    public function isDirEmpty(string $dir)
    {
        return \Maestro\Shim\Rpc::call('fs.isDirEmpty', [$dir]);
    }

    public function isJunction(string $junction)
    {
        return \Maestro\Shim\Rpc::call('fs.isJunction', [$junction]);
    }

    public static function isLocalPath(string $path)
    {
        return \Maestro\Shim\Rpc::call('fs.isLocalPath', [$path]);
    }

    public static function isReadable(string $path)
    {
        return \Maestro\Shim\Rpc::call('fs.isReadable', [$path]);
    }

    public function isSymlinkedDirectory(string $directory)
    {
        return \Maestro\Shim\Rpc::call('fs.isSymlinkedDirectory', [$directory]);
    }

    public function junction(string $target, string $junction)
    {
        return \Maestro\Shim\Rpc::call('fs.junction', [$target, $junction]);
    }

    public function normalizePath(string $path)
    {
        return \Maestro\Shim\Rpc::call('fs.normalizePath', [$path]);
    }

    public function relativeSymlink(string $target, string $link)
    {
        return \Maestro\Shim\Rpc::call('fs.relativeSymlink', [$target, $link]);
    }

    public function remove(string $file)
    {
        return \Maestro\Shim\Rpc::call('fs.remove', [$file]);
    }

    public function removeDirectory(string $directory)
    {
        return \Maestro\Shim\Rpc::call('fs.removeDirectory', [$directory]);
    }

    public function removeDirectoryAsync(string $directory)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Util\\Filesystem::removeDirectoryAsync() in plugins yet');
    }

    public function removeDirectoryPhp(string $directory)
    {
        return \Maestro\Shim\Rpc::call('fs.removeDirectoryPhp', [$directory]);
    }

    public function removeJunction(string $junction)
    {
        return \Maestro\Shim\Rpc::call('fs.removeJunction', [$junction]);
    }

    public function rename(string $source, string $target)
    {
        return \Maestro\Shim\Rpc::call('fs.rename', [$source, $target]);
    }

    public function rmdir(string $path)
    {
        return \Maestro\Shim\Rpc::call('fs.rmdir', [$path]);
    }

    public function safeCopy(string $source, string $target): void
    {
        \Maestro\Shim\Rpc::call('fs.safeCopy', [$source, $target]);
    }

    public function size(string $path)
    {
        return \Maestro\Shim\Rpc::call('fs.size', [$path]);
    }

    public static function trimTrailingSlash(string $path)
    {
        return \Maestro\Shim\Rpc::call('fs.trimTrailingSlash', [$path]);
    }

    public function unlink(string $path)
    {
        return \Maestro\Shim\Rpc::call('fs.unlink', [$path]);
    }
}
