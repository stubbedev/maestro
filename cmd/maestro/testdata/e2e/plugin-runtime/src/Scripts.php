<?php

namespace Project;

use Composer\Script\Event;
use Composer\Util\Filesystem;
use Composer\Util\Platform;
use Composer\Util\ProcessExecutor;

class Scripts
{
    public static function envAndCwd(Event $event): void
    {
        putenv('MAESTRO_E2E_VAR=set-in-php');
        chdir('src');
        $event->getIO()->write('php changed env and cwd');
    }

    public static function afterChdir(Event $event): void
    {
        $fs = new Filesystem();
        $event->getIO()->write([
            'cwd: '.basename(Platform::getCwd()),
            'Platform::getEnv: '.Platform::getEnv('MAESTRO_E2E_VAR'),
            'normalizePath(.): '.basename($fs->normalizePath(realpath('.'))),
            'isAbsolutePath(Scripts.php): '.var_export($fs->isAbsolutePath('Scripts.php'), true),
            'is_file(Scripts.php): '.var_export(is_file('Scripts.php'), true),
        ]);
    }

    public static function shutdown(Event $event): void
    {
        register_shutdown_function(function () {
            echo 'shutdown function ran', PHP_EOL;
            file_put_contents('shutdown.txt', "written at shutdown\n");
        });
        $event->getIO()->write('shutdown function registered');
    }

    public static function exit3(Event $event): void
    {
        $event->getIO()->write('exiting with 3');
        exit(3);
    }

    public static function fatal(Event $event): void
    {
        $event->getIO()->write('about to declare a class twice');
        eval('class MaestroE2EDuplicate {} class MaestroE2EDuplicate {}');
    }

    public static function throws(Event $event): void
    {
        throw new \RuntimeException('boom from a script');
    }

    public static function returnsFalse(Event $event): bool
    {
        $event->getIO()->write('returning false');

        return false;
    }

    public static function deprecation(Event $event): void
    {
        trigger_error('an old API', E_USER_DEPRECATED);
        trigger_error('another old API', E_USER_DEPRECATED);
        $event->getIO()->write('after the deprecations');
    }

    public static function timeout(Event $event): void
    {
        $event->getIO()->write('process timeout: '.ProcessExecutor::getTimeout());
    }
}
