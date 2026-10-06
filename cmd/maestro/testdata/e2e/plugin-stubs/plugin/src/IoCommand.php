<?php

namespace MaestroTest\StubsPlugin;

use Composer\Command\BaseCommand;
use Composer\Factory;
use Composer\Installer;
use Composer\IO\BufferIO;
use Composer\IO\ConsoleIO;
use Symfony\Component\Console\Helper\HelperSet;
use Symfony\Component\Console\Helper\QuestionHelper;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\BufferedOutput;
use Symfony\Component\Console\Output\OutputInterface;

/**
 * IOs created in PHP given to Composer: a Composer and an Installer
 * created with them write to them.
 */
class IoCommand extends BaseCommand
{
    protected function configure(): void
    {
        $this->setName('stubs:io')->setDescription('Gives Composer IOs created in PHP.');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $io = new BufferIO('', OutputInterface::VERBOSITY_VERBOSE);
        $composer = Factory::create($io, null, true);
        $code = Installer::create($io, $composer)->setDryRun(true)->run();
        $output->writeln('buffer io '.$code.':');
        $output->write(preg_replace('{in \d+\.\d+ seconds}', 'in X seconds', $io->getOutput()));

        $buffered = new BufferedOutput();
        $consoleIo = new ConsoleIO(new ArrayInput([]), $buffered, new HelperSet([new QuestionHelper()]));
        $composer = (new Factory())->createComposer($consoleIo, null, true);
        $code = Installer::create($consoleIo, $composer)->setDryRun(true)->setDevMode(false)->run();
        $output->writeln('console io '.$code.':');
        $output->write($buffered->fetch());

        $table = $this->getIO() instanceof ConsoleIO ? $this->getIO()->getTable() : null;
        if ($table !== null) {
            $table->setHeaders(['IO', 'Class'])->setRows([['command', get_class($this->getIO())]])->render();
        }

        return 0;
    }
}
