<?php
// Generates internal/autoload/testdata/oracle/dump.json.gz: Composer's
// AutoloadGenerator::dump() over generated projects (root package, vendor
// packages with psr-0/psr-4/classmap/files/exclude-from-classmap rules and
// target dirs, metapackages, aliases, include paths, platform requirements,
// replaces and provides, PHP files declaring classes in and out of their
// PSR paths, duplicate classes, symlinks), under every generator setting
// (dev mode or its detection from installed.json, dev package names,
// optimize, authoritative class map, APCu, platform-check, platform
// requirement filters, use-include-path, prepend-autoloader, the suffix
// sources, strict ambiguity, vendor dir inside, outside or equal to the
// project dir).
//
// Each scenario records every file dump() writes (except ClassLoader.php
// and LICENSE, which are copied verbatim), the classes of the returned class
// map, the IO output and the exception thrown, with the scenario's root
// directory replaced by "%ROOT%". Scenarios only duplicate classes across
// scanned directories, never within one, so that the result does not depend
// on the directory order; the output still lists the warnings of one
// directory in that order, so the test compares its lines sorted.
//
// Run: php tools/oracle/autoload/generate.php [output file]
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Composer\Autoload\AutoloadGenerator;
use Composer\Config;
use Composer\EventDispatcher\EventDispatcher;
use Composer\Filter\PlatformRequirementFilter\PlatformRequirementFilterFactory;
use Composer\Installer\InstallationManager;
use Composer\IO\BufferIO;
use Composer\Package\AliasPackage;
use Composer\Package\Loader\ArrayLoader;
use Composer\Package\Locker;
use Composer\Package\PackageInterface;
use Composer\Package\RootPackage;
use Composer\Repository\InstalledArrayRepository;
use Composer\Util\Filesystem;

set_error_handler(static function (int $level, string $message, string $file, int $line): bool {
    if ($level === E_DEPRECATED || $level === E_USER_DEPRECATED || 0 === (error_reporting() & $level)) {
        return true;
    }
    throw new \ErrorException($message, 0, $level, $file, $line);
});

mt_srand(20261005);

$repo = dirname(__DIR__, 3);
$output = $argv[1] ?? $repo.'/internal/autoload/testdata/oracle/dump.json.gz';

function pick(array $a) { return $a[mt_rand(0, count($a) - 1)]; }
function chance(int $percent): bool { return mt_rand(1, 100) <= $percent; }

/** The scenario being generated: files to create and class names used. */
final class Gen
{
    /** @var array<string, string> */
    public $files = [];
    /** @var array<string, string> */
    public $symlinks = [];
    /** @var list<string> */
    public $classes = [];
    /** @var array<string, string> class => the package dir declaring it */
    public $owner = [];
    /** @var int */
    public $n = 0;
    /** @var string the package dir classes are generated for */
    public $dir = '';

    public function className(): string
    {
        // a class of another package makes it ambiguous
        $others = array_keys(array_filter($this->owner, function ($dir) { return $dir !== $this->dir; }));
        if ($others !== [] && chance(8)) {
            return pick($others);
        }
        $name = pick(['Foo', 'Bar', 'Baz', 'Qux', 'Lorem', 'Ipsum']).(++$this->n);
        $this->owner[$name] = $this->dir;

        return $name;
    }

    public function php(string $path, string $namespace, string $class, string $kind = 'class'): void
    {
        $ns = $namespace === '' ? '' : 'namespace '.$namespace.'; ';
        $this->files[$path] = '<?php '.$ns.$kind.' '.$class.' {}';
    }
}

/**
 * Autoload rules for a package installed in $dir (relative to the root),
 * with files for them.
 *
 * @return array<string, mixed>
 */
function autoloadRules(Gen $g, string $dir, string $ownerDir, bool $wholeDir): array
{
    $g->dir = $ownerDir;
    $rules = [];
    // the whole package dir is only scanned when that cannot include
    // another package's classes
    $dirs = ['src', 'lib', 'src/', 'classes', 'app/src'];
    if ($wholeDir) {
        array_push($dirs, './', '');
    }

    if (chance(60)) {
        foreach (range(1, mt_rand(1, 3)) as $_) {
            $ns = pick(['Acme\\', 'Acme\\Util\\', 'Foo\\', 'Foo\\Bar\\', '', 'Vendor\\Pkg'.mt_rand(1, 4).'\\', '\\Lead\\']);
            $paths = chance(25) ? [pick($dirs), pick($dirs)] : pick($dirs);
            $rules['psr-4'][$ns] = $paths;
            foreach ((array) $paths as $p) {
                $base = rtrim($dir.'/'.trim($p, '/'), '/.');
                $nsName = trim($ns, '\\');
                foreach (range(1, mt_rand(0, 3)) as $_) {
                    $class = $g->className();
                    $sub = chance(30) ? pick(['Sub', 'Deep/Er']) : '';
                    $fullNs = trim($nsName.($sub !== '' ? '\\'.str_replace('/', '\\', $sub) : ''), '\\');
                    if (chance(15)) {
                        // a PSR-4 violation: the file name differs from the class
                        $g->php($base.'/'.($sub !== '' ? $sub.'/' : '').'Wrong'.$class.'.php', $fullNs, $class);
                    } else {
                        $g->php($base.'/'.($sub !== '' ? $sub.'/' : '').$class.'.php', $fullNs, $class, pick(['class', 'class', 'interface', 'trait', 'enum']));
                    }
                }
            }
        }
    }

    if (chance(40)) {
        foreach (range(1, mt_rand(1, 2)) as $_) {
            $prefix = pick(['Acme', 'Acme_', 'Foo\\Bar', 'Legacy_', 'Main\\Foo', '']);
            $p = pick($dirs);
            $rules['psr-0'][$prefix] = chance(20) ? [$p] : $p;
            $base = rtrim($dir.'/'.trim($p, '/'), '/.');
            foreach (range(1, mt_rand(0, 2)) as $_) {
                $class = $g->className();
                if (str_ends_with($prefix, '_')) {
                    $g->php($base.'/'.rtrim($prefix, '_').'/'.$class.'.php', '', rtrim($prefix, '_').'_'.$class);
                } elseif ($prefix === '') {
                    $g->php($base.'/'.$class.'.php', '', $class);
                } else {
                    $g->php($base.'/'.str_replace('\\', '/', $prefix).'/'.$class.'.php', $prefix, chance(10) ? 'Other'.$class : $class);
                }
            }
        }
    }

    if (chance(50)) {
        foreach (range(1, mt_rand(1, 3)) as $_) {
            $kind = mt_rand(1, 4);
            if ($kind === 1) {
                $file = pick(['classmap.php', 'inc/legacy.inc', 'Thing.hh']);
                $rules['classmap'][] = $file;
                $g->php($dir.'/'.$file, '', $g->className());
            } else {
                $cm = pick($wholeDir ? ['classmap', 'cm/', 'legacy', 'tests', 'fixtures/x', ''] : ['classmap', 'cm/', 'legacy', 'tests', 'fixtures/x']);
                $rules['classmap'][] = $cm;
                $base = rtrim($dir.'/'.trim($cm, '/'), '/');
                foreach (range(1, mt_rand(1, 3)) as $i) {
                    $sub = pick(['', 'a/', 'b/c/', 'Excluded/', 'tests/']);
                    $g->php($base.'/'.$sub.'f'.$i.mt_rand(0, 99).'.php', pick(['', 'Cm', 'Cm\\Sub']), $g->className());
                }
            }
        }
    }

    if (chance(35)) {
        foreach (range(1, mt_rand(1, 3)) as $_) {
            $file = pick(['bootstrap.php', 'functions.php', './helpers.php', 'src/functions.php', 'missing.php']);
            $rules['files'][] = $file;
            if ($file !== 'missing.php') {
                $g->files[$dir.'/'.ltrim($file, './')] = '<?php function f'.(++$g->n).'() {}';
            }
        }
    }

    if (chance(30)) {
        foreach (range(1, mt_rand(1, 3)) as $_) {
            $rules['exclude-from-classmap'][] = pick([
                'src/Excluded/', '/classmap/Excluded/', '**/tests/', 'classmap/*/c/', 'cm/a', 'legacy/f1*.php',
                '../outside', './src', 'lib/Wrong*', 'Excluded', '**/Excluded/**', 'composers', '/src-ca/',
            ]);
        }
    }

    // keys that are not autoload rules, or rules that are not arrays
    if (chance(5)) {
        $rules[pick(['files', 'classmap'])] = 'not-an-array.php';
    }

    return $rules;
}

/** @return array<string, mixed> a package config for ArrayLoader */
function packageConfig(Gen $g, string $name, string $vendorDir, array $others): array
{
    $config = ['name' => $name, 'version' => pick(['1.0.0', '2.3.4', 'dev-main', '0.1.0'])];
    if (chance(10)) {
        $config['type'] = 'metapackage';
    } elseif (chance(10)) {
        $config['type'] = pick(['library', 'composer-plugin', 'project']);
    }
    $targetDir = null;
    if (chance(12)) {
        $targetDir = pick(['Some/Target', 'target', 'Foo/Bar/']);
        $config['target-dir'] = $targetDir;
    }
    $installDir = $vendorDir.'/'.$name.($targetDir !== null ? '/'.rtrim($targetDir, '/') : '');
    $config['autoload'] = autoloadRules($g, $installDir, $vendorDir.'/'.$name, true);
    if ($targetDir !== null && isset($config['autoload']['psr-4']) && chance(70)) {
        unset($config['autoload']['psr-4']);
    }
    if (chance(20)) {
        $config['include-path'] = [pick(['lib/', 'library', '/src/', '.'])];
    }
    foreach ($others as $other) {
        if ($other !== $name && chance(30)) {
            $config['require'][$other] = pick(['*', '^1.0', 'self.version']);
        }
    }
    if (chance(30)) {
        $config['require']['php'] = pick(['^7.2', '>=8.1', '~8.0.3', '^8.2.10 || ^7.4', '<8', '*', '>7.1-dev']);
    }
    if (chance(15)) {
        $config['require']['php-64bit'] = pick(['*', '^8.1']);
    }
    if (chance(30)) {
        foreach (range(1, mt_rand(1, 3)) as $_) {
            $config['require'][pick(['ext-json', 'ext-mbstring', 'ext-Pdo', 'ext-zend-opcache', 'ext-pcntl', 'ext-readline', 'ext-intl', 'ext-fileinfo'])] = pick(['*', '^7.2', '>=1.0']);
        }
    }
    if (chance(15)) {
        $config['replace'][pick(['ext-pdo', 'ext-intl', 'other/replaced', 'ext-mbstring'])] = pick(['*', '7.1.*', '^8.0']);
    }
    if (chance(15)) {
        $config['provide'][pick(['ext-json', 'ext-PDO', 'ext-fileinfo', 'psr/log-implementation'])] = pick(['*', '1.0.0', '^7.1']);
    }

    return $config;
}

/** @return array<string, mixed> */
function scenario(int $i): array
{
    $g = new Gen();
    $layout = pick(['normal', 'normal', 'normal', 'outside', 'same', 'deep']);
    $workDir = 'proj';
    $vendorDir = ['normal' => 'proj/vendor', 'outside' => 'vendor', 'same' => 'proj', 'deep' => 'proj/a/b/vendor'][$layout];

    $names = [];
    foreach (range(1, mt_rand(0, 6)) as $_) {
        $names[] = pick(['a/a', 'b/b', 'c/lorem', 'd/d', 'z/foo', 'acme/util', 'foo/bar-baz', 'single']);
    }
    $names = array_values(array_unique($names));

    $packages = [];
    foreach ($names as $name) {
        $config = packageConfig($g, $name, $vendorDir, $names);
        if (chance(10)) {
            $config['alias'] = '9.9.x-dev';
        }
        $packages[] = $config;
    }

    $root = ['name' => pick(['root/pkg', '__root__', 'test/package']), 'version' => '1.0.0'];
    $wholeDir = $layout === 'outside';
    $root['autoload'] = autoloadRules($g, $workDir, $workDir, $wholeDir);
    if (chance(40)) {
        $root['autoload-dev'] = autoloadRules($g, $workDir, $workDir, $wholeDir);
    }
    // (its psr-0 rules for '' scan the project dir, which includes the
    // packages when it is the vendor dir)
    if ($layout !== 'same' && chance(8)) {
        $root['target-dir'] = pick(['Main/Foo/', 'Main/Foo']);
        unset($root['autoload']['psr-4'], $root['autoload-dev']['psr-4']);
        if (chance(70)) {
            $root['autoload']['psr-0'] = ['Main\\Foo' => '', 'Main\\Bar' => ''];
            $g->php($workDir.'/Bar/Baz.php', 'Main\\Foo\\Bar', 'Baz');
        }
    }
    if (chance(15)) {
        $root['include-path'] = [pick(['/lib', 'src/', 'lib'])];
    }
    foreach ($names as $name) {
        if (chance(60)) {
            $root['require'][$name] = '*';
        } elseif (chance(40)) {
            $root['require-dev'][$name] = '*';
        }
    }
    if (chance(40)) {
        $root['require']['php'] = pick(['^7.2', '>=8.1', '^7.2.8', '>= 7.2', '< 8', '^8.0 || ^7.4.1']);
    }
    if (chance(30)) {
        foreach (range(1, mt_rand(1, 3)) as $_) {
            $root['require'][pick(['ext-xml', 'ext-json', 'ext-pdo', 'ext-bcMath', 'ext-zend-opcache', 'ext-pcntl', 'php-64bit'])] = pick(['*', '^7.2']);
        }
    }
    if (chance(15)) {
        $root['provide'][pick(['ext-xml', 'ext-XML', 'ext-json'])] = '*';
    }
    if (chance(15)) {
        $root['replace'][pick(['ext-pdo', 'ext-BCMath', 'b/b'])] = pick(['^7.1', '7.1.*', '*']);
    }

    // a symlinked directory inside a scanned one
    if (chance(10)) {
        $g->dir = $workDir;
        $g->php($workDir.'/forks/bar/src/exclude/FooExclClass.php', '', $g->className());
        $g->files[$workDir.'/src/linked/.keep'] = '';
        $g->symlinks[$workDir.'/src/linked/bar'] = $workDir.'/forks/bar';
    }

    $devMode = pick([null, true, false]);
    $s = [
        'name' => sprintf('s%03d', $i),
        'workDir' => $workDir,
        'vendorDir' => $vendorDir,
        'targetDir' => pick(['composer', 'composer', 'composer', 'sub/composer']),
        'config' => [
            'platform-check' => pick([true, false, 'php-only', 'php-only']),
            'use-include-path' => chance(15),
            'prepend-autoloader' => pick([true, false, null]),
            'autoloader-suffix' => chance(15) ? 'Cfg'.$i : null,
        ],
        'devMode' => $devMode,
        'installedDev' => $devMode === null ? pick([null, true, false]) : null,
        'devPackageNames' => array_values(array_filter($names, static function () { return chance(25); })),
        'authoritative' => chance(15),
        'apcu' => chance(15),
        'apcuPrefix' => pick(['pfx', "it's"]),
        'scanPsr' => chance(50),
        'suffix' => chance(40) ? 'S'.$i : '',
        'strictAmbiguous' => chance(20),
        'existingAutoload' => chance(30) ? "<?php\nreturn ComposerAutoloaderInitOld$i::getLoader();\n" : null,
        'lockHash' => pick(['0123456789abcdef0123456789abcdef', 'not a hash <<<<', 'deadbeef']),
        'ignore' => pick([false, false, false, true, ['php'], ['ext-*', 'php-64bit'], ['ext-json']]),
        'root' => $root,
        'packages' => $packages,
        'files' => $g->files,
        'symlinks' => $g->symlinks,
    ];
    // without a suffix from the settings or an existing autoload.php the
    // lock's content-hash must be one, as the random fallback differs
    if ($s['suffix'] === '' && $s['config']['autoloader-suffix'] === null && $s['existingAutoload'] === null) {
        $s['lockHash'] = '0123456789abcdef0123456789abcd'.sprintf('%02x', $i % 256);
    }

    return $s;
}

final class StubDispatcher extends EventDispatcher
{
    public function __construct() {}
}

final class StubConfig extends Config
{
    /** @var array<string, mixed> */
    private $values;

    /** @param array<string, mixed> $values */
    public function __construct(array $values)
    {
        $this->values = $values;
    }

    public function get(string $key, int $flags = 0)
    {
        return $this->values[$key] ?? null;
    }
}

final class StubRepository extends InstalledArrayRepository
{
    /** @var list<PackageInterface> */
    private $list;

    /** @param list<PackageInterface> $packages */
    public function __construct(array $packages)
    {
        parent::__construct();
        $this->list = $packages;
    }

    public function getCanonicalPackages(): array
    {
        return $this->list;
    }
}

final class StubLocker extends Locker
{
    /** @var string */
    private $hash;

    public function __construct(string $hash)
    {
        $this->hash = $hash;
    }

    public function isLocked(): bool
    {
        return true;
    }

    public function getLockData(): array
    {
        return ['content-hash' => $this->hash];
    }
}

/** @return array<string, mixed> */
function run(array $s, string $root): array
{
    $fs = new Filesystem();
    foreach ($s['files'] as $path => $content) {
        $fs->ensureDirectoryExists(dirname($root.'/'.$path));
        file_put_contents($root.'/'.$path, $content);
    }
    foreach ($s['symlinks'] as $link => $target) {
        $fs->ensureDirectoryExists(dirname($root.'/'.$link));
        symlink($root.'/'.$target, $root.'/'.$link);
    }
    $fs->ensureDirectoryExists($root.'/'.$s['workDir']);
    $vendorDir = $root.'/'.$s['vendorDir'];
    if ($s['installedDev'] !== null) {
        $fs->ensureDirectoryExists($vendorDir.'/composer');
        file_put_contents($vendorDir.'/composer/installed.json', json_encode(['packages' => [], 'dev' => $s['installedDev']]));
    }
    if ($s['existingAutoload'] !== null) {
        $fs->ensureDirectoryExists($vendorDir);
        file_put_contents($vendorDir.'/autoload.php', $s['existingAutoload']);
    }

    $loader = new ArrayLoader();
    $rootPackage = $loader->load($s['root'], RootPackage::class);
    $packages = [];
    foreach ($s['packages'] as $config) {
        $package = $loader->load($config);
        $packages[] = $package;
        if (isset($config['alias'])) {
            $packages[] = new AliasPackage($package, '9999999-dev', $config['alias']);
        }
    }
    $repository = new StubRepository($packages);
    $repository->setDevPackageNames($s['devPackageNames']);

    $im = new class($vendorDir) extends InstallationManager {
        /** @var string */
        private $vendorDir;

        public function __construct(string $vendorDir)
        {
            $this->vendorDir = $vendorDir;
        }

        public function getInstallPath(PackageInterface $package): ?string
        {
            if ($package->getType() === 'metapackage') {
                return null;
            }
            $targetDir = $package->getTargetDir();

            return $this->vendorDir.'/'.$package->getName().($targetDir ? '/'.$targetDir : '');
        }
    };

    $io = new BufferIO();
    $generator = new AutoloadGenerator(new StubDispatcher(), $io);
    if ($s['devMode'] !== null) {
        $generator->setDevMode($s['devMode']);
    }
    $generator->setClassMapAuthoritative($s['authoritative']);
    $generator->setApcu($s['apcu'], $s['apcuPrefix']);
    $generator->setPlatformRequirementFilter(PlatformRequirementFilterFactory::fromBoolOrList($s['ignore']));
    $config = new StubConfig(['vendor-dir' => $vendorDir] + $s['config']);

    $cwd = getcwd();
    chdir($root.'/'.$s['workDir']);
    $error = null;
    $classes = null;
    try {
        $classMap = $generator->dump($config, $repository, $rootPackage, $im, $s['targetDir'], $s['scanPsr'], $s['suffix'], new StubLocker($s['lockHash']), $s['strictAmbiguous']);
        $classes = array_keys($classMap->getMap());
    } catch (\Throwable $e) {
        $error = ['class' => get_class($e), 'message' => $e->getMessage()];
    } finally {
        chdir($cwd);
    }

    $files = [];
    $target = realpath($vendorDir).'/'.$s['targetDir'];
    foreach (['autoload.php' => $vendorDir.'/autoload.php'] + array_combine(
        array_map(static function ($f) use ($s) { return $s['targetDir'].'/'.$f; }, FILES),
        array_map(static function ($f) use ($target) { return $target.'/'.$f; }, FILES)
    ) as $name => $path) {
        if (is_file($path)) {
            $files[$name] = file_get_contents($path);
        }
    }

    $replace = static function ($v) use ($root) { return is_string($v) ? str_replace($root, '%ROOT%', $v) : $v; };

    return [
        'error' => $error === null ? null : array_map($replace, $error),
        'classes' => $classes,
        'output' => $replace($io->getOutput()),
        'files' => array_map($replace, $files),
    ];
}

const FILES = [
    'autoload_real.php', 'autoload_static.php', 'autoload_namespaces.php', 'autoload_psr4.php',
    'autoload_classmap.php', 'autoload_files.php', 'include_paths.php', 'platform_check.php',
];

$results = [];
$fs = new Filesystem();
for ($i = 0; $i < 400; $i++) {
    $s = scenario($i);
    // a fresh directory per scenario, as PHP's realpath cache outlives
    // deleted files
    $root = sys_get_temp_dir().'/autoload-oracle-'.getmypid().'-'.$i;
    $fs->removeDirectory($root);
    mkdir($root);
    $root = realpath($root);
    $s['result'] = run($s, $root);
    $fs->removeDirectory($root);
    $results[] = $s;
}

@mkdir(dirname($output), 0777, true);
file_put_contents($output, gzencode(json_encode($results, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_PRETTY_PRINT | JSON_THROW_ON_ERROR), 9));
echo count($results)." scenarios written to $output\n";
