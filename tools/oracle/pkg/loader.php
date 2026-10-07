<?php
// Generates the goldens of internal/pkg/loader (testdata/oracle/):
//
//   packages.json.gz    every version of the p2 files in testdata/oracle/p2
//                       (expanded as MetadataMinifier does) through
//                       ArrayLoader::loadPackages and ArrayLoader::load, and
//                       Composer's own composer.lock through
//                       ArrayLoader(null, true)::load: the class, the
//                       ArrayDumper dump and the package's display forms
//                       (for ArrayLoader::load only the md5 of their JSON)
//   validating.json.gz  ValidatingArrayLoader(CHECK_ALL) and ArrayLoader on
//                       real package arrays with random corruptions and
//                       hand-picked invalid values: errors, warnings, the
//                       array handed to the inner loader, exceptions
//   formats.json        new \DateTime() on a corpus of time strings,
//                       filter_var(FILTER_VALIDATE_EMAIL) and
//                       ValidatingArrayLoader::filterUrl on corpora
//   rootloader.json     RootPackageLoader::extractAliases,
//                       extractStabilityFlags and extractReferences
//
// Run: php tools/oracle/pkg/loader.php
require __DIR__.'/common.php';

// the p2 inputs and validating.json.gz are held in memory whole
ini_set('memory_limit', '-1');

use Composer\Package\AliasPackage;
use Composer\Package\CompletePackage;
use Composer\Package\CompletePackageInterface;
use Composer\Package\Dumper\ArrayDumper;
use Composer\Package\Loader\ArrayLoader;
use Composer\Package\Loader\InvalidPackageException;
use Composer\Package\Loader\LoaderInterface;
use Composer\Package\Loader\RootPackageLoader;
use Composer\Package\Loader\ValidatingArrayLoader;
use Composer\Package\PackageInterface;
use Composer\Package\Version\VersionParser;

$out = $root.'/internal/pkg/loader/testdata/oracle';

function short_class($o): string
{
    $c = get_class($o);

    return substr($c, strrpos($c, '\\') + 1);
}

/** The display forms of a package. */
function info(PackageInterface $p): array
{
    $fpv = [];
    foreach ([PackageInterface::DISPLAY_SOURCE_REF_IF_DEV, PackageInterface::DISPLAY_SOURCE_REF, PackageInterface::DISPLAY_DIST_REF] as $mode) {
        foreach ([true, false] as $truncate) {
            $fpv[] = $p->getFullPrettyVersion($truncate, $mode);
        }
    }

    $info = [
        'unique' => $p->getUniqueName(),
        'pretty' => $p->getPrettyString(),
        'string' => (string) $p,
        'names' => $p->getNames(true),
        'namesNoProvides' => $p->getNames(false),
        'dev' => $p->isDev(),
        'stability' => $p->getStability(),
        'priority' => $p->getStabilityPriority(),
        'type' => $p->getType(),
        'targetDir' => $p->getTargetDir(),
        'sourceUrls' => $p->getSourceUrls(),
        'distUrls' => $p->getDistUrls(),
        'fullPrettyVersions' => $fpv,
        'defaultBranch' => $p->isDefaultBranch(),
    ];
    if ($p instanceof CompletePackageInterface) {
        $info['abandoned'] = $p->isAbandoned();
        $info['replacement'] = $p->getReplacementPackage();
    }
    if ($p instanceof AliasPackage) {
        $info['rootAlias'] = $p->isRootPackageAlias();
        $info['selfVersionRequires'] = $p->hasSelfVersionRequires();
        $links = [];
        foreach (['getRequires', 'getDevRequires', 'getConflicts', 'getProvides', 'getReplaces'] as $m) {
            foreach ($p->$m() as $k => $link) {
                $links[] = [$m, (string) $k, (string) $link, $link->getPrettyConstraint(), $link->getConstraint()->getPrettyString()];
            }
        }
        $info['links'] = $links;
    }

    return $info;
}

function describe(PackageInterface $p): array
{
    $d = ['class' => short_class($p), 'dump' => enc((new ArrayDumper())->dump($p)), 'info' => info($p)];
    if ($p instanceof AliasPackage) {
        $d['aliasOf'] = describe($p->getAliasOf());
    }

    return $d;
}

// packages.json.gz
$packages = [];
foreach (p2_inputs($root) as $name => $versions) {
    $loader = new ArrayLoader();
    $packages[$name.'#loadPackages'] = array_map('describe', $loader->loadPackages($versions));
    $loaded = [];
    foreach ($versions as $version) {
        $loaded[] = md5(enc(describe((new ArrayLoader())->load($version))));
    }
    $packages[$name.'#load'] = $loaded;
}
$lock = json_decode(file_get_contents($out.'/composer.lock'), true);
$loaded = [];
foreach (array_merge($lock['packages'], $lock['packages-dev']) as $config) {
    $loaded[] = describe((new ArrayLoader(null, true))->load($config));
}
$packages['composer.lock'] = $loaded;
write_golden($out.'/packages.json.gz', $packages);

// validating.json.gz
class CapturingLoader implements LoaderInterface
{
    public $config;

    public function load(array $config, string $class = 'Composer\Package\CompletePackage'): \Composer\Package\BasePackage
    {
        $this->config = $config;

        return new CompletePackage('captured/package', '1.0.0.0', '1.0.0');
    }
}

$pools = [
    'name' => ['foo/bar', 'Foo/Bar', 'fooBar/BazQux', 'foo', 'foo/-bar', 'fo--oo/bar', 'foo/bar.json', 'com1/foo', 'foo/lpt9', 'php', 'ext-json', '', ' ', 5, null, ['a'], 'foo/bar/baz', 'a/b', 'FOO/bar-BAZ', 'aB/cDE'],
    'version' => ['1.0.0', 'v2.3', 'dev-master', '1.0.x-dev', 'AA', '', ' ', 1, 1.5, true, false, null, [], '1.0.0-beta1', '2.x-dev', 'dev-feature/foo', '1.0 as 2.0', '9999999-dev'],
    'version_normalized' => ['1.0.0.0', '9999999-dev', 5, null, 'garbage'],
    'type' => ['library', 'Composer-Plugin', 'foo bar', 'x_y', '', 5, ['x'], 'php-ext', 'php-ext-zend', 'METAPACKAGE'],
    'target-dir' => ['Foo/Bar', '../x', '', 5, ['a']],
    'description' => ['A package', '', ' ', 5, ['x'], null],
    'homepage' => ['https://example.org', 'http://x', 'foo:bar', 'ftp://example.org', 'example.org', '', 5, '//example.org', 'https://', 'http://exa mple.org/'],
    'keywords' => [['a', 'b'], ['a', 5, 1.5, true, null, ['x']], 'keyword', [], ['dé jà', 'a+b', '微信', 'x/y', ''], ['k' => 'v']],
    'time' => ['2010-10-10T10:10:10+00:00', '2012-01-01', '2012-01-01 12:00:00', '2012-02-31', '2012-13-01', 'foo', '1234567890', '@1234567890', '', 5, '2012-01-01T10:00:00Z', '2012-01-01T10:00:00.123456+02:00', '2099-01-01T00:00:00+00:00', '2012/01/02', 'abc def', '2012-01-01garbage', '20120101', '2012-01-01T10:00:00+0530', '2012-01-01 EST', '2012-1-1'],
    'license' => ['MIT', 'proprietary', 'XXXXX', ' MIT ', ['MIT', 'GPL-3.0-or-later'], ['MIT', 5], [['author' => 'bar'], 'MIT'], 5, true, [], '(MIT or GPL-2.0-only)', 'GPL-2.0+', 'proprietary-ish', 'Apache-2.0 WITH LLVM-exception'],
    'authors' => [
        [['name' => 'A', 'email' => 'a@example.org', 'homepage' => 'https://a.example.org', 'role' => 'Dev']],
        [['name' => 5, 'email' => 'not an email', 'homepage' => 'nope']],
        ['string'], [], 'string',
        [['email' => ['x']], ['homepage' => '']],
        [['name' => 'B', 'email' => 'b@[127.0.0.1]', 'homepage' => 'ftp://x']],
        [['email' => 5]], [['role' => null]],
        [[]],
    ],
    'support' => [
        ['email' => 'mail@example.org', 'issues' => 'http://example.org/', 'irc' => 'irc://irc.example.org/chan'],
        ['source' => []], ['email' => 'nope', 'irc' => 'http://x', 'docs' => 'foo:bar', 'rss' => 5],
        'https://example.org', [], ['chat' => 'ircs://x/y', 'security' => 'https://x/s', 'wiki' => ''],
        ['forum' => null], ['unknown' => 5],
    ],
    'funding' => [
        [['type' => 'github', 'url' => 'https://github.com/x']], [['url' => 'nope']], ['x'], [['type' => 5, 'url' => 6]], [[]], 'x', [],
        [['type' => 'patreon']],
    ],
    'php-ext' => [
        ['extension-name' => 'ext-x', 'priority' => 80, 'support-zts' => true, 'support-nts' => false, 'build-path' => 'src', 'download-url-method' => 'composer-default', 'os-families' => ['linux'], 'configure-options' => [['name' => 'enable-x', 'needs-value' => false, 'description' => 'd']]],
        ['extension-name' => 5, 'priority' => '80', 'support-zts' => 'yes', 'support-nts' => 1, 'build-path' => 5],
        ['download-url-method' => [1, true, []]], ['download-url-method' => []], ['download-url-method' => 'nope'], ['download-url-method' => 5],
        ['os-families' => ['linux'], 'os-families-exclude' => ['windows']], ['os-families' => 'linux'], ['os-families' => []], ['os-families' => ['nope', 5]],
        ['configure-options' => 'x'], ['configure-options' => ['x', ['needs-value' => true], ['name' => 5], ['name' => 'a', 'needs-value' => 'yes', 'description' => 5]]],
        'x', [], ['build-path' => null], ['os-families-exclude' => ['windows', 'bsd']],
    ],
    'require' => [
        ['a/b' => '1.*', 'php' => '>=7.4', 'ext-json' => '*'],
        ['foo/baz' => '*', 'bar/baz' => '>=1.0', 'bar/hacked' => '@stable', 'bar/woo' => '1.0.0', 'bar/unstable' => '0.3.0'],
        ['foo/baz' => '>1, <0.5', 'bar/baz' => 'dev-main, >0.5'],
        ['Foo/Baz' => '^1.0', 'foo/bar' => '1.0', 'foo bar/x' => '1.0', 'a/b' => 5, 'c/d' => '~', 'e/f' => 'self.version', 'g/h' => ['x']],
        'x', [], ['a/b' => '1.0 as 2.0', 'c/d' => 'dev-main#abc123', 'e/f' => '^1.0@dev'],
        ['lib-icu' => '*', 'composer-plugin-api' => '^2.0', 'php-64bit' => '*'],
        ['a/b' => '=1.0.0', 'c/d' => '==2.0', 'e/f' => '1.0.0-beta', 'g/h' => '0.9.9', 'i/j' => '^1 || ^2'],
    ],
    'require-dev' => [['a/b' => '1.*'], ['Foo/Baz' => '^1.0', 'x/y' => 5], 'x'],
    'conflict' => [['a/b' => '<1.0'], ['a/b' => '1.0', 'c/d' => '2.0'], 'x', ['b/c' => '*', 'a/b' => '*']],
    'replace' => [['a/b' => 'self.version'], ['a/b' => '1.0'], 'x', ['c/d' => '*']],
    'provide' => [['psr/log-implementation' => '1.0'], ['a/b' => ['x']]],
    'suggest' => [['a/b' => 'Useful'], ['a/b' => 5, 'c/d' => 'self.version', 'e/f' => ' self.version '], 'x', []],
    'minimum-stability' => ['dev', 'RC', 'rc', 'Rc', 'nope', 5, 'STABLE', ''],
    'autoload' => [
        ['psr-4' => ['Foo\\' => 'src/']], ['psr-4' => ['Foo' => 'src/', '' => 'lib/', 5 => 'x']], ['psr0' => ['foo' => 'src']], 'strings',
        ['classmap' => ['src/'], 'files' => ['f.php'], 'exclude-from-classmap' => ['/tests/']], [5 => 'x'], ['psr-4' => 'x'],
    ],
    'autoload-dev' => [['psr-4' => ['Foo\\Tests\\' => 'tests/']], 'x'],
    'include-path' => [['lib/'], ['lib/', 5, ['x']], 'x'],
    'bin' => ['bin/foo', '../evil', ['bin/a', '../../b', 'c/../d', '..\\x'], ['a', 5, null], '', [], 'bin\\..\\x'],
    'source' => [
        ['type' => 'git', 'url' => 'https://github.com/a/b.git', 'reference' => 'abc'],
        ['type' => 'git', 'url' => '--upload-pack=x', 'reference' => '-x'],
        ['url' => 'x'], ['type' => 5, 'url' => [], 'reference' => 1.5], ['type' => 'perforce', 'url' => 'rsh:touch /tmp/pwned', 'reference' => 'x'],
        ['type' => 'perforce', 'url' => 'ssl:p4.example.org:1666', 'reference' => '//depot'], 'x', [],
        ['type' => 'svn', 'url' => 'https://x', 'reference' => 2019],
        ['type' => 'git', 'url' => 'https://x', 'reference' => 'r', 'mirrors' => [['url' => 'https://mirror/%package%', 'preferred' => true]]],
    ],
    'dist' => [
        ['type' => 'zip', 'url' => 'https://api.github.com/repos/a/b/zipball/0123456789abcdef0123456789abcdef01234567', 'reference' => '0123456789abcdef0123456789abcdef01234567', 'shasum' => ''],
        ['type' => 'zip', 'url' => ' -oProxyCommand=x'], ['type' => 'zip'], ['type' => 'tar', 'url' => 'x', 'reference' => [1]], 'x', [],
        ['type' => 'zip', 'url' => 'https://x/%package%/%version%.zip', 'mirrors' => [['url' => 'https://m/%package%/%reference%.%type%', 'preferred' => false], ['url' => 'https://n/%package%', 'preferred' => true]]],
        ['type' => '', 'url' => ''],
    ],
    'transport-options' => [['ssl' => ['verify_peer' => false]], 'x', []],
    'extra' => [
        ['branch-alias' => ['dev-master' => '2.0-dev', 'dev-old' => '1.0.x-dev', '3.x-dev' => '3.1.x-dev']],
        ['branch-alias' => ['5.x-dev' => '3.1.x-dev', 'dev-x' => 5, 'dev-y' => '1.0', 'dev-z' => 'foo-dev', '4.x-dev' => '4.1-dev']],
        ['branch-alias' => 'x'], 'x', ['random' => ['deep' => true]],
        ['branch-alias' => ['dev-main' => '9999999-dev']],
    ],
    'config' => [['platform' => ['php' => '7.4.0', 'ext-x' => false, 'ext-y' => 5, 'ext-z' => 'nope', 'lib-a' => null]], ['platform' => '7.4'], 'x', ['platform' => true]],
    'scripts' => [['post-install-cmd' => 'X::y', 'test' => ['a', 'b'], 'n' => null, 'i' => 5], 'x', [], ['composer' => 'x']],
    'archive' => [['exclude' => ['/tests']], ['name' => 'foo'], 'x', ['name' => 5], ['exclude' => 'x']],
    'abandoned' => [true, false, 'foo/bar', '', 5],
    'notification-url' => ['https://packagist.org/downloads/', '', 5],
    'installation-source' => ['dist', 'source', 5],
    'default-branch' => [true, false, 'yes'],
    'repositories' => [[['type' => 'vcs', 'url' => 'x']], 'x'],
    'non-feature-branches' => [['main']],
];
$weird = [5, -1, 1.5, true, false, null, '', ' ', 'x', [], ['x'], ['k' => 'v'], [['nested' => [1, 2]]]];

$bases = [];
foreach (p2_inputs($root) as $name => $versions) {
    foreach ($versions as $i => $version) {
        if ($i % 13 === 0) {
            $bases[] = $version;
        }
    }
}
foreach (array_merge($lock['packages'], $lock['packages-dev']) as $i => $config) {
    if ($i % 5 === 0) {
        $bases[] = $config;
    }
}
$bases[] = ['name' => 'foo/bar'];
$bases[] = [];

$cases = [];
foreach ($bases as $base) {
    $cases[] = $base;
    for ($n = 0; $n < 4; $n++) {
        $config = $base;
        $edits = mt_rand(1, 4);
        for ($e = 0; $e < $edits; $e++) {
            $key = pick(array_keys($pools));
            if (chance(15)) {
                $config[$key] = pick($weird);
            } elseif (chance(10)) {
                unset($config[$key]);
            } else {
                $config[$key] = pick($pools[$key]);
            }
        }
        $cases[] = $config;
    }
}
foreach ($pools as $key => $values) {
    foreach ($values as $value) {
        $cases[] = ['name' => 'foo/bar', 'version' => '1.0.0', $key => $value];
    }
    foreach ($weird as $value) {
        $cases[] = ['name' => 'foo/bar', $key => $value];
    }
}
// php-ext needs the matching type
foreach ($pools['php-ext'] as $value) {
    $cases[] = ['name' => 'foo/bar', 'type' => 'php-ext', 'php-ext' => $value];
}

$now = time();
$validating = ['now' => $now];
foreach ($cases as $i => $config) {
    // round-trip through JSON so that both sides start from the same array
    $json = enc($config);
    $config = json_decode($json, true);

    $inner = new CapturingLoader();
    $loader = new ValidatingArrayLoader($inner, true, null, ValidatingArrayLoader::CHECK_ALL);
    $result = ['in' => $json];
    try {
        $loader->load($config);
        $result['passed'] = enc($inner->config);
    } catch (InvalidPackageException $e) {
        $result['invalid'] = $e->getMessage();
        $result['data'] = enc($e->getData());
    } catch (\Throwable $e) {
        $result['e'] = exception($e);
    }
    $result['errors'] = $loader->getErrors();
    $result['warnings'] = $loader->getWarnings();
    $result['arrayLoader'] = attempt(static function () use ($config) {
        return describe((new ArrayLoader(null, true))->load($config));
    });
    $result['rootLoader'] = attempt(static function () use ($config) {
        return describe((new ArrayLoader(null, true))->load($config, 'Composer\Package\RootPackage'));
    });
    $validating[(string) $i] = $result;
}
write_golden($out.'/validating.json.gz', $validating);

// formats.json
$times = $pools['time'];
foreach (['2010-10-10T10:10:10+00:00', '2023-06-01T12:34:56+00:00', '2014-01-01 00:00:00', '1999-12-31T23:59:60Z',
    '2012-01-01T24:00:00', '2012-01-01T10', '2012-01-01t10:00:00z', '  2012-01-01  ', '2012-01-01T10:00:00 +05', '2012-01-01 10:00:00 PST',
    '2012-01-01 10:00:00 CEST', '2012-01-01 10:00 Europe/Paris', '2012-01-01 10:00:00 UTC', '2012-01-01 10:00:00 GMT', '2012-01-01T10:00:00-03:30',
    '@0', '@-1', '@1700000000', '@1700000000.5', '2012-00-01', '2012-01-00', '2012-01-32', '0', '12345', 'x2012', '2012-01-01T25:00:00',
    '2012-01-01T10:60:00', '2012-01-01T10:00:61', '2020-02-29', '2021-02-29', '2012-01-01T10:00:00.1234567Z', '2012-01-01 10:00',
    '2012-01-01T10:00', '2012-1-01', '2012-01-1', '2012-01-01   10:00:00', "2012-01-01\t10:00:00", '2012-01-01T10:00:00+14:00', '2012-01-01T10:00:00-1200'] as $t) {
    $times[] = $t;
}
$formats = ['time' => [], 'email' => [], 'url' => []];
foreach ($times as $t) {
    if (!is_string($t) || $t === '') {
        continue;
    }
    $formats['time'][] = [$t, attempt(static function () use ($t) {
        $d = new \DateTime($t, new \DateTimeZone('UTC'));

        return [$d->format(DATE_RFC3339), $d->getTimestamp(), (int) $d->format('u')];
    })];
}
foreach (['a@example.org', 'a.b+c@example.co.uk', 'a@b', 'a@localhost', '"quoted"@example.org', 'a..b@example.org', '.a@example.org',
    'a@example..org', 'a@[127.0.0.1]', 'a@[IPv6:::1]', 'a@[IPv6:2001:db8::1]', 'a@-example.org', 'a@example-.org', 'a@xn--bcher-kva.example',
    str_repeat('a', 64).'@example.org', str_repeat('a', 65).'@example.org', 'a@'.str_repeat('b', 63).'.org', 'a@'.str_repeat('b', 64).'.org',
    'a@'.str_repeat('b.', 160).'org', 'plainaddress', '@example.org', 'a@', 'a b@example.org', 'a@example.org ', 'Ä@example.org', 'a@exämple.org',
    'mail@example.org', 'not an email', 'x@y.z1', 'x@y.1z', 'x@1.2.3.4', "a\"b@example.org", 'a\\b@example.org', '"a\\"b"@example.org',
    'a@b.c-d', 'a@B.COM', 'A@B.COM', 'a@[999.0.0.1]', 'a@[IPv6:::ffff:127.0.0.1]', 'a@example.org.', 'a!#$%&\'*+/=?^_`{|}~-@example.org'] as $email) {
    $formats['email'][] = [$email, filter_var($email, FILTER_VALIDATE_EMAIL) !== false];
}
$filterUrl = new \ReflectionMethod(ValidatingArrayLoader::class, 'filterUrl');
$filterUrl->setAccessible(true);
$validator = (new \ReflectionClass(ValidatingArrayLoader::class))->newInstanceWithoutConstructor();
foreach (['https://example.org', 'http://x', 'foo:bar', 'ftp://example.org', 'example.org', '', '//example.org', 'https://', 'http://exa mple.org/',
    'HTTP://example.org', 'https://user:pass@example.org:8080/p?q#f', 'https://example.org:99999', 'irc://irc.example.org/chan', 'ircs://x/y',
    'irc:x', 'http:/x', 'http:///x', 'https://[::1]/', 'https://0', 'mailto:a@b', 'https://example.org:80', 'x://y', ':80', 'http://:80',
    'https://a@', 'https://a@b@c', "https://ex\x01ample.org", 'file:///etc/passwd', 'https://example.org#frag', '0://x', 'https://e/x?y'] as $url) {
    $formats['url'][] = [$url, $filterUrl->invoke($validator, $url), $filterUrl->invoke($validator, $url, ['irc', 'ircs'])];
}
write_golden($out.'/formats.json', $formats);

// rootloader.json
$extractAliases = new \ReflectionMethod(RootPackageLoader::class, 'extractAliases');
$extractAliases->setAccessible(true);
$rootLoader = (new \ReflectionClass(RootPackageLoader::class))->newInstanceWithoutConstructor();
$prop = new \ReflectionProperty(ArrayLoader::class, 'versionParser');
$prop->setAccessible(true);
$prop->setValue($rootLoader, new VersionParser());

$constraints = ['~2.1.0-beta2', '1.0.x-dev as 1.2.0', '1.0.*@rc', '~1.0,>=1.0.2@dev', '^2.0@dev || ^2.0@dev', '^2.0@rc || >=3.0@dev , ~3.5@alpha',
    'dev-master || 2.0 , ~3.5-alpha', '3.0-beta || 2.0 , ~3.5-alpha', 'dev-main#abc123', 'dev-main#ABC', '1.0.x-dev#abcdef as 1.0.0', '1.0.0',
    '^1.0', '@dev', '@stable', 'dev-feature as 1.0.0', '1.0.0 as dev-main', 'dev-a as 1.0 | dev-b as 2.0', '1.0 as ', 'foo as bar as baz',
    '1.0.0-alpha3', '2.0-RC1', '1.0.0-beta', '1.0.0-dev', '>=1.0-dev <2.0', '1.0.0@beta', '1.0.0@BETA', '^1.0@Dev', 'dev-main as 1.0.x-dev',
    '1.x-dev', 'dev-master#5b7b1e0b1f37d6a4ec4c2efcafbc7e3c3d8d5bf3', '1.2.3#abc', '~1.0 || dev-main', '*', '', ' ', 'self.version',
    '1.0.0 as 1.0.0-beta', 'dev-x as 2.0.0-RC1', '>1.0-alpha, <2.0-beta'];
$minimums = ['stable', 'RC', 'beta', 'alpha', 'dev'];
$rootloader = [];
foreach ($constraints as $i => $c) {
    $requires = ['Vendor/Pkg' => $c, 'other/pkg' => pick($constraints)];
    $min = pick($minimums);
    $rootloader[(string) $i] = [
        'requires' => enc($requires),
        'minimum' => $min,
        'flags' => attempt(static function () use ($requires, $min) { return enc(RootPackageLoader::extractStabilityFlags($requires, $min, ['vendor/pkg' => 20, 'x/y' => 5])); }),
        'references' => attempt(static function () use ($requires) { return enc(RootPackageLoader::extractReferences($requires, ['x/y' => 'abc'])); }),
        'aliases' => attempt(static function () use ($extractAliases, $rootLoader, $requires) { return enc($extractAliases->invoke($rootLoader, $requires, [])); }),
    ];
}
write_golden($out.'/rootloader.json', $rootloader);
