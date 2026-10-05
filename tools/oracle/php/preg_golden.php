<?php

/*
 * Runs every pattern of internal/php/testdata/preg/corpus.json against its
 * subjects in real PHP and writes internal/php/testdata/preg/golden.json,
 * which the Go PCRE tests compare against.
 *
 * Usage (from the repo root, inside `nix develop`):
 *   php tools/oracle/php/preg_golden.php
 *
 * Strings that are not valid UTF-8 are written as {"base64": "..."}.
 * Per subject it records:
 *   match_all   preg_match_all($p, $s, $m, PREG_SET_ORDER | PREG_UNMATCHED_AS_NULL | PREG_OFFSET_CAPTURE),
 *               each set as a list of [key, value|null, offset] in PHP's key order
 *   replace     preg_replace($p, '<$0|\1|${2}|$10>', $s, -1, $count)
 *   split       preg_split($p, $s, -1, 0)
 *   split_flags preg_split($p, $s, -1, PREG_SPLIT_DELIM_CAPTURE | PREG_SPLIT_NO_EMPTY | PREG_SPLIT_OFFSET_CAPTURE)
 */

declare(strict_types=1);

error_reporting(E_ALL);
ini_set('memory_limit', '-1');

$root = dirname(__DIR__, 3);
$in = $root.'/internal/php/testdata/preg/corpus.json';
$out = $root.'/internal/php/testdata/preg/golden.json';

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
 * Runs $fn, returning [result, error] where error is preg_last_error_msg()
 * (or the warning text) when the call failed.
 */
function run(callable $fn): array
{
    global $warning;
    $warning = null;
    $r = $fn();
    if ($r === false || $r === null) {
        $err = preg_last_error() !== PREG_NO_ERROR ? preg_last_error_msg() : ($warning ?? 'unknown error');

        return [null, $err];
    }

    return [$r, null];
}

$corpus = json_decode((string) file_get_contents($in), true, 512, JSON_THROW_ON_ERROR);
$patterns = [];
foreach ($corpus['patterns'] as $entry) {
    $p = dec($entry['pattern']);
    $warning = null;
    $ok = preg_match($p, '');
    if ($ok === false) {
        $patterns[] = ['pattern' => enc($p), 'valid' => false, 'compile_error' => $warning ?? preg_last_error_msg(), 'results' => []];
        continue;
    }
    $results = [];
    foreach ($entry['subjects'] as $subj) {
        $s = dec($subj);

        $m = null;
        [$count, $err] = run(static function () use ($p, $s, &$m) {
            return preg_match_all($p, $s, $m, PREG_SET_ORDER | PREG_UNMATCHED_AS_NULL | PREG_OFFSET_CAPTURE);
        });
        $sets = [];
        if ($err === null) {
            foreach ($m as $set) {
                $triples = [];
                foreach ($set as $k => [$v, $off]) {
                    $triples[] = [$k, enc($v), $off];
                }
                $sets[] = $triples;
            }
        }
        $matchAll = ['error' => $err, 'count' => $count, 'sets' => $sets];

        $c = 0;
        [$r, $err] = run(static function () use ($p, $s, &$c) {
            return preg_replace($p, REPLACEMENT, $s, -1, $c);
        });
        $replace = ['error' => $err, 'result' => enc($r), 'count' => $c];

        [$r, $err] = run(static fn () => preg_split($p, $s, -1, 0));
        $split = ['error' => $err, 'result' => $r === null ? null : array_map('enc', $r)];

        [$r, $err] = run(static fn () => preg_split($p, $s, -1, PREG_SPLIT_DELIM_CAPTURE | PREG_SPLIT_NO_EMPTY | PREG_SPLIT_OFFSET_CAPTURE));
        $splitFlags = ['error' => $err, 'result' => $r === null ? null : array_map(static fn ($x) => [enc($x[0]), $x[1]], $r)];

        $results[] = [
            'subject' => enc($s),
            'match_all' => $matchAll,
            'replace' => $replace,
            'split' => $split,
            'split_flags' => $splitFlags,
        ];
    }
    $patterns[] = ['pattern' => enc($p), 'valid' => true, 'compile_error' => null, 'results' => $results];
}

$json = json_encode(
    ['php_version' => PHP_VERSION, 'pcre_version' => PCRE_VERSION, 'patterns' => $patterns],
    JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR
);
file_put_contents($out, $json."\n");
fwrite(STDERR, sprintf("%d patterns, %d results, %d bytes\n", count($patterns), array_sum(array_map(static fn ($p) => count($p['results']), $patterns)), strlen($json) + 1));
