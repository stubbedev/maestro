<?php
// Shared helpers for the internal/pkg oracle generators. Every generator
// runs the real Composer 2.10.3 from .ref/composer and writes goldens into
// the testdata/ of the Go package it covers.

$root = dirname(__DIR__, 3);
require_once $root.'/.ref/composer/vendor/autoload.php';

mt_srand(20261005);

set_error_handler(static function (): bool {
    return true; // warnings and deprecations are not part of the goldens
});

const JSON_FLAGS = JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_PRESERVE_ZERO_FRACTION;

function pick(array $a) { return $a[mt_rand(0, count($a) - 1)]; }

function chance(int $percent): bool { return mt_rand(1, 100) <= $percent; }

/** json_encode as the Go side re-encodes values to compare them. */
function enc($v): string { return json_encode($v, JSON_FLAGS | JSON_THROW_ON_ERROR); }

/** Runs $f, returning its result or the exception it throws. */
function attempt(callable $f)
{
    try {
        return $f();
    } catch (\Throwable $e) {
        return ['e' => exception($e)];
    }
}

/**
 * $e as the goldens record it: its class and message, without the call
 * site PHP appends to TypeErrors (", called in X on line N"), which names
 * the machine's checkout and which maestro does not port.
 *
 * @return array{string, string}
 */
function exception(\Throwable $e): array
{
    return [get_class($e), preg_replace('{, called in .* on line \d+$}', '', $e->getMessage())];
}

/** Writes $data as JSON, one top-level entry per line; .gz paths are gzipped. */
function write_golden(string $path, array $data): void
{
    $lines = [];
    foreach ($data as $k => $v) {
        $lines[] = json_encode((string) $k).': '.json_encode($v, JSON_FLAGS | JSON_THROW_ON_ERROR);
    }
    $out = "{\n".implode(",\n", $lines)."\n}\n";
    if (substr($path, -3) === '.gz') {
        $out = gzencode($out, 9);
    }
    @mkdir(dirname($path), 0777, true);
    file_put_contents($path, $out);
    fprintf(STDERR, "%s: %d entries, %d bytes\n", $path, count($data), strlen($out));
}

/** MetadataMinifier::expand. */
function expand(array $versions): array
{
    return \Composer\MetadataMinifier\MetadataMinifier::expand($versions);
}

/** The p2 inputs, name => expanded versions. */
function p2_inputs(string $root): array
{
    $out = [];
    foreach (glob($root.'/internal/pkg/loader/testdata/oracle/p2/*.json.gz') as $file) {
        $data = json_decode(gzdecode(file_get_contents($file)), true, 512, JSON_THROW_ON_ERROR);
        foreach ($data['packages'] as $name => $versions) {
            $out[basename($file, '.json.gz')] = expand($versions);
        }
    }
    ksort($out);

    return $out;
}
