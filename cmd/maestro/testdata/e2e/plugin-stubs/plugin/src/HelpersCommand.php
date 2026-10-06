<?php

namespace MaestroTest\StubsPlugin;

use Composer\Command\BaseCommand;
use Composer\Filter\PlatformRequirementFilter\IgnoreListPlatformRequirementFilter;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Output\OutputInterface;

/**
 * BaseCommand's audit, policy and platform requirement filter helpers on
 * the command's options, the AuditConfig and PolicyConfig given to an
 * Installer.
 */
class HelpersCommand extends BaseCommand
{
    protected function configure(): void
    {
        $this->setName('stubs:helpers')
            ->setDescription('Uses BaseCommand\'s helpers.')
            ->addOption('audit', null, InputOption::VALUE_NONE)
            ->addOption('audit-format', null, InputOption::VALUE_REQUIRED, 'Audit output format.', 'summary')
            ->addOption('no-blocking', null, InputOption::VALUE_NONE)
            ->addOption('ignore-platform-req', null, InputOption::VALUE_REQUIRED | InputOption::VALUE_IS_ARRAY)
            ->addOption('ignore-platform-reqs', null, InputOption::VALUE_NONE);
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $filter = $this->getPlatformRequirementFilter($input);
        $output->writeln('filter '.get_class($filter));
        if ($filter instanceof IgnoreListPlatformRequirementFilter) {
            $output->writeln('ignores ext-foo '.var_export($filter->isIgnored('ext-foo'), true).' php '.var_export($filter->isUpperBoundIgnored('php'), true));
        }

        $output->writeln('audit format '.$this->getAuditFormat($input));
        $auditConfig = $this->createAuditConfig($input);
        $output->writeln('audit config '.get_class($auditConfig).' '.var_export($auditConfig->audit, true).' '.$auditConfig->auditFormat);

        $composer = $this->requireComposer();
        $policyConfig = $this->createPolicyConfig($composer->getConfig(), $input);
        $output->writeln('policy config '.get_class($policyConfig));

        // Both go to an Installer, which runs with them (a dry run).
        $installer = \Composer\Installer::create($this->getIO(), $composer)
            ->setDryRun(true)
            ->setAuditConfig($auditConfig)
            ->setPolicyConfig($policyConfig)
            ->setPlatformRequirementFilter($filter);
        $output->writeln('dry run '.$installer->run());

        return 0;
    }
}
