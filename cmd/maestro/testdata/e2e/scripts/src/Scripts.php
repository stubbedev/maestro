<?php

namespace Acme;

use Composer\Script\Event;

final class Scripts
{
    public static function run(Event $event): void
    {
        file_put_contents('callable.txt', implode("\n", [
            'name=' . $event->getName(),
            'dev=' . ($event->isDevMode() ? '1' : '0'),
            'args=' . implode(',', $event->getArguments()),
            'vendor=' . basename((string) $event->getComposer()->getConfig()->get('vendor-dir')),
        ]) . "\n");
    }

    public static function io(Event $event): void
    {
        $event->getIO()->write('<info>callable says hello</info>');
        $event->getIO()->writeError('to stderr');
    }
}
