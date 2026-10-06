<?php

/*
 * maestro's plugin shim: Composer\Util\ProcessExecutor (docs/PLUGINS.md
 * §4.8): maestro runs the processes (proc.*), with the IO this executor was
 * built with; output streams to the terminal as in Composer. The timeout is
 * Composer's static, kept in step with maestro by the sync engine.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Util;

class ProcessExecutor
{
    protected $captureOutput = false;
    protected $errorOutput = '';
    protected $io;
    protected static $timeout = 300;

    private const STATUS_QUEUED = 1;
    private const STATUS_STARTED = 2;
    private const STATUS_COMPLETED = 3;
    private const STATUS_FAILED = 4;
    private const STATUS_ABORTED = 5;

    /** @var array<int, array<string, mixed>> the asynchronous jobs, run in PHP with Symfony Process as Composer does */
    private $jobs = [];
    /** @var int */
    private $runningJobs = 0;
    /** @var int */
    private $maxJobs = 10;
    /** @var int */
    private $idGen = 0;
    /** @var bool */
    private $allowAsync = false;

    public function __construct(?\Composer\IO\IOInterface $io = null)
    {
        $this->io = $io;
    }

    public function countActiveJobs($index = null): int
    {
        foreach ($this->jobs as $job) {
            if ($job['status'] === self::STATUS_STARTED) {
                if (!$job['process']->isRunning()) {
                    call_user_func($job['resolve'], $job['process']);
                }

                $job['process']->checkTimeout();
            }

            if ($this->runningJobs < $this->maxJobs) {
                if ($job['status'] === self::STATUS_QUEUED) {
                    $this->startJob($job['id']);
                }
            }
        }

        if (null !== $index) {
            return $this->jobs[$index]['status'] < self::STATUS_COMPLETED ? 1 : 0;
        }

        $active = 0;
        foreach ($this->jobs as $job) {
            if ($job['status'] < self::STATUS_COMPLETED) {
                $active++;
            } else {
                unset($this->jobs[$job['id']]);
            }
        }

        return $active;
    }

    public function enableAsync(): void
    {
        $this->allowAsync = true;
    }

    public static function escape($argument): string
    {
        return \Maestro\Shim\Rpc::call('proc.escape', [$argument]);
    }

    public function execute($command, &$output = null, ?string $cwd = null): int
    {
        if (func_num_args() > 1 && is_callable($output)) {
            $result = \Maestro\Shim\Rpc::call('proc.execute', [$command, $cwd, $this->io, true, false, $output]);
        } elseif (func_num_args() > 1) {
            $result = \Maestro\Shim\Rpc::call('proc.execute', [$command, $cwd, $this->io, true, false]);
            $output = $result['output'];
        } else {
            $result = \Maestro\Shim\Rpc::call('proc.execute', [$command, $cwd, $this->io, false, false]);
        }
        $this->errorOutput = $result['errorOutput'];

        return $result['code'];
    }

    public function executeAsync($command, ?string $cwd = null): \React\Promise\PromiseInterface
    {
        if (!$this->allowAsync) {
            throw new \LogicException('You must use the ProcessExecutor instance which is part of a Composer\Loop instance to be able to run async processes');
        }

        $job = [
            'id' => $this->idGen++,
            'status' => self::STATUS_QUEUED,
            'command' => $command,
            'cwd' => $cwd,
        ];

        $resolver = static function ($resolve, $reject) use (&$job): void {
            $job['status'] = ProcessExecutor::STATUS_QUEUED;
            $job['resolve'] = $resolve;
            $job['reject'] = $reject;
        };

        $canceler = static function () use (&$job): void {
            if ($job['status'] === ProcessExecutor::STATUS_QUEUED) {
                $job['status'] = ProcessExecutor::STATUS_ABORTED;
            }
            if ($job['status'] !== ProcessExecutor::STATUS_STARTED) {
                return;
            }
            $job['status'] = ProcessExecutor::STATUS_ABORTED;
            try {
                if (defined('SIGINT')) {
                    $job['process']->signal(SIGINT);
                }
            } catch (\Exception $e) {
                // the process may be gone already
            }
            $job['process']->stop(1);

            throw new \RuntimeException('Aborted process');
        };

        $promise = new \React\Promise\Promise($resolver, $canceler);
        $promise = $promise->then(function () use (&$job) {
            if ($job['process']->isSuccessful()) {
                $job['status'] = ProcessExecutor::STATUS_COMPLETED;
            } else {
                $job['status'] = ProcessExecutor::STATUS_FAILED;
            }

            $this->runningJobs--;

            return $job['process'];
        }, function ($e) use (&$job): void {
            $job['status'] = ProcessExecutor::STATUS_FAILED;

            $this->runningJobs--;

            throw $e;
        });
        $this->jobs[$job['id']] = &$job;

        if ($this->runningJobs < $this->maxJobs) {
            $this->startJob($job['id']);
        }

        // The loops this executor belongs to drive it while they wait,
        // maestro's own included.
        \Maestro\Shim\Rpc::call('proc.asyncStarted', [$this]);

        return $promise;
    }

    public function executeTty($command, ?string $cwd = null): int
    {
        $result = \Maestro\Shim\Rpc::call('proc.execute', [$command, $cwd, $this->io, false, true]);
        $this->errorOutput = $result['errorOutput'];

        return $result['code'];
    }

    private function startJob(int $id): void
    {
        $job = &$this->jobs[$id];
        if ($job['status'] !== self::STATUS_QUEUED) {
            return;
        }

        $job['status'] = self::STATUS_STARTED;
        $this->runningJobs++;

        $command = $job['command'];
        $cwd = $job['cwd'];

        if ($this->io !== null && $this->io->isDebug()) {
            $this->io->writeError(\Maestro\Shim\Rpc::call('proc.describeAsync', [$command, $cwd]));
        }

        try {
            if (is_string($command)) {
                $process = \Symfony\Component\Process\Process::fromShellCommandline($command, $cwd, null, null, static::getTimeout());
            } else {
                $process = new \Symfony\Component\Process\Process($command, $cwd, null, null, static::getTimeout());
            }
        } catch (\Throwable $e) {
            $job['reject']($e);

            return;
        }

        $job['process'] = $process;

        try {
            $process->start();
        } catch (\Throwable $e) {
            $job['reject']($e);

            return;
        }
    }

    public function getErrorOutput(): string
    {
        return $this->errorOutput;
    }

    public static function getTimeout(): int
    {
        return \Maestro\Shim\Sync::getStatic('processTimeout');
    }

    protected function outputHandler(string $type, string $buffer): void
    {
        if ($this->captureOutput) {
            return;
        }

        if (null === $this->io) {
            echo $buffer;

            return;
        }

        if (\Symfony\Component\Process\Process::ERR === $type) {
            $this->io->writeErrorRaw($buffer, false);
        } else {
            $this->io->writeRaw($buffer, false);
        }
    }

    public function requiresGitDirEnv($command): bool
    {
        return \Maestro\Shim\Rpc::call('proc.requiresGitDirEnv', [$command]);
    }

    public function resetMaxJobs(): void
    {
        if (is_numeric($maxJobs = Platform::getEnv('COMPOSER_MAX_PARALLEL_PROCESSES'))) {
            $this->maxJobs = max(1, min(50, (int) $maxJobs));
        } else {
            $this->maxJobs = 10;
        }
    }

    public function setMaxJobs(int $maxJobs): void
    {
        $this->maxJobs = $maxJobs;
    }

    public static function setTimeout(int $timeout): void
    {
        static::$timeout = $timeout;
        \Maestro\Shim\Sync::setStatic('processTimeout', $timeout);
    }

    public function splitLines(?string $output): array
    {
        return \Maestro\Shim\Rpc::call('proc.splitLines', [$output]);
    }

    public function wait($index = null): void
    {
        while (true) {
            if (0 === $this->countActiveJobs($index)) {
                return;
            }

            usleep(1000);
        }
    }
}
