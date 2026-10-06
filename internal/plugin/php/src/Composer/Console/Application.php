<?php

/*
 * maestro's plugin shim: Composer\Console\Application (docs/PLUGINS.md
 * §4.11, §5.11). maestro's running Application crosses as an instance of
 * this class; `new Application()` in PHP creates a new one in maestro.
 * Either way the Symfony part is PHP's (find(), has(), all(), add(),
 * getDefinition(), setAutoExit() ...), over maestro's commands
 * (getDefaultCommands()), and running it (run(), doRun()) runs maestro's
 * Application, re-entrantly; with auto-exit PHP then exits with its code.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Console;

use Maestro\Shim\Console;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;
use Symfony\Component\Console\Input\ArgvInput;
use Symfony\Component\Console\Input\InputDefinition;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Output\OutputInterface;

class Application extends \Symfony\Component\Console\Application
{
    private static $logo = '   ______
  / ____/___  ____ ___  ____  ____  ________  _____
 / /   / __ \/ __ `__ \/ __ \/ __ \/ ___/ _ \/ ___/
/ /___/ /_/ / / / / / / /_/ / /_/ (__  )  __/ /
\____/\____/_/ /_/ /_/ .___/\____/____/\___/_/
                    /_/
';

    protected $composer;
    protected $io;

    public function __construct(string $name = 'Composer', string $version = '')
    {
        if (method_exists($this, 'setCatchErrors')) {
            $this->setCatchErrors(true);
        }

        if ($version === '') {
            $version = \Composer\Composer::getVersion();
        }
        if (function_exists('ini_set') && extension_loaded('xdebug')) {
            ini_set('xdebug.show_exception_trace', '0');
            ini_set('xdebug.scream', '0');
        }

        if (function_exists('date_default_timezone_set') && function_exists('date_default_timezone_get')) {
            date_default_timezone_set(\Composer\Util\Silencer::call('date_default_timezone_get'));
        }

        $this->io = new \Composer\IO\NullIO();

        // Composer's out-of-memory hint is registered once per process, by
        // the Application maestro runs (bootstrap.php).

        parent::__construct($name, $version);

        Rpc::call('app.new', [$this, $name, $version]);
    }

    public function __destruct()
    {
    }

    public function doRun(\Symfony\Component\Console\Input\InputInterface $input, \Symfony\Component\Console\Output\OutputInterface $output): int
    {
        // maestro's doRun(), as a frame of Composer's stack
        // (docs/PLUGINS.md §5.12)
        if (\Maestro\Shim\Frames::resumes($this, __FUNCTION__)) {
            return 0;
        }

        return Rpc::call('app.doRun', [$this, Console::inputValue($input), Console::outputValue($output), Console::addedCommands($this)]);
    }

    public function getComposer(bool $required = true, ?bool $disablePlugins = null, ?bool $disableScripts = null): ?\Composer\Composer
    {
        $this->composer = Rpc::call('app.getComposer', [$this, $required, $disablePlugins, $disableScripts]);

        return $this->composer;
    }

    protected function getDefaultCommands(): array
    {
        return Console::applicationCommands($this);
    }

    protected function getDefaultInputDefinition(): \Symfony\Component\Console\Input\InputDefinition
    {
        $definition = parent::getDefaultInputDefinition();
        $definition->addOption(new InputOption('--profile', null, InputOption::VALUE_NONE, 'Display timing and memory usage information'));
        $definition->addOption(new InputOption('--no-plugins', null, InputOption::VALUE_NONE, 'Whether to disable plugins.'));
        $definition->addOption(new InputOption('--no-scripts', null, InputOption::VALUE_NONE, 'Skips the execution of all scripts defined in composer.json file.'));
        $definition->addOption(new InputOption('--working-dir', '-d', InputOption::VALUE_REQUIRED, 'If specified, use the given directory as working directory.'));
        $definition->addOption(new InputOption('--no-cache', null, InputOption::VALUE_NONE, 'Prevent use of the cache'));

        return $definition;
    }

    public function getDisablePluginsByDefault(): bool
    {
        return Rpc::call('app.getDisablePluginsByDefault', [$this]);
    }

    public function getDisableScriptsByDefault(): bool
    {
        return Rpc::call('app.getDisableScriptsByDefault', [$this]);
    }

    public function getHelp(): string
    {
        return self::$logo . parent::getHelp();
    }

    public function getIO(): \Composer\IO\IOInterface
    {
        $this->io = Rpc::call('app.getIO', [$this]);

        return $this->io;
    }

    public function getInitialWorkingDirectory()
    {
        return Rpc::call('app.getInitialWorkingDirectory', [$this]);
    }

    public function getLongVersion(): string
    {
        $branchAliasString = '';
        if (\Composer\Composer::BRANCH_ALIAS_VERSION && \Composer\Composer::BRANCH_ALIAS_VERSION !== '@package_branch_alias_version'.'@') {
            $branchAliasString = sprintf(' (%s)', \Composer\Composer::BRANCH_ALIAS_VERSION);
        }

        return sprintf(
            '<info>%s</info> version <comment>%s%s</comment> %s',
            $this->getName(),
            $this->getVersion(),
            $branchAliasString,
            \Composer\Composer::RELEASE_DATE
        );
    }

    public function resetComposer(): void
    {
        $this->composer = null;
        Rpc::call('app.resetComposer', [$this]);
    }

    public function run(?\Symfony\Component\Console\Input\InputInterface $input = null, ?\Symfony\Component\Console\Output\OutputInterface $output = null): int
    {
        if (null === $output) {
            $output = \Composer\Factory::createOutput();
        }
        if (null === $input) {
            $input = new ArgvInput();
        }

        $state = Remote::read($this, \Symfony\Component\Console\Application::class, ['autoExit', 'catchExceptions']);
        $exitCode = Rpc::call('app.run', [$this, Console::inputValue($input), Console::outputValue($output), $state['autoExit'], $state['catchExceptions'], Console::addedCommands($this)]);

        if ($state['autoExit']) {
            if ($exitCode > 255) {
                $exitCode = 255;
            }

            exit($exitCode);
        }

        return $exitCode;
    }
}
