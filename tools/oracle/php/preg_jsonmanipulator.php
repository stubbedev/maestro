<?php

/*
 * Writes internal/php/testdata/preg/jsonmanipulator/golden.json: PHP's
 * preg_match results for JsonManipulator's recursive (?&json) patterns
 * (patterns.json, as Composer\Json\JsonManipulator builds them) over a
 * 66 KB composer.json (composer.json), including which of them exhaust
 * pcre.backtrack_limit. Used by TestPregJsonManipulatorGolden and
 * BenchmarkPregJsonManipulator.
 *
 * Usage (from the repo root, inside the devenv shell):
 *   php tools/oracle/php/preg_jsonmanipulator.php
 */

declare(strict_types=1);

$dir = dirname(__DIR__, 3).'/internal/php/testdata/preg/jsonmanipulator';
$pats = json_decode(file_get_contents($dir.'/patterns.json'), false, 512, JSON_THROW_ON_ERROR);
$s = file_get_contents($dir.'/composer.json');
$out = [];
foreach ($pats as $p) {
    $r = preg_match($p, $s, $m, PREG_OFFSET_CAPTURE | PREG_UNMATCHED_AS_NULL);
    $offs = [];
    foreach ($m as $k => $v) {
        if (is_int($k)) {
            $offs[] = $v[1] === -1 ? null : [$v[1], strlen((string) $v[0])];
        }
    }
    $out[] = ['r' => $r, 'err' => preg_last_error_msg(), 'groups' => $offs];
}
file_put_contents($dir.'/golden.json', json_encode([
    'jit' => ini_get('pcre.jit'),
    'pcre' => PCRE_VERSION,
    'php' => PHP_VERSION,
    'results' => $out,
])."\n");
