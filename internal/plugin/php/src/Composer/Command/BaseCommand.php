<?php

/*
 * maestro's plugin shim: Composer\Command\BaseCommand, reimplemented with
 * Composer 2.10.3's behaviour (docs/PLUGINS.md §4.11) for commands run in
 * PHP: the Composer instance it holds (setComposer() or its Application's),
 * its IO, and initialize() with the PRE_COMMAND_RUN event and the COMPOSER_*
 * option variables. maestro's Application runs plugin commands here
 * (command.run, docs/PLUGINS.md §5.7); maestro's own commands cross as
 * instances of their classes, whose hooks run maestro's command when PHP
 * runs one (builtin.*).
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
        // Handle both --audit and --no-audit flags
        if ($input->hasOption('audit')) {
            $audit = (bool) $input->getOption('audit');
        } else {
            $audit = !($input->hasOption('no-audit') && $input->getOption('no-audit'));
        }
        $auditFormat = $input->hasOption('audit-format') ? $this->getAuditFormat($input) : \Composer\Advisory\Auditor::FORMAT_SUMMARY;

        return new \Composer\Advisory\AuditConfig($audit, $auditFormat);
    }

    protected function createComposerInstance(\Symfony\Component\Console\Input\InputInterface $input, \Composer\IO\IOInterface $io, $config = null, ?bool $disablePlugins = null, ?bool $disableScripts = null): \Composer\Composer
    {
        $disablePlugins = $disablePlugins === true || $input->hasParameterOption('--no-plugins');
        $disableScripts = $disableScripts === true || $input->hasParameterOption('--no-scripts');

        $application = parent::getApplication();
        if ($application instanceof \Composer\Console\Application && $application->getDisablePluginsByDefault()) {
            $disablePlugins = true;
        }
        if ($application instanceof \Composer\Console\Application && $application->getDisableScriptsByDefault()) {
            $disableScripts = true;
        }

        return \Composer\Factory::create($io, $config, $disablePlugins, $disableScripts);
    }

    protected function createPolicyConfig(\Composer\Config $config, ?\Symfony\Component\Console\Input\InputInterface $input): \Composer\Policy\PolicyConfig
    {
        // PolicyConfig::fromConfig($config): maestro's (policy.*), for
        // Installer::setPolicyConfig().
        $policyConfig = \Maestro\Shim\Rpc::call('policy.fromConfig', [$config]);

        // --no-blocking / --no-security-blocking: disable ALL blocking (advisories + malware + abandoned + custom)
        $noBlocking = \Composer\Util\Platform::getBoolEnv('COMPOSER_NO_BLOCKING', false)
            || \Composer\Util\Platform::getBoolEnv('COMPOSER_NO_SECURITY_BLOCKING', false)
            || ($input !== null && $input->hasOption('no-security-blocking') && $input->getOption('no-security-blocking'))
            || ($input !== null && $input->hasOption('no-blocking') && $input->getOption('no-blocking'));

        if ($noBlocking) {
            $policyConfig = \Maestro\Shim\Rpc::call('policy.withBlockingDisabled', [$policyConfig]);
        }

        return $policyConfig;
    }

    protected function formatRequirements(array $requirements)
    {
        $requires = [];
        $requirements = $this->normalizeRequirements($requirements);
        foreach ($requirements as $requirement) {
            if (!isset($requirement['version'])) {
                throw \Maestro\Shim\Exceptions::at(new \UnexpectedValueException('Option '.$requirement['name'] .' is missing a version constraint, use e.g. '.$requirement['name'].':^1.0'), 405);
            }
            $requires[$requirement['name']] = $requirement['version'];
        }

        return $requires;
    }

    public function getApplication(): \Composer\Console\Application
    {
        $application = parent::getApplication();
        if (!$application instanceof \Composer\Console\Application) {
            throw \Maestro\Shim\Exceptions::at(new \RuntimeException('Composer commands can only work with an '.\Composer\Console\Application::class.' instance set'), 66);
        }

        return $application;
    }

    protected function getAuditFormat(\Symfony\Component\Console\Input\InputInterface $input, string $optName = 'audit-format'): string
    {
        if (!$input->hasOption($optName)) {
            throw \Maestro\Shim\Exceptions::at(new \LogicException('This should not be called on a Command which has no '.$optName.' option defined.'), 462);
        }

        $val = $input->getOption($optName);
        if (!in_array($val, \Composer\Advisory\Auditor::FORMATS, true)) {
            throw \Maestro\Shim\Exceptions::at(new \InvalidArgumentException('--'.$optName.' must be one of '.implode(', ', \Composer\Advisory\Auditor::FORMATS).'.'), 467);
        }

        return $val;
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
        if (!$input->hasOption('ignore-platform-reqs') || !$input->hasOption('ignore-platform-req')) {
            throw \Maestro\Shim\Exceptions::at(new \LogicException('Calling getPlatformRequirementFilter from a command which does not define the --ignore-platform-req[s] flags is not permitted.'), 379);
        }

        if (true === $input->getOption('ignore-platform-reqs')) {
            return \Composer\Filter\PlatformRequirementFilter\PlatformRequirementFilterFactory::ignoreAll();
        }

        $ignores = $input->getOption('ignore-platform-req');
        if (count($ignores) > 0) {
            return \Composer\Filter\PlatformRequirementFilter\PlatformRequirementFilterFactory::fromBoolOrList($ignores);
        }

        return \Composer\Filter\PlatformRequirementFilter\PlatformRequirementFilterFactory::ignoreNothing();
    }

    protected function getPreferredInstallOptions(\Composer\Config $config, \Symfony\Component\Console\Input\InputInterface $input, bool $keepVcsRequiresPreferSource = false)
    {
        $preferSource = false;
        $preferDist = false;

        switch ($config->get('preferred-install')) {
            case 'source':
                $preferSource = true;
                break;
            case 'dist':
                $preferDist = true;
                break;
            case 'auto':
            default:
                // noop
                break;
        }

        if (!$input->hasOption('prefer-dist') || !$input->hasOption('prefer-source')) {
            return [$preferSource, $preferDist];
        }

        if ($input->hasOption('prefer-install') && is_string($input->getOption('prefer-install'))) {
            if ($input->getOption('prefer-source')) {
                throw \Maestro\Shim\Exceptions::at(new \InvalidArgumentException('--prefer-source can not be used together with --prefer-install'), 347);
            }
            if ($input->getOption('prefer-dist')) {
                throw \Maestro\Shim\Exceptions::at(new \InvalidArgumentException('--prefer-dist can not be used together with --prefer-install'), 350);
            }
            switch ($input->getOption('prefer-install')) {
                case 'dist':
                    $input->setOption('prefer-dist', true);
                    break;
                case 'source':
                    $input->setOption('prefer-source', true);
                    break;
                case 'auto':
                    $preferDist = false;
                    $preferSource = false;
                    break;
                default:
                    throw \Maestro\Shim\Exceptions::at(new \UnexpectedValueException('--prefer-install accepts one of "dist", "source" or "auto", got '.$input->getOption('prefer-install')), 364);
            }
        }

        if ($input->getOption('prefer-source') || $input->getOption('prefer-dist') || ($keepVcsRequiresPreferSource && $input->hasOption('keep-vcs') && $input->getOption('keep-vcs'))) {
            $preferSource = $input->getOption('prefer-source') || ($keepVcsRequiresPreferSource && $input->hasOption('keep-vcs') && $input->getOption('keep-vcs'));
            $preferDist = $input->getOption('prefer-dist');
        }

        return [$preferSource, $preferDist];
    }

    protected function getTerminalWidth()
    {
        $terminal = new \Symfony\Component\Console\Terminal();
        $width = $terminal->getWidth();

        if (\Composer\Util\Platform::isWindows()) {
            $width--;
        } else {
            $width = max(80, $width);
        }

        return $width;
    }

    protected function initialize(\Symfony\Component\Console\Input\InputInterface $input, \Symfony\Component\Console\Output\OutputInterface $output): void
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            // One of maestro's commands run from PHP: maestro's
            // initialize() (the same as below) on its own command.
            \Maestro\Shim\Console::builtin($this, 'initialize', [$input, $output]);

            return;
        }

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
        $parser = new \Composer\Package\Version\VersionParser();

        return $parser->parseNameVersionPairs($requirements);
    }

    protected function renderTable(array $table, \Symfony\Component\Console\Output\OutputInterface $output)
    {
        $renderer = new \Symfony\Component\Console\Helper\Table($output);
        $renderer->setStyle('compact');
        $renderer->setRows($table)->render();
    }

    public function requireComposer(?bool $disablePlugins = null, ?bool $disableScripts = null): \Composer\Composer
    {
        if (null === $this->composer) {
            $application = parent::getApplication();
            if ($application instanceof \Composer\Console\Application) {
                $this->composer = $application->getComposer(true, $disablePlugins, $disableScripts);
                assert($this->composer instanceof \Composer\Composer);
            } else {
                throw \Maestro\Shim\Exceptions::at(new \RuntimeException(
                    'Could not create a Composer\Composer instance, you must inject '.
                    'one if this command is not used with a Composer\Console\Application instance'
                ), 106);
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
