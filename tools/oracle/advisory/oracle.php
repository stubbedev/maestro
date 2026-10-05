<?php
// Generates internal/advisory/testdata/oracle/audit.json by running
// Composer 2.10.3's Auditor (with its PolicyConfig, RepositorySet,
// PackageRepository and FilterListProviderSet) over every combination of
// a set of package lists, policy configs, output formats and warningOnly,
// recording the exit code and the BufferIO output (or the exception).
//
// The "repo" entry is the PackageRepository config (security-advisories,
// filter) every case audits against; the other entries are the cases,
// carrying their inputs so the Go side builds the same objects:
//   packages  [name, version, prettyVersion, abandoned, homepage]
//   policy    the raw config.policy value
//
// Run: php tools/oracle/advisory/oracle.php
require __DIR__.'/../pkg/common.php';

use Composer\Advisory\Auditor;
use Composer\Config;
use Composer\FilterList\FilterListProvider\FilterListProviderSet;
use Composer\IO\BufferIO;
use Composer\IO\NullIO;
use Composer\Package\CompletePackage;
use Composer\Policy\PolicyConfig;
use Composer\Repository\PackageRepository;
use Composer\Repository\RepositorySet;
use Composer\Util\HttpDownloader;

function adv(string $id, string $name, string $affected, array $extra = []): array
{
    return array_merge([
        'advisoryId' => $id,
        'packageName' => $name,
        'title' => 'Advisory '.$id,
        'link' => 'https://example.org/advisories/'.$id,
        'cve' => 'CVE-2024-'.substr(md5($id), 0, 4),
        'affectedVersions' => $affected,
        'sources' => [['name' => 'GitHub', 'remoteId' => 'GHSA-'.$id]],
        'reportedAt' => '2024-03-01 10:20:30',
        'severity' => 'high',
    ], $extra);
}

$repo = [
    'package' => [],
    'security-advisories' => [
        'acme/web' => [
            adv('PKSA-aaaa-bbbb-cccc', 'acme/web', '>=1.0,<1.5'),
            adv('PKSA-dddd-eeee-ffff', 'acme/web', '>=1.2,<2.0', ['severity' => 'medium', 'cve' => null]),
            adv('GHSA-local-1', 'acme/web', '<1.4.1', ['severity' => null, 'link' => null, 'title' => 'Title with <tags> & "quotes"']),
        ],
        'acme/db' => [
            adv('PKSA-1111-2222-3333', 'acme/db', '^2.0', ['severity' => 'low', 'reportedAt' => '2023-12-31T23:59:59+02:00']),
        ],
        'zeta/lib' => [
            adv('PKSA-zzzz-yyyy-xxxx', 'zeta/lib', '*', ['severity' => 'critical', 'title' => 'Ünïcode títle that is quite a bit longer than eighty characters so that the table wraps it somewhere sensible']),
        ],
        '10/numeric' => [
            adv('PKSA-num1-num2-num3', '10/numeric', '*'),
        ],
    ],
    'filter' => [
        'malware' => [
            ['package' => 'evil/pkg', 'constraint' => '*', 'reason' => 'Malicious code', 'url' => 'https://example.org/evil', 'id' => 'MAL-1', 'source' => 'aikido'],
            ['package' => 'acme/db', 'constraint' => '<2.1', 'reason' => 'Typosquat', 'id' => 'MAL-2', 'source' => 'untrusted'],
        ],
        'company' => [
            ['package' => 'acme/web', 'constraint' => '>=1.0', 'reason' => 'Forbidden by policy', 'id' => 'C-1'],
            ['package' => 'zeta/lib', 'constraint' => '*'],
        ],
    ],
];

$packageSets = [
    'clean' => [
        ['acme/web', '2.1.0.0', '2.1.0', false, null],
        ['other/thing', '1.0.0.0', '1.0.0', false, null],
    ],
    'vulnerable' => [
        ['acme/web', '1.3.0.0', '1.3.0', false, 'https://acme.example.org'],
        ['acme/db', '2.0.5.0', '2.0.5', false, null],
        ['zeta/lib', '0.1.0.0', '0.1.0', false, null],
        ['10/numeric', '1.0.0.0', '1.0.0', false, null],
    ],
    'abandoned' => [
        ['acme/web', '2.1.0.0', '2.1.0', 'acme/web2', 'https://acme.example.org'],
        ['old/thing', '1.0.0.0', '1.0.0', true, null],
        ['zeta/lib', '0.1.0.0', '0.1.0', true, null],
    ],
    'filtered' => [
        ['evil/pkg', '1.0.0.0', '1.0.0', false, null],
        ['acme/db', '2.0.5.0', '2.0.5', false, null],
        ['acme/web', '1.0.0.0', '1.0.0', 'x/y', null],
    ],
];

$policies = [
    'default' => [],
    'ignores' => [
        'advisories' => [
            'ignore-id' => ['PKSA-aaaa-bbbb-cccc' => 'Not exploitable', 'GHSA-GHSA-local-1' => null],
            'ignore-severity' => ['low' => null],
        ],
        'abandoned' => ['audit' => 'report', 'ignore' => ['old/*' => 'We know']],
    ],
    'ignore-packages' => [
        'advisories' => ['ignore' => ['zeta/lib' => 'vendored', '10/*' => null]],
        'abandoned' => ['audit' => 'ignore'],
    ],
    'cve-ignore' => [
        'advisories' => ['ignore-id' => [substr('CVE-2024-'.md5('PKSA-1111-2222-3333'), 0, 13) => 'cve']],
    ],
    'lists' => [
        'malware' => ['audit' => 'fail', 'ignore-source' => ['untrusted']],
        'company' => ['audit' => 'report'],
        'abandoned' => ['audit' => 'fail'],
    ],
    'lists-fail' => [
        'malware' => ['audit' => 'report'],
        'company' => ['audit' => 'fail', 'ignore' => ['zeta/lib']],
    ],
];

$cases = [];
foreach ($packageSets as $setName => $set) {
    foreach ($policies as $policyName => $policy) {
        foreach (Auditor::FORMATS as $format) {
            foreach ([true, false] as $warningOnly) {
                $cases[$setName.'/'.$policyName.'/'.$format.'/'.($warningOnly ? 'warn' : 'error')] = [
                    'packages' => $set,
                    'policy' => $policy,
                    'format' => $format,
                    'warningOnly' => $warningOnly,
                ];
            }
        }
    }
}

$repo = json_decode(json_encode($repo, JSON_FLAGS), true);
$out = ['repo' => $repo];
foreach ($cases as $name => $case) {
    // JSON round trip, so both sides read the same values
    $case = json_decode(json_encode($case, JSON_FLAGS), true);

    $packages = [];
    foreach ($case['packages'] as [$pkgName, $version, $pretty, $abandoned, $homepage]) {
        $p = new CompletePackage($pkgName, $version, $pretty);
        $p->setAbandoned($abandoned);
        if ($homepage !== null) {
            $p->setHomepage($homepage);
        }
        $packages[] = $p;
    }

    $config = new Config(false);
    if ($case['policy'] !== []) {
        $config->merge(['config' => ['policy' => $case['policy']]]);
    }
    $policyConfig = PolicyConfig::fromConfig($config);

    $repository = new PackageRepository($repo);
    $repoSet = new RepositorySet();
    $repoSet->addRepository($repository);
    $providerSet = FilterListProviderSet::create($policyConfig, [$repository], new HttpDownloader(new NullIO(), $config));

    $io = new BufferIO();
    $result = attempt(static function () use ($io, $repoSet, $policyConfig, $packages, $case, $providerSet) {
        return (new Auditor())->audit($io, $repoSet, $policyConfig, $packages, $case['format'], $case['warningOnly'], $providerSet);
    });

    $case['result'] = $result;
    $case['output'] = $io->getOutput();
    $out[$name] = $case;
}

write_golden($root.'/internal/advisory/testdata/oracle/audit.json', $out);
echo count($out) - 1, " cases\n";
