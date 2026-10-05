<?php

/*
 * Writes internal/php/testdata/preg/counts.json: for every pattern and
 * subject of golden.json, engine_golden.json and the JsonManipulator
 * workload (jsonmanipulator/), how far preg_match($p, $s, $m) gets
 * against pcre.backtrack_limit, with the JIT (as PHP runs it) and with
 * pcre2_match (pcre.jit=0, as PHP runs the anchored retry after an empty
 * match). Each count is the smallest pcre.backtrack_limit at which the
 * match does not fail with PREG_BACKTRACK_LIMIT_ERROR, that is the
 * highest count of any start position, or -1 above 4000000.
 *
 * Usage (from the repo root, inside the devenv shell):
 *   php tools/oracle/php/preg_counts.php
 */

declare(strict_types=1);

error_reporting(E_ALL);
ini_set('memory_limit', '-1');

$dir = dirname(__DIR__, 3).'/internal/php/testdata/preg';

/** @return list<array{string, string}> */
function cases(string $dir): array
{
    $dec = static fn ($v): string => is_array($v) ? base64_decode($v['base64']) : $v;
    $out = [];
    foreach (['golden.json', 'engine_golden.json'] as $f) {
        foreach (json_decode(file_get_contents($dir.'/'.$f), true, 512, JSON_THROW_ON_ERROR)['patterns'] as $p) {
            if (!$p['valid']) {
                continue;
            }
            foreach ($p['results'] as $r) {
                $out[] = [$dec($p['pattern']), $dec($r['subject'])];
            }
        }
    }
    $subject = file_get_contents($dir.'/jsonmanipulator/composer.json');
    foreach (json_decode(file_get_contents($dir.'/jsonmanipulator/patterns.json'), true, 512, JSON_THROW_ON_ERROR) as $p) {
        $out[] = [$p, $subject];
    }

    return $out;
}

/** @return list<int> */
function counts(array $cases): array
{
    $err = static function (string $p, string $s, int $limit): bool {
        ini_set('pcre.backtrack_limit', (string) $limit);
        @preg_match($p, $s, $m);

        return preg_last_error() === PREG_BACKTRACK_LIMIT_ERROR;
    };
    $out = [];
    foreach ($cases as [$p, $s]) {
        $hi = 4000000;
        if ($err($p, $s, $hi)) {
            $out[] = -1;
            continue;
        }
        $lo = -1;
        while ($hi - $lo > 1) {
            $mid = intdiv($lo + $hi, 2);
            if ($err($p, $s, $mid)) {
                $lo = $mid;
            } else {
                $hi = $mid;
            }
        }
        $out[] = $hi;
    }

    return $out;
}

if (($argv[1] ?? '') === '--counts') {
    // A child run: pcre.jit is set on the command line, since patterns
    // compiled with the JIT keep using it.
    echo json_encode(counts(cases($dir)));
    exit(0);
}

$run = static function (string $jit): array {
    $out = shell_exec(escapeshellarg(PHP_BINARY).' -d pcre.jit='.$jit.' '.escapeshellarg(__FILE__).' --counts');

    return json_decode((string) $out, true, 512, JSON_THROW_ON_ERROR);
};
$jit = $run('1');
$interp = $run('0');
$rows = [];
foreach ($jit as $i => $n) {
    $rows[] = [$n, $interp[$i]];
}
file_put_contents($dir.'/counts.json', json_encode([
    'php' => PHP_VERSION,
    'pcre' => PCRE_VERSION,
    'counts' => $rows,
])."\n");
fwrite(STDERR, count($rows)." cases\n");
