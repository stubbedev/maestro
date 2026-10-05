<?php

namespace MaestroTest\GlobalPlugin;

use Composer\Composer;
use Composer\EventDispatcher\EventSubscriberInterface;
use Composer\IO\IOInterface;
use Composer\Plugin\PluginInterface;
use Composer\Script\Event;

class Plugin implements PluginInterface, EventSubscriberInterface
{
    private $composer;
    private $io;

    public function activate(Composer $composer, IOInterface $io): void
    {
        $this->composer = $composer;
        $this->io = $io;
        $io->write('global plugin activated for '.$composer->getPackage()->getName().' global='.var_export($composer->isGlobal(), true));
    }

    public function deactivate(Composer $composer, IOInterface $io): void
    {
    }

    public function uninstall(Composer $composer, IOInterface $io): void
    {
    }

    public static function getSubscribedEvents(): array
    {
        return [
            'post-install-cmd' => 'post',
            'post-update-cmd' => 'post',
        ];
    }

    public function post(Event $event): void
    {
        $pm = $this->composer->getPluginManager();
        $global = $pm->getGlobalComposer();
        $this->io->write([
            'global plugin saw '.$event->getName(),
            'registered: '.implode(',', $pm->getRegisteredPlugins()),
            'plugins: '.count($pm->getPlugins()),
            'global composer: '.($global ? $global->getPackage()->getName() : 'none'),
        ]);
    }
}
