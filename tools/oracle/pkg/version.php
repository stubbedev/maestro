<?php
// Generates internal/pkg/version/testdata/oracle/version.json:
//
//   bump         VersionBumper::bumpRequirement over constraints x versions
//                (some dev versions with a branch alias)
//   recommended  VersionSelector::findRecommendedRequireVersion over
//                versions, branch aliases and ext- names; "php" is the
//                PHP_MAJOR.MINOR.RELEASE version that ran it
//   best         VersionSelector::findBestCandidate (without a platform
//                repository) over random version sets and stabilities:
//                the index of the selected package
//
// Run: php tools/oracle/pkg/version.php
require __DIR__.'/common.php';

use Composer\Package\AliasPackage;
use Composer\Package\CompletePackage;
use Composer\Package\Package;
use Composer\Package\Version\VersionBumper;
use Composer\Package\Version\VersionParser;
use Composer\Package\Version\VersionSelector;
use Composer\Repository\RepositorySet;
use Composer\Semver\Constraint\ConstraintInterface;

$parser = new VersionParser();
$out = ['php' => PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION.'.'.PHP_RELEASE_VERSION];

$constraints = ['^1.0', '^v1.0', '^1.2', '^1.0.0', '^1.2 || ^2.3', '^1.2 || ^2.3 || ^2', '^1.2 || ^2.3.3 || ^2', '^3@dev', '~2', '~2.2', '~2.2.3',
    '~2.0.0', '~2.2.3.1', '>=3.0', '>=v3.0', '>2.2.3', '^0.3 || ^0.4', '*', '2.*', 'v2.*', '2.x', '2.x.x', '2.4.*', '2.4.3.*', 'dev-main',
    '1.0.0', '^1.0@beta', '>=1.0,<2.0', '>=1.0 <2.0', '^1.0|^2.0', '~1.0 || dev-main', '^2.0.0-beta1', '>=2', '^0.1', '^0.0.1', '1.*.*', '~1',
    '^1.0 , >=1.2', '* || ^1', '^10.0', '^1.0@dev || ^2.0@dev'];
$versions = ['1.2.1', '1.0.0', '1.2.0', '1.3.2', '2.4.0', '3.2.x-dev', '2.1-beta.1', 'dev-foo', 'dev-main', '2.4.3', '2.2.6.2', '2.2.6', '2.0.0',
    '2025.1.583', '2.2.4', '2.2.4.0', '2.2.4.5', '3.4.5', '0.4.3', '1.2.3', '0.0.5', '10.1.0', '1.10.2', '2.0.0-RC1', '1.0.0-beta2', '1.x-dev'];
$aliases = [null, null, null, '3.3.x-dev', '2.1-dev', '9999999-dev', '1.0.x-dev'];
$bump = [];
foreach ($constraints as $constraint) {
    foreach ($versions as $version) {
        $alias = pick($aliases);
        $package = new Package('foo/bar', $parser->normalize($version), $version);
        if ($alias !== null) {
            $package->setExtra(['branch-alias' => [$version => $alias]]);
        }
        $bump[] = [$constraint, $version, $alias, attempt(static function () use ($parser, $constraint, $package) {
            return (new VersionBumper())->bumpRequirement($parser->parseConstraints($constraint), $package);
        })];
    }
}
$out['bump'] = $bump;

$recommended = [];
$selector = new VersionSelector((new \ReflectionClass(RepositorySet::class))->newInstanceWithoutConstructor());
foreach (array_merge($versions, ['1.2', 'v1.2.1', '3.1.2-pl2', '3.1.2-patch', '2.0-beta.1', '3.1.2-alpha5', '3.0-RC2', '0.1.0', '0.0.3-alpha',
    '0.0.3.4-alpha', '3.0.0.2-RC2', '1.2.1.1020402', 'v20121020', 'v20121020.2', '3.1.2-dev', '3.x-dev', $out['php'], '5.3.0']) as $version) {
    foreach ([null, '2.1.x-dev', '2.1-dev', '2.1.3.x-dev', '2.x-dev', '0.3.x-dev', '9999999-dev', '3.0.x-dev', '3.0-dev'] as $alias) {
        foreach (['foo/bar', 'ext-filter', 'ext-xdebug'] as $name) {
            if ($alias !== null && $name !== 'foo/bar') {
                continue;
            }
            $package = new Package($name, $parser->normalize($version), $version);
            if ($alias !== null) {
                $package->setExtra(['branch-alias' => [$version => $alias]]);
            }
            $recommended[] = [$name, $version, $alias, attempt(static function () use ($selector, $package) {
                return $selector->findRecommendedRequireVersion($package);
            })];
        }
    }
}
$out['recommended'] = $recommended;

class FixedRepositorySet extends RepositorySet
{
    public $packages = [];

    public function __construct()
    {
    }

    public function findPackages($name, ?ConstraintInterface $constraint = null, $flags = 0): array
    {
        return $this->packages;
    }
}

$pool = ['1.0.0', '1.1.0-beta', '1.2.0-alpha', '2.x-dev', '2.0.0-beta3', '1.0.0-RC1', 'dev-main', '1.10.0', '1.9.0', '2.0.0', '0.1.0',
    '1.0.0-dev', '3.0.0-alpha2', '3.0.0-beta1', 'dev-feature', '1.0.1'];
$best = [];
$repoSet = new FixedRepositorySet();
$selector = new VersionSelector($repoSet);
for ($i = 0; $i < 400; $i++) {
    $list = [];
    for ($j = mt_rand(0, 8); $j > 0; $j--) {
        $list[] = [pick($pool), chance(15)];
    }
    $packages = [];
    foreach ($list as [$version, $defaultAlias]) {
        $package = new CompletePackage('foo/bar', $parser->normalize($version), $version);
        $packages[] = $defaultAlias ? new AliasPackage($package, VersionParser::DEFAULT_BRANCH_ALIAS, VersionParser::DEFAULT_BRANCH_ALIAS) : $package;
    }
    $repoSet->packages = $packages;
    $stability = pick(['stable', 'RC', 'beta', 'alpha', 'dev']);
    $result = $selector->findBestCandidate('foo/bar', null, $stability);
    $index = null;
    foreach ($packages as $k => $p) {
        if ($p === $result || ($p instanceof AliasPackage && $p->getAliasOf() === $result)) {
            $index = [$k, $p === $result];
            break;
        }
    }
    $best[] = [$list, $stability, $index];
}
$out['best'] = $best;

write_golden($root.'/internal/pkg/version/testdata/oracle/version.json', $out);
