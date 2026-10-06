<?php

namespace MaestroTest\Commands;

use Composer\Command\BaseCommand;
use Symfony\Component\Console\Completion\CompletionInput;
use Symfony\Component\Console\Completion\CompletionSuggestions;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\StringInput;
use Symfony\Component\Console\Output\BufferedOutput;
use Symfony\Component\Console\Output\OutputInterface;

/**
 * Runs maestro's own commands from PHP, as plugins run Composer's.
 */
class BuiltinCommand extends BaseCommand
{
    protected function configure(): void
    {
        $this->setName('maestro:builtin');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $app = $this->getApplication();

        // Composer's licenses command on the command's output; the
        // PRE_COMMAND_RUN listener switches it to JSON, through the input
        // object passed here.
        $licensesInput = new ArrayInput(['--format' => 'text']);
        $code = $app->find('licenses')->run($licensesInput, $output);
        $output->writeln('licenses '.$code.' '.$licensesInput->getOption('format'));

        // On a PHP output, from a string input.
        $buffer = new BufferedOutput();
        $code = $app->find('licenses')->run(new StringInput('--format=summary'), $buffer);
        $output->writeln('summary '.$code.': '.str_replace("\n", '|', trim($buffer->fetch())));

        // interact() of run-script lists the scripts without one.
        $buffer = new BufferedOutput();
        $code = $app->find('run-script')->run(new ArrayInput(['script' => 'greet', 'args' => ['them']]), $buffer);
        $output->writeln('run-script '.$code);

        // The command's own errors.
        try {
            $app->find('licenses')->run(new ArrayInput(['--format' => 'nope']), new BufferedOutput());
        } catch (\Exception $e) {
            $output->writeln('error '.get_class($e).': '.$e->getMessage());
        }

        $output->writeln('proxy '.var_export($app->find('outdated')->isProxyCommand(), true).' '.var_export($app->find('install')->isProxyCommand(), true));

        $suggestions = new CompletionSuggestions();
        $completion = CompletionInput::fromTokens(['composer', 'global', 'ins'], 2);
        $global = $app->find('global');
        $global->mergeApplicationDefinition();
        $completion->bind($global->getDefinition());
        $global->complete($completion, $suggestions);
        $values = array_map(function ($s) { return $s->getValue(); }, $suggestions->getValueSuggestions());
        $output->writeln('complete '.var_export(in_array('install', $values, true), true));

        return 0;
    }
}
