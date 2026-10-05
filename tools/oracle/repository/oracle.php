<?php
// Generates the goldens of internal/repository and internal/locker by
// running Composer 2.10.3's own FilesystemRepository and Locker:
//
//   internal/repository/testdata/oracle/installed.json.gz
//       package sets => vendor/composer/installed.json and installed.php
//       as FilesystemRepository::write dumps them
//   internal/locker/testdata/oracle/lock.json.gz
//       package sets => composer.lock as Locker::setLockData writes it
//   internal/locker/testdata/oracle/contenthash.json
//       composer.json documents => Locker::getContentHash
//
// Package versions are sampled from the real Packagist metadata in
// internal/pkg/loader/testdata/oracle/p2. Every input goes through a JSON
// round trip, so the Go side reads exactly what PHP loaded.
//
// Run: php tools/oracle/repository/oracle.php
require __DIR__.'/../pkg/common.php';

use Composer\Installer\InstallationManager;
use Composer\Json\JsonFile;
use Composer\Package\Loader\ArrayLoader;
use Composer\Package\Locker;
use Composer\Package\PackageInterface;
use Composer\Package\RootAliasPackage;
use Composer\Package\RootPackage;
use Composer\Package\RootPackageInterface;
use Composer\IO\NullIO;
use Composer\Repository\FilesystemRepository;

$root = dirname(__DIR__, 3);

/** An InstallationManager answering install paths from a map. */
final class PathMap extends InstallationManager
{
    /** @var array<string, ?string> */
    public $paths = [];
    public $cwd;

    public function __construct()
    {
    }

    public function getInstallPath(PackageInterface $package): ?string
    {
        if ($package instanceof RootPackageInterface) {
            return $this->cwd;
        }

        return $this->paths[$package->getName()] ?? 'vendor/'.$package->getName();
    }
}

function roundtrip($v)
{
    return json_decode(json_encode($v, JSON_FLAGS | JSON_THROW_ON_ERROR), true, 512, JSON_THROW_ON_ERROR);
}

function rmrf(string $dir): void
{
    if (!is_dir($dir)) {
        return;
    }
    foreach (new RecursiveIteratorIterator(new RecursiveDirectoryIterator($dir, FilesystemIterator::SKIP_DOTS), RecursiveIteratorIterator::CHILD_FIRST) as $f) {
        $f->isDir() ? rmdir($f->getPathname()) : unlink($f->getPathname());
    }
    rmdir($dir);
}

// A pool of package versions to sample from.
$pool = [];
foreach (p2_inputs($root) as $name => $versions) {
    foreach ($versions as $v) {
        $pool[] = $v;
    }
}

/** A random package set: a list of package arrays with distinct names. */
function package_set(array $pool, int $n): array
{
    $set = [];
    $names = [];
    for ($i = 0; $i < $n * 4 && count($set) < $n; $i++) {
        $p = pick($pool);
        // rename to vary names and sorting while keeping real metadata
        if (chance(60)) {
            $p['name'] = pick(['acme', 'Vendor', 'a', 'z-z', 'foo.bar', '0vendor']).'/'.pick(['pkg', 'Lib', 'b', 'tool-kit', 'x_y', '9']).mt_rand(0, 30);
        }
        if (isset($names[strtolower($p['name'])])) {
            continue;
        }
        $names[strtolower($p['name'])] = true;
        if (chance(15)) {
            $p['installation-source'] = pick(['dist', 'source']);
        }
        if (chance(10)) {
            $p['replace'] = ($p['replace'] ?? []) + ['replaced/'.mt_rand(0, 5) => pick(['self.version', '^1.0', '*'])];
        }
        if (chance(10)) {
            $p['provide'] = ($p['provide'] ?? []) + ['psr/log-implementation' => pick(['1.0|2.0|3.0', 'self.version']), 'ext-foo' => '*'];
        }
        if (chance(10)) {
            $p['extra']['branch-alias']['dev-main'] = '9.9.x-dev';
            $p['version'] = 'dev-main';
        }
        unset($p['version_normalized']);
        $set[] = $p;
    }

    return $set;
}

function root_config(): array
{
    $config = ['name' => pick(['__root__', 'my/project', 'Root/App']), 'version' => pick(['dev-master', '1.0.0', 'dev-feature', '2.3.4'])];
    if (chance(50)) {
        $config['source'] = ['type' => 'git', 'url' => 'https://example.org/r.git', 'reference' => 'abc123'];
    }
    if (chance(30)) {
        $config['dist'] = ['type' => 'zip', 'url' => 'https://example.org/r.zip', 'reference' => 'def456'];
    }
    if (chance(30)) {
        $config['provide'] = ['psr/log-implementation' => '1.0', 'virtual/'.mt_rand(0, 3) => 'self.version'];
    }
    if (chance(30)) {
        $config['replace'] = ['replaced/0' => 'self.version'];
    }
    if (chance(20)) {
        $config['type'] = 'project';
    }

    return $config;
}

$tmp = sys_get_temp_dir().'/maestro-repository-oracle';
rmrf($tmp);
mkdir($tmp, 0777, true);
$tmp = realpath($tmp);
chdir($tmp);

// installed.json / installed.php
$installed = [];
for ($case = 0; $case < 40; $case++) {
    $set = roundtrip(package_set($pool, mt_rand(0, 12)));
    $rootConfig = roundtrip(root_config());
    $rootAlias = chance(30) ? pick(['1.10.x-dev', '2.0.x-dev', '3.4']) : null;
    $dumpVersions = $case % 5 !== 4;
    $devNames = [];
    $paths = [];
    foreach ($set as $p) {
        $name = strtolower($p['name']);
        if (chance(30)) {
            $devNames[] = $name;
        }
        if (chance(15)) {
            $paths[$name] = pick(['', '/opt/abs/'.$name, 'vendor/../other/'.$name, 'vendor/'.$name.'/sub']);
        }
    }
    if (chance(20)) {
        $devNames[] = 'not/installed';
    }

    $dir = $tmp.'/vendor/composer';
    rmrf($tmp.'/vendor');
    $loader = new ArrayLoader(null, true);
    $rootPackage = $loader->load($rootConfig, RootPackage::class);
    if ($rootAlias !== null) {
        $rootPackage = new RootAliasPackage($rootPackage, (new \Composer\Package\Version\VersionParser())->normalize($rootAlias), $rootAlias);
    }
    $repo = new FilesystemRepository(new JsonFile($dir.'/installed.json'), $dumpVersions, $rootPackage);
    $repo->setDevPackageNames($devNames);
    foreach ($set as $data) {
        $repo->addPackage($loader->load($data));
    }
    $im = new PathMap();
    $im->paths = $paths;
    $im->cwd = $tmp;
    $devMode = chance(50);
    $result = attempt(static function () use ($repo, $im, $dir, $dumpVersions, $devMode) {
        $repo->write($devMode, $im);

        return ['json' => file_get_contents($dir.'/installed.json'), 'php' => $dumpVersions ? file_get_contents($dir.'/installed.php') : null];
    });
    $installed[] = [
        'packages' => $set,
        'root' => $rootConfig,
        'root_alias' => $rootAlias,
        'dump_versions' => $dumpVersions,
        'dev_mode' => $devMode,
        'dev_names' => $devNames,
        'paths' => (object) $paths,
        'result' => $result,
    ];
}
write_golden($root.'/internal/repository/testdata/oracle/installed.json.gz', $installed);

// composer.lock
$locks = [];
for ($case = 0; $case < 40; $case++) {
    $set = roundtrip(package_set($pool, mt_rand(0, 12)));
    $dev = chance(80) ? roundtrip(package_set($pool, mt_rand(0, 8))) : null;
    foreach ([&$set, &$dev] as &$list) {
        if ($list === null) {
            continue;
        }
        foreach ($list as &$p) {
            // getPackageTime would run git: keep dev packages from source out
            if (($p['installation-source'] ?? null) === 'source') {
                $p['installation-source'] = 'dist';
            }
        }
        unset($p);
    }
    unset($list);
    $composerJson = json_encode(roundtrip(root_config()) + ['require' => ['php' => '>=7.2']], JSON_PRETTY_PRINT);
    $platform = chance(50) ? ['php' => pick(['^8.1', '>=7.4']), 'ext-json' => '*'] : [];
    $platformDev = chance(30) ? ['ext-xdebug' => '*'] : [];
    $aliases = chance(30) ? [['package' => 'a/b', 'version' => pick(['dev-master', 'dev-main', '1.0.0.0']), 'alias' => '1.0.x-dev', 'alias_normalized' => '1.0.9999999.9999999-dev']] : [];
    $minimumStability = pick(['stable', 'dev', 'beta', 'RC']);
    $stabilityFlags = chance(50) ? ['z/z' => 20, 'a/a' => 10] : [];
    $preferStable = chance(50);
    $preferLowest = chance(20);
    $overrides = chance(30) ? ['php' => '8.1.0', 'ext-foo' => false] : [];

    $lockPath = $tmp.'/composer.lock';
    @unlink($lockPath);
    $locker = new Locker(new NullIO(), new JsonFile($lockPath), new PathMap(), $composerJson);
    $loader = new ArrayLoader(null, true);
    $packages = array_map([$loader, 'load'], $set);
    $devPackages = $dev === null ? null : array_map([$loader, 'load'], $dev);
    $result = attempt(static function () use ($locker, $packages, $devPackages, $platform, $platformDev, $aliases, $minimumStability, $stabilityFlags, $preferStable, $preferLowest, $overrides, $lockPath) {
        $changed = $locker->setLockData($packages, $devPackages, $platform, $platformDev, $aliases, $minimumStability, $stabilityFlags, $preferStable, $preferLowest, $overrides);
        $changedAgain = $locker->setLockData($packages, $devPackages, $platform, $platformDev, $aliases, $minimumStability, $stabilityFlags, $preferStable, $preferLowest, $overrides);

        return ['lock' => file_get_contents($lockPath), 'changed' => $changed, 'changed_again' => $changedAgain];
    });
    $locks[] = [
        'packages' => $set,
        'dev' => $dev,
        'composer_json' => $composerJson,
        'platform' => (object) $platform,
        'platform_dev' => (object) $platformDev,
        'aliases' => $aliases,
        'minimum_stability' => $minimumStability,
        'stability_flags' => (object) $stabilityFlags,
        'prefer_stable' => $preferStable,
        'prefer_lowest' => $preferLowest,
        'platform_overrides' => (object) $overrides,
        'result' => $result,
    ];
}
write_golden($root.'/internal/locker/testdata/oracle/lock.json.gz', $locks);

// content-hash
$hashes = [];
$documents = [
    '{}',
    '{"name": "a/b"}',
    '{"require": {"php": "^8.1", "a/b": "1.*"}, "name": "x/y", "description": "ignored"}',
    '{"config": {"platform": {"php": "8.1.0"}, "sort-packages": true}}',
    '{"config": {"sort-packages": true}}',
    '{"config": {"platform": {}}}',
    '{"extra": {"branch-alias": {"dev-main": "1.x-dev"}}, "repositories": [{"type": "vcs", "url": "https://example.org/x/y"}]}',
    '{"minimum-stability": "dev", "prefer-stable": true, "version": "1.2.3"}',
    '{"require-dev": {"phpunit/phpunit": "^10"}, "conflict": {"a/b": "<1"}, "replace": {"c/d": "self.version"}, "provide": {"e/f": "1.0"}}',
    '{"name": "a/été", "extra": {"url": "https://x/y?z=€"}}',
    '{"repositories": {"packagist.org": false}}',
    '{"autoload": {"psr-4": {"A\\\\": "src/"}}, "scripts": {"test": "phpunit"}}',
];
foreach ($pool as $i => $p) {
    if ($i % 401 === 0) {
        $documents[] = json_encode($p, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES);
    }
}
foreach ($documents as $doc) {
    $hashes[] = ['json' => $doc, 'hash' => attempt(static function () use ($doc) { return Locker::getContentHash($doc); })];
}
write_golden($root.'/internal/locker/testdata/oracle/contenthash.json', $hashes);

// InstalledRepository::getDependents (why / why-not)
function dump_dependents(array $results): array
{
    $out = [];
    foreach ($results as [$package, $link, $children]) {
        $out[] = [
            $package->getPrettyName().' '.$package->getPrettyVersion(),
            [$link->getSource(), $link->getTarget(), $link->getDescription(), $link->getPrettyConstraint()],
            $children === false ? false : dump_dependents($children),
        ];
    }

    return $out;
}

$dependents = [];
$graphNames = ['a/a', 'b/b', 'c/c', 'd/d', 'e/e', 'f/f', 'g/g'];
for ($case = 0; $case < 80; $case++) {
    $packages = [];
    foreach ($graphNames as $name) {
        if (chance(15)) {
            continue;
        }
        $p = ['name' => $name, 'version' => pick(['1.0.0', '2.0.0', '1.5.0'])];
        foreach (['require' => 50, 'replace' => 10, 'conflict' => 15, 'provide' => 10] as $type => $percent) {
            foreach ($graphNames as $target) {
                if ($target !== $name && chance($percent / 3)) {
                    $p[$type][$target] = pick(['*', '^1.0', '^2.0', '<1.5', 'self.version']);
                }
            }
        }
        if (chance(20)) {
            $p['require']['php'] = pick(['^8.0', '>=99']);
        }
        if (chance(20)) {
            $p['require']['x/missing'] = '^1.0';
        }
        $packages[] = $p;
    }
    $rootConfig = ['name' => '__root__', 'version' => '1.0.0'];
    foreach ($graphNames as $target) {
        if (chance(30)) {
            $rootConfig['require'][$target] = pick(['*', '^1.0', '^2.0']);
        } elseif (chance(15)) {
            $rootConfig['require-dev'][$target] = pick(['*', '^1.0']);
        }
    }
    $packages = roundtrip($packages);
    $rootConfig = roundtrip($rootConfig);
    $needle = chance(80) ? [pick($graphNames)] : [pick($graphNames), pick($graphNames)];
    $constraint = chance(50) ? null : pick(['^1.0', '^2.0', '*', '1.5.0']);
    $invert = chance(40);
    $recurse = chance(60);

    $loader = new ArrayLoader(null, true);
    $lockRepo = new \Composer\Repository\LockArrayRepository();
    foreach ($packages as $data) {
        $lockRepo->addPackage($loader->load($data));
    }
    $rootRepo = new \Composer\Repository\RootPackageRepository($loader->load($rootConfig, RootPackage::class));
    $repo = new \Composer\Repository\InstalledRepository([$rootRepo, $lockRepo]);
    $parsed = $constraint === null ? null : (new \Composer\Package\Version\VersionParser())->parseConstraints($constraint);
    $dependents[] = [
        'packages' => $packages,
        'root' => $rootConfig,
        'needle' => $needle,
        'constraint' => $constraint,
        'invert' => $invert,
        'recurse' => $recurse,
        'result' => attempt(static function () use ($repo, $needle, $parsed, $invert, $recurse) {
            return dump_dependents($repo->getDependents($needle, $parsed, $invert, $recurse));
        }),
    ];
}
write_golden($root.'/internal/repository/testdata/oracle/dependents.json.gz', $dependents);

rmrf($tmp);
