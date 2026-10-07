<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

use Composer\Autoload\ClassLoader;
use Composer\IO\ConsoleIO;
use Composer\Util\ProcessExecutor;
use Symfony\Component\Console\Application;
use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\Input\StringInput;
use Symfony\Component\Console\Output\ConsoleOutput;

/**
 * The PHP half of maestro's event dispatch (docs/PLUGINS.md §5.5): maestro
 * runs Composer's doDispatch() and calls these for the PHP code of a
 * dispatch. Each one does the checks Composer does in PHP first and calls
 * maestro back (`dispatch.before`) right before the PHP code runs, where
 * maestro writes its "> ..." line.
 */
final class Dispatch
{
    /** @var ClassLoader|null the loader of makeAutoloader() */
    private static $loader;

    /** @var array<string, mixed>|null what that loader was built from */
    private static $loaderContents;

    /** @var list<array<int, mixed>> spl_autoload_functions() per open dispatch */
    private static $autoloadersBefore = [];

    public static function register(): void
    {
        Server::register('listener.call', [self::class, 'listener']);
        Server::register('script.php', [self::class, 'phpScript']);
        Server::register('script.commandClass', [self::class, 'commandClass']);
        Server::register('autoload.install', [self::class, 'installAutoloader']);
        Server::register('dispatch.begin', [self::class, 'begin']);
        Server::register('dispatch.end', [self::class, 'end']);
        Server::register('callable.invoke', [self::class, 'invoke']);
    }

    /**
     * `listener.call`: `$callable($event)` after is_callable().
     *
     * @param array<string, mixed> $a
     * @return array<string, mixed>
     */
    public static function listener(array $a): array
    {
        $callable = Listeners::callable($a['h']);
        if (!is_callable($callable)) {
            return ['status' => 'notCallable'];
        }
        Rpc::call('dispatch.before');

        $return = $callable($a['event']);

        return ['status' => 'ok', 'returnedFalse' => false === $return];
    }

    /**
     * `script.php`: `$className::$methodName($event)` after class_exists()
     * and is_callable().
     *
     * @param array<string, mixed> $a
     * @return array<string, mixed>
     */
    public static function phpScript(array $a): array
    {
        $className = $a['class'];
        $methodName = $a['method'];
        if (!class_exists($className)) {
            return ['status' => 'notAutoloadable'];
        }
        if (!is_callable($className.'::'.$methodName)) {
            return ['status' => 'notCallable'];
        }
        Rpc::call('dispatch.before');

        $return = $className::$methodName($a['event']);

        return ['status' => 'ok', 'returnedFalse' => false === $return];
    }

    /**
     * `script.commandClass`: runs a Symfony command class as Composer's
     * doDispatch() does, in a fresh Application writing to the run's
     * output.
     *
     * @param array<string, mixed> $a
     * @return array<string, mixed>
     */
    public static function commandClass(array $a): array
    {
        $className = $a['class'];
        $event = $a['event'];
        if (!class_exists($className)) {
            return ['status' => 'notAutoloadable'];
        }
        if (!is_a($className, Command::class, true)) {
            return ['status' => 'notCommand'];
        }
        if (!Rpc::call('dispatch.before')) {
            return ['status' => 'skipped'];
        }

        $app = new Application();
        $app->setCatchExceptions(false);
        if (method_exists($app, 'setCatchErrors')) {
            $app->setCatchErrors(false);
        }
        $app->setAutoExit(false);
        $cmd = new $className($event->getName());
        $app->add($cmd);
        $app->setDefaultCommand((string) $cmd->getName(), true);

        // reusing the output from $this->io is mostly needed for tests, but generally speaking
        // it does not hurt to keep the same stream as the current Application
        $output = isset($a['output']) ? $a['output'] : new ConsoleOutput();

        $return = $app->run(new StringInput($a['input']), $output);

        return ['status' => 'ok', 'code' => $return];
    }

    /**
     * `autoload.install`: makeAutoloader's loader replaces the previous
     * one (createLoader() then register(false); `files` are not
     * required). The parts `same` names are the previous loader's, which
     * maestro does not send again.
     *
     * @param array<string, mixed> $a
     */
    public static function installAutoloader(array $a): void
    {
        foreach (isset($a['same']) ? $a['same'] : [] as $key) {
            $a[$key] = self::$loaderContents[$key];
        }
        unset($a['same']);
        if (self::$loader !== null) {
            self::$loader->unregister();
        }

        $loader = new ClassLoader($a['vendorDir']);
        if (isset($a['psr0'])) {
            foreach ($a['psr0'] as $namespace => $path) {
                $loader->add($namespace, $path);
            }
        }
        if (isset($a['psr4'])) {
            foreach ($a['psr4'] as $namespace => $path) {
                $loader->addPsr4($namespace, $path);
            }
        }
        if (isset($a['classmap'])) {
            $loader->addClassMap($a['classmap']);
        }

        self::$loader = $loader;
        $loader->register(false);
        // what maestro takes PHP to have once this returns
        self::$loaderContents = $a;
    }

    /**
     * `dispatch.begin`: doDispatch() remembers the autoloaders before its
     * first listener.
     *
     * @param array<string, mixed> $a
     */
    public static function begin(array $a): void
    {
        self::$autoloadersBefore[(int) $a['depth']] = spl_autoload_functions();
    }

    /**
     * `dispatch.end`: doDispatch()'s finally: autoloaders prepended
     * meanwhile are appended instead, so Composer's classes load first.
     *
     * @param array<string, mixed> $a
     */
    public static function end(array $a): void
    {
        $depth = (int) $a['depth'];
        $autoloadersBefore = isset(self::$autoloadersBefore[$depth]) ? self::$autoloadersBefore[$depth] : [];
        unset(self::$autoloadersBefore[$depth]);

        $knownIdentifiers = [];
        foreach ($autoloadersBefore as $key => $cb) {
            $knownIdentifiers[self::getCallbackIdentifier($cb)] = ['key' => $key, 'callback' => $cb];
        }
        foreach (spl_autoload_functions() as $cb) {
            // once we get to the first known autoloader, we can leave any appended autoloader without problems
            if (isset($knownIdentifiers[self::getCallbackIdentifier($cb)]) && $knownIdentifiers[self::getCallbackIdentifier($cb)]['key'] === 0) {
                break;
            }

            // other newly appeared prepended autoloaders should be appended instead to ensure Composer loads its classes first
            if ($cb instanceof ClassLoader) {
                $cb->unregister();
                $cb->register(false);
            } else {
                spl_autoload_unregister($cb);
                spl_autoload_register($cb);
            }
        }
    }

    /**
     * `callable.invoke`: calls a PHP callable maestro holds (a validator).
     *
     * @param array<string, mixed> $a
     * @return mixed
     */
    public static function invoke(array $a)
    {
        $callable = is_object($a['callable']) ? Listeners::callable($a['callable']) : $a['callable'];

        return $callable(...array_values($a['args']));
    }

    /**
     * @param mixed $cb
     */
    private static function getCallbackIdentifier($cb): string
    {
        if (is_string($cb)) {
            return 'fn:'.$cb;
        }
        if (is_object($cb)) {
            return 'obj:'.spl_object_id($cb);
        }
        if (is_array($cb)) {
            return 'array:'.(is_string($cb[0]) ? $cb[0] : get_class($cb[0]) .'#'.spl_object_id($cb[0])).'::'.$cb[1];
        }

        // not great but also do not want to break everything here
        return 'unsupported';
    }
}
