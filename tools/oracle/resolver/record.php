<?php
// Records the repository data the real-world resolver oracles replay
// offline (internal/resolver/testdata/oracle/<project>.json.gz): it runs
// Composer 2.10.3's PoolBuilder against packagist.org for a project's
// composer.json and saves every package the pool loaded, with the derived
// root package data, so that tools/oracle/resolver/solve.php and the Go
// tests can rebuild the same pool without the network.
//
// Projects:
//   private-app      the composer.json of a private application, at
//                    $MAESTRO_PRIVATE_APP/composer.json (read only); its
//                    private repositories are dropped, and so are the
//                    requirements only they provide
//   laravel          laravel/laravel, latest stable release on Packagist
//   symfony-demo     symfony/symfony-demo, latest stable release on Packagist
//
// Run (needs the network): php tools/oracle/resolver/record.php [project...]
require __DIR__.'/common.php';

use Composer\Config;
use Composer\Factory;
use Composer\IO\NullIO;
use Composer\Json\JsonFile;
use Composer\Package\AliasPackage;
use Composer\Package\Dumper\ArrayDumper;
use Composer\Package\Loader\RootPackageLoader;
use Composer\Package\RootPackage;
use Composer\Package\Version\VersionParser;
use Composer\Repository\RepositoryFactory;
use Composer\Repository\RepositorySet;
use Composer\DependencyResolver\Request;

ini_set('memory_limit', '-1');
$cacheDir = sys_get_temp_dir().'/maestro-resolver-oracle';
putenv('COMPOSER_HOME='.$cacheDir.'/home');
putenv('COMPOSER_CACHE_DIR='.$cacheDir.'/cache');

/** The composer.json of the latest stable release of a Packagist package. */
function packagistComposerJson(string $name): array
{
    $data = json_decode(file_get_contents('https://repo.packagist.org/p2/'.$name.'.json'), true);
    $versions = \Composer\MetadataMinifier\MetadataMinifier::expand($data['packages'][$name]);
    foreach ($versions as $version) {
        if (VersionParser::parseStability($version['version']) !== 'stable') {
            continue;
        }
        $json = array_intersect_key($version, array_flip(['name', 'require', 'require-dev', 'conflict', 'provide', 'replace', 'minimum-stability', 'prefer-stable']));
        $json['version'] = $version['version'];

        return $json;
    }
    throw new \RuntimeException('no stable release of '.$name);
}

function projectComposerJson(string $project): array
{
    switch ($project) {
        case 'private-app':
            $dir = getenv('MAESTRO_PRIVATE_APP');
            if (!is_string($dir) || $dir === '') {
                throw new \RuntimeException('set MAESTRO_PRIVATE_APP to the private application\'s checkout');
            }
            $json = json_decode(file_get_contents($dir.'/composer.json'), true);
            $json = array_intersect_key($json, array_flip(['name', 'require', 'require-dev', 'conflict', 'provide', 'replace', 'minimum-stability', 'prefer-stable', 'repositories']));
            // only the inline package repositories are reproducible
            $json['repositories'] = array_values(array_filter($json['repositories'], static function ($repo) {
                return $repo['type'] === 'package';
            }));
            // its branch comes from a private VCS repository
            unset($json['require']['spiritix/lada-cache']);
            $json['version'] = '1.0.0';

            return $json;
        case 'laravel':
            return packagistComposerJson('laravel/laravel');
        case 'symfony-demo':
            return packagistComposerJson('symfony/symfony-demo');
    }
    throw new \InvalidArgumentException('unknown project '.$project);
}

function record(string $project): void
{
    $json = projectComposerJson($project);
    $io = new NullIO();

    for ($attempt = 0; ; $attempt++) {
        $config = Factory::createConfig($io, sys_get_temp_dir());
        if (isset($json['repositories'])) {
            $config->merge(['repositories' => $json['repositories']]);
        }
        $httpDownloader = Factory::createHttpDownloader($io, $config);
        $rm = RepositoryFactory::manager($io, $config, $httpDownloader);
        $root = (new RootPackageLoader($rm, $config))->load($json, RootPackage::class, sys_get_temp_dir());

        $data = rootData($root, $json);
        foreach (platformVersions() as $name => $version) {
            $data['platform'][] = [$name, $version];
        }
        [$repositorySet, $request] = buildRequest($data, $rm->getRepositories());
        $pool = $repositorySet->createPool($request, $io);

        // requirements nothing public provides are dropped (private repositories)
        $missing = [];
        foreach ($data['requires'] as $name => $constraint) {
            if (!preg_match('{^(php|ext-|lib-|composer)}', $name) && count($pool->whatProvides($name)) === 0) {
                $missing[] = $name;
            }
        }
        if ($missing === []) {
            break;
        }
        if ($attempt > 0) {
            throw new \RuntimeException($project.': still missing '.implode(', ', $missing));
        }
        foreach (['require', 'require-dev'] as $key) {
            foreach ($missing as $name) {
                unset($json[$key][$name]);
            }
        }
        fwrite(STDERR, $project.': dropped '.implode(', ', $missing)."\n");
    }

    $dumper = new ArrayDumper();
    $packages = [];
    foreach ($pool->getPackages() as $package) {
        if ($package instanceof AliasPackage || $package->getRepository() === null || $package->getRepository() instanceof \Composer\Repository\RootPackageRepository || in_array($package, $data['platformPackages'], true)) {
            continue;
        }
        $packages[] = $dumper->dump($package);
    }
    unset($data['platformPackages'], $data['platform']);
    $data['packages'] = $packages;

    $path = __DIR__.'/../../../internal/resolver/testdata/oracle/'.$project.'.json.gz';
    @mkdir(dirname($path), 0777, true);
    file_put_contents($path, gzencode(json_encode($data, JSON_FLAGS | JSON_THROW_ON_ERROR)."\n", 9));
    fwrite(STDERR, $project.': '.count($packages).' packages recorded in '.$path."\n");
}

foreach (array_slice($argv, 1) ?: ['private-app', 'laravel', 'symfony-demo'] as $project) {
    record($project);
}
