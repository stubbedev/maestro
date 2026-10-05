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

    public function __construct(?\Composer\IO\IOInterface $io = null)
    {
        $this->io = $io;
    }

    public function countActiveJobs($index = null): int
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Util\\ProcessExecutor::countActiveJobs() in plugins yet');
    }

    public function enableAsync(): void
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Util\\ProcessExecutor::enableAsync() in plugins yet');
    }

    public static function escape($argument): string
    {
        return \Maestro\Shim\Rpc::call('proc.escape', [$argument]);
    }

    public function execute($command, &$output = null, ?string $cwd = null): int
    {
        if (func_num_args() > 1) {
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
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Util\\ProcessExecutor::executeAsync() in plugins yet');
    }

    public function executeTty($command, ?string $cwd = null): int
    {
        $result = \Maestro\Shim\Rpc::call('proc.execute', [$command, $cwd, $this->io, false, true]);
        $this->errorOutput = $result['errorOutput'];

        return $result['code'];
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
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Util\\ProcessExecutor::resetMaxJobs() in plugins yet');
    }

    public function setMaxJobs(int $maxJobs): void
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Util\\ProcessExecutor::setMaxJobs() in plugins yet');
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
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Util\\ProcessExecutor::wait() in plugins yet');
    }
}
