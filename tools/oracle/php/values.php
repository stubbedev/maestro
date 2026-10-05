<?php

/*
 * Differential oracle for internal/php: runs PHP's own array, comparison,
 * sort, JSON, var_export, float and string functions on tricky inputs and
 * writes the results to internal/php/testdata/oracle/*.json, which the Go
 * tests compare against.
 *
 * Usage (from the repo root, inside the devenv shell):
 *   php tools/oracle/php/values.php
 *
 * PHP values travel as serialize() strings; strings that are not valid
 * UTF-8 are written as {"base64": "..."}. Errors are written as
 * {"error": message}.
 */

declare(strict_types=1);

error_reporting(E_ALL);
ini_set('memory_limit', '-1');

// Notices and warnings (array to string conversion, ...) are expected.
set_error_handler(static fn (): bool => true);

$root = dirname(__DIR__, 3);
$dir = $root.'/internal/php/testdata/oracle';
@mkdir($dir, 0777, true);

function enc(string $s)
{
    return preg_match('//u', $s) ? $s : ['base64' => base64_encode($s)];
}

function ser($v)
{
    return enc(serialize($v));
}

/** Runs $fn, returning its result or {"error": message}. */
function attempt(callable $fn, callable $encode)
{
    $warning = null;
    set_error_handler(static function (int $no, string $msg) use (&$warning): bool {
        $warning = $msg;

        return true;
    });
    try {
        $r = $fn();
    } catch (\Throwable $e) {
        restore_error_handler();

        return ['error' => $e->getMessage()];
    }
    restore_error_handler();

    return $encode($r);
}

function write(string $name, array $cases): void
{
    global $dir;
    $flags = JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR;
    $json = "[\n".implode(",\n", array_map(static fn ($c) => json_encode($c, $flags), $cases))."\n]\n";
    file_put_contents($dir.'/'.$name.'.json', $json);
    fwrite(STDERR, sprintf("%s: %d cases\n", $name, count($cases)));
}

// Deterministic pseudo-random doubles from their bit patterns.
mt_srand(20261005);
function randomDouble(): float
{
    do {
        $bytes = '';
        for ($i = 0; $i < 8; $i++) {
            $bytes .= chr(mt_rand(0, 255));
        }
        $d = unpack('e', $bytes)[1];
    } while (is_nan($d) || is_infinite($d));

    return $d;
}

$floats = [0.0, -0.0, 1.0, -1.0, 0.1, 0.2, 0.3, 0.1 + 0.2, 1/3, 2/3, 1e25, 1e22, 1e21, 1e20, 1e16, 1e15, 1e14, 1e-4, 1e-5, 1e-7, 1.5,
    123456789.123, 1234567890123456789.0, 9007199254740993.0, 5e-324, 2.2250738585072014e-308, PHP_FLOAT_MAX, PHP_FLOAT_EPSILON,
    -1.5e-10, 100.0, 1e100, 0.5, 0.05, 0.005, 3.14159, 2.5, 1e-15, 123e45, 7.0E-10, 1.0E+15, 99999999999999.9, 0.30000000000000004,
    4.35, 0.000021, 1e300 * 10, -1e300 * 10, NAN];
for ($i = 0; $i < 300; $i++) {
    $floats[] = randomDouble();
}
for ($i = 0; $i < 100; $i++) {
    $floats[] = (float) (round(randomDouble() / 1e300, mt_rand(0, 12)) ?: mt_rand(1, 1000) / mt_rand(1, 1000));
}

// --- floats -----------------------------------------------------------------

$cases = [];
foreach ($floats as $f) {
    $cases[] = [
        'value' => ser($f),
        'string' => (string) $f,
        'var_export' => var_export($f, true),
        'json' => attempt(static fn () => json_encode($f, JSON_THROW_ON_ERROR), static fn ($r) => $r),
        'json_zero_fraction' => attempt(static fn () => json_encode($f, JSON_PRESERVE_ZERO_FRACTION | JSON_THROW_ON_ERROR), static fn ($r) => $r),
        'int' => (string) (int) $f,
    ];
}
write('floats', $cases);

// --- numeric strings and casts ---------------------------------------------

$strings = ['', '0', '1', '-1', '+1', '01', '1.5', '.5', '5.', '-.5', '1e3', '1E3', '1e', '1e+', '1e+5', '1e-5', ' 1', '1 ', ' 1 ', "\t\n1\r\v\f", '1x', 'x1',
    '0x1A', '0b11', '1_000', '9223372036854775807', '9223372036854775808', '-9223372036854775808', '-9223372036854775809', '99999999999999999999',
    '00000000000000000000123', '1e1000', '-1e1000', 'inf', 'INF', 'nan', '1.0', '1.', '.', '-', '+', '- 1', '1e5x', '9223372036854775808e-',
    '9223372036854775807 ', '-9223372036854775808 ', '0.1e-1', '123abc', 'abc', ' ', "1\0", '0.0', '-0', '-0.0', '1,5', '١', "1\xff"];
$cases = [];
foreach ($strings as $s) {
    $cases[] = [
        'value' => enc($s),
        'int' => (string) (int) $s,
        'float' => ser((float) $s),
        'bool' => (bool) $s,
        'is_numeric' => is_numeric($s),
        'key' => ser(array_keys([$s => 1])[0]),
    ];
}
write('strings_numeric', $cases);

// --- comparison ------------------------------------------------------------

$values = [null, true, false, 0, 1, -1, 2, PHP_INT_MAX, PHP_INT_MIN, 0.0, -0.0, 1.0, 1.5, -1.5, NAN, INF, -INF, 1e20,
    '', '0', '1', '-1', '1.0', '1.5', 'abc', 'ABC', 'abd', '1e3', '1000', ' 1', '1 ', '0x1A', 'null', 'true', 'a', '9223372036854775807', '9223372036854775808',
    '1e1000', '-1e1000', 'INF', [], [0], [1], [1, 2], [2, 1], ['a' => 1], ['b' => 1], ['a' => 1, 'b' => 2], ['b' => 2, 'a' => 1], [null], [[1]],
    new stdClass(), (object) ['a' => 1], (object) ['a' => 2], (object) ['b' => 1]];
$cases = [];
foreach ($values as $a) {
    $row = [];
    foreach ($values as $b) {
        $row[] = [$a <=> $b, $a == $b, $a === $b];
    }
    $cases[] = ['value' => ser($a), 'results' => $row];
}
write('compare', $cases);

// --- sorting ---------------------------------------------------------------

$arrays = [
    [], [1], [3, 1, 2], ['b', 'a', 'c'], ['10', '9', '2', '1'], ['img12', 'img10', 'IMG2', 'img1'], [1, '1', 1.0, true, '01', null, '', 'a'],
    ['b' => 1, 'a' => 2, 'c' => 0], [10 => 'a', 9 => 'b', '1x' => 'c', 'x1' => 'd', -1 => 'e'], [0.5, '0.5', '.5', 'abc', 0, false, null, []],
    ['B', 'a', 'C', 'b', 'A', 'c'], ['1e3', '1000', '999', '1e2'], [NAN, 1, NAN, 0], [[1, 2], [1], [0, 5], ['a' => 1]],
    ['x10' => 1, 'x9' => 2, 'X1' => 3, '10' => 4, '9' => 5, 'a' => 6], [true, false, null, 0, 1, '0', '1', 'a', ''],
];
for ($n = 5; $n <= 60; $n += 5) {
    $a = [];
    for ($i = 0; $i < $n; $i++) {
        $pick = mt_rand(0, 5);
        $a[] = match ($pick) {
            0 => mt_rand(0, 9),
            1 => (string) mt_rand(0, 9),
            2 => mt_rand(0, 20) / 4,
            3 => chr(mt_rand(97, 102)).mt_rand(0, 12),
            4 => mt_rand(0, 1) === 1,
            default => ['a', 'B', 'b', 'A', '', '0', ' 1', '1e1'][mt_rand(0, 7)],
        };
    }
    $arrays[] = $a;
    // keyed and with many duplicates (stability)
    $k = [];
    foreach ($a as $i => $v) {
        $k[mt_rand(0, 1) ? 'k'.$i : $i * 3] = mt_rand(0, 3);
    }
    $arrays[] = $k;
}
$sortFlags = ['regular' => SORT_REGULAR, 'numeric' => SORT_NUMERIC, 'string' => SORT_STRING, 'string_case' => SORT_STRING | SORT_FLAG_CASE,
    'natural' => SORT_NATURAL, 'natural_case' => SORT_NATURAL | SORT_FLAG_CASE, 'locale' => SORT_LOCALE_STRING];
$cases = [];
foreach ($arrays as $a) {
    $res = [];
    foreach (['sort', 'rsort', 'asort', 'arsort', 'ksort', 'krsort'] as $fn) {
        foreach ($sortFlags as $fname => $flag) {
            $c = $a;
            $fn($c, $flag);
            $c[] = 'next';
            $res[$fn.'/'.$fname] = ser($c);
        }
    }
    $c = $a;
    usort($c, static fn ($x, $y) => $x <=> $y);
    $res['usort/spaceship'] = ser($c);
    $c = $a;
    uasort($c, static fn ($x, $y) => strcmp((string) json_encode($x), (string) json_encode($y)));
    $res['uasort/json'] = ser($c);
    $c = $a;
    uksort($c, static fn ($x, $y) => strnatcasecmp((string) $x, (string) $y));
    $res['uksort/natcase'] = ser($c);
    $c = $a;
    usort($c, static fn ($x, $y) => 0);
    $res['usort/zero'] = ser($c);
    $c = $a;
    usort($c, static fn ($x, $y) => is_int($x) ? -1 : 1);
    $res['usort/intransitive'] = ser($c);
    $cases[] = ['value' => ser($a), 'results' => $res];
}
write('sort', $cases);

// --- array functions -------------------------------------------------------

$a1 = ['a' => 1, 5 => 'x', 'b' => [1, 2], 2 => 'y', '3' => 'z'];
$a2 = ['b' => [3], 'a' => 9, 0 => 'w', 'c' => ['k' => 'v']];
$a3 = [-5 => 'n', 'a' => ['b' => ['c' => 1]], 'z' => null];
$l1 = ['x', 'y', 'x', 1, '1', true, null, 1.0, '01', 'X'];
$keysets = [[], [1, 2, 3], ['a', 'b'], [-3 => 'a'], [5 => 'a', 'b'], $a1, $a2, $a3, $l1, ['1' => 'a', '01' => 'b', '1.5' => 'c', true => 'd', null => 'e']];
$cases = [];
$ops = [
    'array_merge' => static fn ($x, $y) => array_merge($x, $y),
    'array_merge_recursive' => static fn ($x, $y) => array_merge_recursive($x, $y),
    'array_replace' => static fn ($x, $y) => array_replace($x, $y),
    'array_replace_recursive' => static fn ($x, $y) => array_replace_recursive($x, $y),
    'array_diff' => static fn ($x, $y) => @array_diff($x, $y),
    'array_intersect' => static fn ($x, $y) => @array_intersect($x, $y),
    'array_diff_key' => static fn ($x, $y) => array_diff_key($x, $y),
    'array_intersect_key' => static fn ($x, $y) => array_intersect_key($x, $y),
    'array_diff_assoc' => static fn ($x, $y) => @array_diff_assoc($x, $y),
    'array_combine' => static fn ($x, $y) => array_combine(array_map(static fn ($v) => is_array($v) ? 'arr' : $v, $x), $y),
];
foreach ($keysets as $x) {
    foreach ($keysets as $y) {
        $res = [];
        foreach ($ops as $name => $op) {
            $res[$name] = attempt(static function () use ($op, $x, $y) {
                $r = $op($x, $y);
                $r[] = 'next';

                return $r;
            }, 'ser');
        }
        $cases[] = ['x' => ser($x), 'y' => ser($y), 'results' => $res];
    }
}
write('array_binary', $cases);

$cases = [];
foreach ($keysets as $x) {
    $res = [];
    $withNext = static function ($r) {
        $r[] = 'next';

        return ser($r);
    };
    $res['array_unique'] = attempt(static fn () => @array_unique($x), $withNext);
    $res['array_flip'] = attempt(static fn () => @array_flip($x), $withNext);
    $res['array_values'] = attempt(static fn () => array_values($x), $withNext);
    $res['array_keys'] = attempt(static fn () => array_keys($x), $withNext);
    $res['array_reverse'] = attempt(static fn () => array_reverse($x), $withNext);
    $res['array_reverse_keys'] = attempt(static fn () => array_reverse($x, true), $withNext);
    $res['array_is_list'] = array_is_list($x);
    $res['array_fill_keys'] = attempt(static fn () => array_fill_keys(array_map(static fn ($v) => is_array($v) ? 'arr' : $v, $x), 0), $withNext);
    $res['array_filter'] = attempt(static fn () => array_filter($x), $withNext);
    $res['array_chunk2'] = attempt(static fn () => array_chunk($x, 2), 'ser');
    $res['array_chunk2_keys'] = attempt(static fn () => array_chunk($x, 2, true), 'ser');
    $res['array_pad'] = attempt(static fn () => array_pad($x, 7, 'p'), $withNext);
    $res['array_pad_left'] = attempt(static fn () => array_pad($x, -7, 'p'), $withNext);
    foreach ([[0, null], [1, null], [-2, null], [1, 2], [1, -1], [-3, 2], [10, null], [0, 0], [2, -10]] as [$o, $l]) {
        $res["array_slice/$o/".($l ?? 'null')] = attempt(static fn () => array_slice($x, $o, $l), $withNext);
        $res["array_slice_keys/$o/".($l ?? 'null')] = attempt(static fn () => array_slice($x, $o, $l, true), $withNext);
        $res["array_splice/$o/".($l ?? 'null')] = attempt(static function () use ($x, $o, $l) {
            $c = $x;
            $removed = array_splice($c, $o, $l, ['r1', 'r2']);
            $c[] = 'next';
            $removed[] = 'next';

            return [$c, $removed];
        }, 'ser');
    }
    $res['array_pop'] = attempt(static function () use ($x) {
        $c = $x;
        $v = array_pop($c);
        $c[] = 'next';

        return [$v, $c];
    }, 'ser');
    $res['array_shift'] = attempt(static function () use ($x) {
        $c = $x;
        $v = array_shift($c);
        $c[] = 'next';

        return [$v, $c];
    }, 'ser');
    $res['array_unshift'] = attempt(static function () use ($x) {
        $c = $x;
        array_unshift($c, 'u1', 'u2');
        $c[] = 'next';

        return $c;
    }, 'ser');
    foreach ([1, '1', 'x', null, true, 0, '01', 1.0] as $i => $needle) {
        $res["search/$i"] = ser([array_search($needle, $x), array_search($needle, $x, true), in_array($needle, $x), in_array($needle, $x, true)]);
    }
    $cases[] = ['x' => ser($x), 'results' => $res];
}
write('array_unary', $cases);

// key coercion and next free index
$scripts = [
    [['set', -5], ['append']], [['set', 5], ['unset', 5], ['append']], [['append'], ['append'], ['unset', 1], ['append']],
    [['set', '7'], ['append']], [['set', PHP_INT_MAX], ['set', 'a']], [['set', -1], ['set', -3], ['append']], [['set', 1.7], ['append']],
    [['set', true], ['set', false], ['set', null], ['append']], [['set', '-0'], ['set', '00'], ['append']], [['set', PHP_INT_MIN], ['append']],
    [['set', '9223372036854775808'], ['append']], [['set', 3], ['pop'], ['append']], [['set', 3], ['set', 1], ['pop'], ['append']],
    [['append'], ['append'], ['shift'], ['append']], [['set', 'a'], ['set', 5], ['shift'], ['append']], [['set', 'a'], ['shift'], ['append']],
];
$cases = [];
foreach ($scripts as $script) {
    $a = [];
    foreach ($script as $op) {
        switch ($op[0]) {
            case 'set': $a[$op[1]] = 'v'; break;
            case 'append': $a[] = 'next'; break;
            case 'unset': unset($a[$op[1]]); break;
            case 'pop': array_pop($a); break;
            case 'shift': array_shift($a); break;
        }
    }
    $cases[] = ['script' => ser($script), 'result' => ser($a)];
}
write('array_keys_next', $cases);

// --- JSON ------------------------------------------------------------------

$jsonInputs = ['', ' ', 'null', 'true', 'false', 'nul', 'truex', '0', '-0', '01', '1.', '1.5', '-1.5e3', '1E+2', '1e', '.5', '-', '+1',
    '9223372036854775807', '9223372036854775808', '-9223372036854775808', '-9223372036854775809', '12345678901234567890123', '1e400', '-1e-400',
    '""', '"a"', '"\u00e9"', '"\ud83d\ude00"', '"\ud83d"', '"\ude00"', '"\ud83dx"', '"\u0000"', '"\x"', '"\u12"', '"a\tb"', "\"a\x01\"", "\"\x7f\"",
    "\"\xff\"", "\"a\xc3\"", "\"\xed\xa0\x80\"", '"\/\\\\\"\b\f\n\r\t"', '[]', '[1,2]', '[1,]', '[,1]', '[1 2]', '[1}', '{}', '{"a":1}', '{"a":1,}',
    '{"a" 1}', '{1:2}', '{"a":1}}', '{"a":1]', '[1]]', '{"":1}', '{"_empty_":1}', '{"0":1,"1":2}', '{"a":1,"a":2}', '{"\u0000a":1}', '{"a\u0000":1}',
    "[1]\x00", "\x00", ' [ 1 , { "a" : [ ] } ] ', "\t\n\r [1]", "\f[1]", '[1]x', '"\u00e9"x', '[[[[1]]]]', str_repeat('[', 513).str_repeat(']', 513),
    str_repeat('[', 512).str_repeat(']', 512), '{"a":{"b":{"c":[1,{"d":null}]}}}', '[1.0, 0.1, 1e2, -0.0]', '"' . str_repeat('x', 100) . '"', "\xef\xbb\xbf[1]",
    "[\"\xc3\xa9\", \"\xe2\x82\xac\"]", '{"a":"b","c":{"d":[1,2,{"e":"f"}]}}', '["\u2028\u2029"]', '[9007199254740993]', '[1E400]', '{"123":1,"-5":2,"05":3}'];
$cases = [];
foreach ($jsonInputs as $in) {
    $res = [];
    foreach (['assoc' => [true, 0], 'object' => [false, 0], 'bigint' => [true, JSON_BIGINT_AS_STRING], 'ignore' => [true, JSON_INVALID_UTF8_IGNORE],
        'substitute' => [true, JSON_INVALID_UTF8_SUBSTITUTE], 'object_bigint' => [false, JSON_BIGINT_AS_STRING]] as $name => [$assoc, $flags]) {
        $r = json_decode($in, $assoc, 512, $flags);
        $res[$name] = json_last_error() === JSON_ERROR_NONE ? ['value' => ser($r)] : ['error' => json_last_error_msg()];
    }
    foreach ([1, 2, 3] as $depth) {
        $r = json_decode($in, true, $depth);
        $res["depth$depth"] = json_last_error() === JSON_ERROR_NONE ? ['value' => ser($r)] : ['error' => json_last_error_msg()];
    }
    $cases[] = ['input' => enc($in), 'results' => $res];
}
write('json_decode', $cases);

$o = new stdClass();
$o->a = 1;
$o->{'0'} = 'zero';
$o->{''} = 'empty';
$encodeValues = [null, true, false, 0, -1, PHP_INT_MAX, PHP_INT_MIN, 0.0, -0.0, 1.0, 0.1, 1e25, 1e-7, NAN, INF, '', 'a', 'é', '€', '😀', "\u{2028}\u{2029}",
    '/', '\\', '"', "'", '<>&', "\x00\x01\x1f\x7f", "\t\n\r\f\x08", "\xff", "a\xc3", "\xed\xa0\x80", "\xc0\xaf", '123', '1.5', '1e3', '-0', ' 1', '0x1A', '1.0',
    [], [1, 2], [1 => 1], [0 => 'a', 2 => 'b'], ['a' => 1], ['a' => [], 'b' => new stdClass(), 'c' => [[]]], new stdClass(), $o, (object) ['x' => (object) []],
    [[1, [2, [3]]]], ['k' => "\xff", 'ok' => 1], ["\xff" => 1], ['a' => NAN], [1.0, 2.5], ['é' => ['/' => '€']], [-5 => 'x'], ['0' => 'a', '1' => 'b'],
    ['1' => 'a', '0' => 'b'], array_fill(0, 3, null)];
$encodeFlags = ['none' => 0, 'pretty' => JSON_PRETTY_PRINT, 'composer' => JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE,
    'slashes_unicode' => JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE, 'zero' => JSON_PRESERVE_ZERO_FRACTION, 'force' => JSON_FORCE_OBJECT,
    'force_pretty' => JSON_FORCE_OBJECT | JSON_PRETTY_PRINT, 'hex' => JSON_HEX_TAG | JSON_HEX_AMP | JSON_HEX_APOS | JSON_HEX_QUOT,
    'ignore' => JSON_INVALID_UTF8_IGNORE, 'substitute' => JSON_INVALID_UTF8_SUBSTITUTE, 'substitute_unicode' => JSON_INVALID_UTF8_SUBSTITUTE | JSON_UNESCAPED_UNICODE,
    'partial' => JSON_PARTIAL_OUTPUT_ON_ERROR, 'numeric' => JSON_NUMERIC_CHECK, 'line_terminators' => JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_LINE_TERMINATORS,
    'throw' => JSON_THROW_ON_ERROR];
$cases = [];
foreach ($encodeValues as $v) {
    $res = [];
    foreach ($encodeFlags as $name => $flags) {
        $r = attempt(static fn () => json_encode($v, $flags), static fn ($r) => $r);
        if (is_array($r)) {
            $res[$name] = $r;
            continue;
        }
        $res[$name] = ['result' => $r === false ? false : enc($r), 'error' => json_last_error_msg()];
    }
    $d = [];
    foreach ([1, 2, 3] as $depth) {
        $r = json_encode($v, 0, $depth);
        $d["depth$depth"] = ['result' => $r === false ? false : enc($r), 'error' => json_last_error_msg()];
    }
    $res += $d;
    $cases[] = ['value' => ser($v), 'results' => $res];
}
write('json_encode', $cases);

// --- var_export ------------------------------------------------------------

$o2 = new stdClass();
$o2->{"it's"} = "a\\b";
$o2->{"n\0ul"} = [1 => (object) []];
$exportValues = array_merge($encodeValues, [
    PHP_INT_MIN, "a'b\\c", "\0", "a\0b\0", "\\'", ['nested' => ['deep' => ['deeper' => [1, 2.5, null, true, "x'y"]]]], [-1 => -1, PHP_INT_MIN => 'min'],
    ["a\0" => "\0", "'" => '\\'], $o2, [[], [[]], new stdClass()], [1.0, -0.0, 1e100, 0.1],
]);
$cases = [];
foreach ($exportValues as $v) {
    $cases[] = ['value' => ser($v), 'var_export' => enc(var_export($v, true))];
}
write('var_export', $cases);

// --- string functions ------------------------------------------------------

$texts = ['', 'a', 'A', 'hello world', 'Hello World', 'HELLO', 'élan ÉLAN', "  \t\n padded \r\v\0 ", 'xxhixx', 'a-b_c d', "line1\nline2", 'ÄÖÜ äöü',
    'ß', "\xff\xfe", 'mIxEd CaSe', '123abc', 'the quick brown fox jumps over the lazy dog', 'a  b   c', 'averyveryverylongword short', '-a-b-',
    "multi\nline text that is long", 'ab', 'a b c d e f g', 'abcdefghij', 'a\\b', ' ', "tab\there"];
$cases = [];
foreach ($texts as $s) {
    $r = [
        'strtolower' => enc(strtolower($s)), 'strtoupper' => enc(strtoupper($s)), 'ucfirst' => enc(ucfirst($s)), 'lcfirst' => enc(lcfirst($s)),
        'ucwords' => enc(ucwords($s)), 'ucwords_dash' => enc(ucwords($s, '-')), 'ucwords_empty' => enc(ucwords($s, '')),
        'trim' => enc(trim($s)), 'ltrim' => enc(ltrim($s)), 'rtrim' => enc(rtrim($s)),
    ];
    foreach (['x', 'a..z', 'z..a', 'a..', '..a', 'a..b..c', '.', '..', '...', " \t", "\0..\x20", 'xh', ''] as $chars) {
        $r['trim/'.bin2hex($chars)] = attempt(static fn () => enc(trim($s, $chars)), static fn ($x) => $x);
        $r['ltrim/'.bin2hex($chars)] = attempt(static fn () => enc(ltrim($s, $chars)), static fn ($x) => $x);
        $r['rtrim/'.bin2hex($chars)] = attempt(static fn () => enc(rtrim($s, $chars)), static fn ($x) => $x);
    }
    foreach ([[10, ' ', STR_PAD_RIGHT], [10, '-=', STR_PAD_LEFT], [11, 'ab', STR_PAD_BOTH], [2, 'x', STR_PAD_BOTH], [-1, 'x', STR_PAD_LEFT], [5, '', STR_PAD_RIGHT]] as $i => [$len, $pad, $type]) {
        $r["str_pad/$i"] = attempt(static fn () => enc(str_pad($s, $len, $pad, $type)), static fn ($x) => $x);
    }
    foreach ([[5, "\n", false], [5, "\n", true], [10, '<br>', false], [3, '--', true], [1, ' ', false], [0, "\n", false], [0, "\n", true], [75, "\n", false], [5, '', false], [8, 'oo', true]] as $i => [$w, $b, $cut]) {
        $r["wordwrap/$i"] = attempt(static fn () => enc(wordwrap($s, $w, $b, $cut)), static fn ($x) => $x);
    }
    foreach ([[0, null], [1, null], [-2, null], [1, 2], [1, -1], [-3, 2], [100, null], [0, -100], [-100, 2]] as $i => [$o, $l]) {
        $r["substr/$i"] = enc(substr($s, $o, $l));
    }
    $r['strtr'] = enc(strtr($s, 'abc', 'xy'));
    $r['strtr_pairs'] = enc(strtr($s, ['a' => 'A', 'ab' => 'X', 'hello' => 'bye', '' => 'E', ' ' => '_']));
    $cases[] = ['value' => enc($s), 'results' => $r];
}
write('strings', $cases);

$natural = ['', 'a', 'A', 'a1', 'a2', 'a10', 'a01', 'a001', 'a 1', 'a  1', ' a1', 'img12.png', 'img10.png', 'IMG2.png', 'img1.png', '1', '01', '001', '10',
    '1.5', '1.10', '1.05', 'x0', 'x00', 'x', 'x-1', 'x-2', '1a', '1b', '0', '00', '000a', 'abc', 'ABC', 'abd', 'version 1.2.10', 'version 1.2.9',
    "a\t1", 'a 10', 'a 9', '099', '99', "1\0", 'ä1', 'Ä1', '0.1', '0.01', '1e3'];
$cases = [];
foreach ($natural as $a) {
    $row = [];
    foreach ($natural as $b) {
        $row[] = [strnatcmp($a, $b), strnatcasecmp($a, $b), strcmp($a, $b) <=> 0, strcasecmp($a, $b) <=> 0];
    }
    $cases[] = ['value' => $a, 'results' => $row];
}
write('strnatcmp', $cases);
