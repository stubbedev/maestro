<?php

/*
 * maestro's plugin shim: Composer\Util\Filesystem (docs/PLUGINS.md §4.8):
 * the methods are maestro's internal/util Filesystem (fs.*), but for
 * directorySize(), getProcess() and removeDirectoryAsync(), whose process
 * runs on this executor's asynchronous jobs as in Composer. Relative paths
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
        $it = new \RecursiveDirectoryIterator($directory, \RecursiveDirectoryIterator::SKIP_DOTS);
        $ri = new \RecursiveIteratorIterator($it, \RecursiveIteratorIterator::CHILD_FIRST);

        $size = 0;
        foreach ($ri as $file) {
            if ($file->isFile()) {
                $size += $file->getSize();
            }
        }

        return $size;
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
        if (null === $this->processExecutor) {
            $this->processExecutor = new ProcessExecutor();
        }

        return $this->processExecutor;
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
        // Composer's removeEdgeCases() is maestro's (fs.removeEdgeCases),
        // but for its proc_open() check, which is this process's.
        $edgeCaseResult = \Maestro\Shim\Rpc::call('fs.removeEdgeCases', [$directory]);
        if ($edgeCaseResult === null && !\function_exists('proc_open')) {
            $edgeCaseResult = $this->removeDirectoryPhp($directory);
        }
        if ($edgeCaseResult !== null) {
            return \React\Promise\resolve($edgeCaseResult);
        }

        if (Platform::isWindows()) {
            $cmd = ['rmdir', '/S', '/Q', Platform::realpath($directory)];
        } else {
            $cmd = ['rm', '-rf', $directory];
        }

        // The removal runs on this executor's asynchronous jobs, as in
        // Composer (the executor of a Loop).
        $promise = $this->getProcess()->executeAsync($cmd);

        return $promise->then(function ($process) use ($directory) {
            // clear stat cache because external processes aren't tracked by the php stat cache
            clearstatcache();

            if ($process->isSuccessful()) {
                if (!is_dir($directory)) {
                    return \React\Promise\resolve(true);
                }
            }

            return \React\Promise\resolve($this->removeDirectoryPhp($directory));
        });
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
