<?php

namespace Project;

use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\OutputInterface;

class GreetCommand extends Command
{
    protected function configure(): void
    {
        $this->addArgument('who', InputArgument::OPTIONAL, 'Who', 'you');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $output->writeln('greetings '.$input->getArgument('who').' from '.$this->getName());

        return 0;
    }
}
