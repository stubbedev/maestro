<?php
// Shared by the resolver oracle scripts: the platform the projects are
// resolved for, the root package data the recordings keep, and the
// RepositorySet/Request setup of Installer::doUpdate (createRepositorySet,
// createRequest, requirePackagesForUpdate) that internal/resolver's
// oracle test mirrors.

require_once __DIR__.'/../pkg/common.php';

use Composer\Package\BasePackage;
use Composer\Package\CompletePackage;
use Composer\Package\Link;
use Composer\Package\Loader\ArrayLoader;
use Composer\Package\RootPackage;
use Composer\Package\RootPackageInterface;
use Composer\Package\Version\VersionParser;
use Composer\Repository\ArrayRepository;
use Composer\Repository\RepositorySet;
use Composer\Repository\RootPackageRepository;
use Composer\DependencyResolver\Request;
use Composer\Semver\Constraint\Constraint;

/** The platform packages (name => pretty version) projects resolve against. */
function platformVersions(): array
{
    $platform = [
        'php' => '8.5.1',
        'php-64bit' => '8.5.1',
        'composer' => '2.10.3',
        'composer-plugin-api' => '2.9.0',
        'composer-runtime-api' => '2.2.2',
        'ext-mongodb' => '2.5.0',
        'ext-redis' => '6.2.0',
        'lib-icu' => '76.1',
        'lib-libxml' => '2.13.8',
        'lib-curl' => '8.14.1',
        'lib-openssl' => '3.5.1',
        'lib-pcre' => '10.45',
        'lib-zip' => '1.11.4',
    ];
    foreach (['apcu', 'bcmath', 'bz2', 'calendar', 'ctype', 'curl', 'date', 'dom', 'exif', 'fileinfo', 'filter', 'ftp', 'gd', 'gettext', 'gmp', 'hash', 'iconv', 'igbinary', 'imagick', 'intl', 'json', 'ldap', 'libxml', 'mbstring', 'mysqli', 'mysqlnd', 'opcache', 'openssl', 'pcntl', 'pcre', 'pdo', 'pdo_mysql', 'pdo_pgsql', 'pdo_sqlite', 'pgsql', 'phar', 'posix', 'random', 'readline', 'reflection', 'session', 'shmop', 'simplexml', 'soap', 'sockets', 'sodium', 'spl', 'sqlite3', 'standard', 'sysvmsg', 'sysvsem', 'sysvshm', 'tokenizer', 'xml', 'xmlreader', 'xmlwriter', 'xsl', 'zend-opcache', 'zip', 'zlib'] as $ext) {
        $platform['ext-'.$ext] = '8.5.1';
    }

    return $platform;
}

/**
 * The root package data RootPackageLoader derives, as the recordings keep
 * it. The platform is not recorded: solve.php resolves for platformVersions().
 */
function rootData(RootPackageInterface $root, array $json): array
{
    $stabilityFlags = $root->getStabilityFlags();
    $stabilityFlags[$root->getName()] = BasePackage::STABILITIES[VersionParser::parseStability($root->getVersion())];

    return [
        'root' => $json,
        'minimumStability' => $root->getMinimumStability(),
        'preferStable' => $root->getPreferStable(),
        'stabilityFlags' => $stabilityFlags,
        'aliases' => $root->getAliases(),
        'references' => $root->getReferences(),
        'requires' => array_map(static function (Link $link) { return $link->getPrettyConstraint(); }, array_merge($root->getRequires(), $root->getDevRequires())),
        'platform' => [],
        'platformPackages' => [],
    ];
}

/**
 * Installer::doUpdate's RepositorySet and Request for the recorded data:
 * [RepositorySet, Request].
 */
function buildRequest(array &$data, array $repositories): array
{
    $parser = new VersionParser();
    $platformPackages = [];
    foreach ($data['platform'] as [$name, $version]) {
        $platformPackages[] = new CompletePackage($name, $parser->normalize($version), $version);
    }
    $platformRepo = new ArrayRepository($platformPackages);
    $data['platformPackages'] = $platformPackages;

    $root = (new ArrayLoader(null, true))->load($data['root'], RootPackage::class);
    $links = array_merge($root->getRequires(), $root->getDevRequires());

    $rootRequires = [];
    foreach ($links as $req => $link) {
        $rootRequires[$req] = $link->getConstraint();
    }

    $fixedRootPackage = clone $root;
    $fixedRootPackage->setRequires([]);
    $fixedRootPackage->setDevRequires([]);

    $repositorySet = new RepositorySet($data['minimumStability'], $data['stabilityFlags'], $data['aliases'], $data['references'], $rootRequires);
    $repositorySet->addRepository(new RootPackageRepository($fixedRootPackage));
    $repositorySet->addRepository($platformRepo);
    foreach ($repositories as $repository) {
        $repositorySet->addRepository($repository);
    }

    $request = new Request();
    $request->fixPackage($fixedRootPackage);
    $provided = $fixedRootPackage->getProvides();
    foreach ($platformRepo->getPackages() as $package) {
        if (!isset($provided[$package->getName()]) || !$provided[$package->getName()]->getConstraint()->matches(new Constraint('=', $package->getVersion()))) {
            $request->fixPackage($package);
        }
    }
    foreach ($links as $link) {
        $request->requireName($link->getTarget(), $link->getConstraint());
    }

    return [$repositorySet, $request];
}
