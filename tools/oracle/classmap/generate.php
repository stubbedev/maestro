<?php
// Generates internal/classmap/testdata/oracle/generate.json: scenarios of
// ClassMapGenerator::scanPaths() calls (classmap/psr-0/psr-4, namespaces,
// exclusion regexes, excluded dirs, globs, avoidDuplicateScans, errors) over
// the test fixtures and over the Composer checkout in .ref/composer (the
// psr-4/psr-0/classmap rules of its optimized autoloader), with the
// resulting class map, ambiguous classes and PSR violations in the order
// PHP produced them.
//
// Every string has the working directory of its scenario replaced by
// "<cwd>". Exclusion regexes are kept to the subset Go's regexp shares
// with PCRE. The large scenarios over .ref/composer only keep a digest of
// the order-independent part of their result (see digest()), unless --full
// is given.
//
// Run: php tools/oracle/classmap/generate.php [output file] [--full]
require __DIR__.'/common.php';

use Composer\ClassMapGenerator\ClassMapGenerator;

$repo = dirname(__DIR__, 3);
$output = $argv[1] ?? $repo.'/internal/classmap/testdata/oracle/generate.json';
$full = in_array('--full', $argv, true);

/**
 * md5 of the result without what depends on the directory order: lines of
 * the error, the map sorted by class with "*" as the path of ambiguous
 * classes, each ambiguous class with all its paths sorted, the sorted
 * violations and the raw violations sorted by path.
 */
function digest(array $result): string
{
    $lines = [];
    if ($result['error'] !== null) {
        $lines[] = "error\t".$result['error']['class']."\t".$result['error']['message'];
    }
    $ambiguous = [];
    foreach ($result['ambiguous'] as [$class, $paths]) {
        $ambiguous[$class] = $paths;
    }
    $map = $result['map'];
    usort($map, static function ($a, $b) { return strcmp($a[0], $b[0]); });
    $ambiguousLines = [];
    foreach ($map as [$class, $path]) {
        if (isset($ambiguous[$class])) {
            $all = array_merge([$path], $ambiguous[$class]);
            sort($all, SORT_STRING);
            $ambiguousLines[] = "ambiguous\t".$class."\t".implode("\t", $all);
            $path = '*';
        }
        $lines[] = "map\t".$class."\t".$path;
    }
    $violations = $result['violations'];
    sort($violations, SORT_STRING);
    foreach ($violations as $v) {
        $lines[] = "violation\t".$v;
    }
    $raw = $result['rawViolations'];
    usort($raw, static function ($a, $b) { return strcmp($a[0], $b[0]); });
    foreach ($raw as [$path, $violations]) {
        $line = "raw\t".$path;
        foreach ($violations as [$warning, $class]) {
            $line .= "\t".$warning."\t".$class;
        }
        $lines[] = $line;
    }

    return md5(implode("\n", array_merge($lines, $ambiguousLines)));
}

/** @return array<string, mixed> */
function step(string $path, string $type = 'classmap', ?string $namespace = null, ?string $excluded = null, array $excludedDirs = []): array
{
    return ['path' => $path, 'type' => $type, 'namespace' => $namespace, 'excluded' => $excluded, 'excludedDirs' => $excludedDirs];
}

$F = '<cwd>/tests/Fixtures';
$scenarios = [
    ['name' => 'fixtures-classmap', 'cwd' => 'testdata', 'steps' => [step('tests/Fixtures')]],
    ['name' => 'fixtures-classmap-abs', 'cwd' => 'testdata', 'steps' => [step($F)]],
    ['name' => 'fixtures-classmap-dedupe', 'cwd' => 'testdata', 'dedupe' => true, 'steps' => [
        step($F.'/classmap'), step($F), step($F.'/beta', 'psr-0', ''),
    ]],
    ['name' => 'fixtures-psr4-violations', 'cwd' => 'testdata', 'steps' => [step($F.'/psrViolations', 'psr-4', 'ExpectedNamespace\\')]],
    ['name' => 'fixtures-psr4-relative', 'cwd' => 'testdata', 'steps' => [step('tests/Fixtures/psrViolations', 'psr-4', 'ExpectedNamespace\\')]],
    ['name' => 'fixtures-psr4-trailing-slash', 'cwd' => 'testdata', 'steps' => [step($F.'/psrViolations/', 'psr-4', 'ExpectedNamespace\\')]],
    ['name' => 'fixtures-psr4-empty-ns', 'cwd' => 'testdata', 'steps' => [step($F.'/beta', 'psr-4', '')]],
    ['name' => 'fixtures-psr4-namespaced', 'cwd' => 'testdata', 'steps' => [step($F.'/Namespaced', 'psr-4', 'Namespaced\\')]],
    ['name' => 'fixtures-psr0-prefix', 'cwd' => 'testdata', 'steps' => [step($F.'/psr0NamespacePrefix', 'psr-0', 'Acme_')]],
    ['name' => 'fixtures-psr0-empty', 'cwd' => 'testdata', 'steps' => [step($F.'/psr0NamespacePrefix', 'psr-0', '')]],
    ['name' => 'fixtures-psr0-beta', 'cwd' => 'testdata', 'dedupe' => true, 'steps' => [
        step($F.'/beta', 'psr-0', 'NamespaceCollision\\'), step($F.'/beta', 'psr-0', 'PrefixCollision_'), step($F.'/beta', 'psr-4', ''),
    ]],
    ['name' => 'fixtures-psr0-pearlike', 'cwd' => 'testdata', 'steps' => [step($F, 'psr-0', 'Pearlike_')]],
    ['name' => 'fixtures-excluded-dirs', 'cwd' => 'testdata', 'steps' => [
        step($F, 'classmap', null, null, ['NamespaceCollision', 'classmap/', 'beta/PrefixCollision/A']),
    ]],
    ['name' => 'fixtures-excluded-regex', 'cwd' => 'testdata', 'steps' => [
        step($F, 'classmap', null, '{(/Fixtures/classmap/|Pearlike/Ba)}'),
    ]],
    ['name' => 'fixtures-glob', 'cwd' => 'testdata', 'steps' => [step('tests/Fixtures/php*'), step($F.'/{beta,Pearlike}/*')]],
    ['name' => 'fixtures-glob-missing', 'cwd' => 'testdata', 'steps' => [step($F.'/nothing*')]],
    ['name' => 'fixtures-single-file', 'cwd' => 'testdata', 'steps' => [step('tests/Fixtures/classmap/multipleNs.php'), step($F.'/template/notphp.inc')]],
    ['name' => 'fixtures-missing', 'cwd' => 'testdata', 'steps' => [step($F.'/classmap'), step('tests/Fixtures/missing')]],
    ['name' => 'fixtures-not-php', 'cwd' => 'testdata', 'steps' => [step($F.'/classmap/notPhpFile.md')]],
];

// The PSR and classmap rules of Composer's own optimized autoloader.
$ref = $repo.'/.ref/composer';
$steps = [];
foreach (json_decode(file_get_contents($ref.'/vendor/composer/installed.json'), true)['packages'] as $package) {
    foreach ($package['autoload']['classmap'] ?? [] as $path) {
        $steps[] = step('<cwd>/vendor/composer/'.$package['install-path'].'/'.$path);
    }
}
$namespaces = [];
foreach (['psr-4' => require $ref.'/vendor/composer/autoload_psr4.php', 'psr-0' => require $ref.'/vendor/composer/autoload_namespaces.php'] as $type => $map) {
    foreach ($map as $namespace => $paths) {
        $namespaces[$namespace][] = [$type, $paths];
    }
}
krsort($namespaces);
foreach ($namespaces as $namespace => $groups) {
    foreach ($groups as [$type, $paths]) {
        foreach ($paths as $path) {
            $steps[] = step(str_replace($ref, '<cwd>', $path), $type, (string) $namespace);
        }
    }
}
$scenarios[] = ['name' => 'composer-optimized', 'cwd' => 'ref', 'dedupe' => true, 'steps' => $steps];
$scenarios[] = ['name' => 'composer-tests-classmap', 'cwd' => 'ref', 'steps' => [step('tests'), step('src')]];
$scenarios[] = ['name' => 'composer-vendor-excluded', 'cwd' => 'ref', 'steps' => [
    step('vendor', 'classmap', null, '{/(Tests|tests|Fixtures)/}', ['composer', 'symfony/polyfill-mbstring']),
]];

// A tree with symlinks, dot files, VCS and unreadable directories, built
// in a temporary directory: [path, kind, content] where kind is file, link
// (content is the target) or a file/dir mode applied after creation.
$tree = [
    ['src/A.php', 'file', "<?php class A {}"],
    ['src/.hidden/H.php', 'file', "<?php class H {}"],
    ['src/.git/G.php', 'file', "<?php class G {}"],
    ['src/CVS/C.php', 'file', "<?php class C {}"],
    ['src/CVSx/D.php', 'file', "<?php class D {}"],
    ['src/sub/.dot.php', 'file', "<?php class Dot {}"],
    ['src/sub/B.php', 'file', "<?php class B {}"],
    ['src/sub/upper.PHP', 'file', "<?php class Upper {}"],
    ['src/sub/hack.hh', 'file', "<?php class Hack {}"],
    ['src/sub/noext', 'file', "<?php class NoExt {}"],
    ['src/sub/with space.php', 'file', "<?php class WithSpace {}"],
    ['src/sub/deep/E.inc', 'file', "<?php namespace Deep; class E {}"],
    ['src/link', 'link', 'sub'],
    ['src/filelink.php', 'link', 'sub/B.php'],
    ['src/dotlink/.x/Y.php', 'file', "<?php class Y {}"],
    ['broken/ok.php', 'file', "<?php class Ok {}"],
    ['broken/broken.php', 'link', 'nowhere.php'],
    ['perm/a.php', 'file', "<?php class PermA {}"],
    ['perm/noperm/x.php', 'file', "<?php class X {}"],
    ['perm/noperm', 'mode', 0],
    ['unreadable/u.php', 'file', "<?php class U {}"],
    ['unreadable/u.php', 'mode', 0],
    ['psr/Vendor/Pkg/Foo.php', 'file', "<?php namespace Vendor\\Pkg; class Foo {}"],
    ['psr/Vendor/Pkg/Bad.php', 'file', "<?php namespace Vendor\\Pkg; class Wrong {}"],
    ['psr/Vendor/Pkg/Two.php', 'file', "<?php namespace Vendor\\Pkg; class Two {} class Other {}"],
    ['psr/Vendor_Pkg_Old.php', 'file', "<?php class Vendor_Pkg_Old {}"],
    ['psr/Vendor/Pkg/Sub/Deep.php', 'file', "<?php namespace Vendor\\Pkg\\Sub; class Deep {}"],
];
foreach ([
    'tree-classmap' => [step('src')],
    'tree-classmap-abs' => [step('<cwd>/src/')],
    'tree-classmap-dedupe' => [step('src'), step('src/link'), step('<cwd>/src/sub')],
    'tree-excluded' => [step('src', 'classmap', null, '{/sub/}')],
    'tree-excluded-dirs' => [step('src', 'classmap', null, null, ['sub', 'link', 'dotlink/.x'])],
    'tree-glob' => [step('*/sub'), step('{src,psr}/V*')],
    'tree-broken' => [step('broken')],
    'tree-perm' => [step('perm')],
    'tree-unreadable' => [step('unreadable')],
    'tree-psr4' => [step('psr', 'psr-4', 'Vendor\\'), step('<cwd>/psr/Vendor', 'psr-4', 'Vendor\\'), step('<cwd>/psr/', 'psr-4', '')],
    'tree-psr4-dedupe' => [step('psr/Vendor', 'psr-4', 'Vendor\\'), step('psr', 'psr-4', ''), step('psr', 'psr-0', '')],
    'tree-psr4-dedupe-abs' => [step('<cwd>/psr/Vendor', 'psr-4', 'Vendor\\'), step('<cwd>/psr', 'psr-0', ''), step('<cwd>/psr', 'classmap')],
    'tree-psr0' => [step('<cwd>/psr', 'psr-0', 'Vendor'), step('<cwd>/psr', 'psr-0', 'Vendor_'), step('psr', 'psr-0', '')],
] as $name => $steps) {
    $scenarios[] = ['name' => $name, 'cwd' => 'tree', 'tree' => $tree, 'dedupe' => str_contains($name, 'dedupe'), 'steps' => $steps];
}

function buildTree(string $dir, array $tree): void
{
    foreach ($tree as [$path, $kind, $content]) {
        $full = $dir.'/'.$path;
        if (!is_dir(dirname($full))) {
            mkdir(dirname($full), 0777, true);
        }
        if ($kind === 'file') {
            file_put_contents($full, $content);
        } elseif ($kind === 'link') {
            symlink($content, $full);
        } else {
            chmod($full, $content);
        }
    }
}

function removeTree(string $dir): void
{
    foreach (scandir($dir) as $entry) {
        if ($entry === '.' || $entry === '..') {
            continue;
        }
        $path = $dir.'/'.$entry;
        if (is_link($path) || !is_dir($path)) {
            unlink($path);
        } else {
            chmod($path, 0755);
            removeTree($path);
        }
    }
    rmdir($dir);
}

$cwds = ['testdata' => $repo.'/internal/classmap/testdata', 'ref' => $ref];
foreach ($scenarios as &$scenario) {
    $temp = null;
    if ($scenario['cwd'] === 'tree') {
        $temp = realpath(sys_get_temp_dir()).'/classmap-oracle-'.getmypid();
        mkdir($temp);
        buildTree($temp, $scenario['tree']);
        $cwds['tree'] = $temp;
    }
    $cwd = $cwds[$scenario['cwd']];
    chdir($cwd);
    $strip = static function (string $s) use ($cwd): string {
        return str_replace($cwd, '<cwd>', $s);
    };
    $generator = new ClassMapGenerator(['php', 'inc', 'hh']);
    if ($scenario['dedupe'] ?? false) {
        $generator->avoidDuplicateScans();
    }
    $error = null;
    try {
        foreach ($scenario['steps'] as $step) {
            $generator->scanPaths(str_replace('<cwd>', $cwd, $step['path']), $step['excluded'], $step['type'], $step['namespace'], $step['excludedDirs']);
        }
    } catch (\Throwable $e) {
        $error = ['class' => get_class($e), 'message' => $strip($e->getMessage())];
    }
    $classMap = $generator->getClassMap();
    $map = [];
    foreach ($classMap->getMap() as $class => $path) {
        $map[] = [$class, $strip($path)];
    }
    $ambiguous = [];
    foreach ($classMap->getAmbiguousClasses(false) as $class => $paths) {
        $ambiguous[] = [$class, array_map($strip, $paths)];
    }
    $filtered = [];
    foreach ($classMap->getAmbiguousClasses() as $class => $paths) {
        $filtered[] = [$class, array_map($strip, $paths)];
    }
    $raw = [];
    foreach ($classMap->getRawPsrViolations() as $path => $violations) {
        $raw[] = [$strip($path), array_map(static function ($v) use ($strip) {
            return [$strip($v['warning']), $v['className']];
        }, $violations)];
    }
    $scenario['result'] = [
        'error' => $error,
        'map' => $map,
        'ambiguous' => $ambiguous,
        'ambiguousFiltered' => $filtered,
        'violations' => array_map($strip, $classMap->getPsrViolations()),
        'rawViolations' => $raw,
    ];
    if ($scenario['cwd'] === 'ref' && !$full) {
        $scenario['result'] = ['digest' => digest($scenario['result'])];
    }
    if ($temp !== null) {
        chdir($repo);
        removeTree($temp);
    }
}
unset($scenario);

file_put_contents($output, json_encode($scenarios, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_INVALID_UTF8_SUBSTITUTE)."\n");
