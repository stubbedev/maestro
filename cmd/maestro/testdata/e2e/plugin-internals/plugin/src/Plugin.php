<?php

namespace MaestroTest\InternalsPlugin;

use Composer\Composer;
use Composer\EventDispatcher\EventSubscriberInterface;
use Composer\Installer\InstallerEvent;
use Composer\Installer\InstallerEvents;
use Composer\IO\IOInterface;
use Composer\Plugin\Capability\CommandProvider;
use Composer\Plugin\Capable;
use Composer\Plugin\PluginInterface;
use Composer\Script\Event;
use Composer\Script\ScriptEvents;

class Plugin implements PluginInterface, EventSubscriberInterface, Capable
{
    /** @var Composer */
    private $composer;

    /** @var IOInterface */
    private $io;

    public function activate(Composer $composer, IOInterface $io): void
    {
        $this->composer = $composer;
        $this->io = $io;
        if (getenv('INTERNALS_THROW') === 'activate') {
            self::fail('activate');
        }
    }

    public function deactivate(Composer $composer, IOInterface $io): void
    {
    }

    public function uninstall(Composer $composer, IOInterface $io): void
    {
    }

    public function getCapabilities(): array
    {
        return [CommandProvider::class => Commands::class];
    }

    public static function getSubscribedEvents(): array
    {
        return [
            InstallerEvents::PRE_OPERATIONS_EXEC => 'preOperationsExec',
            ScriptEvents::POST_INSTALL_CMD => 'postInstall',
        ];
    }

    /**
     * A process started asynchronously on Composer's loop: Composer's own
     * waits (the operations' downloads and installs) run it and its
     * callback.
     */
    public function preOperationsExec(InstallerEvent $event): void
    {
        $io = $this->io;
        $this->composer->getLoop()->getProcessExecutor()->executeAsync('echo from an async process')->then(function ($process) use ($io) {
            $io->write('async: '.trim($process->getOutput()));
        });
        $io->write('async process started');
    }

    public function postInstall(Event $event): void
    {
        if (getenv('INTERNALS_THROW') === 'listener') {
            self::fail('a listener');
        }
    }

    /** A frame of the plugin's own between Composer's and the throw. */
    public static function fail(string $where): void
    {
        throw new \RuntimeException('thrown in '.$where);
    }
}
