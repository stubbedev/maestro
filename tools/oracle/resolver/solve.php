<?php
// Generates internal/resolver/testdata/oracle/solve.json: Composer
// 2.10.3's update resolution (pool building with the optimizer, solving,
// the lock packages and operations, or the problem output) of the projects
// recorded by record.php, for a few variants of each, offline.
//
// With --bench it instead prints how long creating the pool and solving
// the base variant take (best of 5 runs), for comparison with the Go
// benchmark BenchmarkOracleSolve.
//
// Run: php tools/oracle/resolver/solve.php [--bench]
require __DIR__.'/common.php';

use Composer\DependencyResolver\DefaultPolicy;
use Composer\DependencyResolver\PoolOptimizer;
use Composer\DependencyResolver\Solver;
use Composer\DependencyResolver\SolverProblemsException;
use Composer\IO\NullIO;
use Composer\Package\Loader\ArrayLoader;
use Composer\Repository\ArrayRepository;

ini_set('memory_limit', '-1');

const PROJECTS = ['private-app', 'laravel', 'symfony-demo'];

/** The variants each project is resolved in; the Go test applies the same. */
function variants(): array
{
    return [
        ['name' => 'base'],
        ['name' => 'lowest', 'preferLowest' => true],
        ['name' => 'unstable', 'preferStable' => false, 'minimumStability' => 'dev'],
        ['name' => 'php-8.2', 'platform' => ['php' => '8.2.0', 'php-64bit' => '8.2.0']],
        ['name' => 'no-mongodb-intl', 'removePlatform' => ['ext-mongodb', 'ext-intl']],
        ['name' => 'conflict', 'require' => ['symfony/http-foundation' => '^5.4', 'psr/log' => '^1.0']],
        ['name' => 'missing', 'require' => ['acme/does-not-exist' => '^1.0', 'monolog/monolog' => '^99.0']],
    ];
}

function applyVariant(array $data, array $variant): array
{
    foreach ($variant['platform'] ?? [] as $name => $version) {
        foreach ($data['platform'] as $i => [$platformName]) {
            if ($platformName === $name) {
                $data['platform'][$i][1] = $version;
            }
        }
    }
    if (isset($variant['removePlatform'])) {
        $data['platform'] = array_values(array_filter($data['platform'], static function ($p) use ($variant) {
            return !in_array($p[0], $variant['removePlatform'], true);
        }));
    }
    foreach ($variant['require'] ?? [] as $name => $constraint) {
        $data['root']['require'][$name] = $constraint;
    }
    if (isset($variant['minimumStability'])) {
        $data['minimumStability'] = $variant['minimumStability'];
    }

    return $data;
}

function loadRepository(array $data): ArrayRepository
{
    $loader = new ArrayLoader(null, true);
    $repo = new ArrayRepository();
    foreach ($data['packages'] as $packageData) {
        $repo->addPackage($loader->load($packageData));
    }

    return $repo;
}

function solve(array $data, array $variant): array
{
    $data = applyVariant($data, $variant);
    $io = new NullIO();
    [$repositorySet, $request] = buildRequest($data, [loadRepository($data)]);
    $preferStable = $variant['preferStable'] ?? $data['preferStable'];
    $policy = new DefaultPolicy($preferStable, $variant['preferLowest'] ?? false);
    $pool = $repositorySet->createPool($request, $io, null, new PoolOptimizer($policy));
    $result = ['pool' => count($pool)];

    $solver = new Solver($policy, $pool, $io);
    try {
        $transaction = $solver->solve($request);
    } catch (SolverProblemsException $e) {
        $result['problems'] = $e->getPrettyString($repositorySet, $request, $pool, false);
        $result['problemsVerbose'] = $e->getPrettyString($repositorySet, $request, $pool, true);

        return $result;
    }
    $result['rules'] = $solver->getRuleSetSize();
    $result['lock'] = array_map(static function ($p) {
        return $p->getPrettyName().' '.$p->getPrettyVersion().' '.$p->getVersion();
    }, $transaction->getNewLockPackages(false));
    $result['operations'] = array_map('strval', $transaction->getOperations());

    return $result;
}

$platform = [];
foreach (platformVersions() as $name => $version) {
    $platform[] = [$name, $version];
}
$recordings = [];
foreach (PROJECTS as $project) {
    $recordings[$project] = json_decode(gzdecode(file_get_contents(__DIR__.'/../../../internal/resolver/testdata/oracle/'.$project.'.json.gz')), true);
    $recordings[$project]['platform'] = $platform;
}

if (in_array('--bench', $argv, true)) {
    foreach (PROJECTS as $project) {
        $best = INF;
        for ($i = 0; $i < 5; $i++) {
            $data = $recordings[$project];
            $repo = loadRepository($data);
            $start = microtime(true);
            [$repositorySet, $request] = buildRequest($data, [$repo]);
            $policy = new DefaultPolicy($data['preferStable'], false);
            $pool = $repositorySet->createPool($request, new NullIO(), null, new PoolOptimizer($policy));
            (new Solver($policy, $pool, new NullIO()))->solve($request);
            $best = min($best, microtime(true) - $start);
        }
        printf("%s: %.1f ms (createPool + solve, best of 5)\n", $project, $best * 1000);
    }

    return;
}

$golden = [
    // what the problem messages read from the PHP running Composer
    'loadedExtensions' => array_map('strtolower', get_loaded_extensions()),
    'iniFiles' => \Composer\Util\IniHelper::getAll(),
    'platform' => $platform,
    'variants' => variants(),
    'results' => [],
];
foreach (PROJECTS as $project) {
    foreach (variants() as $variant) {
        $golden['results'][$project][$variant['name']] = solve($recordings[$project], $variant);
        fwrite(STDERR, $project.' '.$variant['name'].": done\n");
    }
}
// The php.ini files are the machine's: the golden names placeholders,
// which the Go test's environment reports as its ini files.
$iniPaths = array_filter($golden['iniFiles'], static fn (string $f): bool => $f !== '');
$placeholders = array_map(static fn (int $i): string => $i === 0 ? '@PHPINI@' : "@PHPINI-$i@", array_keys($iniPaths));
array_walk_recursive($golden, static function (&$v) use ($iniPaths, $placeholders) {
    if (is_string($v)) {
        $v = str_replace($iniPaths, $placeholders, $v);
    }
});
write_golden(__DIR__.'/../../../internal/resolver/testdata/oracle/solve.json', $golden);
