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
        // The frames of Composer's objects on the stack, as plugins find
        // them with debug_backtrace() (docs/PLUGINS.md §5.12).
        $frames = [];
        foreach (debug_backtrace(\DEBUG_BACKTRACE_PROVIDE_OBJECT) as $trace) {
            if (isset($trace['object'], $trace['class']) && (strpos($trace['class'], 'Composer\\') === 0 || strpos($trace['class'], 'Symfony\\Component\\Console\\') === 0)) {
                $frames[] = $trace['class'].$trace['type'].$trace['function'].'('.count($trace['args']).') '.get_class($trace['object']);
            }
        }
        $GLOBALS['maestroTestFrames'][$event->getCommand()] = $frames;

        if ($event->getCommand() === 'licenses' && $event->getInput()->getOption('format') === 'text') {
            $event->getInput()->setOption('format', 'json');
        }
    }
}
