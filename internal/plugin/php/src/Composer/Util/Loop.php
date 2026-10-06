<?php

/*
 * maestro's plugin shim: Composer\Util\Loop (docs/PLUGINS.md §4.8): a proxy
 * of maestro's event loop (loop.*); one created in PHP is maestro's too.
 * wait() runs maestro's loop until no job is left, settling the promises
 * of maestro's work (and so PHP's then() callbacks), as Composer's does.
 * The asynchronous processes of PHP code run in PHP, on the executor
 * getProcessExecutor() gives, and maestro's loop drives them whenever it
 * waits (docs/PLUGINS.md §5.12).
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
        if ($processExecutor !== null) {
            $processExecutor->enableAsync();
            Rpc::call('loop.phpProcessExecutor', [$this, $processExecutor]);
        }
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
            // maestro's loop drives and counts them whenever it waits
            Rpc::call('loop.phpProcessExecutor', [$this, $this->processExecutor]);
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

        // The jobs are maestro's (downloads, its processes) and the
        // processes PHP code started on this loop's executor, which
        // maestro's loop drives and counts with its own, as Composer's
        // countActiveJobs() does.
        if ($progress !== null) {
            $progress->start(Rpc::call('loop.countJobs', [$this]));
        }
        Rpc::call('loop.wait', [$this]);
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
