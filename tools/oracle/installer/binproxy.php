<?php
// Generates internal/installer/testdata/oracle/binproxy.json: what
// Composer's BinaryInstaller (installBinaries, removeBinaries) does to a
// project directory for many bin configurations: PHP files with and
// without shebangs, shell scripts, phars, CRLF and whitespace variants,
// empty files, bins in subdirectories, missing bins, directories, bins
// escaping the package, the PHPUnit workaround, a null vendor dir, the
// three bin-compat modes, .bat and .exe bins, existing files, symlinks and
// .bat proxies in the bin dir, bin dirs inside and outside vendor, paths
// with spaces and quotes, and removal.
//
// Each scenario lists the files it starts from and records every file,
// symlink and directory under its root afterwards (contents base64, mode
// with umask 022), the IO output and the exception message, with the
// scenario's root replaced by "%ROOT%".
//
// Run: php tools/oracle/installer/binproxy.php [output file]
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Composer\Installer\BinaryInstaller;
use Composer\IO\BufferIO;
use Composer\Package\Package;
use Composer\Util\Filesystem;

set_error_handler(static function (int $level, string $message, string $file, int $line): bool {
    if ($level === E_DEPRECATED || $level === E_USER_DEPRECATED || 0 === (error_reporting() & $level)) {
        return true;
    }
    throw new \ErrorException($message, 0, $level, $file, $line);
});

umask(022);

$out = $argv[1] ?? dirname(__DIR__, 3).'/internal/installer/testdata/oracle/binproxy.json';

$phar = base64_decode('IyEvdXNyL2Jpbi9lbnYgcGhwCjw/cGhwCgpQaGFyOjptYXBQaGFyKCd0ZXN0LnBoYXInKTsKCnJlcXVpcmUgJ3BoYXI6Ly90ZXN0LnBoYXIvcnVuLnBocCc7CgpfX0hBTFRfQ09NUElMRVIoKTsgPz4NCj4AAAABAAAAEQAAAAEACQAAAHRlc3QucGhhcgAAAAAHAAAAcnVuLnBocCoAAADb9n9hKgAAAMUDDWGkAQAAAAAAADw/cGhwIGVjaG8gInN1Y2Nlc3MgIi4kX1NFUlZFUlsiYXJndiJdWzFdO1SOC0IE3+UN0yzrHIwyspp9slhmAgAAAEdCTUI=');

$contents = [
    'php' => "<?php\n\necho 'success '.\$_SERVER['argv'][1];\n",
    'php-shebang' => "#!/usr/bin/env php\n<?php\n\necho 'success '.\$_SERVER['argv'][1];\n",
    'php-declare' => "#!/usr/bin/env php\n<?php declare(strict_types=1);\n\necho 'success';\n",
    'php-crlf' => "#!/usr/bin/php\r\n<?php echo 1;\r\n",
    'php-space' => "\n\n \t<?php echo 1;\n",
    'php-shebang-space' => "#!/usr/bin/env php -d memory_limit=1G\n\n  <?php echo 1;\n",
    'php-late' => str_repeat('#', 600)."\n<?php echo 1;\n",
    'sh' => "#!/bin/sh\necho hello\n",
    'bash' => "#!/usr/bin/env bash\necho hello\n",
    'node' => "#!/usr/local/bin/node\nconsole.log(1)\n",
    'python' => "#!/usr/bin/env python3 -u\nprint(1)\n",
    'noshebang' => "echo hello\n",
    'empty' => '',
    'phar' => $phar,
    'bat' => "@echo off\r\necho hi\r\n",
];

$scenarios = [];
$add = static function (string $name, array $s) use (&$scenarios): void {
    $s += [
        'binCompat' => 'proxy',
        'vendorDir' => 'vendor',
        'binDir' => 'vendor/bin',
        'package' => 'foo/bar',
        'installPath' => 'vendor/foo/bar',
        'files' => [],
        'symlinks' => [],
        'dirs' => [],
        'steps' => [['install', true]],
    ];
    $s['name'] = $name;
    $scenarios[] = $s;
};

foreach (['proxy', 'full', 'auto'] as $compat) {
    foreach ($contents as $kind => $content) {
        $bin = $kind === 'bat' ? 'bin/tool.bat' : 'bin/tool';
        $add("$compat $kind", [
            'binCompat' => $compat,
            'binaries' => [$bin],
            'files' => ["vendor/foo/bar/$bin" => [$content, 0644]],
        ]);
    }
}

$add('exe bin full', ['binCompat' => 'full', 'binaries' => ['tool.exe'], 'files' => ['vendor/foo/bar/tool.exe' => ["MZ\0\0", 0755]]]);
$add('root bin', ['binaries' => ['tool'], 'files' => ['vendor/foo/bar/tool' => [$contents['php-shebang'], 0644]]]);
$add('several bins', [
    'binaries' => ['bin/a', 'bin/b', 'scripts/c.sh', 'missing', 'adir'],
    'files' => [
        'vendor/foo/bar/bin/a' => [$contents['php'], 0644],
        'vendor/foo/bar/bin/b' => [$contents['sh'], 0600],
        'vendor/foo/bar/scripts/c.sh' => [$contents['bash'], 0755],
    ],
    'dirs' => ['vendor/foo/bar/adir'],
]);
$add('null vendor dir', ['vendorDir' => null, 'binaries' => ['bin/tool'], 'files' => ['vendor/foo/bar/bin/tool' => [$contents['php-shebang'], 0644]]]);
$add('null vendor dir plain php', ['vendorDir' => null, 'binaries' => ['bin/tool'], 'files' => ['vendor/foo/bar/bin/tool' => [$contents['php'], 0644]]]);
$add('phpunit', ['package' => 'phpunit/phpunit', 'installPath' => 'vendor/phpunit/phpunit', 'binaries' => ['phpunit'], 'files' => ['vendor/phpunit/phpunit/phpunit' => [$contents['php-shebang'], 0644]]]);
$add('phpunit no shebang', ['package' => 'phpunit/phpunit', 'installPath' => 'vendor/phpunit/phpunit', 'binaries' => ['phpunit'], 'files' => ['vendor/phpunit/phpunit/phpunit' => [$contents['php'], 0644]]]);
$add('phpunit full', ['binCompat' => 'full', 'package' => 'phpunit/phpunit', 'installPath' => 'vendor/phpunit/phpunit', 'binaries' => ['phpunit'], 'files' => ['vendor/phpunit/phpunit/phpunit' => [$contents['php-shebang'], 0644]]]);
$add('bin dir outside vendor', ['binDir' => 'bin', 'binaries' => ['bin/tool', 'bin/sh'], 'files' => ['vendor/foo/bar/bin/tool' => [$contents['php-shebang'], 0644], 'vendor/foo/bar/bin/sh' => [$contents['sh'], 0644]]]);
$add('deep bin dir', ['binDir' => 'tools/x/bin', 'binaries' => ['bin/tool', 'bin/sh'], 'files' => ['vendor/foo/bar/bin/tool' => [$contents['php-shebang'], 0644], 'vendor/foo/bar/bin/sh' => [$contents['sh'], 0644]]]);
$add('vendor dir elsewhere', ['vendorDir' => 'lib/vendor', 'binDir' => 'lib/vendor/bin', 'installPath' => 'lib/vendor/foo/bar', 'binaries' => ['bin/tool'], 'files' => ['lib/vendor/foo/bar/bin/tool' => [$contents['php'], 0644]]]);
$add('custom install path', ['installPath' => 'custom/place', 'binaries' => ['bin/tool', 'bin/sh'], 'files' => ['custom/place/bin/tool' => [$contents['php-shebang'], 0644], 'custom/place/bin/sh' => [$contents['sh'], 0644]]]);
$add('spaces and quotes', ['installPath' => "vendor/foo/my pkg's", 'binaries' => ['b in/to ol', "b in/it's"], 'files' => ["vendor/foo/my pkg's/b in/to ol" => [$contents['sh'], 0644], "vendor/foo/my pkg's/b in/it's" => [$contents['php-shebang'], 0644]]]);
$add('spaces and quotes full', ['binCompat' => 'full', 'installPath' => "vendor/foo/my pkg's", 'binaries' => ['b in/to ol', "b in/it's"], 'files' => ["vendor/foo/my pkg's/b in/to ol" => [$contents['sh'], 0644], "vendor/foo/my pkg's/b in/it's" => [$contents['php-shebang'], 0644]]]);
$add('existing file', ['binaries' => ['bin/tool'], 'files' => ['vendor/foo/bar/bin/tool' => [$contents['php'], 0644], 'vendor/bin/tool' => ["mine\n", 0644]]]);
$add('existing file no warning', ['binaries' => ['bin/tool'], 'files' => ['vendor/foo/bar/bin/tool' => [$contents['php'], 0644], 'vendor/bin/tool' => ["mine\n", 0644]], 'steps' => [['install', false]]]);
$add('existing symlink to bin', ['binaries' => ['bin/tool'], 'files' => ['vendor/foo/bar/bin/tool' => [$contents['php'], 0644]], 'symlinks' => ['vendor/bin/tool' => '../foo/bar/bin/tool']]);
$add('existing symlink elsewhere', ['binaries' => ['bin/tool'], 'files' => ['vendor/foo/bar/bin/tool' => [$contents['php'], 0644], 'other/target' => ["other\n", 0644]], 'symlinks' => ['vendor/bin/tool' => '../../other/target']]);
$add('dangling symlink', ['binaries' => ['bin/tool'], 'files' => ['vendor/foo/bar/bin/tool' => [$contents['sh'], 0644]], 'symlinks' => ['vendor/bin/tool' => '../../nowhere'], 'dirs' => []]);
$add('existing bat', ['binCompat' => 'full', 'binaries' => ['bin/tool'], 'files' => ['vendor/foo/bar/bin/tool' => [$contents['php'], 0644], 'vendor/bin/tool.bat' => ["old\r\n", 0644]]]);
$add('escaping symlink', ['binaries' => ['bin/pwn'], 'files' => ['victim.sh' => ["#!/bin/sh\necho pwned\n", 0644]], 'symlinks' => ['vendor/foo/bar/bin/pwn' => '../../../../victim.sh']]);
$add('traversing bin', ['binaries' => ['../../../victim.sh'], 'files' => ['victim.sh' => ["#!/bin/sh\necho pwned\n", 0600]], 'dirs' => ['vendor/foo/bar']]);
$add('symlink inside package', ['binaries' => ['bin/tool'], 'files' => ['vendor/foo/bar/src/tool.php' => [$contents['php-shebang'], 0644]], 'symlinks' => ['vendor/foo/bar/bin/tool' => '../src/tool.php']]);
$add('install twice', ['binaries' => ['bin/tool'], 'files' => ['vendor/foo/bar/bin/tool' => [$contents['php'], 0644]], 'steps' => [['install', true], ['install', true], ['install', false]]]);
$add('remove', ['binCompat' => 'full', 'binaries' => ['bin/tool', 'bin/sh'], 'files' => ['vendor/foo/bar/bin/tool' => [$contents['php'], 0644], 'vendor/foo/bar/bin/sh' => [$contents['sh'], 0644]], 'steps' => [['install', true], ['remove']]]);
$add('remove keeps other files', ['binaries' => ['bin/tool'], 'files' => ['vendor/foo/bar/bin/tool' => [$contents['php'], 0644], 'vendor/bin/other' => ["x\n", 0644]], 'steps' => [['install', true], ['remove']]]);
$add('remove without binaries', ['binaries' => [], 'steps' => [['remove']]]);
$add('remove missing proxies', ['binaries' => ['bin/tool'], 'symlinks' => ['vendor/bin/tool' => 'nowhere'], 'steps' => [['remove']]]);
$add('no binaries', ['binaries' => [], 'steps' => [['install', true]]]);

$fs = new Filesystem();
$results = [];

foreach ($scenarios as $s) {
    $root = sys_get_temp_dir().'/maestro-binproxy-'.bin2hex(random_bytes(6));
    mkdir($root, 0777, true);
    $root = realpath($root);

    foreach ($s['dirs'] as $dir) {
        @mkdir($root.'/'.$dir, 0777, true);
    }
    foreach ($s['files'] as $path => [$content, $mode]) {
        @mkdir(dirname($root.'/'.$path), 0777, true);
        file_put_contents($root.'/'.$path, $content);
        chmod($root.'/'.$path, $mode);
    }
    foreach ($s['symlinks'] as $path => $target) {
        @mkdir(dirname($root.'/'.$path), 0777, true);
        symlink($target, $root.'/'.$path);
    }

    $io = new BufferIO();
    $vendorDir = $s['vendorDir'] === null ? null : $root.'/'.$s['vendorDir'];
    $installer = new BinaryInstaller($io, $root.'/'.$s['binDir'], $s['binCompat'], $fs, $vendorDir);
    $package = new Package($s['package'], '1.0.0.0', '1.0.0');
    $package->setBinaries($s['binaries']);

    $exception = null;
    try {
        foreach ($s['steps'] as $step) {
            if ($step[0] === 'install') {
                $installer->installBinaries($package, $root.'/'.$s['installPath'], $step[1]);
            } else {
                $installer->removeBinaries($package);
            }
        }
    } catch (\Throwable $e) {
        $exception = str_replace($root, '%ROOT%', $e->getMessage());
    }

    clearstatcache();
    $tree = [];
    $it = new RecursiveIteratorIterator(new RecursiveDirectoryIterator($root, FilesystemIterator::SKIP_DOTS), RecursiveIteratorIterator::SELF_FIRST);
    foreach ($it as $file) {
        $path = substr($file->getPathname(), strlen($root) + 1);
        if (is_link($file->getPathname())) {
            $tree[$path] = ['type' => 'link', 'target' => readlink($file->getPathname())];
        } elseif (is_dir($file->getPathname())) {
            $tree[$path] = ['type' => 'dir'];
        } else {
            $tree[$path] = [
                'type' => 'file',
                'mode' => sprintf('%04o', fileperms($file->getPathname()) & 0777),
                'content' => base64_encode(str_replace($root, '%ROOT%', file_get_contents($file->getPathname()))),
            ];
        }
    }
    ksort($tree, SORT_STRING);

    $files = [];
    foreach ($s['files'] as $path => [$content, $mode]) {
        $files[$path] = ['content' => base64_encode($content), 'mode' => sprintf('%04o', $mode)];
    }

    $results[] = [
        'name' => $s['name'],
        'binCompat' => $s['binCompat'],
        'vendorDir' => $s['vendorDir'],
        'binDir' => $s['binDir'],
        'package' => $s['package'],
        'installPath' => $s['installPath'],
        'binaries' => $s['binaries'],
        'files' => (object) $files,
        'symlinks' => (object) $s['symlinks'],
        'dirs' => $s['dirs'],
        'steps' => $s['steps'],
        'tree' => (object) $tree,
        'output' => str_replace($root, '%ROOT%', $io->getOutput()),
        'exception' => $exception,
    ];

    $fs->removeDirectory($root);
}

@mkdir(dirname($out), 0777, true);
file_put_contents($out, json_encode($results, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE)."\n");
fwrite(STDERR, count($results)." scenarios written to $out\n");
