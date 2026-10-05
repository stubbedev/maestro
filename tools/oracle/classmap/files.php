<?php
// Generates internal/classmap/testdata/oracle/files.json: for every file of
// the synthetic set (testdata/synthetic) and the class-map-generator test
// fixtures (testdata/tests/Fixtures), the php_strip_whitespace() output
// (base64, omitted above 4 KiB) and the record() of common.php.
// Run: php tools/oracle/classmap/files.php
require __DIR__.'/common.php';

$testdata = dirname(__DIR__, 3).'/internal/classmap/testdata';
$out = [];
foreach (['synthetic', 'tests/Fixtures'] as $dir) {
    $it = new RecursiveIteratorIterator(new RecursiveDirectoryIterator($testdata.'/'.$dir, FilesystemIterator::SKIP_DOTS));
    $paths = [];
    foreach ($it as $file) {
        $paths[] = substr($file->getPathname(), strlen($testdata) + 1);
    }
    sort($paths, SORT_STRING);
    foreach ($paths as $path) {
        $strip = (string) @php_strip_whitespace($testdata.'/'.$path);
        $out[] = [
            'path' => $path,
            'record' => record($testdata.'/'.$path, $testdata.'/'),
            'strip' => strlen($strip) <= 4096 ? base64_encode($strip) : null,
        ];
    }
}
file_put_contents($testdata.'/oracle/files.json', json_encode($out, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES)."\n");
