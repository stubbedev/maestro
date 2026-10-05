<?php

/*
 * maestro's plugin shim: Composer\Command\BaseCommand, reimplemented with
 * Composer 2.10.3's behaviour (docs/PLUGINS.md §4.11) for commands run in
 * PHP: the Composer instance it holds (setComposer() or its Application's),
 * its IO, and initialize() with the PRE_COMMAND_RUN event and the COMPOSER_*
 * option variables. Running plugin commands in maestro's Application comes
 * with phase 4.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Command;

abstract class BaseCommand extends \Symfony\Component\Console\Command\Command
{
    private $composer;

    private $io;

    public function complete(\Symfony\Component\Console\Completion\CompletionInput $input, \Symfony\Component\Console\Completion\CompletionSuggestions $suggestions): void
    {
        $definition = $this->getDefinition();
        $name = (string) $input->getCompletionName();
        if (\Symfony\Component\Console\Completion\CompletionInput::TYPE_OPTION_VALUE === $input->getCompletionType()
            && $definition->hasOption($name)
            && ($option = $definition->getOption($name)) instanceof \Composer\Console\Input\InputOption
        ) {
            $option->complete($input, $suggestions);
        } elseif (\Symfony\Component\Console\Completion\CompletionInput::TYPE_ARGUMENT_VALUE === $input->getCompletionType()
            && $definition->hasArgument($name)
            && ($argument = $definition->getArgument($name)) instanceof \Composer\Console\Input\InputArgument
        ) {
            $argument->complete($input, $suggestions);
        } else {
            parent::complete($input, $suggestions);
        }
    }

    protected function createAuditConfig(\Symfony\Component\Console\Input\InputInterface $input): \Composer\Advisory\AuditConfig
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Command\\BaseCommand::createAuditConfig() in plugins yet');
    }

    protected function createComposerInstance(\Symfony\Component\Console\Input\InputInterface $input, \Composer\IO\IOInterface $io, $config = null, ?bool $disablePlugins = null, ?bool $disableScripts = null): \Composer\Composer
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Command\\BaseCommand::createComposerInstance() in plugins yet');
    }

    protected function createPolicyConfig(\Composer\Config $config, ?\Symfony\Component\Console\Input\InputInterface $input): \Composer\Policy\PolicyConfig
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Command\\BaseCommand::createPolicyConfig() in plugins yet');
    }

    protected function formatRequirements(array $requirements)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Command\\BaseCommand::formatRequirements() in plugins yet');
    }

    public function getApplication(): \Composer\Console\Application
    {
        $application = parent::getApplication();
        if (!$application instanceof \Composer\Console\Application) {
            throw new \RuntimeException('Composer commands can only work with an '.\Composer\Console\Application::class.' instance set');
        }

        return $application;
    }

    protected function getAuditFormat(\Symfony\Component\Console\Input\InputInterface $input, string $optName = 'audit-format'): string
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Command\\BaseCommand::getAuditFormat() in plugins yet');
    }

    public function getComposer(bool $required = true, ?bool $disablePlugins = null, ?bool $disableScripts = null)
    {
        if ($required) {
            return $this->requireComposer($disablePlugins, $disableScripts);
        }

        return $this->tryComposer($disablePlugins, $disableScripts);
    }

    public function getIO()
    {
        if (null === $this->io) {
            $application = parent::getApplication();
            if ($application instanceof \Composer\Console\Application) {
                $this->io = $application->getIO();
            } else {
                $this->io = new \Composer\IO\NullIO();
            }
        }

        return $this->io;
    }

    protected function getPlatformRequirementFilter(\Symfony\Component\Console\Input\InputInterface $input): \Composer\Filter\PlatformRequirementFilter\PlatformRequirementFilterInterface
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Command\\BaseCommand::getPlatformRequirementFilter() in plugins yet');
    }

    protected function getPreferredInstallOptions(\Composer\Config $config, \Symfony\Component\Console\Input\InputInterface $input, bool $keepVcsRequiresPreferSource = false)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Command\\BaseCommand::getPreferredInstallOptions() in plugins yet');
    }

    protected function getTerminalWidth()
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Command\\BaseCommand::getTerminalWidth() in plugins yet');
    }

    protected function initialize(\Symfony\Component\Console\Input\InputInterface $input, \Symfony\Component\Console\Output\OutputInterface $output): void
    {
        // initialize a plugin-enabled Composer instance, either local or global
        $disablePlugins = $input->hasParameterOption('--no-plugins');
        $disableScripts = $input->hasParameterOption('--no-scripts');

        $application = parent::getApplication();
        if ($application instanceof \Composer\Console\Application && $application->getDisablePluginsByDefault()) {
            $disablePlugins = true;
        }
        if ($application instanceof \Composer\Console\Application && $application->getDisableScriptsByDefault()) {
            $disableScripts = true;
        }

        if ($this instanceof SelfUpdateCommand) {
            $disablePlugins = true;
            $disableScripts = true;
        }

        $composer = $this->tryComposer($disablePlugins, $disableScripts);
        $io = $this->getIO();

        if (null === $composer) {
            $composer = \Composer\Factory::createGlobal($this->getIO(), $disablePlugins, $disableScripts);
        }
        if ($composer) {
            $preCommandRunEvent = new \Composer\Plugin\PreCommandRunEvent(\Composer\Plugin\PluginEvents::PRE_COMMAND_RUN, $input, $this->getName());
            $composer->getEventDispatcher()->dispatch($preCommandRunEvent->getName(), $preCommandRunEvent);
        }

        if (true === $input->hasParameterOption(['--no-ansi']) && $input->hasOption('no-progress')) {
            $input->setOption('no-progress', true);
        }

        $envOptions = [
            'COMPOSER_NO_AUDIT' => ['no-audit'],
            'COMPOSER_NO_DEV' => ['no-dev', 'update-no-dev'],
            'COMPOSER_PREFER_STABLE' => ['prefer-stable'],
            'COMPOSER_PREFER_LOWEST' => ['prefer-lowest'],
            'COMPOSER_MINIMAL_CHANGES' => ['minimal-changes'],
            'COMPOSER_WITH_DEPENDENCIES' => ['with-dependencies'],
            'COMPOSER_WITH_ALL_DEPENDENCIES' => ['with-all-dependencies'],
            'COMPOSER_NO_SECURITY_BLOCKING' => ['no-security-blocking'],
            'COMPOSER_NO_BLOCKING' => ['no-blocking'],
        ];
        foreach ($envOptions as $envName => $optionNames) {
            foreach ($optionNames as $optionName) {
                if (true === $input->hasOption($optionName)) {
                    if (false === $input->getOption($optionName) && (bool) \Composer\Util\Platform::getEnv($envName)) {
                        $input->setOption($optionName, true);
                    }
                }
            }
        }

        if (true === $input->hasOption('ignore-platform-reqs')) {
            if (!$input->getOption('ignore-platform-reqs') && (bool) \Composer\Util\Platform::getEnv('COMPOSER_IGNORE_PLATFORM_REQS')) {
                $input->setOption('ignore-platform-reqs', true);

                $io->writeError('<warning>COMPOSER_IGNORE_PLATFORM_REQS is set. You may experience unexpected errors.</warning>');
            }
        }

        if (true === $input->hasOption('ignore-platform-req') && (!$input->hasOption('ignore-platform-reqs') || !$input->getOption('ignore-platform-reqs'))) {
            $ignorePlatformReqEnv = \Composer\Util\Platform::getEnv('COMPOSER_IGNORE_PLATFORM_REQ');
            if (0 === count($input->getOption('ignore-platform-req')) && is_string($ignorePlatformReqEnv) && '' !== $ignorePlatformReqEnv) {
                $input->setOption('ignore-platform-req', explode(',', $ignorePlatformReqEnv));

                $io->writeError('<warning>COMPOSER_IGNORE_PLATFORM_REQ is set to ignore '.$ignorePlatformReqEnv.'. You may experience unexpected errors.</warning>');
            }
        }

        parent::initialize($input, $output);
    }

    public function isProxyCommand()
    {
        return false;
    }

    protected function normalizeRequirements(array $requirements)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Command\\BaseCommand::normalizeRequirements() in plugins yet');
    }

    protected function renderTable(array $table, \Symfony\Component\Console\Output\OutputInterface $output)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Command\\BaseCommand::renderTable() in plugins yet');
    }

    public function requireComposer(?bool $disablePlugins = null, ?bool $disableScripts = null): \Composer\Composer
    {
        if (null === $this->composer) {
            $application = parent::getApplication();
            if ($application instanceof \Composer\Console\Application) {
                $this->composer = $application->getComposer(true, $disablePlugins, $disableScripts);
                assert($this->composer instanceof \Composer\Composer);
            } else {
                throw new \RuntimeException(
                    'Could not create a Composer\Composer instance, you must inject '.
                    'one if this command is not used with a Composer\Console\Application instance'
                );
            }
        }

        return $this->composer;
    }

    public function resetComposer()
    {
        $this->composer = null;
        $this->getApplication()->resetComposer();
    }

    public function setComposer(\Composer\Composer $composer)
    {
        $this->composer = $composer;
    }

    public function setIO(\Composer\IO\IOInterface $io)
    {
        $this->io = $io;
    }

    public function tryComposer(?bool $disablePlugins = null, ?bool $disableScripts = null): ?\Composer\Composer
    {
        if (null === $this->composer) {
            $application = parent::getApplication();
            if ($application instanceof \Composer\Console\Application) {
                $this->composer = $application->getComposer(false, $disablePlugins, $disableScripts);
            }
        }

        return $this->composer;
    }
}
