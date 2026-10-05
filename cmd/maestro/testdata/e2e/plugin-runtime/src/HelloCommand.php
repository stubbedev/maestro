<?php

namespace Project;

use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Output\OutputInterface;

class HelloCommand extends Command
{
    protected function configure(): void
    {
        $this
            ->addArgument('who', InputArgument::OPTIONAL, 'Who to greet', 'world')
            ->addOption('shout', null, InputOption::VALUE_NONE, 'Shout');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $greeting = 'hello '.$input->getArgument('who').' from '.$this->getName();
        $output->writeln($input->getOption('shout') ? strtoupper($greeting) : $greeting);
        $output->writeln('<info>verbose only</info>', OutputInterface::VERBOSITY_VERBOSE);

        return 4;
    }
}
