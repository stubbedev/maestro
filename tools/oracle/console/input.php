<?php
// Generates internal/console/testdata/oracle/input.json: ArgvInput parsing
// (arguments, options, errors, getFirstArgument, hasParameterOption,
// getParameterOption, __toString) against several definitions,
// StringInput tokenization and ArrayInput parsing.
// Run: php tools/oracle/console/input.php
// The definitions are mirrored by inputOracleDefinitions() in
// internal/console/oracle_input_test.go.
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Symfony\Component\Console\Input\ArgvInput;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Input\InputArgument as A;
use Symfony\Component\Console\Input\InputDefinition;
use Symfony\Component\Console\Input\InputOption as O;
use Symfony\Component\Console\Input\StringInput;

error_reporting(E_ALL & ~E_WARNING & ~E_DEPRECATED);

function definitions(): array
{
    return [
        'empty' => new InputDefinition(),
        'composer' => new InputDefinition([
            new A('command', A::REQUIRED, 'The command to execute'),
            new O('--help', '-h', O::VALUE_NONE),
            new O('--quiet', '-q', O::VALUE_NONE),
            new O('--verbose', '-v|vv|vvv', O::VALUE_NONE),
            new O('--version', '-V', O::VALUE_NONE),
            new O('--ansi', '', O::VALUE_NEGATABLE),
            new O('--no-interaction', '-n', O::VALUE_NONE),
            new O('--profile', null, O::VALUE_NONE),
            new O('--no-plugins', null, O::VALUE_NONE),
            new O('--no-scripts', null, O::VALUE_NONE),
            new O('--working-dir', '-d', O::VALUE_REQUIRED),
            new O('--no-cache', null, O::VALUE_NONE),
        ]),
        'require' => new InputDefinition([
            new A('command', A::REQUIRED),
            new A('packages', A::OPTIONAL | A::IS_ARRAY),
            new O('dev', null, O::VALUE_NONE),
            new O('dry-run', null, O::VALUE_NONE),
            new O('prefer-source', null, O::VALUE_NONE),
            new O('update-with-dependencies', 'w', O::VALUE_NONE),
            new O('with-all-dependencies', 'W', O::VALUE_NONE),
            new O('ignore-platform-req', null, O::VALUE_REQUIRED | O::VALUE_IS_ARRAY),
            new O('optimize-autoloader', 'o', O::VALUE_NONE),
            new O('classmap-authoritative', 'a', O::VALUE_NONE),
            new O('apcu-autoloader-prefix', null, O::VALUE_REQUIRED),
            new O('no-interaction', 'n', O::VALUE_NONE),
            new O('verbose', 'v|vv|vvv', O::VALUE_NONE),
        ]),
        'mixed' => new InputDefinition([
            new A('a', A::REQUIRED),
            new A('b', A::OPTIONAL, '', 'x'),
            new O('req', 'r', O::VALUE_REQUIRED),
            new O('opt', 'o', O::VALUE_OPTIONAL, '', 'd'),
            new O('arr', 'A', O::VALUE_OPTIONAL | O::VALUE_IS_ARRAY),
            new O('neg', null, O::VALUE_NEGATABLE),
            new O('flag', 'f', O::VALUE_NONE),
            new O('multi', 'm|mm', O::VALUE_NONE),
        ]),
        'arrayarg' => new InputDefinition([
            new A('files', A::IS_ARRAY),
            new O('x', 'x', O::VALUE_NONE),
            new O('val', 'V', O::VALUE_REQUIRED, '', 'dflt'),
        ]),
    ];
}

function pairs(array $a): array
{
    $out = [];
    foreach ($a as $k => $v) {
        $out[] = [$k, $v];
    }

    return $out;
}

function err(\Throwable $e): array
{
    return ['class' => get_class($e), 'message' => $e->getMessage()];
}

$argvs = [
    [], [''], ['--'], ['-'], ['--', '--'], ['--', '-x'], ['foo'], ['foo', 'bar'], ['foo', 'bar', 'baz', 'qux'],
    ['-v'], ['-vv'], ['-vvv'], ['-vvvv'], ['--verbose'], ['--verbose=1'], ['--verbose', '2'], ['-q'], ['-qv'], ['-vq'],
    ['-n'], ['-nq', 'foo'], ['--ansi'], ['--no-ansi'], ['--no-ansi=1'], ['--ansi=0'], ['-h'], ['--help', 'foo'],
    ['-d', 'dir', 'install'], ['-ddir', 'install'], ['--working-dir=dir', 'install'], ['--working-dir', 'install'],
    ['--working-dir='], ['--working-dir=', 'x'], ['-d'], ['-d', '-v'], ['-d', ''], ['install', '-d', 'x', 'y'],
    ['require', 'a/b', 'c/d:^1', '--dev', '-W'], ['require', '--ignore-platform-req=ext-a', '--ignore-platform-req', 'ext-b', 'x/y'],
    ['require', '--ignore-platform-req'], ['require', '-oa', 'x'], ['require', '-ao', '--', '-x'], ['require', '-ow'],
    ['require', '-on'], ['require', '--apcu-autoloader-prefix', '--dev'], ['require', '--apcu-autoloader-prefix=', 'z'],
    ['require', '--dev=1'], ['require', '--unknown'], ['require', '-z'], ['require', '-oz'], ['require', '-ö'], ['require', '-oЩ'],
    ['require', 'a', '--', '--dev', '-v'], ['require', '-'], ['require', ''], ['require', '', '', 'x'],
    ['a1'], ['a1', 'b1'], ['a1', 'b1', 'c1'], ['-r'], ['-rv', 'a'], ['-r', 'v', 'a'], ['--req', '--', 'a'], ['--req', '-', 'a'],
    ['a', '-o'], ['a', '-o', 'b'], ['a', '-o', '-f'], ['a', '-o', ''], ['a', '-ofoo'], ['a', '--opt'], ['a', '--opt='], ['a', '--opt=', 'b'],
    ['a', '--opt', '', 'b'], ['a', '-A'], ['a', '-A', '-A', 'x', '--arr=y', '--arr', '--arr='], ['a', '-Afx'], ['a', '-fA'], ['a', '-fAq'],
    ['a', '--neg'], ['a', '--no-neg'], ['a', '--neg', '--no-neg'], ['a', '--no-neg=x'], ['a', '--neg=x'], ['a', '-f', '-f'],
    ['a', '-m'], ['a', '-mm'], ['a', '-mmm'], ['a', '-fm'], ['a', '-mf'], ['a', '--multi'], ['a', '-fr'], ['a', '-fr', 'x'], ['a', '-frx'],
    ['a', '-rf'], ['--', 'a', '-r'], ['a', '--flag=1'], ['a', '--flag='], ['a', '---x'], ['a', '--=x'], ['a', '-=x'], ['a', '-r=x'],
    ['-x', 'a', '-x', 'b'], ['a', '-V'], ['a', '-V', 'v'], ['-V', 'v', 'a', 'b'], ['-Vv', 'a'], ['a', '-xV'], ['a', '-xVv'],
    ['0'], ['0', '0'], ['-0'], ['--0'], ['-x0'], ['a', 'b', 'c', 'd', 'e'], ['--req=a=b', 'c'], ['-r', '--', 'a'],
    ['unicode-é', '--req=日本'], ['-fЩ', 'a'], ['a', "with space", "quote'd", "new\nline"],
];
$defs = definitions();

$out = ['argv' => [], 'string' => [], 'array' => []];
foreach ($defs as $defName => $_) {
    foreach ($argvs as $tokens) {
        $def = definitions()[$defName];
        $input = new ArgvInput(array_merge(['cli.php'], $tokens));
        $case = ['def' => $defName, 'argv' => $tokens];
        try {
            $input->bind($def);
            $case['arguments'] = pairs($input->getArguments());
            $case['options'] = pairs($input->getOptions());
            try {
                $input->validate();
            } catch (\Throwable $e) {
                $case['validate'] = err($e);
            }
        } catch (\Throwable $e) {
            $case['error'] = err($e);
        }
        $case['firstArgument'] = $input->getFirstArgument();
        try {
            $case['toString'] = (string) $input;
        } catch (\Throwable $e) {
            $case['toStringError'] = err($e);
        }
        $probes = [['-v'], ['--verbose'], ['-o', '--opt'], ['--req', '-r'], ['--'], ['-d'], ['--working-dir'], [''], ['-'], ['a']];
        foreach ($probes as $p) {
            $case['has'][] = [$p, $input->hasParameterOption($p), $input->hasParameterOption($p, true)];
            $case['get'][] = [$p, $input->getParameterOption($p, 'DEFAULT'), $input->getParameterOption($p, false, true)];
        }
        $out['argv'][] = $case;
    }
}

$strings = [
    '', ' ', 'foo', '  foo  bar  ', '"quoted"', "'quoted'", '"a b" c', "'a\"b'", '"a\'b"', '"a\\"b"', "'a\\'b'",
    '\\"x\\"', '\\', 'a\\', 'a\\ b', '-a"foo bar"', '--long="foo bar""another"', "--o='x'\"y\"'z'", '-a=b', '--a=', '"--a=b"',
    "\"unterminated", "'unterminated", "a\"b\"c", "a'b'c", '"\\n\\t\\x41\\101\\z"', "'\\n'", "x\ty\nz\rw\vv\fu",
    '"" \'\'', 'a""b', "a''b", '--x=""', "--x=''", '日本 語', '"日本 語"', '=x', '"="', "--arg=\\\"'Jenny'\\''s'\\\"",
    'foo -a -ffoo --long bar', '"a"b"c"', "'a'b'c'", '\\x', 'a\\\\b', '"a\\\\"', '"\\x4"', '"\\8"', '"\\0"',
];
foreach ($strings as $s) {
    try {
        $input = new StringInput($s);
        $r = new \ReflectionProperty(ArgvInput::class, 'tokens');
        $r->setAccessible(true);
        $case = ['input' => $s, 'tokens' => $r->getValue($input)];
        try {
            $case['toString'] = (string) $input;
        } catch (\Throwable $e) {
            $case['toStringError'] = err($e);
        }
        $out['string'][] = $case;
    } catch (\Throwable $e) {
        $out['string'][] = ['input' => $s, 'error' => err($e)];
    }
}

$arrays = [
    ['mixed', [['a', 'x']]],
    ['mixed', [['a', 'x'], ['b', 'y'], ['--req', 'r'], ['-o', null], ['--arr', ['p', 'q']], ['--no-neg', null], ['-f', null]]],
    ['mixed', [['a', 'x'], ['--opt', null]]],
    ['mixed', [['a', 'x'], ['--opt', '']]],
    ['mixed', [['a', 'x'], ['--req', null]]],
    ['mixed', [['a', 'x'], ['--neg', null]]],
    ['mixed', [['a', 'x'], ['--neg', 'v']]],
    ['mixed', [['a', 'x'], ['--no-neg', 'v']]],
    ['mixed', [['a', 'x'], ['-m', null]]],
    ['mixed', [['a', 'x'], ['-mm', 1]]],
    ['mixed', [['a', 'x'], ['-zz', 1]]],
    ['mixed', [['a', 'x'], ['--zz', 1]]],
    ['mixed', [['c', 'x']]],
    ['mixed', [['--', null], ['a', 'x']]],
    ['mixed', [[0, 'x']]],
    ['mixed', [[0, 'x'], [1, 'y']]],
    ['mixed', [[0, 'x'], [5, 'y']]],
    ['mixed', [['1', 'x'], ['a', 'y']]],
    ['mixed', [['a', 'x'], ['--flag', false]]],
    ['mixed', [['a', 'x'], ['--flag', 0]]],
    ['mixed', [['a', 'x'], ['--opt', '0']]],
    ['arrayarg', [['files', ['a', 'b']], ['-x', null], ['-V', 'v w']]],
    ['arrayarg', [['files', 'notarray']]],
    ['arrayarg', [[0, ['a', 'b']]]],
    ['composer', [['command', 'install'], ['--working-dir', 'd'], ['--no-ansi', null], ['-vvv', null]]],
    ['composer', [['command', 'install'], ['--ansi', null], ['-n', true]]],
    ['empty', []],
    ['empty', [['--', 'x']]],
    ['empty', [['x', 'y']]],
];
foreach ($arrays as [$defName, $p]) {
    $params = [];
    foreach ($p as [$k, $v]) {
        $params[$k] = $v;
    }
    $case = ['def' => $defName, 'params' => $p];
    try {
        $input = new ArrayInput($params, definitions()[$defName]);
        $case['arguments'] = pairs($input->getArguments());
        $case['options'] = pairs($input->getOptions());
    } catch (\Throwable $e) {
        $case['error'] = err($e);
        $input = new ArrayInput($params);
    }
    $case['firstArgument'] = $input->getFirstArgument();
    try {
        $case['toString'] = (string) $input;
    } catch (\Throwable $e) {
        $case['toStringError'] = err($e);
    }
    foreach ([['--req'], ['-o', '--opt'], ['--'], ['x'], ['a'], ['1']] as $probe) {
        $case['has'][] = [$probe, $input->hasParameterOption($probe), $input->hasParameterOption($probe, true)];
        $case['get'][] = [$probe, $input->getParameterOption($probe, 'DEFAULT'), $input->getParameterOption($probe, false, true)];
    }
    $out['array'][] = $case;
}

// One case per line keeps the golden compact yet diffable.
$flags = JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR;
$json = "{\n";
$sections = [];
foreach ($out as $section => $cases) {
    $sections[] = json_encode($section, $flags).": [\n".implode(",\n", array_map(fn ($c) => json_encode($c, $flags), $cases))."\n]";
}
$json .= implode(",\n", $sections)."\n}\n";
file_put_contents(dirname(__DIR__, 3).'/internal/console/testdata/oracle/input.json', $json);
