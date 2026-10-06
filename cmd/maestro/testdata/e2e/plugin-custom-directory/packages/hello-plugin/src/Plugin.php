<?php

namespace Local\Hello;

use Composer\Composer;
use Composer\EventDispatcher\EventSubscriberInterface;
use Composer\IO\IOInterface;
use Composer\Plugin\PluginInterface;
use Composer\Script\Event;
use Composer\Script\ScriptEvents;

class Plugin implements PluginInterface, EventSubscriberInterface
{
    public function activate(Composer $composer, IOInterface $io)
    {
        $io->write('hello-plugin activated from '.basename(dirname(__DIR__)).': '.$composer->getInstallationManager()->getInstallPath($composer->getRepositoryManager()->getLocalRepository()->findPackage('local/hello-plugin', '*')));
    }

    public function deactivate(Composer $composer, IOInterface $io)
    {
        $io->write('hello-plugin deactivated');
    }

    public function uninstall(Composer $composer, IOInterface $io)
    {
        $io->write('hello-plugin uninstalled');
    }

    public static function getSubscribedEvents()
    {
        return [ScriptEvents::POST_AUTOLOAD_DUMP => 'dumped'];
    }

    public function dumped(Event $event)
    {
        $event->getIO()->write('hello-plugin saw '.$event->getName());
    }
}
