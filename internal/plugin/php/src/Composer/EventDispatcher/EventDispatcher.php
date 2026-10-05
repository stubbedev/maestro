<?php

/*
 * maestro's plugin shim: Composer\EventDispatcher\EventDispatcher
 * (docs/PLUGINS.md §4.4, §5.5), a service proxy of maestro's dispatcher
 * (ed.*): listeners are kept and run by maestro, which calls PHP for the
 * PHP callables. addSubscriber() expands getSubscribedEvents() here, as
 * Composer does. Creating one in PHP is not supported yet.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\EventDispatcher;

class EventDispatcher
{
    protected $composer;
    protected $io;
    protected $listeners = [];
    protected $loader;
    protected $process;
    protected $runScripts = true;

    public function __construct(\Composer\PartialComposer $composer, \Composer\IO\IOInterface $io, ?\Composer\Util\ProcessExecutor $process = null)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\EventDispatcher\\EventDispatcher::__construct() in plugins yet');
    }

    public function addListener(string $eventName, $listener, int $priority = 0): void
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(self::class, 'addListener');
        }

        \Maestro\Shim\Rpc::call('ed.addListener', [$this, $eventName, \Maestro\Shim\Listeners::describe($listener), $priority]);
    }

    public function addSubscriber(\Composer\EventDispatcher\EventSubscriberInterface $subscriber): void
    {
        foreach ($subscriber->getSubscribedEvents() as $eventName => $params) {
            if (is_string($params)) {
                $this->addListener($eventName, [$subscriber, $params]);
            } elseif (is_string($params[0])) {
                $this->addListener($eventName, [$subscriber, $params[0]], $params[1] ?? 0);
            } else {
                foreach ($params as $listener) {
                    $this->addListener($eventName, [$subscriber, $listener[0]], $listener[1] ?? 0);
                }
            }
        }
    }

    public function dispatch(?string $eventName, ?\Composer\EventDispatcher\Event $event = null): int
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(self::class, 'dispatch');
        }

        return \Maestro\Shim\Rpc::call('ed.dispatch', [$this, $eventName, $event]);
    }

    public function dispatchInstallerEvent(string $eventName, bool $devMode, bool $executeOperations, \Composer\DependencyResolver\Transaction $transaction): int
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(self::class, 'dispatchInstallerEvent');
        }

        return \Maestro\Shim\Rpc::call('ed.dispatchInstallerEvent', [$this, $eventName, $devMode, $executeOperations, $transaction]);
    }

    public function dispatchPackageEvent(string $eventName, bool $devMode, \Composer\Repository\RepositoryInterface $localRepo, array $operations, \Composer\DependencyResolver\Operation\OperationInterface $operation): int
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(self::class, 'dispatchPackageEvent');
        }

        return \Maestro\Shim\Rpc::call('ed.dispatchPackageEvent', [$this, $eventName, $devMode, $localRepo, $operations, $operation]);
    }

    public function dispatchScript(string $eventName, bool $devMode = false, array $additionalArgs = [], array $flags = []): int
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(self::class, 'dispatchScript');
        }

        return \Maestro\Shim\Rpc::call('ed.dispatchScript', [$this, $eventName, $devMode, $additionalArgs, $flags]);
    }

    protected function doDispatch(\Composer\EventDispatcher\Event $event)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\EventDispatcher\\EventDispatcher::doDispatch() in plugins yet');
    }

    protected function executeEventPhpScript(string $className, string $methodName, \Composer\EventDispatcher\Event $event)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\EventDispatcher\\EventDispatcher::executeEventPhpScript() in plugins yet');
    }

    protected function executeTty(string $exec): int
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\EventDispatcher\\EventDispatcher::executeTty() in plugins yet');
    }

    protected function getListeners(\Composer\EventDispatcher\Event $event): array
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\EventDispatcher\\EventDispatcher::getListeners() in plugins yet');
    }

    protected function getPhpExecCommand(): string
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\EventDispatcher\\EventDispatcher::getPhpExecCommand() in plugins yet');
    }

    protected function getScriptListeners(\Composer\EventDispatcher\Event $event): array
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\EventDispatcher\\EventDispatcher::getScriptListeners() in plugins yet');
    }

    public function hasEventListeners(\Composer\EventDispatcher\Event $event): bool
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(self::class, 'hasEventListeners');
        }

        return \Maestro\Shim\Rpc::call('ed.hasEventListeners', [$this, $event]);
    }

    protected function isCommandClass(string $callable): bool
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\EventDispatcher\\EventDispatcher::isCommandClass() in plugins yet');
    }

    protected function isComposerScript(string $callable): bool
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\EventDispatcher\\EventDispatcher::isComposerScript() in plugins yet');
    }

    protected function isPhpScript(string $callable): bool
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\EventDispatcher\\EventDispatcher::isPhpScript() in plugins yet');
    }

    protected function popEvent(): ?string
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\EventDispatcher\\EventDispatcher::popEvent() in plugins yet');
    }

    protected function pushEvent(\Composer\EventDispatcher\Event $event): int
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\EventDispatcher\\EventDispatcher::pushEvent() in plugins yet');
    }

    public function removeListener($listener): void
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(self::class, 'removeListener');
        }

        \Maestro\Shim\Rpc::call('ed.removeListener', [$this, \Maestro\Shim\Listeners::match($listener)]);
    }

    public function setRunScripts(bool $runScripts = true): self
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(self::class, 'setRunScripts');
        }
        \Maestro\Shim\Rpc::call('ed.setRunScripts', [$this, $runScripts]);
        $this->runScripts = $runScripts;

        return $this;
    }
}
