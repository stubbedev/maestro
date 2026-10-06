<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.12). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * Backtrace frames (docs/PLUGINS.md §5.12): the method calls that would be
 * on Composer's PHP call stack when maestro runs plugin code (the
 * Application's doRun() with its input and output, the running command's
 * initialize(), interact(), execute() or run(), the Installer's run(), the
 * PluginManager's loading), which symfony/flex, php-http/discovery and
 * symfony/thanks look for with debug_backtrace().
 *
 * A call carrying `frames` runs its handler inside them: each frame is
 * entered by calling Composer's method itself on its object with its
 * arguments, so that debug_backtrace() shows Composer's `class`,
 * `function`, `object` and `args`. The shim's implementation of the method
 * starts with Frames::resumes(), which runs the rest of the call there
 * (the inner frames, then the handler) instead of the method's work; the
 * bundled Symfony Console's doRun(), doRunCommand() and Command::run() do
 * so as SymfonyHooks loads them, their files left as Composer's. Frames of
 * methods that do not resume are left out: every frame PHP shows is one
 * of Composer's.
 */
final class Frames
{
    /**
     * Composer's methods whose shim implementation resumes a frame, by
     * "Class->method"; with the methods of maestro's own commands
     * (Console::builtin()).
     */
    private const ENTERED = [
        'Composer\\Console\\Application->run' => true,
        'Composer\\Console\\Application->doRun' => true,
        'Composer\\Console\\Application->getComposer' => true,
        'Composer\\Console\\Application->getPluginCommands' => true,
        // the bundled Console's, which resume as SymfonyHooks loads them
        'Symfony\\Component\\Console\\Application->run' => true,
        'Symfony\\Component\\Console\\Application->doRun' => true,
        'Symfony\\Component\\Console\\Application->doRunCommand' => true,
        'Symfony\\Component\\Console\\Command\\Command->run' => true,
        'Composer\\Installer->run' => true,
        'Composer\\Installer->doUpdate' => true,
        'Composer\\Installer->doInstall' => true,
        'Composer\\Plugin\\PluginManager->loadInstalledPlugins' => true,
        'Composer\\Plugin\\PluginManager->loadRepository' => true,
        'Composer\\Plugin\\PluginManager->registerPackage' => true,
    ];

    /** The methods of maestro's commands that Console::builtin() runs. */
    private const COMMAND_METHODS = ['initialize' => true, 'interact' => true, 'execute' => true, 'run' => true];

    /**
     * The frames being entered, innermost last: the object, the method,
     * the rest of the call, whether it ran and its result.
     *
     * @var list<array{0: object, 1: string, 2: callable(): mixed, 3: bool, 4: mixed}>
     */
    private static $pending = [];

    /**
     * Runs $fn inside the frames, outermost first.
     *
     * @param list<array{0: object, 1?: list<mixed>, 2?: string}> $frames
     * @param callable(): mixed $fn
     * @return mixed
     */
    public static function run(array $frames, callable $fn)
    {
        $result = self::enter(array_values($frames), 0, $fn);
        self::reportAddedCommands($frames);

        return $result;
    }

    /**
     * @param list<array{0: object, 1?: list<mixed>, 2?: string}> $frames
     * @param callable(): mixed $fn
     * @return mixed
     */
    public static function enter(array $frames, int $i, callable $fn)
    {
        if (!isset($frames[$i])) {
            return $fn();
        }
        $object = $frames[$i][0];
        $args = isset($frames[$i][1]) && is_array($frames[$i][1]) ? array_values($frames[$i][1]) : [];
        $function = isset($frames[$i][2]) ? (string) $frames[$i][2] : '';
        $next = static function () use ($frames, $i, $fn) {
            return Frames::enter($frames, $i + 1, $fn);
        };

        $method = is_object($object) ? self::method($object, $function) : null;
        if ($method === null) {
            return $next();
        }

        self::$pending[] = [$object, $method->getName(), $next, false, null];
        $k = count(self::$pending) - 1;
        try {
            $method->getClosure($object)(...$args);
        } catch (\Throwable $e) {
            $resumed = self::$pending[$k][3];
            array_splice(self::$pending, $k);
            if ($resumed) {
                throw $e;
            }

            // the method failed before resuming (its arguments): the rest
            // of the call runs without the frame
            return $next();
        }
        $entry = self::$pending[$k];
        array_splice(self::$pending, $k);

        return $entry[3] ? $entry[4] : $next();
    }

    /**
     * Called first by the shim's implementation of a method a frame
     * enters: when the innermost frame being entered is this call, runs
     * the rest of the call, keeping its result for enter(), and returns
     * true; the method then returns at once. What the rest of the call
     * throws goes through the method, as in Composer.
     *
     * @param object $object
     */
    public static function resumes($object, string $method): bool
    {
        $k = count(self::$pending) - 1;
        if ($k < 0 || self::$pending[$k][3] || self::$pending[$k][0] !== $object || self::$pending[$k][1] !== $method) {
            return false;
        }
        self::$pending[$k][3] = true;
        $result = (self::$pending[$k][2])();
        self::$pending[$k][4] = $result;

        return true;
    }

    /**
     * The method a frame enters: Composer's "Class->method", when the
     * shim's implementation resumes it (ENTERED, or a method of one of
     * maestro's commands) and $object is of that class.
     *
     * @param object $object
     */
    private static function method($object, string $function): ?\ReflectionMethod
    {
        $pos = strpos($function, '->');
        if ($pos === false) {
            return null;
        }
        $class = substr($function, 0, $pos);
        $name = substr($function, $pos + 2);
        $command = strpos($class, 'Composer\\Command\\') === 0 && isset(self::COMMAND_METHODS[$name]) && Remote::owned($object);
        if (!isset(self::ENTERED[$function]) && !$command) {
            return null;
        }
        if (!$object instanceof $class || !method_exists($class, $name)) {
            return null;
        }
        $method = new \ReflectionMethod($class, $name);
        if ($method->getDeclaringClass()->getName() !== $class || $method->isStatic()) {
            return null;
        }
        if (PHP_VERSION_ID < 80100) {
            $method->setAccessible(true);
        }

        return $method;
    }

    /**
     * Commands plugin code added to maestro's running Application with
     * Symfony's add() (flex and thanks do it from activate()): maestro's
     * Application registers them right away, as Composer's has them.
     *
     * @param list<array{0: object, 1?: list<mixed>, 2?: string}> $frames
     */
    private static function reportAddedCommands(array $frames): void
    {
        foreach ($frames as $frame) {
            $app = $frame[0];
            if ($app instanceof \Composer\Console\Application && Remote::owned($app)) {
                $added = Console::addedCommands($app);
                if ($added !== []) {
                    Rpc::call('app.added', [$app, $added]);
                }
            }
        }
    }
}
