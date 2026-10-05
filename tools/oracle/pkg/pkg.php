<?php
// Generates the goldens of internal/pkg (testdata/oracle/pkg.json) and the
// naming golden of internal/pkg/loader (testdata/oracle/naming.json):
//
//   pkg.json     VersionParser::parseNameVersionPairs and isUpgrade,
//                BasePackage::packageNameToRegexp/packageNamesToRegexp,
//                PackageSorter::sortPackages on random dependency graphs
//                and getMostCurrentVersion
//   naming.json  PlatformRepository::isPlatformPackage and
//                ValidatingArrayLoader::hasPackageNamingError over a
//                corpus of package names
//
// Run: php tools/oracle/pkg/pkg.php
require __DIR__.'/common.php';

use Composer\Package\BasePackage;
use Composer\Package\Link;
use Composer\Package\Loader\ValidatingArrayLoader;
use Composer\Package\Package;
use Composer\Package\Version\VersionParser;
use Composer\Repository\PlatformRepository;
use Composer\Semver\Constraint\MatchAllConstraint;
use Composer\Util\PackageSorter;

// naming.json
$names = ['foo/bar', 'Foo/Bar', 'fooBar/BazQux', 'FOOBar/BAZqux', 'foo', 'foo/-bar', 'foo/-bar-', 'fo--oo/bar', 'fo-oo/bar__baz', 'fo-oo/bar_.baz',
    'foo/bar---baz', 'foo/bar--baz', 'npm-asset/angular--core', 'foo/bar.json', 'foo/bar.JSON', 'com1/foo', 'foo/lpt9', 'nul/x', 'COM1/foo', 'php', 'PHP',
    'php-64bit', 'php-ipv6', 'php-zts', 'php-debug', 'php-foo', 'hhvm', 'HHVM', 'ext-json', 'ext-', 'ext-a', 'ext-a.b_c-d', 'ext-a..b', 'ext-a-', 'ext--a',
    'lib-icu', 'lib-ICU-uc', 'composer', 'composer-plugin-api', 'composer-runtime-api', 'composer-api', 'Composer-Plugin-API', 'ext-é', 'ext-a b',
    "ext-a\n", 'a/b', 'a/b/c', 'a.b/c_d', 'a/b.', '-a/b', 'a_/b', '0/0', 'a/B', 'aB/c', 'myVendor/myPackage', 'MyVendor/MyPackage', 'ABc/DEf', '', ' ',
    'foo/bar ', 'vendor/pkg-', 'vendor/p--k', 'vendor/p---k', 'vendor/p.-k', 'vendor/p_k', 'v/p__k', 'ext-ſ', 'compoſer', 'php-zt'];
for ($i = 0; $i < 300; $i++) {
    $chars = 'abcAB09-_./ ';
    $n = mt_rand(1, 12);
    $s = pick(['', 'ext-', 'lib-', 'php', 'foo/', 'composer']);
    for ($j = 0; $j < $n; $j++) {
        $s .= $chars[mt_rand(0, strlen($chars) - 1)];
    }
    $names[] = $s;
}
$naming = [];
foreach (array_values(array_unique($names)) as $i => $name) {
    $naming[(string) $i] = [$name, PlatformRepository::isPlatformPackage($name),
        ValidatingArrayLoader::hasPackageNamingError($name), ValidatingArrayLoader::hasPackageNamingError($name, true)];
}
write_golden($root.'/internal/pkg/loader/testdata/oracle/naming.json', $naming);

// pkg.json
$out = [];
$parser = new VersionParser();
$args = ['php', 'php:^7.0', 'php=8', 'ext-apcu', 'foo/bar', 'foo/bar:1.0', 'foo/bar=^2', 'foo/bar ^3', '^7.0', '*', '*@dev', 'foo/*', 'bar*',
    'acme/baz', '1.0.0', 'dev-main', ' foo/bar ', 'lib-icu', 'composer-plugin-api', '2.*', '*foo', 'x', '', 'a b c', 'monolog/monolog:^2.0 || ^3.0'];
$pairs = [];
for ($i = 0; $i < 200; $i++) {
    $list = [];
    for ($j = mt_rand(1, 5); $j > 0; $j--) {
        $list[] = pick($args);
    }
    $pairs[] = [$list, $parser->parseNameVersionPairs($list)];
}
$out['parseNameVersionPairs'] = $pairs;

$versions = ['0.9.0.0', '1.0.0.0', '1.0.0.0-beta1', '1.0.0.0-RC1', '1.0.0.0-dev', '2.0.0.0', '1.10.0.0', '1.9.0.0', 'dev-master', 'dev-trunk',
    'dev-default', 'dev-foo', '9999999-dev', '1.0.x-dev', '1.9999999.9999999.9999999-dev'];
$upgrades = [];
foreach ($versions as $from) {
    foreach ($versions as $to) {
        $upgrades[] = [$from, $to, attempt(static function () use ($from, $to) { return VersionParser::isUpgrade($from, $to); })];
    }
}
$out['isUpgrade'] = $upgrades;

$regexps = [];
foreach ([['ext-*', 'monolog/monolog'], ['php'], ['*'], ['foo', 'bar'], ['vendor/*-bundle', 'a.b/c+d', 'x/y?z', 'a\\b', 'a{1}/b'], ['*/*'], []] as $list) {
    foreach (['{^%s$}i', '{^(?:%s)$}iD', '%s', '§%s§', '{%s}', '100%% %s'] as $wrap) {
        $regexps[] = [$list, $wrap, BasePackage::packageNamesToRegexp($list, $wrap), count($list) > 0 ? BasePackage::packageNameToRegexp($list[0], $wrap) : null];
    }
}
$out['regexps'] = $regexps;

$sorts = [];
for ($i = 0; $i < 300; $i++) {
    $n = mt_rand(1, 12);
    $names = [];
    for ($j = 0; $j < $n; $j++) {
        $names[] = pick(['foo', 'bar', 'baz', 'Foo', 'a10', 'a9', 'a', 'b', 'c', 'x1', 'x10', 'x2']).'/'.pick(['one', 'two', 'three', 'p1', 'p10', 'p2']).$j;
    }
    $graph = [];
    $packages = [];
    foreach ($names as $name) {
        $requires = [];
        foreach ($names as $other) {
            if (chance(25)) {
                $requires[] = $other;
            }
        }
        if (chance(30)) {
            $requires[] = 'outside/'.mt_rand(1, 3);
        }
        $graph[] = [$name, $requires];
        $package = new Package($name, '1.0.0.0', '1.0.0');
        $links = [];
        foreach ($requires as $r) {
            $links[strtolower($r)] = new Link($package->getName(), $r, new MatchAllConstraint());
        }
        $package->setRequires($links);
        $packages[] = $package;
    }
    $weights = [];
    if (chance(30)) {
        $weights[strtolower(pick($names))] = mt_rand(-1000, 1000);
    }
    $sorted = array_map(static function ($p) { return $p->getName(); }, PackageSorter::sortPackages($packages, $weights));
    $alpha = array_map(static function ($p) { return $p->getName(); }, PackageSorter::sortPackagesAlphabetically($packages));
    $sorts[] = [$graph, (object) $weights, $sorted, $alpha];
}
$out['sortPackages'] = $sorts;

$current = [];
for ($i = 0; $i < 100; $i++) {
    $list = [];
    for ($j = mt_rand(0, 6); $j > 0; $j--) {
        $list[] = [pick(['1.0.0', '2.0.0', '1.10.0', '1.9.0', '2.0.0-beta', 'dev-main', '3.0.x-dev', '0.1']), chance(10)];
    }
    $packages = [];
    foreach ($list as $k => [$v, $default]) {
        $p = new Package('a/b', $parser->normalize($v), $v);
        $p->setIsDefaultBranch($default);
        $packages[] = $p;
    }
    $best = PackageSorter::getMostCurrentVersion($packages);
    $current[] = [$list, $best === null ? null : array_search($best, $packages, true)];
}
$out['getMostCurrentVersion'] = $current;
write_golden($root.'/internal/pkg/testdata/oracle/pkg.json', $out);
