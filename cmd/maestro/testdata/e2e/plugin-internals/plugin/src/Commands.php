<?php

namespace MaestroTest\InternalsPlugin;

use Composer\Command\BaseCommand;
use Composer\Plugin\Capability\CommandProvider;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\OutputInterface;

class Commands implements CommandProvider
{
    public function getCommands(): array
    {
        return [new ThrowCommand()];
    }
}

class ThrowCommand extends BaseCommand
{
    protected function configure(): void
    {
        $this->setName('internals:throw')->setDescription('Throws, from the command or from Composer code it calls');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        if (getenv('INTERNALS_THROW') === 'composer') {
            // an exception of Composer's own code, under the command
            $this->requireComposer()->getRepositoryManager()->createRepository('nope', []);
        }
        Plugin::fail('a command');

        return 0;
    }
}
