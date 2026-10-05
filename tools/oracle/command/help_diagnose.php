<?php
// Writes internal/command/testdata/help/diagnose.txt: the output of
// `composer help diagnose` from the reference Composer in .ref/composer,
// run in an empty directory with a throwaway COMPOSER_HOME.
//
// Usage (devenv shell, repo root): php tools/oracle/command/help_diagnose.php

$root = dirname(__DIR__, 3);
$tmp = sys_get_temp_dir().'/maestro-oracle-help-'.bin2hex(random_bytes(5));
mkdir($tmp.'/home', 0777, true);

$cmd = 'cd '.escapeshellarg($tmp)
    .' && COMPOSER_HOME='.escapeshellarg($tmp.'/home')
    .' COMPOSER_NO_INTERACTION=1 NO_COLOR=1 COLUMNS=80 '
    .escapeshellarg(PHP_BINARY).' '.escapeshellarg($root.'/.ref/composer/bin/composer')
    .' help diagnose --no-ansi 2>/dev/null';
$output = shell_exec($cmd);

exec('rm -rf '.escapeshellarg($tmp));

if (!is_string($output) || $output === '') {
    fwrite(STDERR, "no output\n");
    exit(1);
}

file_put_contents($root.'/internal/command/testdata/help/diagnose.txt', $output);
