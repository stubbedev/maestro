<?php
// Generates the goldens of internal/pkg/archiver: generated package trees
// archived by Composer 2.10.3's ArchivableFilesFinder, PharArchiver,
// ZipArchiver and ArchiveManager (root packages), and
// ArchiveManager::getPackageFilenameParts over generated packages.
//
// Run (in the devenv shell): php tools/oracle/archiver/archiver.php
//
// Each tree is created under a work directory, <W>, as <W>/pkg (the
// sources) beside <W>/outside and <W>/pkg2 (symbolic link targets);
// messages carry "<W>" in place of the work directory. Archives are
// stored as written (base64); the Go test normalizes the entry timestamps.

$root = dirname(__DIR__, 3);
require_once $root.'/.ref/composer/vendor/autoload.php';

use Composer\Package\Archiver\ArchivableFilesFinder;
use Composer\Package\Archiver\ArchiveManager;
use Composer\Package\Archiver\PharArchiver;
use Composer\Package\Archiver\ZipArchiver;
use Composer\Package\CompletePackage;
use Composer\Package\RootPackage;
use Composer\Util\Filesystem;

\Composer\Util\ErrorHandler::register();
// libzip and PharData's zip writer store DOS times in local time; the Go
// test (internal/pkg/archiver/main_test.go) runs in the same zone.
putenv('TZ=Europe/Copenhagen');
date_default_timezone_set('Europe/Copenhagen');
mt_srand(20261005);
umask(022);

const JSON_FLAGS = JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_PRESERVE_ZERO_FRACTION;

function pick(array $a) { return $a[mt_rand(0, count($a) - 1)]; }
function chance(int $percent): bool { return mt_rand(1, 100) <= $percent; }

/** Runs $f, returning ['ok' => result] or ['e' => [class, message]]. */
function attempt(callable $f, string $work): array
{
    try {
        return ['ok' => $f()];
    } catch (\Throwable $e) {
        return ['e' => [get_class($e), str_replace($work, '<W>', $e->getMessage())]];
    }
}

function randomContent(): string
{
    switch (mt_rand(0, 4)) {
        case 0: return '';
        case 1: return str_repeat(pick(['a', 'ab', "line\n", '<?php echo 1;']), mt_rand(1, 3000));
        case 2: $s = ''; for ($i = mt_rand(1, 400); $i > 0; $i--) { $s .= chr(mt_rand(0, 255)); } return $s;
        case 3: return pick(['x', 'hello', "{\n    \"name\": \"a/b\"\n}\n", "\0\0\0"]);
        default: $s = ''; for ($i = mt_rand(1, 60); $i > 0; $i--) { $s .= pick(['foo ', 'bar ', 'baz', "\n", 'qux ']); } return $s;
    }
}

function randomName(): string
{
    $names = ['a', 'b', 'src', 'lib', 'tests', 'docs', 'README.md', 'composer.json', 'file.txt', 'x.php', 'y.PHP',
        '.hidden', '.gitignore', '.git', '.svn', 'CVS', '.hg', '_darcs', 'vendor', 'build', 'prefixA.foo', 'prefixB.foo',
        'sp ace', 'ünïcödé', '#weird', '!bang', 'br{a,b}ce', 'sq[1]', 'q?', 'dollar$', 'paren(1)', 'plus+', 'caret^',
        'p.phar', '.pharx', 'tilde~', 'percent%41', 'e', 'f', 'g', 'parameters.yml', 'parameters.yml.dist', 'node_modules'];
    if (chance(3)) {
        return str_repeat(pick(['d', 'long']), mt_rand(20, 60));
    }
    if (chance(2)) {
        return pick(['star*', 'back\\slash', "ctl\x01x", "ctl\x1ax", 'colon:x']);
    }

    return pick($names);
}

function randomGlob(): string
{
    $g = pick(['*.md', '/tests', 'tests/', '/docs/', 'prefixA.foo', '!/prefixA.foo', '!*.md', '.*', '/*', '**/x.php',
        'src/**', '/src/**/b', 'lib/*.php', '{a,b}', 'a?', '\*', 'vendor', '!vendor/b', '/build/', 'e/f', '*', '!/e',
        '/README.md', 'sq[1]', 'br{a,b}ce', '!.git*', '#weird', '\#weird', '\!bang', '!!bang', 'parameters.yml', '/f/',
        '**', 'src/', '/a/b/', 'ünï*', '{unclosed', '[x', 'dollar$', '*/*/x.php', '*.PHP']);
    return $g;
}

/** A random tree: [path, type, content|target, mode, mtime]. */
function randomTree(): array
{
    $entries = [];
    $dirs = [''];
    $count = mt_rand(0, 25);
    for ($i = 0; $i < $count; $i++) {
        $parent = pick($dirs);
        $path = ($parent === '' ? '' : $parent.'/').randomName();
        if (isset($entries[$path])) {
            continue;
        }
        $mtime = mt_rand(315532800, 1900000000);
        $r = mt_rand(1, 100);
        if ($r <= 55) {
            $entries[$path] = [$path, 'file', randomContent(), pick([0644, 0644, 0755, 0600, 0664, 0444, 04755]), $mtime];
        } elseif ($r <= 85) {
            $entries[$path] = [$path, 'dir', null, pick([0755, 0755, 0700, 0777, 0750]), $mtime];
            $dirs[] = $path;
        } else {
            $target = pick(['../outside/file', '../outside', '../pkg2/file', 'missing', '/', 'README.md', 'a', 'src', 'e/f', '../pkg/a']);
            $entries[$path] = [$path, 'link', $target, 0, 0];
        }
    }
    if (chance(40)) {
        $lines = [];
        for ($i = mt_rand(1, 8); $i > 0; $i--) {
            $lines[] = pick(['', '# comment', randomGlob().' export-ignore', randomGlob().' -export-ignore', randomGlob()."\texport-ignore",
                randomGlob().' text', randomGlob().' export-ignore diff', '  '.randomGlob().'  export-ignore  ']);
        }
        $entries['.gitattributes'] = ['.gitattributes', 'file', implode(pick(["\n", "\r\n"]), $lines), 0644, 1600000000];
    }
    if (chance(5)) {
        $entries['locked'] = ['locked', 'file', 'secret', 0000, 1600000000];
    }

    return array_values($entries);
}

/** The tree as the golden holds it: file contents in base64. */
function encodeTree(array $tree): array
{
    return array_map(static function ($e) {
        $e[2] = $e[1] === 'file' ? base64_encode($e[2]) : $e[2];

        return $e;
    }, $tree);
}

function makeTree(string $work, array $tree): void
{
    $fs = new Filesystem();
    $fs->removeDirectory($work);
    mkdir($work.'/pkg', 0777, true);
    mkdir($work.'/outside', 0777, true);
    mkdir($work.'/pkg2', 0777, true);
    file_put_contents($work.'/outside/file', 'outside');
    file_put_contents($work.'/pkg2/file', 'pkg2');
    $dirs = [];
    foreach ($tree as [$path, $type, $content, $mode, $mtime]) {
        $full = $work.'/pkg/'.$path;
        if ($type === 'file') {
            file_put_contents($full, $content);
            chmod($full, $mode);
            touch($full, $mtime);
        } elseif ($type === 'dir') {
            mkdir($full);
            $dirs[] = [$full, $mode, $mtime];
        } else {
            // symlink() resolves relative targets against the working
            // directory and fails when they do not exist there
            exec('ln -s '.escapeshellarg($content).' '.escapeshellarg($full), $output, $code);
            if ($code !== 0) {
                throw new \RuntimeException('ln -s failed for '.$full);
            }
        }
    }
    // deepest first, so creating entries does not change their times
    foreach (array_reverse($dirs) as [$full, $mode, $mtime]) {
        chmod($full, $mode);
        touch($full, $mtime);
    }
}

function finderList(string $sources, array $excludes, bool $ignore, string $work): array
{
    return attempt(static function () use ($sources, $excludes, $ignore) {
        $out = [];
        foreach (new ArchivableFilesFinder($sources, $excludes, $ignore) as $file) {
            $out[] = [$file->getRelativePathname(), $file->isDir()];
        }

        return $out;
    }, $work);
}

/**
 * Archives a new copy of $tree with $archiver into <W>/out/<name>, returning
 * the result and the written files. Every call gets its own work directory:
 * phar caches archives by path and crashes when one is deleted and created
 * again.
 */
function archiveWith($archiver, array $tree, string $name, string $format, array $excludes, bool $ignore): array
{
    $work = newWork();
    makeTree($work, $tree);
    mkdir($work.'/out');
    mkdir($work.'/out/dir.d');
    $result = attempt(static function () use ($archiver, $work, $name, $format, $excludes, $ignore) {
        return str_replace($work, '<W>', $archiver->archive($work.'/pkg', $work.'/out/'.$name, $format, $excludes, $ignore));
    }, $work);
    $files = [];
    foreach (['', 'dir.d/'] as $dir) {
        foreach (array_diff(scandir($work.'/out/'.$dir), ['.', '..']) as $file) {
            if (is_file($work.'/out/'.$dir.$file)) {
                $files[$dir.$file] = base64_encode(file_get_contents($work.'/out/'.$dir.$file));
            }
        }
    }
    ksort($files);
    $result['files'] = (object) $files;

    return $result;
}

$base = sys_get_temp_dir().'/maestro-oracle-archiver';
(new Filesystem())->removeDirectory($base);

/** A new work directory. */
function newWork(): string
{
    static $n = 0;
    global $base;

    return $base.'/'.(++$n);
}
$cases = [];
$phar = new PharArchiver();
$zip = new ZipArchiver();
for ($n = 0; $n < 200; $n++) {
    $tree = randomTree();
    $excludes = [];
    for ($i = mt_rand(0, 4); $i > 0; $i--) {
        $excludes[] = randomGlob();
    }
    $ignore = chance(15);
    $work = newWork();
    makeTree($work, $tree);
    $case = ['tree' => encodeTree($tree), 'excludes' => $excludes, 'ignore' => $ignore];
    $case['finder'] = finderList($work.'/pkg', $excludes, $ignore, $work);
    foreach (['tar', 'zip', 'tar.gz'] as $format) {
        $case['phar'][$format] = archiveWith($phar, $tree, 'p.'.$format, $format, $excludes, $ignore);
    }
    $case['zip'] = archiveWith($zip, $tree, 'z.zip', 'zip', $excludes, $ignore);
    $cases[] = $case;
}

// odd targets and formats for PharArchiver, on a small tree
$targets = [];
$tree = [['a', 'file', 'x', 0644, 1600000000], ['d', 'dir', null, 0755, 1600000000], ['d/b', 'file', 'yy', 0600, 1600000000], ['e', 'dir', null, 0755, 1600000000]];
foreach ([['p', 'tar'], ['p.foo', 'tar'], ['p.zip', 'tar'], ['q.tar', 'zip'], ['r', 'zip'], ['x.y.tar.gz', 'tar.gz'], ['.h.tar.gz', 'tar.gz'],
    ['sub/missing/p.tar', 'tar'], ['p.tar', 'rar'], ['tar', 'tar'], ['p.tar.gz', 'tar'], ['ptar', 'tar'], ['p.phar', 'tar'], ['p.phar.tar', 'tar'],
    ['p..tar', 'tar'], ['p.tar.', 'tar'], ['P.TAR', 'tar'], ['p.zz.zip', 'tar'], ['p.zip.tar', 'tar'], ['p.bzip', 'tar'],
    ['P.TAR.GZ', 'tar.gz'], ['p.tgz.tar.gz', 'tar.gz'], ['p.zip', 'zip'], ['dir.d/p.tar', 'tar'], ['p.tar.bz2', 'tar.bz2']] as [$name, $format]) {
    $targets[] = ['name' => $name, 'format' => $format, 'result' => archiveWith($phar, $tree, $name, $format, [], false)];
}

// root packages archived by ArchiveManager from the working directory
$managerCases = [];
$io = new \Composer\IO\NullIO();
$dm = new \Composer\Downloader\DownloadManager($io);
$loop = new \Composer\Util\Loop(new \Composer\Util\HttpDownloader($io, new \Composer\Config()));
for ($n = 0; $n < 20; $n++) {
    $manager = new ArchiveManager($dm, $loop);
    if (chance(70)) {
        $manager->addArchiver(new ZipArchiver());
    }
    $manager->addArchiver(new PharArchiver());
    $tree = [['src', 'dir', null, 0755, 1600000000], ['src/a.php', 'file', '<?php', 0644, 1600000000], ['dist', 'dir', null, 0755, 1600000000],
        ['dist/vendor-name-1.0.0.tar', 'file', 'old', 0644, 1600000000], ['dist/vendor-name-2.0.zip', 'file', 'old', 0644, 1600000000],
        ['dist/custom.tar.gz', 'file', 'old', 0644, 1600000000], ['dist/other.tar', 'file', 'old', 0644, 1600000000],
        ['vendor-name.zip', 'file', 'old', 0644, 1600000000], ['arch-name-x.tar.bz2', 'file', 'old', 0644, 1600000000]];
    $work = newWork();
    makeTree($work, $tree);
    $package = new RootPackage('vendor/name', '1.0.0.0', pick(['1.0.0', 'dev-main', 'v2/x']));
    if (chance(30)) {
        $package->setArchiveName('arch-name');
    }
    if (chance(50)) {
        $package->setArchiveExcludes(pick([['/src'], ['*.tar'], ['!dist/other.tar', 'dist'], []]));
    }
    if (chance(50)) {
        $package->setSourceReference(pick(['main', 'abc', str_repeat('ab', 20)]));
    }
    $fileName = chance(30) ? pick(['custom', 'sub/name', 'vendor-name']) : null;
    $format = pick(['tar', 'tar', 'zip', 'tar.gz', '', '0', 'rar']);
    $targetDir = pick(['dist', '.', 'new/dir']);
    $cwd = getcwd();
    chdir($work.'/pkg');
    $result = attempt(static function () use ($manager, $package, $format, $targetDir, $fileName, $work) {
        return str_replace($work, '<W>', $manager->archive($package, $format, $targetDir, $fileName));
    }, $work);
    chdir($cwd);
    if (isset($result['ok'])) {
        $result['bytes'] = base64_encode(file_get_contents(str_replace('<W>', $work, $result['ok'])));
    }
    $managerCases[] = ['zip' => count((function () { return $this->archivers; })->call($manager)) === 2, 'archiveName' => $package->getArchiveName(),
        'excludes' => $package->getArchiveExcludes(), 'sourceReference' => $package->getSourceReference(), 'prettyVersion' => $package->getPrettyVersion(),
        'fileName' => $fileName, 'format' => $format, 'targetDir' => $targetDir, 'tree' => encodeTree($tree), 'result' => $result];
}

// getPackageFilenameParts
$parts = [];
$manager = new ArchiveManager($dm, $loop);
for ($n = 0; $n < 300; $n++) {
    $name = pick(['vendor/name', 'Vendor/Name', 'a/b.c', 'ünï/cödé', 'x/y_z-1', 'a/b+c', 'a/b c']);
    $version = pick(['1.0.0', 'dev-main', 'dev-feature/x', 'v1.0/beta', '2.0.x-dev']);
    $package = new CompletePackage($name, '1.0.0.0', $version);
    $archiveName = pick([null, null, 'custom', '', 'a/b', '0']);
    if ($archiveName !== null) {
        $package->setArchiveName($archiveName);
    }
    $package->setDistReference(pick([null, str_repeat('a1', 20), str_repeat('A1', 20), str_repeat('a1', 20)."\n", 'abc', 'x/y', '', str_repeat('f', 39)]));
    $package->setDistType(pick([null, 'zip', 'tar/x']));
    $package->setSourceReference(pick([null, 'main', str_repeat('ab', 20), '', 'feature/x']));
    $parts[] = ['name' => $name, 'version' => $version, 'archiveName' => $archiveName, 'distReference' => $package->getDistReference(),
        'distType' => $package->getDistType(), 'sourceReference' => $package->getSourceReference(),
        'parts' => $manager->getPackageFilenameParts($package), 'filename' => $manager->getPackageFilename($package)];
}

(new Filesystem())->removeDirectory($base);

$out = json_encode(['cases' => $cases, 'targets' => $targets, 'manager' => $managerCases, 'parts' => $parts], JSON_FLAGS | JSON_THROW_ON_ERROR);
$path = $root.'/internal/pkg/archiver/testdata/oracle/archiver.json.gz';
@mkdir(dirname($path), 0777, true);
file_put_contents($path, gzencode($out, 9));
fprintf(STDERR, "%s: %d cases, %d bytes\n", $path, count($cases), filesize($path));
