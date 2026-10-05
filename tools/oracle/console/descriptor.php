<?php
// Generates internal/console/testdata/oracle/descriptor.json: the exact
// bytes the txt, xml, json and md descriptors write for the objects of
// Tests/Descriptor/ObjectsProvider.php plus extra cases (negatable options,
// namespaces, short descriptions, json_encoding flags).
// Run: php tools/oracle/console/descriptor.php
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Symfony\Component\Console\Application;
use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\Helper\DescriptorHelper;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputDefinition;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Output\BufferedOutput;

$_SERVER['PHP_SELF'] = '/nonexistent/bin/composer';
$_SERVER['argv'][0] = '/nonexistent/bin/composer';
putenv('COLUMNS=80');

function cmd(string $name, callable $configure): Command
{
    $c = new Command($name);
    $configure($c);

    return $c;
}

function command1(): Command
{
    return cmd('descriptor:command1', function ($c) {
        $c->setAliases(['alias1', 'alias2'])->setDescription('command 1 description')->setHelp('command 1 help');
    });
}
function command2(): Command
{
    return cmd('descriptor:command2', function ($c) {
        $c->setDescription('command 2 description')->setHelp('command 2 help')
            ->addUsage('-o|--option_name <argument_name>')->addUsage('<argument_name>')
            ->addArgument('argument_name', InputArgument::REQUIRED)
            ->addOption('option_name', 'o', InputOption::VALUE_NONE);
    });
}
function command3(): Command
{
    return cmd('descriptor:command3', function ($c) {
        $c->setDescription('command 3 description')->setHelp('command 3 help')->setHidden(true);
    });
}
function command4(): Command
{
    return cmd('descriptor:command4', function ($c) {
        $c->setAliases(['descriptor:alias_command4', 'command4:descriptor']);
    });
}
function commandMb(): Command
{
    return cmd('descriptor:åèä', function ($c) {
        $c->setDescription('command åèä description')->setHelp('command åèä help')
            ->addUsage('-o|--option_name <argument_name>')->addUsage('<argument_name>')
            ->addArgument('argument_åèä', InputArgument::REQUIRED)
            ->addOption('option_åèä', 'o', InputOption::VALUE_NONE);
    });
}
function commandExtra(): Command
{
    return cmd('extra:cmd', function ($c) {
        $c->setDescription("multi\nline & <special> \"chars\"")->setHelp("Help for %command.name%:\n  %command.full_name% --x\r\n\ttab")
            ->addArgument('list', InputArgument::IS_ARRAY, "list\n  description", ['a', 'b<c>'])
            ->addOption('neg', null, InputOption::VALUE_NEGATABLE, 'negatable')
            ->addOption('num', 'N|M', InputOption::VALUE_OPTIONAL, 'number', 1.5)
            ->addOption('int', null, InputOption::VALUE_REQUIRED, 'int', 42)
            ->addOption('t', null, InputOption::VALUE_OPTIONAL, 'bool default', true)
            ->addOption('arr', 'A', InputOption::VALUE_REQUIRED | InputOption::VALUE_IS_ARRAY, 'arr', ['x/y', 'é'])
            ->addUsage('--neg extra');
        foreach (['a &amp; b <c> &lt; &#65;&#x42;', 'x & y', 'p &foo; q', 't &amp', 'a & b; c', 'a &b c; d', 'q &#xZZ; r', 'q &#0; r', '&lt', 'a &; b', '&amp;amp;', 'x &#65 y', '&quot;&apos;'] as $u) {
            $c->addUsage($u);
        }
    });
}

function app(string $name, array $commands, string $version = 'UNKNOWN'): Application
{
    $a = new Application($name, $version);
    foreach ($commands as $c) {
        $a->add($c);
    }

    return $a;
}

$objects = [
    'input_argument_1' => fn () => new InputArgument('argument_name', InputArgument::REQUIRED),
    'input_argument_2' => fn () => new InputArgument('argument_name', InputArgument::IS_ARRAY, 'argument description'),
    'input_argument_3' => fn () => new InputArgument('argument_name', InputArgument::OPTIONAL, 'argument description', 'default_value'),
    'input_argument_4' => fn () => new InputArgument('argument_name', InputArgument::REQUIRED, "multiline\nargument description"),
    'input_argument_with_style' => fn () => new InputArgument('argument_name', InputArgument::OPTIONAL, 'argument description', '<comment>style</>'),
    'input_argument_with_default_inf_value' => fn () => new InputArgument('argument_name', InputArgument::OPTIONAL, 'argument description', \INF),
    'input_argument_false' => fn () => new InputArgument('argument_name', InputArgument::OPTIONAL, 'd', false),
    'input_argument_zero' => fn () => new InputArgument('argument_name', InputArgument::OPTIONAL, 'd', '0'),
    'input_option_1' => fn () => new InputOption('option_name', 'o', InputOption::VALUE_NONE),
    'input_option_2' => fn () => new InputOption('option_name', 'o', InputOption::VALUE_OPTIONAL, 'option description', 'default_value'),
    'input_option_3' => fn () => new InputOption('option_name', 'o', InputOption::VALUE_REQUIRED, 'option description'),
    'input_option_4' => fn () => new InputOption('option_name', 'o', InputOption::VALUE_IS_ARRAY | InputOption::VALUE_OPTIONAL, 'option description', []),
    'input_option_5' => fn () => new InputOption('option_name', 'o', InputOption::VALUE_REQUIRED, "multiline\noption description"),
    'input_option_6' => fn () => new InputOption('option_name', ['o', 'O'], InputOption::VALUE_REQUIRED, 'option with multiple shortcuts'),
    'input_option_with_style' => fn () => new InputOption('option_name', 'o', InputOption::VALUE_REQUIRED, 'option description', '<comment>style</>'),
    'input_option_with_style_array' => fn () => new InputOption('option_name', 'o', InputOption::VALUE_IS_ARRAY | InputOption::VALUE_REQUIRED, 'option description', ['<comment>Hello</comment>', '<info>world</info>']),
    'input_option_with_default_inf_value' => fn () => new InputOption('option_name', 'o', InputOption::VALUE_OPTIONAL, 'option description', \INF),
    'input_option_negatable' => fn () => new InputOption('option_name', null, InputOption::VALUE_NEGATABLE, "nega\ttable & <x>"),
    'input_option_float' => fn () => new InputOption('option_name', 'o', InputOption::VALUE_OPTIONAL, 'f', 0.1),
    'input_definition_1' => fn () => new InputDefinition(),
    'input_definition_2' => fn () => new InputDefinition([new InputArgument('argument_name', InputArgument::REQUIRED)]),
    'input_definition_3' => fn () => new InputDefinition([new InputOption('option_name', 'o', InputOption::VALUE_NONE)]),
    'input_definition_4' => fn () => new InputDefinition([
        new InputArgument('argument_name', InputArgument::REQUIRED),
        new InputOption('option_name', 'o', InputOption::VALUE_NONE),
    ]),
    'command_1' => fn () => command1(),
    'command_2' => fn () => command2(),
    'command_mbstring' => fn () => commandMb(),
    'command_extra' => fn () => commandExtra(),
    'application_1' => fn () => new Application(),
    'application_2' => fn () => app('My Symfony application', [command1(), command2(), command3(), command4()], 'v1.0'),
    'application_mbstring' => fn () => app('MbString åpplicätion', [commandMb()]),
    'application_extra' => fn () => app('Extra', [commandExtra(), command4(), cmd('1', fn ($c) => $c->setDescription('one')), cmd('22:foo', fn ($c) => null)]),
];

$optionSets = [
    'default' => [],
    'short' => ['short' => true],
    'namespace' => ['namespace' => 'descriptor'],
    'raw_text' => ['raw_text' => true],
    'pretty' => ['json_encoding' => JSON_PRETTY_PRINT],
];

$helper = new DescriptorHelper();
$out = [];
foreach ($objects as $name => $factory) {
    foreach (['txt', 'xml', 'json', 'md'] as $format) {
        foreach ($optionSets as $setName => $set) {
            if ('namespace' === $setName && !str_starts_with($name, 'application_2')) {
                continue;
            }
            if ('pretty' === $setName && 'json' !== $format) {
                continue;
            }
            foreach ([false, true] as $decorated) {
                $output = new BufferedOutput(BufferedOutput::VERBOSITY_NORMAL, $decorated);
                $case = ['object' => $name, 'format' => $format, 'options' => $setName, 'decorated' => $decorated];
                try {
                    $helper->describe($output, $factory(), ['format' => $format] + $set);
                    $case['output'] = $output->fetch();
                } catch (\Throwable $e) {
                    $case['error'] = $e->getMessage();
                }
                $out[] = $case;
            }
        }
    }
}

file_put_contents(dirname(__DIR__, 3).'/internal/console/testdata/oracle/descriptor.json', json_encode($out, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE)."\n");
