<?php

namespace App;

use Composer\Script\Event;

final class Scripts
{
    public static function postInstall(Event $event): void
    {
        $composer = $event->getComposer();
        $packages = $composer->getRepositoryManager()->getLocalRepository()->getPackages();

        file_put_contents('post-install.txt', implode("\n", [
            'dev='.($event->isDevMode() ? '1' : '0'),
            'vendor='.basename((string) $composer->getConfig()->get('vendor-dir')),
            'packages='.count($packages),
        ])."\n");
    }
}
