<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

use Composer\Autoload\ClassLoader;
use Composer\Command\BaseCommand;
use Composer\Downloader\DownloaderInterface;
use Composer\Filter\PlatformRequirementFilter\PlatformRequirementFilterInterface;
use Composer\IO\IOInterface;
use Composer\Repository\RepositoryInterface;
use Symfony\Component\Console\Application as SymfonyApplication;
use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\Completion\CompletionInput;
use Symfony\Component\Console\Completion\CompletionSuggestions;
use Symfony\Component\Console\Input\ArgvInput;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\StringInput;
use Symfony\Component\Console\Output\ConsoleOutputInterface;
use Symfony\Component\Console\Output\OutputInterface;
use Symfony\Component\Console\SingleCommandApplication;

/**
 * The PHP half of commands and the Application (docs/PLUGINS.md §5.7,
 * §5.11): maestro's Application lists, describes and dispatches the
 * commands written in PHP from their descriptions, and runs them here, on
 * the vendored Symfony Console.
 */
final class Console
{
    /**
     * The commands each Application object (spl_object_id) got from
     * maestro or gave it (by spl_object_id): the others were added in PHP.
     *
     * @var array<int, array<int, true>>
     */
    private static $known = [];

    /** @var Adapter\InputAdapter */
    private static $inputAdapter;

    public static function register(): void
    {
        Server::register('capability.commands', [self::class, 'pluginCommands']);
        Server::register('command.run', [self::class, 'run']);
        Server::register('command.complete', [self::class, 'complete']);
        Server::register('command.script', [self::class, 'scriptCommand']);
        Server::register('autoload.register', [self::class, 'registerLoader']);
        Server::register('object.call', [self::class, 'call']);
        Server::register('object.promise', [self::class, 'callPromise']);
        Server::register('object.new', [self::class, 'create']);
        self::$inputAdapter = new Adapter\InputAdapter();
        Mirrors::register(self::$inputAdapter);
        Mirrors::register(new Adapter\OutputAdapter());
        Mirrors::register(new Adapter\CommandAdapter());
        Mirrors::register(new Adapter\ApplicationAdapter());
    }

    /**
     * `capability.commands`: Application::getPluginCommands(), the commands
     * of every CommandProvider capability with Composer's checks, as
     * descriptions.
     *
     * @param array<string, mixed> $a
     * @return list<array<string, mixed>>
     */
    public static function pluginCommands(array $a): array
    {
        $composer = $a['composer'];
        $commands = [];

        $pm = $composer->getPluginManager();
        foreach ($pm->getPluginCapabilities('Composer\Plugin\Capability\CommandProvider', ['composer' => $composer, 'io' => $a['io']]) as $capability) {
            $newCommands = $capability->getCommands();
            if (!is_array($newCommands)) {
                throw new \UnexpectedValueException('Plugin capability '.get_class($capability).' failed to return an array from getCommands');
            }
            foreach ($newCommands as $command) {
                if (!$command instanceof BaseCommand) {
                    throw new \UnexpectedValueException('Plugin capability '.get_class($capability).' returned an invalid value, we expected an array of Composer\Command\BaseCommand objects');
                }
            }
            $commands = array_merge($commands, $newCommands);
        }

        return array_map([self::class, 'describe'], array_values($commands));
    }

    /**
     * What maestro's Application needs of a command written in PHP to
     * list, describe and complete it.
     *
     * @return array<string, mixed>
     */
    public static function describe(Command $command): array
    {
        $props = Remote::read($command, Command::class, ['ignoreValidationErrors', 'processTitle']);

        return [
            'object' => $command,
            'class' => get_class($command),
            'base' => $command instanceof BaseCommand,
            'name' => $command->getName(),
            'aliases' => array_values($command->getAliases()),
            'description' => $command->getDescription(),
            'help' => $command->getHelp(),
            'hidden' => $command->isHidden(),
            'enabled' => $command->isEnabled(),
            'proxy' => $command instanceof BaseCommand && $command->isProxyCommand(),
            'ignoreValidationErrors' => $props['ignoreValidationErrors'],
            'processTitle' => $props['processTitle'],
            'usages' => array_values($command->getUsages()),
            'definition' => Definitions::describe($command->getNativeDefinition()),
        ];
    }

    /**
     * `command.run`: `$command->run($input, $output)` for maestro's
     * Application, the command attached to it and named as it names it.
     *
     * @param array<string, mixed> $a
     */
    public static function run(array $a): int
    {
        /** @var Command $command */
        $command = $a['command'];
        self::attach($command, $a['app']);
        if ($a['name'] !== null && $command->getName() !== $a['name']) {
            $command->setName($a['name']);
        }
        if ($command->getDescription() !== $a['description']) {
            $command->setDescription($a['description']);
        }

        return $command->run($a['input'], $a['output']);
    }

    /**
     * `command.complete`: `$command->complete()` for the command line maestro
     * completes; the suggested values and option names.
     *
     * @param array<string, mixed> $a
     * @return array<string, list<string>>
     */
    public static function complete(array $a): array
    {
        /** @var Command $command */
        $command = $a['command'];
        self::attach($command, $a['app']);

        $input = CompletionInput::fromTokens(array_values($a['tokens']), (int) $a['index']);
        $command->mergeApplicationDefinition();
        $input->bind($command->getDefinition());

        $suggestions = new CompletionSuggestions();
        $command->complete($input, $suggestions);

        $values = [];
        foreach ($suggestions->getValueSuggestions() as $suggestion) {
            $values[] = (string) $suggestion->getValue();
        }
        $options = [];
        foreach ($suggestions->getOptionSuggestions() as $option) {
            $options[] = $option->getName();
        }

        return ['values' => $values, 'options' => $options];
    }

    /**
     * `command.script`: the command of a composer.json script naming a
     * Symfony Command class (Application::doRun), or null.
     *
     * @param array<string, mixed> $a
     * @return array<string, mixed>|null
     */
    public static function scriptCommand(array $a): ?array
    {
        $dummy = $a['class'];
        $script = $a['script'];
        if (!(class_exists($dummy) && is_subclass_of($dummy, Command::class))) {
            return null;
        }
        if (is_subclass_of($dummy, SingleCommandApplication::class)) {
            $a['io']->writeError('<warning>The script named '.$script.' extends SingleCommandApplication which is not compatible with Composer 2.9+, make sure you extend Symfony\Component\Console\Command instead.</warning>');
        }

        return self::describe(new $dummy($script));
    }

    /**
     * A method of one of maestro's own commands (an instance of its
     * Composer class that maestro's Application holds) run from PHP: the
     * hooks Symfony's Command::run() calls on it ($app->find('install')
     * ->run($input, $output) in a plugin) and the commands' own run(),
     * isProxyCommand() and complete(). maestro runs its command's (`builtin.*`,
     * docs/PLUGINS.md §5.7), on the input and output it is given.
     *
     * @param list<mixed> $args
     * @return mixed
     */
    public static function builtin(Command $command, string $method, array $args)
    {
        switch ($method) {
            case 'initialize':
                return Rpc::call('builtin.initialize', [$command, self::inputValue($args[0]), self::outputValue($args[1])]);
            case 'interact':
                return Rpc::call('builtin.interact', [$command, self::inputValue($args[0]), self::outputValue($args[1])]);
            case 'execute':
                return Rpc::call('builtin.execute', [$command, self::inputValue($args[0]), self::outputValue($args[1])]);
            case 'run':
                return Rpc::call('builtin.run', [$command, self::inputValue($args[0]), self::outputValue($args[1])]);
            case 'isProxyCommand':
                return Rpc::call('builtin.isProxyCommand', [$command]);
            case 'complete':
                /** @var CompletionInput $input */
                $input = $args[0];
                /** @var CompletionSuggestions $suggestions */
                $suggestions = $args[1];
                $state = Remote::read($input, CompletionInput::class, ['tokens', 'currentIndex']);
                $res = Rpc::call('builtin.complete', [$command, array_values($state['tokens']), $state['currentIndex']]);
                $definition = $command->getDefinition();
                $options = [];
                foreach ($res['options'] as $name) {
                    if ($definition->hasOption($name)) {
                        $options[] = $definition->getOption($name);
                    }
                }
                if ($options !== []) {
                    $suggestions->suggestOptions($options);
                }
                if ($res['values'] !== []) {
                    $suggestions->suggestValues($res['values']);
                }

                return null;
        }

        throw new ProtocolException('maestro shim: no command method '.$method);
    }

    /**
     * `autoload.register`: registers a class loader (the project's, which
     * Composer's Application registers before looking for script commands).
     *
     * @param array<string, mixed> $a
     */
    public static function registerLoader(array $a): void
    {
        $loader = new ClassLoader($a['vendorDir']);
        foreach (isset($a['psr0']) ? $a['psr0'] : [] as $namespace => $path) {
            $loader->add($namespace, $path);
        }
        foreach (isset($a['psr4']) ? $a['psr4'] : [] as $namespace => $path) {
            $loader->addPsr4($namespace, $path);
        }
        if (isset($a['classmap'])) {
            $loader->addClassMap($a['classmap']);
        }
        $loader->register(false);
    }

    /**
     * `object.call`: a method of a PHP output maestro writes to.
     *
     * @param array<string, mixed> $a
     * @return mixed
     */
    public static function call(array $a)
    {
        $object = $a['object'];
        $method = $a['method'];
        if (!self::callable($object, $method)) {
            throw new ProtocolException('maestro shim: maestro cannot call '.get_class($object).'::'.$method.'()');
        }

        return $object->$method(...array_values($a['args']));
    }

    /**
     * `object.promise`: a method returning ?PromiseInterface of a PHP
     * downloader maestro uses; the promise is handed over as
     * Promises::watch() does.
     *
     * @param array<string, mixed> $a
     * @return array{id: int, s: string}|null
     */
    public static function callPromise(array $a)
    {
        $promise = self::call($a);

        return $promise === null ? null : Promises::watch($promise);
    }

    /**
     * `object.new`: `new $class(...$args)` of a repository class a plugin
     * registered (RepositoryManager::setRepositoryClass()).
     *
     * @param array<string, mixed> $a
     * @return object
     */
    public static function create(array $a)
    {
        $class = $a['class'];
        if (!is_string($class) || !class_exists($class) || !is_subclass_of($class, RepositoryInterface::class)) {
            throw new \InvalidArgumentException('Repository class '.(is_string($class) ? $class : gettype($class)).' does not exist or is not a repository');
        }

        return new $class(...array_values($a['args']));
    }

    /**
     * The methods maestro calls on PHP objects it uses: outputs it writes
     * to, IOs, platform requirement filters, repositories and downloaders
     * created in PHP.
     *
     * @param object $object
     */
    private static function callable($object, string $method): bool
    {
        if ($object instanceof OutputInterface) {
            return in_array($method, ['write', 'setVerbosity', 'getVerbosity', 'setDecorated', 'isDecorated'], true);
        }
        if ($object instanceof RepositoryInterface) {
            return in_array($method, ['getRepoName', 'hasPackage', 'findPackage', 'findPackages', 'getPackages', 'loadPackages', 'search', 'getProviders', 'count'], true);
        }
        if ($object instanceof IOInterface) {
            return in_array($method, [
                'isInteractive', 'isVerbose', 'isVeryVerbose', 'isDebug', 'isDecorated',
                'write', 'writeError', 'writeRaw', 'writeErrorRaw', 'overwrite', 'overwriteError',
                'ask', 'askConfirmation', 'askAndValidate', 'askAndHideAnswer', 'select',
                'getAuthentications', 'hasAuthentication', 'getAuthentication', 'setAuthentication', 'loadConfiguration',
                'emergency', 'alert', 'critical', 'error', 'warning', 'notice', 'info', 'debug', 'log',
            ], true);
        }
        if ($object instanceof PlatformRequirementFilterInterface) {
            return in_array($method, ['isIgnored', 'isUpperBoundIgnored'], true);
        }
        if ($object instanceof DownloaderInterface) {
            return in_array($method, ['getInstallationSource', 'download', 'prepare', 'install', 'update', 'remove', 'cleanup'], true);
        }

        return false;
    }

    /**
     * An input as maestro takes it: maestro's own, or the description of
     * one created in PHP.
     *
     * @return InputInterface|array<string, mixed>
     */
    public static function inputValue(InputInterface $input)
    {
        if (Remote::owned($input)) {
            return $input;
        }
        if (($input instanceof ArgvInput || $input instanceof ArrayInput) && Handles::lookup($input) === null) {
            // maestro adopts it as it crosses: binding it binds this object.
            Mirrors::adopt($input, self::$inputAdapter);

            return $input;
        }
        if ($input instanceof ArrayInput) {
            $d = ['kind' => 'array', 'parameters' => Remote::read($input, ArrayInput::class, ['parameters'])['parameters']];
        } elseif ($input instanceof ArgvInput) {
            $d = ['kind' => $input instanceof StringInput ? 'string' : 'argv', 'tokens' => Remote::read($input, ArgvInput::class, ['tokens'])['tokens']];
        } else {
            throw new UnsupportedApiException('maestro does not support running Composer with a '.get_class($input).' in plugins yet');
        }
        $d['interactive'] = $input->isInteractive();
        $d['object'] = $input;

        return $d;
    }

    /**
     * An output as maestro takes it: maestro's own, or the description of
     * one created in PHP (which maestro then writes to).
     *
     * @return OutputInterface|array<string, mixed>
     */
    public static function outputValue(OutputInterface $output)
    {
        if (Remote::owned($output)) {
            return $output;
        }
        $d = ['object' => $output, 'verbosity' => $output->getVerbosity(), 'decorated' => $output->isDecorated()];
        if ($output instanceof ConsoleOutputInterface) {
            $error = $output->getErrorOutput();
            $d['error'] = ['object' => $error, 'verbosity' => $error->getVerbosity(), 'decorated' => $error->isDecorated()];
        }

        return $d;
    }

    /**
     * The commands added to an Application in PHP (Symfony's add()) that
     * maestro does not have yet, as descriptions.
     *
     * @return list<array<string, mixed>>
     */
    public static function addedCommands(SymfonyApplication $app): array
    {
        $appId = spl_object_id($app);
        $added = [];
        foreach (Remote::read($app, SymfonyApplication::class, ['commands'])['commands'] as $command) {
            $id = spl_object_id($command);
            if (isset(self::$known[$appId][$id]) || Remote::owned($command)) {
                continue;
            }
            self::$known[$appId][$id] = true;
            $added[] = self::describe($command);
        }

        return $added;
    }

    /**
     * The commands of a maestro Application (its getDefaultCommands()).
     *
     * @return list<Command>
     */
    public static function applicationCommands(SymfonyApplication $app): array
    {
        $commands = Rpc::call('app.commands', [$app]);
        foreach ($commands as $command) {
            self::$known[spl_object_id($app)][spl_object_id($command)] = true;
        }

        return $commands;
    }

    /**
     * Attaches a command to the Application maestro runs it in.
     *
     * @param SymfonyApplication|null $app
     */
    private static function attach(Command $command, $app): void
    {
        if ($app !== null && Remote::read($command, Command::class, ['application'])['application'] !== $app) {
            $command->setApplication($app);
        }
    }
}
