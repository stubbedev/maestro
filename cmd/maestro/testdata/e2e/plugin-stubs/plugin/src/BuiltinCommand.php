<?php

namespace MaestroTest\StubsPlugin;

use Composer\Command\BaseCommand;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\StringInput;
use Symfony\Component\Console\Output\BufferedOutput;
use Symfony\Component\Console\Output\OutputInterface;

/**
 * Runs Composer's own commands from PHP: $app->find($name)->run().
 */
class BuiltinCommand extends BaseCommand
{
    protected function configure(): void
    {
        $this->setName('stubs:builtin')->setDescription('Runs Composer\'s commands from PHP.');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $app = $this->getApplication();

        $licenses = $app->find('licenses');
        $output->writeln('class '.get_class($licenses));
        $licensesInput = new ArrayInput([]);
        $code = $licenses->run($licensesInput, $output);
        $output->writeln('licenses '.$code.' format='.$licensesInput->getOption('format').' command='.var_export($licensesInput->getArgument('command'), true));

        $buffer = new BufferedOutput();
        $code = $app->find('licenses')->run(new StringInput('--format=summary'), $buffer);
        $output->writeln('summary '.$code.':');
        $output->write($buffer->fetch());

        $code = $app->find('depends')->run(new ArrayInput(['package' => 'maestro-test/stubs-plugin']), $output);
        $output->writeln('depends '.$code);

        $code = $app->find('run-script')->run(new ArrayInput(['script' => 'hello']), $output);
        $output->writeln('run-script '.$code);

        try {
            $app->find('licenses')->run(new ArrayInput(['--format' => 'nope']), $output);
        } catch (\Exception $e) {
            $output->writeln('error '.get_class($e).': '.$e->getMessage());
        }

        $output->writeln('proxy '.var_export($app->find('outdated')->isProxyCommand(), true).' '.var_export($app->find('install')->isProxyCommand(), true));

        return 0;
    }
}
