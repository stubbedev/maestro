<?php

/*
 * Runs every pattern of a PCRE corpus against its subjects in real PHP and
 * writes the goldens the Go PCRE tests compare against:
 *   internal/php/testdata/preg/corpus.json -> golden.json  (Composer's patterns, see preg_collect.php)
 *   internal/php/testdata/preg/engine.json -> engine_golden.json  (engine features, see preg_engine.php)
 *
 * Usage (from the repo root, inside the devenv shell):
 *   php tools/oracle/php/preg_golden.php
 *
 * Strings that are not valid UTF-8 are written as {"base64": "..."}. Errors
 * are preg_last_error_msg() texts. Per subject it records:
 *   match        preg_match($p, $s, $m): [result, [[key, value], ...]]
 *   match_at     preg_match($p, $s, $m, 0, 2), the same way
 *   match_all    preg_match_all($p, $s, $m, PREG_SET_ORDER | PREG_UNMATCHED_AS_NULL | PREG_OFFSET_CAPTURE),
 *                each set as a list of [key, value|null, offset] in PHP's key order
 *   replace      preg_replace($p, '<$0|\1|${2}|$10>', $s, -1, $count): [result, count]
 *   replace1     the same with $limit = 1
 *   split        preg_split($p, $s)
 *   split2       preg_split($p, $s, 2)
 *   split_flags  preg_split($p, $s, -1, PREG_SPLIT_DELIM_CAPTURE | PREG_SPLIT_NO_EMPTY | PREG_SPLIT_OFFSET_CAPTURE)
 * Each is {"error": msg|null, "result": ...}. The output has one pattern
 * per line.
 */

declare(strict_types=1);

error_reporting(E_ALL);
ini_set('memory_limit', '-1');

$root = dirname(__DIR__, 3);
$dir = $root.'/internal/php/testdata/preg';

const REPLACEMENT = '<$0|\1|${2}|$10>';

function enc(?string $s)
{
    if ($s === null) {
        return null;
    }

    return preg_match('//u', $s) ? $s : ['base64' => base64_encode($s)];
}

function dec($v): string
{
    return is_array($v) ? base64_decode($v['base64'], true) : $v;
}

$warning = null;
set_error_handler(static function (int $no, string $msg) use (&$warning): bool {
    $warning = $msg;

    return true;
});

/**
 * Runs $fn, returning {error, result} where error is preg_last_error_msg()
 * when the call failed.
 */
function run(callable $fn, callable $encode): array
{
    global $warning;
    $warning = null;
    $r = $fn();
    if ($r === false || $r === null) {
        return ['error' => preg_last_error_msg(), 'result' => null];
    }

    return ['error' => null, 'result' => $encode($r)];
}

/** @param array<int|string, mixed> $m */
function pairs(array $m): array
{
    $out = [];
    foreach ($m as $k => $v) {
        $out[] = [$k, enc($v)];
    }

    return $out;
}

function golden(string $in, string $out): void
{
    global $warning;
    $corpus = json_decode((string) file_get_contents($in), true, 512, JSON_THROW_ON_ERROR);
    $lines = [];
    $results = 0;
    foreach ($corpus['patterns'] as $entry) {
        $p = dec($entry['pattern']);
        // A pattern that does not compile makes PHP warn; errors while
        // matching do not.
        $warning = null;
        if (preg_match($p, '') === false && $warning !== null) {
            $lines[] = ['pattern' => enc($p), 'valid' => false, 'compile_error' => $warning, 'results' => []];
            continue;
        }
        $res = [];
        foreach ($entry['subjects'] as $subj) {
            $s = dec($subj);
            $m = null;
            $r = [
                'subject' => enc($s),
                'match' => run(static function () use ($p, $s, &$m) {
                    return preg_match($p, $s, $m);
                }, static function ($r) use (&$m) {
                    return [$r, pairs($m)];
                }),
                'match_at' => run(static function () use ($p, $s, &$m) {
                    return preg_match($p, $s, $m, 0, 2);
                }, static function ($r) use (&$m) {
                    return [$r, pairs($m)];
                }),
                'match_all' => run(static function () use ($p, $s, &$m) {
                    return preg_match_all($p, $s, $m, PREG_SET_ORDER | PREG_UNMATCHED_AS_NULL | PREG_OFFSET_CAPTURE);
                }, static function ($r) use (&$m) {
                    $sets = [];
                    foreach ($m as $set) {
                        $triples = [];
                        foreach ($set as $k => [$v, $off]) {
                            $triples[] = [$k, enc($v), $off];
                        }
                        $sets[] = $triples;
                    }

                    return $sets;
                }),
            ];
            foreach (['replace' => -1, 'replace1' => 1] as $name => $limit) {
                $c = 0;
                $r[$name] = run(static function () use ($p, $s, $limit, &$c) {
                    return preg_replace($p, REPLACEMENT, $s, $limit, $c);
                }, static function ($x) use (&$c) {
                    return [enc($x), $c];
                });
            }
            $r['split'] = run(static fn () => preg_split($p, $s), static fn ($x) => array_map('enc', $x));
            $r['split2'] = run(static fn () => preg_split($p, $s, 2), static fn ($x) => array_map('enc', $x));
            $r['split_flags'] = run(
                static fn () => preg_split($p, $s, -1, PREG_SPLIT_DELIM_CAPTURE | PREG_SPLIT_NO_EMPTY | PREG_SPLIT_OFFSET_CAPTURE),
                static fn ($x) => array_map(static fn ($y) => [enc($y[0]), $y[1]], $x)
            );
            $res[] = $r;
            $results++;
        }
        $lines[] = ['pattern' => enc($p), 'valid' => true, 'compile_error' => null, 'results' => $res];
    }

    $flags = JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR;
    $json = '{"php_version":'.json_encode(PHP_VERSION).',"pcre_version":'.json_encode(PCRE_VERSION).",\"patterns\":[\n";
    $json .= implode(",\n", array_map(static fn ($l) => json_encode($l, $flags), $lines));
    $json .= "\n]}\n";
    file_put_contents($out, $json);
    fwrite(STDERR, sprintf("%s: %d patterns, %d subjects, %d bytes\n", basename($out), count($lines), $results, strlen($json)));
}

golden($dir.'/corpus.json', $dir.'/golden.json');
golden($dir.'/engine.json', $dir.'/engine_golden.json');
