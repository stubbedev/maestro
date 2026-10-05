<?php
// Generates internal/pkg/comparer/testdata/oracle/comparer.json: pairs of
// random directory trees and Comparer::getChanged(true) and
// getChangedAsString for them. A tree is a list of [path, kind, content]
// with kind "file", "dir" or "link".
//
// Run: php tools/oracle/pkg/comparer.php
require __DIR__.'/common.php';

use Composer\Package\Comparer\Comparer;
use Composer\Util\Filesystem;

function build_tree(string $dir, array $entries): void
{
    @mkdir($dir, 0777, true);
    foreach ($entries as [$path, $kind, $content]) {
        $full = $dir.'/'.$path;
        @mkdir(dirname($full), 0777, true);
        if ($kind === 'dir') {
            @mkdir($full, 0777, true);
        } elseif ($kind === 'link') {
            @symlink($content, $full);
        } else {
            file_put_contents($full, $content);
        }
    }
}

$names = ['a.php', 'b.txt', 'c', 'README', 'x.json', '1', '10'];
$dirs = ['', 'src', 'src/sub', 'lib', 'empty'];
$cases = [];
$fs = new Filesystem();
for ($i = 0; $i < 150; $i++) {
    $source = [];
    for ($j = mt_rand(0, 8); $j > 0; $j--) {
        $dir = pick($dirs);
        $path = ltrim($dir.'/'.pick($names), '/');
        $kind = chance(10) ? 'dir' : (chance(8) ? 'link' : 'file');
        $content = $kind === 'link' ? pick(['target', '../x', 'a.php']) : pick(['', 'one', 'two', 'three']);
        $source[$path] = [$path, $kind, $content];
    }
    $update = $source;
    foreach ($update as $path => $entry) {
        if (chance(25)) {
            unset($update[$path]);
        } elseif (chance(30) && $entry[1] === 'file') {
            $update[$path][2] = pick(['', 'one', 'changed', 'three']);
        }
    }
    for ($j = mt_rand(0, 3); $j > 0; $j--) {
        $path = ltrim(pick($dirs).'/'.pick($names), '/');
        if (!isset($update[$path])) {
            $update[$path] = [$path, 'file', pick(['', 'new'])];
        }
    }
    // a path cannot be both a file and a directory
    foreach ([&$source, &$update] as &$tree) {
        foreach (array_keys($tree) as $path) {
            foreach (array_keys($tree) as $other) {
                if (strpos($other, $path.'/') === 0) {
                    unset($tree[$path]);
                    break;
                }
            }
        }
    }
    unset($tree);

    $base = sys_get_temp_dir().'/maestro-comparer-'.getmypid();
    $fs->removeDirectory($base);
    build_tree($base.'/source', array_values($source));
    build_tree($base.'/update', array_values($update));
    $comparer = new Comparer();
    $comparer->setSource($base.'/source');
    $comparer->setUpdate($base.'/update');
    $comparer->doCompare();
    $cases[(string) $i] = [array_values($source), array_values($update), $comparer->getChanged(true), $comparer->getChanged(false), $comparer->getChangedAsString(true)];
    $fs->removeDirectory($base);
}
write_golden($root.'/internal/pkg/comparer/testdata/oracle/comparer.json', $cases);
