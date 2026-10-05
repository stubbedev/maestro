<?php
// Generates internal/console/testdata/oracle/completion.json:
// CompletionInput::fromString() tokens, (string) casts and bind() results
// against a fixed definition for many command lines and cursor positions.
// Run: php tools/oracle/console/completion.php
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Symfony\Component\Console\Completion\CompletionInput;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputDefinition;
use Symfony\Component\Console\Input\InputOption;

error_reporting(E_ALL & ~E_WARNING);

$strings = [
    '', 'bin/console', 'bin/console ', ' bin/console', 'bin/console cache:clear', 'bin/console  --env  prod',
    'bin/console --env=prod', 'bin/console -eprod', 'bin/console cache:clear "multi word string"',
    "bin/console cache:clear 'multi word string'", 'a "b c', 'a "b\\" c"', "a 'b\\' c'", 'a ""', "a ''", 'a "x"y',
    "a\tb\nc", "a\nb\n", 'a \\"b', '"a b" c', 'x "y" "z w"', "a  'q' ", 'a "\'mixed\'"', 'é ü 日本',
];

$out = ['fromString' => [], 'bind' => []];
foreach ($strings as $s) {
    $input = CompletionInput::fromString($s, 1);
    $p = (new \ReflectionClass($input))->getProperty('tokens');
    $p->setAccessible(true);
    $out['fromString'][] = ['input' => $s, 'tokens' => $p->getValue($input)];
}

function definition(): InputDefinition
{
    return new InputDefinition([
        new InputOption('with-required-value', 'r', InputOption::VALUE_REQUIRED),
        new InputOption('with-optional-value', 'o', InputOption::VALUE_OPTIONAL),
        new InputOption('without-value', 'n', InputOption::VALUE_NONE),
        new InputOption('array', 'a', InputOption::VALUE_REQUIRED | InputOption::VALUE_IS_ARRAY),
        new InputOption('neg', null, InputOption::VALUE_NEGATABLE),
        new InputArgument('required-arg', InputArgument::REQUIRED),
        new InputArgument('optional-arg', InputArgument::OPTIONAL),
        new InputArgument('list', InputArgument::IS_ARRAY),
    ]);
}

$tokenSets = [
    ['bin/console'], ['bin/console', '-'], ['bin/console', '--'], ['bin/console', '--with'], ['bin/console', '-r'],
    ['bin/console', '-rsymf'], ['bin/console', '-r', 'symf'], ['bin/console', '--with-required-value='],
    ['bin/console', '--with-required-value=0'], ['bin/console', '--with-required-value=x', 'y'],
    ['bin/console', '-o'], ['bin/console', '-n'], ['bin/console', '-nr'], ['bin/console', '-nrfoo'],
    ['bin/console', '--neg'], ['bin/console', '--no-neg'], ['bin/console', '--no-'], ['bin/console', '-a', 'x', '-a'],
    ['bin/console', 'a', 'b'], ['bin/console', 'a', 'b', 'c', 'd'], ['bin/console', 'a', '--', '-x'],
    ['bin/console', '--unknown', 'a'], ['bin/console', '-x'], ['bin/console', ''], ['bin/console', 'a', ''],
    ['bin/console', '--without-value', '-'], ['bin/console', '---'], ['bin/console', '0'],
];

foreach ($tokenSets as $tokens) {
    for ($i = 0; $i <= count($tokens); ++$i) {
        $input = CompletionInput::fromTokens($tokens, $i);
        $case = ['tokens' => $tokens, 'current' => $i];
        try {
            $input->bind(definition());
            $case['type'] = $input->getCompletionType();
            $case['name'] = $input->getCompletionName();
            $case['value'] = $input->getCompletionValue();
            $case['string'] = (string) $input;
        } catch (\Throwable $e) {
            $case['error'] = get_class($e).': '.$e->getMessage();
        }
        $out['bind'][] = $case;
    }
}

file_put_contents(dirname(__DIR__, 3).'/internal/console/testdata/oracle/completion.json', json_encode($out, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE)."\n");
