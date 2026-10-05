<?php

namespace Project;

use Composer\Script\Event;

class Scripts
{
    public static function postInstall(Event $event): void
    {
        $io = $event->getIO();
        $composer = $event->getComposer();
        $io->write('<info>script</info> '.$event->getName().' dev='.var_export($event->isDevMode(), true));
        $io->write('script vendor-dir='.basename($composer->getConfig()->get('vendor-dir')));
        $io->write('script root='.$composer->getPackage()->getName().' '.$composer->getPackage()->getPrettyVersion());
        $io->write('script dispatch custom='.$composer->getEventDispatcher()->dispatchScript('custom', true, ['a1']));
        // Platform::putEnv() reaches the processes Composer starts; a
        // plain putenv() of a new variable does not.
        \Composer\Util\Platform::putEnv('MAESTRO_TEST_PUTENV', 'from-php');
        putenv('MAESTRO_TEST_PLAIN=plain');
    }

    public static function postAutoloadDump(Event $event): void
    {
        $event->getIO()->write('autoload dumped, generated class exists: '.var_export(class_exists('Generated\\Thing'), true));
    }

    public static function custom(Event $event)
    {
        $event->getIO()->write('custom args='.implode(',', $event->getArguments()).' originating='.($event->getOriginatingEvent() ? 'yes' : 'no'));

        return false;
    }
}
