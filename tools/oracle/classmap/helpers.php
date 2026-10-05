<?php
// Generates internal/classmap/testdata/oracle/helpers.json: the results of
// the small pure functions the port reimplements by hand, for many inputs:
// ClassMapGenerator::normalizePath()/isAbsolutePath(), its separator
// collapsing regex, ClassMap's default duplicates filter, Finder's dot file
// and excluded directory patterns, fnmatch() with FNM_PERIOD as glob() uses
// it, pathinfo() extensions, and ClassMap's ordered bookkeeping.
// Run: php tools/oracle/classmap/helpers.php
require __DIR__.'/common.php';

use Composer\ClassMapGenerator\ClassMap;
use Composer\ClassMapGenerator\ClassMapGenerator;
use Composer\Pcre\Preg;

$call = static function (string $method, ...$args) {
    return (function () use ($method, $args) {
        return self::$method(...$args);
    })->bindTo(null, ClassMapGenerator::class)();
};

$paths = [
    '', '/', '//', '///', 'a', 'a/', '/a', '/a/', 'a//b', 'a/./b', 'a/../b', '../a', '../../a', 'a/../../b', '/../a', '/a/..', '/a/../..',
    './a', '.', '..', 'a/..', 'a/b/../../..', '\\a\\b', 'C:\\a\\..\\b', 'c:/a', 'c:', 'C:', 'c:a', 'file://c:/a', 'file://C:/a/../b',
    'phar://foo.phar/a/../b', 'ab:', 'ab:x', 'ab://', 'x:', '1:/a', 'a1:/b', 'http://x/../y', '//server/share/../x', '\\\\server\\share',
    'vfs://root/a/./b', 'a/b/c/../../d', '/a/b/../c/./d/', 'a/../..', '../..', '..\\..\\x', "a\nb/../c", 'ftp://C:/x', 'ssh2.sftp://h/x',
    'a:b/c', 'Z:/', 'z:\\', '/var/www/../../../etc', 'phar://c:/x', 'https://ab:/x',
];
$normalize = $absolute = $collapse = $extension = [];
foreach ($paths as $p) {
    $normalize[] = [$p, $call('normalizePath', $p)];
    $absolute[] = [$p, $call('isAbsolutePath', $p)];
}
$separators = [
    '', '/', '//', 'a//b', 'a///b', 'a\\\\b', 'a\\/\\b', 'a:/b', 'a://b', 'a:///b', 'a:////b', '//a', '\\\\a', 'a/\\', ':/', '://', 'x:\\\\y',
    'a//b//c', 'file:///tmp//x', '/a/b/', 'a\\b', 'phar://a//b', 'a:b//c', ':://',
];
foreach ($separators as $p) {
    $collapse[] = [$p, Preg::replace('{(?<!:)[\\\\/]{2,}}', '/', $p)];
}
foreach (['a.php', 'a.PHP', '.php', 'a.', 'a', 'a.b.php', '/x/a.php', '/x.y/a', '/x/a.php/', 'a.php\n', "a.php\n", 'dir.inc/x.hh', '/x/.hh', '..', '/x/..php'] as $p) {
    $extension[] = [$p, pathinfo($p, PATHINFO_EXTENSION)];
}

$filterSubjects = [
    '/a/test/b', '/a/tests/b', '/a/Tests/b', '/a/TESTS/b', '/a/testss/b', '/a/test', '/a/tests', '/test/', 'test/', '/a/fixture/x', '/a/fixtures/x',
    '/a/example/x', '/a/examples/x', '/a/stub/x', '/a/stubs/x', '/a/stubx/x', '/a/mytest/x', '/a/test-x/x', '/a/tests-x/', '/a/tests/', '//tests/',
    '/a/Stubs/', '/a/EXAMPLE/', 'C:/x/tests/y', '/a/testS/b', '/a/tes/b', '/fixtures', '/fixtures/', '/a/sTuBs/b', "/a/test\n/",
];
$filter = [];
foreach ($filterSubjects as $s) {
    $filter[] = [$s, Preg::isMatch('{/(test|fixture|example|stub)s?/}i', $s)];
}

$dotSubjects = [
    '', '.', '..', '.a', 'a/.b', 'a/.b/c', 'a/b.c', '.a/b', 'a/./b', 'a/..', '..a', 'a/.', 'a/.\n', ".a\n", ".\n", ".\nx", ".a\nb", ".a\nb/c",
    "a/.\n/x", "x/.b\n", "x/.b\nc/d", '.git', 'src/.github/workflows/x.php', 'a.b/.c', "./x", "x/..",
];
$dots = [];
foreach ($dotSubjects as $s) {
    $dots[] = [$s, (bool) preg_match('#(^|/)\..+(/|$)#', $s)];
}

$excludeCases = [];
foreach ([['a/b'], ['a/b', 'x/y'], ['/abs'], ['a/b/']] as $dirs) {
    $patterns = array_map(static function ($d) {
        return preg_quote(rtrim($d, '/'), '#');
    }, $dirs);
    $regex = '#(?:^|/)(?:'.implode('|', $patterns).')(?:/|$)#';
    foreach (['a/b', 'a/b/c', 'x/a/b', 'xa/b', 'a/bc', 'a/b/', '', 'x/y', 'q/x/y/z', "a/b\n", "a/b\nc", '/abs', 'r/abs/q', 'abs'] as $s) {
        $excludeCases[] = [$dirs, $s, (bool) preg_match($regex, $s)];
    }
}

$fnmatch = [];
$patterns = ['*', '?', 'a*', '*.php', '.*', '\\.*', '[a-c]*', '[!a-c]*', '[^a-c]*', '[]]*', '[!]]*', '[a-]*', '[[:alpha:]]*', '[[:digit:]]*',
    '[[:upper:]]x', 'a\\*', '\\a', 'a?c', '*a*', '[', '[a', 'a[', '[.]*', '?*', '*?', '[\\]]*', 'a[b-d]e', '[z-a]', '[[:punct:]]', '\\', ''];
$names = ['a', 'abc', '.a', '.', 'a.php', '.php', 'b', 'z', ']', ']x', '-', '-x', 'A', '1', '9z', 'a*', 'ac', 'abd', 'ace', '[', '[a', 'a[', '\\', '*', '!', ''];
foreach ($patterns as $p) {
    foreach ($names as $n) {
        $fnmatch[] = [$p, $n, fnmatch($p, $n, FNM_PERIOD)];
    }
}

// ClassMap bookkeeping: addClass() replacing a path keeps the key's
// position, unset() + re-add of violations moves them to the end, clear by
// prefix, sort.
$cm = new ClassMap();
$cm->addPsrViolation('w1', 'C1', '/p/a.php');
$cm->addPsrViolation('w2', 'C2', '\\p\\b.php/');
$cm->addPsrViolation('w3', 'C3', '/p/a.php');
$cm->addPsrViolation('w4', 'C4', '/q/c.php');
$cm->addClass('Zed', '/p/b.php');
$cm->addPsrViolation('w5', 'C5', '/p/b.php');
$cm->addClass('Alpha', '/x/alpha.php');
$cm->addClass('Zed', '/x/zed.php');
$cm->addClass('Beta\\X', '/x/b.php');
$cm->addClass('beta', '/x/lower.php');
$cm->addClass('Beta', '/x/upper.php');
$cm->addClass('_u', '/x/u.php');
$cm->addAmbiguousClass('Alpha', '/y/alpha.php');
$cm->addAmbiguousClass('Zed', '/tests/zed.php');
$cm->addAmbiguousClass('Alpha', '/y/Fixtures/alpha.php');
$before = ['map' => $cm->getMap(), 'raw' => $cm->getRawPsrViolations(), 'violations' => $cm->getPsrViolations()];
$cm->clearPsrViolationsByPath('/p/');
$cm->addPsrViolation('w6', 'C6', '/p/a.php');
$cm->sort();
$after = [
    'map' => $cm->getMap(), 'raw' => $cm->getRawPsrViolations(), 'violations' => $cm->getPsrViolations(),
    'ambiguous' => $cm->getAmbiguousClasses(false), 'ambiguousFiltered' => $cm->getAmbiguousClasses(), 'count' => count($cm),
];
$listMap = static function (array $map): array {
    $out = [];
    foreach ($map as $k => $v) {
        $out[] = [(string) $k, $v];
    }

    return $out;
};
$classMap = [];
foreach (['before' => $before, 'after' => $after] as $k => $state) {
    foreach ($state as $field => $value) {
        $classMap[$k][$field] = in_array($field, ['map', 'raw', 'ambiguous', 'ambiguousFiltered'], true) ? $listMap($value) : $value;
    }
}

file_put_contents(dirname(__DIR__, 3).'/internal/classmap/testdata/oracle/helpers.json', json_encode([
    'normalizePath' => $normalize,
    'isAbsolutePath' => $absolute,
    'collapseSeparators' => $collapse,
    'extension' => $extension,
    'duplicatesFilter' => $filter,
    'dotPath' => $dots,
    'excludePattern' => $excludeCases,
    'fnmatch' => $fnmatch,
    'classMap' => $classMap,
], JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES)."\n");
