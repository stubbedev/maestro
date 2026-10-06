<?php

/*
 * maestro's plugin shim: Composer\Util\Loop (docs/PLUGINS.md §4.8): a proxy
 * of maestro's event loop (loop.*); one created in PHP is maestro's too.
 * wait() runs maestro's loop until no job is left, settling the promises
 * of maestro's work (and so PHP's then() callbacks), as Composer's does.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Util;

use Maestro\Shim\Rpc;
use Symfony\Component\Console\Helper\ProgressBar;

class Loop
{
    /** @var ProcessExecutor|null */
    private $processExecutor;

    public function __construct(HttpDownloader $httpDownloader, ?ProcessExecutor $processExecutor = null)
    {
        Rpc::call('loop.new', [$this, $httpDownloader]);
        $this->processExecutor = $processExecutor;
    }

    public function getHttpDownloader(): HttpDownloader
    {
        return Rpc::call('loop.getHttpDownloader', [$this]);
    }

    public function getProcessExecutor(): ?ProcessExecutor
    {
        if ($this->processExecutor === null && Rpc::call('loop.hasProcessExecutor', [$this])) {
            // maestro's loop runs its own processes; the asynchronous
            // processes of PHP code run in PHP, as Composer runs them.
            $this->processExecutor = new ProcessExecutor();
            $this->processExecutor->enableAsync();
        }

        return $this->processExecutor;
    }

    public function wait(array $promises, ?ProgressBar $progress = null): void
    {
        $uncaught = null;

        \React\Promise\all($promises)->then(
            static function (): void {
            },
            static function (\Throwable $e) use (&$uncaught): void {
                $uncaught = $e;
            }
        );

        if ($progress !== null) {
            $progress->start(Rpc::call('loop.countJobs', [$this]));
        }
        // maestro's jobs (downloads, its processes), then the processes PHP
        // code started, whose callbacks may start more of either.
        do {
            Rpc::call('loop.wait', [$this]);
            $phpJobs = false;
            while ($this->processExecutor !== null && $this->processExecutor->countActiveJobs() > 0) {
                $phpJobs = true;
                usleep(1000);
            }
        } while ($phpJobs);
        if ($progress !== null) {
            $progress->finish();
        }

        if (null !== $uncaught) {
            throw $uncaught;
        }
    }

    public function abortJobs(): void
    {
        Rpc::call('loop.abortJobs', [$this]);
    }
}
