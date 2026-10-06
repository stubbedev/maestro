<?php

namespace MaestroTest\Commands;

use Composer\Command\BaseCommand;
use Composer\Console\Application;
use Composer\Factory;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\BufferedOutput;
use Symfony\Component\Console\Output\OutputInterface;

class NestedCommand extends BaseCommand
{
    protected function configure(): void
    {
        $this->setName('maestro:nested');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $composer = (new Factory())->createComposer($this->getIO());
        $output->writeln('created '.$composer->getPackage()->getName().' '.var_export($composer === $this->requireComposer(), true));

        $app = new Application();
        $app->setAutoExit(false);
        $code = $app->run(new ArrayInput(['command' => 'licenses', '--format' => 'summary']), $output);
        $output->writeln('nested '.$code);

        $buffer = new BufferedOutput();
        $code = $app->run(new ArrayInput(['command' => 'maestro:hello', '--name' => 'nested']), $buffer);
        $output->writeln('buffered '.$code.': '.str_replace("\n", '|', $buffer->fetch()));

        return 0;
    }
}
