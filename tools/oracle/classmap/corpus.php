<?php
// Generates internal/classmap/testdata/oracle/corpus.golden: one line per
// .php file under .ref/composer/{src,vendor,tests}, "<relative path>\t<record>"
// (see record() in common.php), sorted by path.
// Run: php tools/oracle/classmap/corpus.php
require __DIR__.'/common.php';

$root = dirname(__DIR__, 3).'/.ref/composer';
$paths = [];
foreach (['src', 'vendor', 'tests'] as $dir) {
    $it = new RecursiveIteratorIterator(new RecursiveDirectoryIterator($root.'/'.$dir, FilesystemIterator::SKIP_DOTS));
    foreach ($it as $file) {
        if (substr($file->getFilename(), -4) === '.php') {
            $paths[] = substr($file->getPathname(), strlen($root) + 1);
        }
    }
}
sort($paths, SORT_STRING);
$out = '';
foreach ($paths as $path) {
    $out .= $path."\t".record($root.'/'.$path, $root.'/')."\n";
}
file_put_contents(dirname(__DIR__, 3).'/internal/classmap/testdata/oracle/corpus.golden', $out);
