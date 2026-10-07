<?php
// Generates internal/pkg/comparer/testdata/oracle/comparer.json: pairs of
// random directory trees and Comparer::getChanged(true) and
// getChangedAsString for them, then fixed cases of what may not be read.
// A tree is a list of [path, kind, content] with kind "file", "dir",
// "link", "unreadable" (a file nobody may read: run it as another user
// than root) or "hardlink" (in the update tree, a hard link to the source
// tree's file at content).
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
        } elseif ($kind === 'hardlink') {
            link(dirname($dir).'/source/'.$content, $full);
        } else {
            file_put_contents($full, $content);
            if ($kind === 'unreadable') {
                chmod($full, 0);
            }
        }
    }
}

function compare_trees(array $source, array $update): array
{
    global $fs;
    $base = sys_get_temp_dir().'/maestro-comparer-'.getmypid();
    $fs->removeDirectory($base);
    build_tree($base.'/source', $source);
    build_tree($base.'/update', $update);
    $comparer = new Comparer();
    $comparer->setSource($base.'/source');
    $comparer->setUpdate($base.'/update');
    $comparer->doCompare();
    $case = [$source, $update, $comparer->getChanged(true), $comparer->getChanged(false), $comparer->getChangedAsString(true)];
    $fs->removeDirectory($base);

    return $case;
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

    $cases[(string) $i] = compare_trees(array_values($source), array_values($update));
}

// what hash_file cannot read is false, which only equals false; a hard
// link is the same file
$fixed = [
    'unreadable both, same size' => [[['a.php', 'unreadable', 'one']], [['a.php', 'unreadable', 'two']]],
    'unreadable both, other sizes' => [[['a.php', 'unreadable', 'one']], [['a.php', 'unreadable', 'three']]],
    'unreadable update, same size' => [[['a.php', 'file', 'one']], [['a.php', 'unreadable', 'two']]],
    'unreadable update, other size' => [[['a.php', 'file', 'one']], [['a.php', 'unreadable', 'three']]],
    'unreadable source, other size' => [[['a.php', 'unreadable', 'one']], [['a.php', 'file', 'three']]],
    'unreadable only in update' => [[['a.php', 'file', 'one']], [['a.php', 'file', 'one'], ['b.php', 'unreadable', 'new']]],
    'unreadable only in source' => [[['a.php', 'file', 'one'], ['b.php', 'unreadable', 'old']], [['a.php', 'file', 'one']]],
    'hard link' => [[['src/a.php', 'file', 'one']], [['src/a.php', 'hardlink', 'src/a.php']]],
    'same size, other content' => [[['a.php', 'file', 'one']], [['a.php', 'file', 'two']]],
    'file and link' => [[['a.php', 'file', 'one']], [['a.php', 'link', 'target']]],
    'link and file' => [[['a.php', 'link', 'target']], [['a.php', 'file', 'target']]],
];
foreach ($fixed as $name => [$source, $update]) {
    $cases[$name] = compare_trees($source, $update);
}
write_golden($root.'/internal/pkg/comparer/testdata/oracle/comparer.json', $cases);
