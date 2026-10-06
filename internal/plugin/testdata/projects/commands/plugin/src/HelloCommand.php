<?php

namespace MaestroTest\Commands;

use Composer\Command\BaseCommand;
use Composer\Console\Input\InputOption;
use Composer\IO\BufferIO;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\OutputInterface;

class HelloCommand extends BaseCommand
{
    protected function configure(): void
    {
        $this->setName('maestro:hello')
            ->setAliases(['mh'])
            ->setDescription('Says hello.')
            ->setHelp('The <info>%command.name%</info> command says hello.')
            ->addArgument('times', InputArgument::OPTIONAL, 'How often', '1');
        $this->getDefinition()->addOption(new InputOption('name', null, InputOption::VALUE_REQUIRED, 'Who to greet', 'world', ['alice', 'bob']));
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $output->writeln('hello '.$input->getOption('name').' x'.$input->getArgument('times'));
        $this->getIO()->write('<info>io</info> '.$this->requireComposer()->getPackage()->getName());

        $app = $this->getApplication();
        $output->writeln(sprintf(
            'app %s %s %s %s',
            get_class($app),
            var_export($app->has('install'), true),
            get_class($app->find('install')),
            var_export($app->getDefinition()->hasOption('no-plugins'), true)
        ));
        $output->writeln('install args: '.implode(',', array_keys($app->find('install')->getDefinition()->getArguments())));

        $output->writeln(sprintf(
            'helpers %s %s %s',
            json_encode($this->formatRequirements(['acme/lib:^1.0', 'acme/other=2.0'])),
            json_encode($this->getPreferredInstallOptions($this->requireComposer()->getConfig(), $input)),
            var_export($this->getTerminalWidth() >= 80, true)
        ));

        $io = new BufferIO();
        $io->write('buffered <info>output</info>');
        $io->writeError('and errors');
        $output->writeln(str_replace("\n", '|', $io->getOutput()));

        return 3;
    }
}
