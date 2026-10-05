<?php
// Generates internal/repository/composerrepo/testdata/oracle/composerrepo.json.gz
// by running Composer 2.10.3's own ComposerRepository through scenarios
// against a fake repository server: the Packagist p2 files of
// internal/pkg/loader/testdata/oracle/p2 and small inline files, served
// with Last-Modified dates and If-Modified-Since handling.
//
// For every step the golden records the requests made (URL and transport
// options, in order), the result, the output (verbose BufferIO) and the
// files of the repository's metadata cache (name => sha256 of the
// content), so that caches are interchangeable with Composer's.
//
// Run: php tools/oracle/composerrepo/oracle.php
require __DIR__.'/../pkg/common.php';

use Composer\Config;
use Composer\Downloader\TransportException;
use Composer\IO\BufferIO;
use Composer\IO\NullIO;
use Composer\Package\AliasPackage;
use Composer\Package\Dumper\ArrayDumper;
use Composer\Package\Package;
use Composer\Package\PackageInterface;
use Composer\Repository\ComposerRepository;
use Composer\Repository\RepositoryInterface;
use Composer\Semver\Constraint\MatchAllConstraint;
use Composer\Package\Version\VersionParser;
use Composer\Util\Http\Response;
use Composer\Util\HttpDownloader;
use React\Promise\PromiseInterface;
use Symfony\Component\Console\Output\OutputInterface;

$root = dirname(__DIR__, 3);

/** A repository server answering from a file map. */
final class FakeServer extends HttpDownloader
{
    /** @var array<string, array{status?: int, lastModified?: string, fixture?: string, body?: string}> */
    public $files = [];
    /** @var list<array{string, mixed}> */
    public $log = [];
    /** @var string */
    public $fixtures;

    public function __construct(string $fixtures)
    {
        parent::__construct(new NullIO(), new Config(false));
        $this->fixtures = $fixtures;
    }

    public function get($url, $options = [])
    {
        $this->log[] = [$url, $options];

        return $this->serve($url, $options);
    }

    public function add($url, $options = []): PromiseInterface
    {
        $this->log[] = [$url, $options];
        try {
            return \React\Promise\resolve($this->serve($url, $options));
        } catch (\Throwable $e) {
            return \React\Promise\reject($e);
        }
    }

    private function serve(string $url, array $options): Response
    {
        $file = $this->files[$url] ?? ['status' => 404];
        $status = $file['status'] ?? 200;
        if ($status >= 400) {
            $e = new TransportException('The "'.$url.'" file could not be downloaded (HTTP/1.1 '.$status.' Error)', $status);
            $e->setStatusCode($status);
            throw $e;
        }

        $headers = ['HTTP/1.1 200 OK'];
        if (isset($file['lastModified'])) {
            foreach ((array) ($options['http']['header'] ?? []) as $header) {
                if ($header === 'If-Modified-Since: '.$file['lastModified']) {
                    return new Response(['url' => $url], 304, ['HTTP/1.1 304 Not Modified'], '');
                }
            }
            $headers[] = 'Last-Modified: '.$file['lastModified'];
        }

        $body = isset($file['fixture']) ? gzdecode(file_get_contents($this->fixtures.'/'.$file['fixture'].'.json.gz')) : $file['body'];

        return new Response(['url' => $url], 200, $headers, $body);
    }
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

/** The files of a cache directory: relative path => sha256 of the content. */
function cache_files(string $dir): array
{
    $out = [];
    if (is_dir($dir)) {
        foreach (new RecursiveIteratorIterator(new RecursiveDirectoryIterator($dir, FilesystemIterator::SKIP_DOTS)) as $f) {
            $out[substr($f->getPathname(), strlen($dir) + 1)] = hash('sha256', file_get_contents($f->getPathname()));
        }
    }
    ksort($out, SORT_STRING);

    return $out;
}

function short_class($o): string
{
    $c = get_class($o);

    return substr($c, strrpos($c, '\\') + 1);
}

function describe(?PackageInterface $p): ?array
{
    if ($p === null) {
        return null;
    }

    return [
        short_class($p),
        $p->getUniqueName(),
        $p->getPrettyVersion(),
        $p instanceof AliasPackage ? $p->getAliasOf()->getUniqueName() : null,
        $p->getRepository() ? $p->getRepository()->getRepoName() : null,
        $p->getNotificationUrl(),
        $p->getDistUrls(),
        $p->getSourceUrls(),
        $p->getTransportOptions(),
        hash('sha256', enc((new ArrayDumper())->dump($p))),
    ];
}

function describe_all(array $packages): array
{
    return array_values(array_map('describe', $packages));
}

function constraint_map(VersionParser $parser, array $map): array
{
    $out = [];
    foreach ($map as $name => $c) {
        $out[$name] = $c === null ? null : ($c === '*' ? new MatchAllConstraint() : $parser->parseConstraints($c));
    }

    return $out;
}

function run_step(ComposerRepository $repo, array $step)
{
    $parser = new VersionParser();
    switch ($step['op']) {
        case 'loadPackages':
            $already = [];
            foreach ($step['alreadyLoaded'] ?? [] as $name => $versions) {
                foreach ($versions as $v) {
                    $already[$name][$v] = new Package($name, $v, $v);
                }
            }
            $r = $repo->loadPackages(constraint_map($parser, $step['names']), $step['acceptable'] ?? [], $step['flags'] ?? [], $already);

            return ['namesFound' => $r['namesFound'], 'packages' => describe_all($r['packages'])];
        case 'findPackage':
            return describe($repo->findPackage($step['name'], $step['constraint']));
        case 'findPackages':
            return describe_all($repo->findPackages($step['name'], $step['constraint']));
        case 'getPackages':
            return describe_all($repo->getPackages());
        case 'count':
            return count($repo);
        case 'getPackageNames':
            return array_values($repo->getPackageNames($step['filter']));
        case 'search':
            return $repo->search($step['query'], $step['mode'], $step['type']);
        case 'getProviders':
            return array_values(array_map(static function (array $p): array {
                return [$p['name'], $p['description'] ?? null, $p['type'] ?? ''];
            }, $repo->getProviders($step['name'])));
        case 'hasSecurityAdvisories':
            return $repo->hasSecurityAdvisories();
        case 'getSecurityAdvisories':
            $r = $repo->getSecurityAdvisories(constraint_map($parser, $step['map']), $step['partial']);
            $advisories = [];
            foreach ($r['advisories'] as $name => $list) {
                $advisories[$name] = array_map(static function ($a) {
                    return [short_class($a), $a->jsonSerialize()];
                }, $list);
            }

            return ['namesFound' => $r['namesFound'], 'advisories' => $advisories];
        case 'hasFilter':
            return $repo->hasFilter();
        case 'getFilterLists':
            return $repo->getFilterLists();
        case 'getFilter':
            $r = $repo->getFilter(constraint_map($parser, $step['map']), $step['lists']);
            $out = [];
            foreach ($r['filter'] as $list => $entries) {
                foreach ($entries as $e) {
                    $out[$list][] = [$e->packageName, $e->listName, $e->constraint->getPrettyString(), (string) $e->constraint, $e->url, $e->reason, $e->id, $e->source];
                }
            }

            return $out;
    }

    throw new \LogicException('unknown op '.$step['op']);
}

function run_scenario(string $fixtures, array $scenario): array
{
    $tmp = sys_get_temp_dir().'/maestro-composerrepo-oracle-'.getmypid();
    rmrf($tmp);
    mkdir($tmp, 0777, true);
    $server = new FakeServer($fixtures);
    $server->files = $scenario['files'];

    $config = new Config(false, $tmp);
    $config->merge(['config' => ['home' => $tmp, 'cache-dir' => $tmp.'/cache']]);
    $cacheDir = $tmp.'/cache/repo';

    $results = [];
    $repo = null;
    $io = null;
    foreach ($scenario['steps'] as $step) {
        foreach ($step['files'] ?? [] as $url => $file) {
            $server->files[$url] = $file;
        }
        $server->log = [];
        if ($step['op'] === 'new') {
            $io = new BufferIO('', OutputInterface::VERBOSITY_VERBOSE);
            $result = attempt(static function () use (&$repo, $step, $io, $config, $server) {
                $repo = new ComposerRepository($step['config'], $io, $config, $server);

                return $repo->getRepoName();
            });
        } else {
            $result = attempt(static function () use ($repo, $step) {
                return run_step($repo, $step);
            });
        }
        $results[] = [
            'requests' => $server->log,
            'result' => $result,
            'output' => $io->getOutput(),
            'cache' => cache_files($cacheDir),
        ];
        // each step's output separately
        $io = new BufferIO('', OutputInterface::VERBOSITY_VERBOSE);
        if ($repo !== null) {
            $ref = new \ReflectionProperty(ComposerRepository::class, 'io');
            $ref->setAccessible(true);
            $ref->setValue($repo, $io);
        }
    }
    rmrf($tmp);

    return ['files' => $scenario['files'], 'steps' => $scenario['steps'], 'results' => $results];
}

$lm = 'Mon, 01 Sep 2025 10:00:00 GMT';
$lm2 = 'Tue, 02 Sep 2025 10:00:00 GMT';
$p2 = static function (string $fixture, string $lastModified = 'Mon, 01 Sep 2025 10:00:00 GMT'): array {
    return ['fixture' => $fixture, 'lastModified' => $lastModified];
};
$json = static function ($data, ?string $lastModified = null, int $status = 200): array {
    $f = ['body' => json_encode($data, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE)];
    if ($lastModified !== null) {
        $f['lastModified'] = $lastModified;
    }
    if ($status !== 200) {
        $f['status'] = $status;
    }

    return $f;
};

// a small package in the three metadata formats
$small = static function (string $name, array $versions, bool $minify = true): array {
    $list = [];
    foreach ($versions as $v => $extra) {
        $list[] = array_merge([
            'name' => $name,
            'description' => 'A test package',
            'version' => $v,
            'type' => 'library',
            'dist' => ['type' => 'zip', 'url' => 'https://example.org/dists/'.$name.'/'.$v.'.zip', 'reference' => sha1($name.$v), 'shasum' => ''],
            'source' => ['type' => 'git', 'url' => 'https://example.org/'.$name.'.git', 'reference' => sha1($name.$v)],
            'require' => ['php' => '>=7.2'],
        ], $extra);
    }

    return $minify ? \Composer\MetadataMinifier\MetadataMinifier::minify($list) : $list;
};

$packagistRoot = [
    'packages' => [],
    'notify-batch' => 'https://packagist.org/downloads/',
    'providers-url' => '/p/%package%$%hash%.json',
    'metadata-url' => '/p2/%package%.json',
    'search' => 'https://packagist.org/search.json?q=%query%&type=%type%',
    'list' => 'https://packagist.org/packages/list.json',
    'providers-api' => 'https://packagist.org/providers/%package%.json',
    'security-advisories' => ['metadata' => true, 'api-url' => 'https://packagist.org/api/security-advisories/'],
    'provider-includes' => ['p/provider-2025$%hash%.json' => ['sha256' => 'abc']],
    'mirrors' => [['dist-url' => 'https://mirror.example.org/%package%/%version%/%reference%.%type%', 'preferred' => true]],
    'warnings' => [
        ['message' => 'A warning for this <comment>Composer</comment> version', 'versions' => '>=2.0'],
        ['message' => 'Not shown', 'versions' => '<1.0'],
    ],
    'infos' => [['message' => 'An info', 'versions' => '*']],
];

$p2Files = [];
foreach (['doctrine/orm', 'guzzlehttp/guzzle', 'laravel/framework', 'monolog/monolog', 'phpunit/phpunit', 'symfony/symfony'] as $name) {
    $fixture = str_replace('/', '_', $name);
    $p2Files['https://repo.packagist.org/p2/'.$name.'.json'] = $p2($fixture);
    $p2Files['https://repo.packagist.org/p2/'.$name.'~dev.json'] = $p2($fixture.'~dev');
}

$scenarios = [];

$scenarios['packagist'] = [
    'files' => array_merge([
        'https://repo.packagist.org/packages.json' => $json($packagistRoot, $lm),
        'https://packagist.org/search.json?q=mono&type=' => $json(['results' => [
            ['name' => 'monolog/monolog', 'description' => 'Sends your logs', 'url' => 'https://packagist.org/packages/monolog/monolog', 'repository' => 'https://github.com/Seldaek/monolog', 'downloads' => 1, 'favers' => 2],
            ['name' => 'virtual/mono', 'description' => '', 'virtual' => true],
            ['name' => 'old/mono', 'description' => null, 'abandoned' => 'monolog/monolog'],
        ], 'total' => 3]),
        'https://packagist.org/search.json?q=foo+bar&type=library' => $json(['results' => []]),
        'https://packagist.org/packages/list.json?filter=symfony%2F%2A' => $json(['packageNames' => ['symfony/symfony', 'symfony/console']]),
        'https://packagist.org/packages/list.json' => $json(['packageNames' => ['symfony/symfony', 'symfony/console', 'monolog/monolog', 'acme/foo']]),
        'https://packagist.org/packages/list.json?vendor=symfony&filter=symfony%2Fcon%2A' => $json(['packageNames' => ['symfony/console']]),
        'https://packagist.org/providers/psr/log-implementation.json' => $json(['providers' => [
            ['name' => 'monolog/monolog', 'description' => 'Sends your logs', 'type' => 'library'],
            ['name' => 'acme/logger'],
        ]]),
        'https://packagist.org/api/security-advisories/' => $json(['advisories' => [
            'monolog/monolog' => [
                ['advisoryId' => 'PKSA-test-1', 'packageName' => 'monolog/monolog', 'affectedVersions' => '>=2.0.0,<2.1.0', 'title' => 'Test advisory', 'cve' => 'CVE-2025-0001', 'link' => 'https://example.org/adv', 'reportedAt' => '2025-01-02 03:04:05', 'sources' => [['name' => 'GitHub', 'remoteId' => 'GHSA-1']], 'severity' => 'high'],
                ['advisoryId' => 'PKSA-test-2', 'packageName' => 'monolog/monolog', 'affectedVersions' => '>=3.0.0', 'title' => 'Other advisory', 'cve' => null, 'link' => null, 'reportedAt' => '2025-02-02T03:04:05+00:00', 'sources' => [['name' => 'FriendsOfPHP', 'remoteId' => 'x']], 'severity' => null],
            ],
            'unrequested/package' => [],
        ]]),
    ], $p2Files),
    'steps' => [
        ['op' => 'new', 'config' => ['url' => 'https://packagist.org']],
        ['op' => 'loadPackages', 'names' => ['monolog/monolog' => '^2.0 || ^3.0', 'guzzlehttp/guzzle' => null, 'php' => '>=7', 'laravel/framework' => '^10.0', 'acme/missing' => null], 'acceptable' => ['stable' => 0], 'flags' => []],
        ['op' => 'loadPackages', 'names' => ['symfony/symfony' => 'dev-main || ^7.0', 'phpunit/phpunit' => '*', 'Doctrine/ORM' => '^2.0'], 'acceptable' => ['stable' => 0, 'dev' => 20], 'flags' => ['symfony/symfony' => 20], 'alreadyLoaded' => ['phpunit/phpunit' => ['10.5.0.0', '9.6.0.0']]],
        ['op' => 'findPackage', 'name' => 'Monolog/Monolog', 'constraint' => '^2.0'],
        ['op' => 'findPackages', 'name' => 'guzzlehttp/guzzle', 'constraint' => '^7.0'],
        ['op' => 'findPackages', 'name' => 'doctrine/orm', 'constraint' => null],
        ['op' => 'findPackage', 'name' => 'acme/missing', 'constraint' => '*'],
        ['op' => 'getPackageNames', 'filter' => 'symfony/*'],
        ['op' => 'getPackageNames', 'filter' => null],
        ['op' => 'search', 'query' => 'mono', 'mode' => 0, 'type' => null],
        ['op' => 'search', 'query' => 'foo bar', 'mode' => 0, 'type' => 'library'],
        ['op' => 'search', 'query' => '^symfony/con', 'mode' => 1, 'type' => null],
        ['op' => 'search', 'query' => 'sym mon', 'mode' => 1, 'type' => null],
        ['op' => 'search', 'query' => 'sym', 'mode' => 2, 'type' => null],
        ['op' => 'getProviders', 'name' => 'psr/log-implementation'],
        ['op' => 'getProviders', 'name' => 'missing/thing'],
        ['op' => 'hasSecurityAdvisories'],
        ['op' => 'getSecurityAdvisories', 'map' => ['monolog/monolog' => '==2.0.0.0', 'guzzlehttp/guzzle' => '*', 'php' => '*'], 'partial' => false],
        ['op' => 'getSecurityAdvisories', 'map' => ['monolog/monolog' => '*', 'guzzlehttp/guzzle' => '==6.5.0.0', 'laravel/framework' => '*'], 'partial' => true],
        ['op' => 'getPackages'],
        ['op' => 'hasFilter'],
        ['op' => 'getFilterLists'],
        // a new run: the cache is warm, everything revalidates with If-Modified-Since
        ['op' => 'new', 'config' => ['url' => 'https://repo.packagist.org', 'options' => ['http' => ['header' => 'X-Test: 1']]]],
        ['op' => 'loadPackages', 'names' => ['monolog/monolog' => '^2.0 || ^3.0', 'guzzlehttp/guzzle' => null], 'acceptable' => ['stable' => 0, 'dev' => 20], 'flags' => []],
        // a third run where the server has a newer file
        ['op' => 'new', 'config' => ['url' => 'https://repo.packagist.org/']],
        ['op' => 'loadPackages', 'names' => ['monolog/monolog' => null], 'acceptable' => ['stable' => 0], 'flags' => [], 'files' => ['https://repo.packagist.org/p2/monolog/monolog.json' => $p2('monolog_monolog', $lm2)]],
        ['op' => 'search', 'query' => 'sym', 'mode' => 2, 'type' => null],
    ],
];

$scenarios['dev-only-and-non-minified'] = [
    'files' => [
        'http://example.org/packages.json' => $json(['metadata-url' => '/p2/%package%.json', 'notify' => '/notify', 'available-package-patterns' => ['acme/*']]),
        'http://example.org/p2/acme/one.json' => $json(['packages' => ['acme/one' => $small('acme/one', ['1.0.0' => [], '1.1.0' => ['require' => ['php' => '>=8.0']], '2.0.0-beta1' => []], false)]], $lm),
        'http://example.org/p2/acme/one~dev.json' => $json(['minified' => 'composer/2.0', 'packages' => ['acme/one' => $small('acme/one', ['dev-main' => ['extra' => ['branch-alias' => ['dev-main' => '2.x-dev']], 'default-branch' => true], 'dev-feature' => []])]], $lm),
        'http://example.org/p2/acme/two.json' => ['status' => 500],
    ],
    'steps' => [
        ['op' => 'new', 'config' => ['url' => 'example.org']],
        ['op' => 'loadPackages', 'names' => ['acme/one' => null, 'other/pkg' => null], 'acceptable' => ['dev' => 20], 'flags' => []],
        ['op' => 'loadPackages', 'names' => ['acme/one' => '>=1.1'], 'acceptable' => ['stable' => 0, 'beta' => 10, 'dev' => 20], 'flags' => []],
        ['op' => 'findPackages', 'name' => 'acme/one', 'constraint' => null],
        ['op' => 'getPackageNames', 'filter' => null],
        ['op' => 'getPackages'],
        ['op' => 'loadPackages', 'names' => ['acme/two' => null], 'acceptable' => ['stable' => 0], 'flags' => []],
        // the server breaks: the cache is used, with a warning
        ['op' => 'new', 'config' => ['url' => 'http://example.org/']],
        ['op' => 'loadPackages', 'names' => ['acme/one' => null], 'acceptable' => ['stable' => 0, 'dev' => 20], 'flags' => [], 'files' => [
            'http://example.org/p2/acme/one.json' => ['status' => 500],
            'http://example.org/p2/acme/one~dev.json' => ['status' => 499],
        ]],
        ['op' => 'loadPackages', 'names' => ['acme/three' => null], 'acceptable' => ['stable' => 0], 'flags' => [], 'files' => [
            'http://example.org/p2/acme/three.json' => ['status' => 499],
        ]],
    ],
];

$advisoryList = [
    ['advisoryId' => 'PKSA-a', 'packageName' => 'acme/lib', 'affectedVersions' => '<1.1', 'title' => 'T', 'cve' => null, 'link' => null, 'reportedAt' => '2024-01-01 00:00:00', 'sources' => [['name' => 'S', 'remoteId' => 'r']], 'severity' => 'low'],
    ['advisoryId' => 'PKSA-b', 'packageName' => 'acme/lib', 'affectedVersions' => '>=1.0,<=3.20-test2'],
];
$scenarios['available-packages-advisories-filter'] = [
    'files' => [
        'https://repo.example.org/packages.json' => $json([
            'metadata-url' => 'https://repo.example.org/p2/%package%.json',
            'available-packages' => ['Acme/Lib', 'acme/tool', 'acme/lib'],
            'security-advisories' => ['metadata' => true],
            'filter' => ['metadata' => true, 'lists' => ['malware' => ['enabled' => true], 'advisories' => ['enabled' => true], 'ignore-me' => ['enabled' => true], 'custom' => ['enabled' => true], 'off' => ['enabled' => false]], 'summary-url' => '/summary.json'],
        ], $lm),
        'https://repo.example.org/summary.json' => $json(['filter' => ['malware' => ['acme/lib' => '<2.0'], 'custom' => ['Acme/Tool' => '*']]], $lm),
        'https://repo.example.org/p2/acme/lib.json' => $json([
            'minified' => 'composer/2.0',
            'packages' => ['acme/lib' => $small('acme/lib', ['1.0.0' => [], '1.1.0' => ['provide' => ['psr/log-implementation' => '1.0']]])],
            'security-advisories' => $advisoryList,
            'filter' => ['malware' => [['constraint' => '1.0.0', 'reason' => 'bad', 'source' => 'src'], ['constraint' => '>=1.0', 'package' => 'acme/lib', 'url' => 'https://x']], 'custom' => [['constraint' => '*']]],
        ], $lm),
        'https://repo.example.org/p2/acme/tool.json' => $json(['packages' => [], 'filter' => ['custom' => [['constraint' => '*', 'id' => 'T-1']]]], $lm),
    ],
    'steps' => [
        ['op' => 'new', 'config' => ['url' => 'https://repo.example.org', 'filter' => ['custom' => false]]],
        ['op' => 'loadPackages', 'names' => ['acme/lib' => null, 'unknown/pkg' => null, 'acme/tool' => null], 'acceptable' => ['stable' => 0], 'flags' => []],
        ['op' => 'getPackageNames', 'filter' => null],
        ['op' => 'getPackageNames', 'filter' => 'acme/t*'],
        ['op' => 'hasSecurityAdvisories'],
        ['op' => 'getSecurityAdvisories', 'map' => ['acme/lib' => '==1.0.0.0', 'unknown/pkg' => '*'], 'partial' => true],
        ['op' => 'getSecurityAdvisories', 'map' => ['acme/lib' => '*'], 'partial' => false],
        ['op' => 'hasFilter'],
        ['op' => 'getFilterLists'],
        ['op' => 'new', 'config' => ['url' => 'https://repo.example.org']],
        ['op' => 'getFilterLists'],
        ['op' => 'getFilter', 'map' => ['acme/lib' => '==1.0.0.0', 'acme/tool' => '*', 'unknown/pkg' => '*'], 'lists' => ['malware', 'custom']],
        ['op' => 'getFilter', 'map' => ['acme/lib' => '==1.0.0.0'], 'lists' => ['malware']],
        ['op' => 'getPackages'],
        ['op' => 'new', 'config' => ['url' => 'https://repo.example.org', 'filter' => 'yes']],
    ],
];

$scenarios['lazy-v1-partial'] = [
    'files' => [
        'https://lazy.example.org/packages.json' => $json([
            'providers-lazy-url' => '/p/%package%.json',
            'notify-batch' => 'https://lazy.example.org/notify',
            'packages' => [
                'inline/pkg' => ['1.0.0' => ['name' => 'inline/pkg', 'version' => '1.0.0', 'provide' => ['virtual/thing' => '1.0'], 'description' => 'inline'], '2.0.0' => ['name' => 'inline/pkg', 'version' => '2.0.0']],
                'Mismatched/Key' => ['1.0.0' => ['name' => 'other/name', 'version' => '1.0.0', 'type' => 'metapackage', 'provide' => ['virtual/thing' => '*']]],
            ],
            'mirrors' => [['git-url' => 'https://git.mirror/%package%/%normalizedUrl%.%type%', 'preferred' => false], ['hg-url' => 'https://hg.mirror/%package%']],
        ], $lm),
        'https://lazy.example.org/p/lazy/pkg.json' => $json(['packages' => ['lazy/pkg' => [
            '1.0.0' => ['name' => 'lazy/pkg', 'version' => '1.0.0', 'uid' => 11, 'source' => ['type' => 'git', 'url' => 'https://example.org/lazy.git', 'reference' => 'abc']],
            'dev-master' => ['name' => 'lazy/pkg', 'version' => 'dev-master', 'uid' => 12, 'extra' => ['branch-alias' => ['dev-master' => '1.1.x-dev']], 'version_normalized' => '9999999-dev'],
        ], 'other/pkg' => ['1.0.0' => ['name' => 'other/pkg', 'version' => '1.0.0', 'uid' => 13]]]], $lm),
    ],
    'steps' => [
        ['op' => 'new', 'config' => ['url' => 'https://lazy.example.org']],
        ['op' => 'loadPackages', 'names' => ['inline/pkg' => '^1.0', 'lazy/pkg' => null, 'missing/pkg' => null], 'acceptable' => ['stable' => 0, 'dev' => 20], 'flags' => []],
        ['op' => 'findPackage', 'name' => 'inline/pkg', 'constraint' => '^2.0'],
        ['op' => 'findPackages', 'name' => 'lazy/pkg', 'constraint' => null],
        ['op' => 'getProviders', 'name' => 'virtual/thing'],
        ['op' => 'getPackageNames', 'filter' => null],
        ['op' => 'getPackages'],
        ['op' => 'count'],
        ['op' => 'new', 'config' => ['url' => 'https://lazy.example.org']],
        ['op' => 'loadPackages', 'names' => ['lazy/pkg' => null], 'acceptable' => ['stable' => 0], 'flags' => []],
        ['op' => 'loadPackages', 'names' => ['lazy/pkg' => null], 'acceptable' => ['stable' => 0, 'dev' => 20], 'flags' => [], 'alreadyLoaded' => ['lazy/pkg' => ['1.0.0.0']]],
    ],
];

$include = json_encode(['providers' => ['static/pkg' => ['sha256' => 'PLACEHOLDER']]]);
$provider = json_encode(['packages' => ['static/pkg' => [
    '1.0.0' => ['name' => 'static/pkg', 'version' => '1.0.0', 'uid' => 1, 'dist' => ['type' => 'zip', 'url' => 'https://static.example.org/dist/static-1.0.0.zip', 'reference' => 'r1']],
    '1.1.0' => ['name' => 'static/pkg', 'version' => '1.1.0', 'uid' => 2],
]]]);
$include = str_replace('PLACEHOLDER', hash('sha256', $provider), $include);
$includeHash = hash('sha256', $include);
$scenarios['v1-providers'] = [
    'files' => [
        'https://static.example.org/packages.json' => $json([
            'providers-url' => '/p/%package%$%hash%.json',
            'provider-includes' => ['p/include$%hash%.json' => ['sha256' => $includeHash]],
            'search' => '/search.json?q=%query%',
        ]),
        'https://static.example.org/p/include%24'.$includeHash.'.json' => ['body' => $include],
        'https://static.example.org/p/static/pkg%24'.hash('sha256', $provider).'.json' => ['body' => $provider],
    ],
    'steps' => [
        ['op' => 'new', 'config' => ['url' => 'https://static.example.org', 'options' => ['ssl' => ['verify_peer' => true]]]],
        ['op' => 'getPackageNames', 'filter' => null],
        ['op' => 'loadPackages', 'names' => ['static/pkg' => '^1.0', 'nope/pkg' => null], 'acceptable' => ['stable' => 0], 'flags' => []],
        ['op' => 'findPackage', 'name' => 'static/pkg', 'constraint' => '1.1.0'],
        ['op' => 'search', 'query' => 'stat', 'mode' => 1, 'type' => null],
        ['op' => 'getPackages'],
        ['op' => 'new', 'config' => ['url' => 'https://static.example.org']],
        ['op' => 'findPackages', 'name' => 'static/pkg', 'constraint' => null],
    ],
];

$includeAll = json_encode(['packages' => ['inc/pkg' => ['1.0.0' => ['name' => 'inc/pkg', 'version' => '1.0.0', 'description' => 'from include', 'keywords' => ['included'], 'provide' => ['virtual/x' => '1.0']]]]]);
$scenarios['v1-static-packages'] = [
    'files' => [
        'http://old.example.org/packages.json' => $json([
            'packages' => [
                'a/pkg' => ['1.0.0' => ['name' => 'a/pkg', 'version' => '1.0.0', 'description' => 'first', 'dist' => ['type' => 'zip', 'url' => 'http://old.example.org/dist/a.zip']], '2.0.0' => ['name' => 'A/Pkg', 'version' => '2.0.0', 'abandoned' => 'b/pkg']],
                'b/pkg' => ['1.0.0' => ['name' => 'c/pkg', 'version' => '1.0.0']],
            ],
            'includes' => ['include/all$'.sha1($includeAll).'.json' => ['sha1' => sha1($includeAll)]],
            'notify' => '/notify/%package%',
        ]),
        'http://old.example.org/include/all%24'.sha1($includeAll).'.json' => ['body' => $includeAll],
    ],
    'steps' => [
        ['op' => 'new', 'config' => ['url' => 'old.example.org/packages.json', 'allow_ssl_downgrade' => true]],
        ['op' => 'getPackages'],
        ['op' => 'count'],
        ['op' => 'loadPackages', 'names' => ['a/pkg' => '^1.0', 'inc/pkg' => null], 'acceptable' => ['stable' => 0], 'flags' => []],
        ['op' => 'search', 'query' => 'included', 'mode' => 0, 'type' => null],
        ['op' => 'search', 'query' => 'pkg', 'mode' => 2, 'type' => null],
        ['op' => 'getProviders', 'name' => 'virtual/x'],
        ['op' => 'findPackage', 'name' => 'A/PKG', 'constraint' => '>=2'],
        ['op' => 'getPackageNames', 'filter' => '*/pkg'],
    ],
];

$scenarios['legacy-format-and-errors'] = [
    'files' => [
        'https://legacy.example.org/packages.json' => $json(['foo/bar' => ['name' => 'foo/bar', 'versions' => ['1.0.0' => ['name' => 'foo/bar', 'version' => '1.0.0'], 'bad' => ['name' => 'foo/bar', 'version' => 'not a version']]]]),
        'https://broken.example.org/packages.json' => ['status' => 500],
        'https://advisories.example.org/packages.json' => $json(['metadata-url' => '/p2/%package%.json', 'security-advisories' => ['metadata' => true]]),
    ],
    'steps' => [
        ['op' => 'new', 'config' => ['url' => 'https://legacy.example.org']],
        ['op' => 'getPackages'],
        ['op' => 'new', 'config' => ['url' => 'https://broken.example.org']],
        ['op' => 'getPackageNames', 'filter' => null],
        ['op' => 'new', 'config' => ['url' => 'https://advisories.example.org']],
        ['op' => 'hasSecurityAdvisories'],
        ['op' => 'new', 'config' => ['url' => 'https?://secure.example.org/']],
        ['op' => 'new', 'config' => ['url' => 'https://x.example.org', 'filter' => ['a' => 'no']]],
        ['op' => 'new', 'config' => ['url' => 'https://x.example.org', 'filter' => [0 => false]]],
    ],
];

$fixtures = $root.'/internal/pkg/loader/testdata/oracle/p2';
$golden = [];
foreach ($scenarios as $name => $scenario) {
    $golden[$name] = run_scenario($fixtures, $scenario);
}

write_golden($root.'/internal/repository/composerrepo/testdata/oracle/composerrepo.json.gz', $golden);
