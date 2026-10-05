<?php

/*
 * Codec goldens for internal/plugin/rpc (docs/PLUGINS.md §9.1): PHP values
 * encoded by the shim's own Codec (pre-pass + json_encode with the shim's
 * flags), one file per case in internal/plugin/rpc/testdata/codec/. The Go
 * test decodes each, encodes it again and requires the same bytes.
 *
 * Usage, from the repository root in the dev shell:
 *   php tools/oracle/plugin/codec.php
 */

error_reporting(-1);
ini_set('display_errors', 'stderr');
ini_set('serialize_precision', '-1');

$shim = __DIR__.'/../../../internal/plugin/php';
require $shim.'/src/Maestro/Shim/Autoloader.php';
\Maestro\Shim\Autoloader::register($shim);

use Maestro\Shim\Codec;

$std = new stdClass();
$std->a = 1;
$std->{'b c'} = [1, 2];
$std->nested = new stdClass();

$cases = [
    'scalars' => [null, true, false, 0, -1, PHP_INT_MAX, PHP_INT_MIN, '', 'text', "quote \" and \\ backslash", 'slash/es', "tab\tnewline\ncr\r"],
    'unicode' => ['é', 'ü€𝄞', "line\u{2028}separator\u{2029}", "nul \0 inside", "\u{7f}del", '😀'],
    'binary' => ["\xff\xfe", "a\x80b", "\xc3\x28", "\xed\xa0\x80"],
    'floats' => [1.0, 0.1, 1e100, -0.0, 0.5, 1.5e-7, 123456789.0, 1e15, 1e16, -2.5, 3.0e-5, PHP_FLOAT_EPSILON, 0.30000000000000004],
    'nonfinite' => [INF, -INF, NAN],
    'keys' => [5 => 'five', '6' => 'six', '07' => 'zero seven', '-3' => 'minus three', '1.5' => 'one and a half', 'x' => 'ex', '' => 'empty'],
    'lists' => [[], [1, 2, 3], [[]], [[1], [2, [3, [4]]]], [0 => 'a', 1 => 'b'], [1 => 'a', 2 => 'b'], [0 => 'a', 2 => 'c']],
    'maps' => ['a' => ['b' => ['c' => []]], 'list' => ['x', 'y'], 'empty' => []],
    'nulkeys' => ["\0first" => 1, 'second' => 2],
    'nulkeys_later' => ['first' => 1, "\0second" => 2],
    'binarykeys' => ["\xff" => 'binary key', 'ok' => 'fine'],
    'nested_tags' => ['bin' => "\xff", 'inf' => INF, 'pairs' => ["\0k" => "\xfe"], 'list' => [NAN, "\x80"]],
    'stdclass' => $std,
    'stdclass_empty' => new stdClass(),
    'stdclass_in_array' => ['o' => (object) ['k' => 'v'], 'list' => [(object) []]],
    'tag_lookalike' => ['o' => ['k' => "\0o"], "\0o" => 5],
    'deep' => [[[[[[[[[[[[[[[[[[[[['deep']]]]]]]]]]]]]]]]]]]]],
    'big' => array_map(function ($i) {
        return ['id' => $i, 'name' => 'package/'.$i, 'version' => $i.'.0.0', 'weight' => $i / 7];
    }, range(1, 200)),
];

$dir = __DIR__.'/../../../internal/plugin/rpc/testdata/codec';
foreach (glob($dir.'/*.json') as $old) {
    unlink($old);
}
foreach ($cases as $name => $value) {
    file_put_contents($dir.'/'.$name.'.json', Codec::json(Codec::encode($value)));
}

echo count($cases), " codec goldens written\n";
