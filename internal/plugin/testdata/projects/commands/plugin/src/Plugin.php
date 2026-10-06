<?php

namespace MaestroTest\Commands;

use Composer\Composer;
use Composer\EventDispatcher\EventSubscriberInterface;
use Composer\IO\IOInterface;
use Composer\Plugin\Capability\CommandProvider as CommandProviderCapability;
use Composer\Plugin\Capable;
use Composer\Plugin\PluginEvents;
use Composer\Plugin\PluginInterface;
use Composer\Plugin\PreCommandRunEvent;

class Plugin implements PluginInterface, Capable, EventSubscriberInterface
{
    public function activate(Composer $composer, IOInterface $io)
    {
    }

    public function deactivate(Composer $composer, IOInterface $io)
    {
    }

    public function uninstall(Composer $composer, IOInterface $io)
    {
    }

    public function getCapabilities()
    {
        return [CommandProviderCapability::class => CommandProvider::class];
    }

    public static function getSubscribedEvents()
    {
        return [PluginEvents::PRE_COMMAND_RUN => 'preCommandRun'];
    }

    /**
     * Changes the input of a command maestro runs: the change must reach it.
     */
    public function preCommandRun(PreCommandRunEvent $event)
    {
        if ($event->getCommand() === 'licenses') {
            $event->getInput()->setOption('format', 'json');
        }
    }
}
