<?php

/*
 * maestro's plugin shim: Composer\EventDispatcher\EventDispatcher
 * (docs/PLUGINS.md §4.4, §5.5), a service proxy of maestro's dispatcher
 * (ed.*): listeners are kept and run by maestro, which calls PHP for the
 * PHP callables. addSubscriber() expands getSubscribedEvents() here, as
 * Composer does. One created in PHP is maestro's too (ed.new); one whose
 * constructor did not run has no maestro peer, and its methods throw.
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
        \Maestro\Shim\Rpc::call('ed.new', [$this, $composer, $io, $process]);
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

    // The protected methods are maestro's dispatcher's (a subclass's
    // parent:: and $this-> calls), but for the three pure is*() checks;
    // maestro's dispatch does not call a subclass's overrides of them.

    protected function doDispatch(\Composer\EventDispatcher\Event $event)
    {
        self::requirePeer($this, 'doDispatch');

        return \Maestro\Shim\Rpc::call('ed.doDispatch', [$this, $event]);
    }

    protected function executeEventPhpScript(string $className, string $methodName, \Composer\EventDispatcher\Event $event)
    {
        self::requirePeer($this, 'executeEventPhpScript');
        \Maestro\Shim\Rpc::call('ed.echoPhpScript', [$this, $className, $methodName, $event]);

        return $className::$methodName($event);
    }

    protected function executeTty(string $exec): int
    {
        self::requirePeer($this, 'executeTty');

        return \Maestro\Shim\Rpc::call('ed.executeTty', [$this, $exec]);
    }

    protected function getListeners(\Composer\EventDispatcher\Event $event): array
    {
        self::requirePeer($this, 'getListeners');

        return self::listeners(\Maestro\Shim\Rpc::call('ed.getListeners', [$this, $event]));
    }

    protected function getPhpExecCommand(): string
    {
        self::requirePeer($this, 'getPhpExecCommand');

        return \Maestro\Shim\Rpc::call('ed.getPhpExecCommand', [$this]);
    }

    protected function getScriptListeners(\Composer\EventDispatcher\Event $event): array
    {
        self::requirePeer($this, 'getScriptListeners');

        return self::listeners(\Maestro\Shim\Rpc::call('ed.getScriptListeners', [$this, $event]));
    }

    /**
     * The listeners maestro describes as the callables they are: scripts as
     * strings, PHP callables as themselves, maestro's own as callables.
     *
     * @param list<mixed> $list
     * @return list<mixed>
     */
    private static function listeners(array $list): array
    {
        $out = [];
        foreach ($list as $listener) {
            if (is_array($listener) && array_key_exists('h', $listener)) {
                $listener = \Maestro\Shim\Listeners::callable($listener['h']);
            } elseif (is_array($listener) && array_key_exists('go', $listener)) {
                $listener = $listener['go'];
            }
            $out[] = $listener;
        }

        return $out;
    }

    /**
     * A dispatcher whose constructor did not run (a subclass not calling
     * parent::__construct()) has no maestro peer: its methods cannot run.
     *
     * @param object $dispatcher
     */
    private static function requirePeer($dispatcher, string $method): void
    {
        if (!\Maestro\Shim\Remote::owned($dispatcher)) {
            \Maestro\Shim\Remote::unsupported(self::class, $method);
        }
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
        return str_contains($callable, '\\') && !str_contains($callable, ' ') && str_ends_with($callable, 'Command');
    }

    protected function isComposerScript(string $callable): bool
    {
        return str_starts_with($callable, '@') && !str_starts_with($callable, '@php ') && !str_starts_with($callable, '@putenv ');
    }

    protected function isPhpScript(string $callable): bool
    {
        return false === strpos($callable, ' ') && false !== strpos($callable, '::');
    }

    protected function popEvent(): ?string
    {
        self::requirePeer($this, 'popEvent');

        return \Maestro\Shim\Rpc::call('ed.popEvent', [$this]);
    }

    protected function pushEvent(\Composer\EventDispatcher\Event $event): int
    {
        self::requirePeer($this, 'pushEvent');

        return \Maestro\Shim\Rpc::call('ed.pushEvent', [$this, $event]);
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
