<?php

/*
 * The parent class and interfaces of every exception class
 * internal/phperr's class table names, as PHP 8.4 with Composer 2.10.3
 * (.ref/composer, its vendor/) and maestro's shim loaded reports them:
 * writes internal/phperr/testdata/oracle/classes.json ({class: {parent:
 * parent|null, interfaces: [...]}}, null for a root; a class PHP does not
 * know is left out).
 *
 * Usage (from the repo root, inside the devenv shell):
 *   php tools/oracle/phperr/classes.php
 */

declare(strict_types=1);

$root = dirname(__DIR__, 3);
require $root.'/.ref/composer/vendor/autoload.php';
require $root.'/internal/plugin/php/src/Maestro/Shim/UnsupportedApiException.php';

$source = (string) file_get_contents($root.'/internal/phperr/classes.go');
$start = (int) strpos($source, 'var parents = map[string]string{');
$table = substr($source, $start, (int) strpos($source, "\n}\n", $start) - $start);
preg_match_all('{^\s*(?:"([^"]+)"|`([^`]+)`)\s*:}m', $table, $m, PREG_SET_ORDER);

$out = [];
foreach ($m as $match) {
    $class = $match[1] !== '' ? $match[1] : $match[2];
    if (!class_exists($class)) {
        continue;
    }
    $parent = get_parent_class($class);
    $interfaces = array_values(class_implements($class));
    sort($interfaces);
    $out[$class] = ['parent' => $parent === false ? null : $parent, 'interfaces' => $interfaces];
}
ksort($out);

file_put_contents($root.'/internal/phperr/testdata/oracle/classes.json', json_encode($out, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_THROW_ON_ERROR)."\n");
fwrite(STDERR, count($out)." of ".count($m)." classes\n");
