<?php
// Generates internal/policy/testdata/oracle/policy.json.gz: PolicyConfig::fromConfig
// over thousands of generated config.policy / config.audit shapes and
// environment combinations, from the real Composer\Policy classes.
//
// Each case records the values Config::get('policy') and Config::get('audit')
// returned (the inputs PolicyConfig reads), the environment, and either the
// serialised PolicyConfig (with its with* variants) or the exception.
// PHP arrays are encoded as {"a": [[key, value], ...]}.
//
// Run: php tools/oracle/config/policy.php
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Composer\Config;
use Composer\Policy\AdvisoriesPolicyConfig;
use Composer\Policy\CustomListPolicyConfig;
use Composer\Policy\IgnoreUnreachable;
use Composer\Policy\ListPolicyConfig;
use Composer\Policy\MalwarePolicyConfig;
use Composer\Policy\PolicyConfig;
use Composer\Util\Platform;

error_reporting(E_ALL & ~E_WARNING & ~E_NOTICE);
mt_srand(20261005);

function pick(array $a) { return $a[mt_rand(0, count($a) - 1)]; }
function chance(int $n): bool { return mt_rand(0, $n - 1) === 0; }

function encode($v)
{
    if (is_array($v)) {
        $pairs = [];
        foreach ($v as $k => $item) {
            $pairs[] = [$k, encode($item)];
        }

        return ['a' => $pairs];
    }

    return $v;
}

$constraints = ['^1.0', '1.0', '>=2.0 <3.0', '*', 'dev-master', '~1.2 || ^2.0', '1.0.*', '^1.0', '^2.0', '>=1'];
$badConstraints = ['foo bar baz', '>>1'];
$packages = ['vendor/foo', 'vendor/bar', 'acme/*', 'acme/abandoned', 'Vendor/Mixed.Case', 'foo', 'a/b-c'];
$ids = ['CVE-2024-1234', 'GHSA-xxxx-yyyy', 'PKSA-1', 'CVE-1'];
$severities = ['low', 'medium', 'high', 'critical'];
$reasons = ['reason', 'other reason', 'fork ready', 'a; b', 'b', ''];

function ruleObject(): array
{
    global $constraints, $reasons;
    $r = [];
    global $badConstraints;
    if (mt_rand(0, 1)) { $r['constraint'] = chance(30) ? pick($badConstraints) : pick($constraints); }
    if (mt_rand(0, 1)) { $r['reason'] = mt_rand(0, 4) ? pick($reasons) : null; }
    if (chance(3)) { $r['on-block'] = (bool) mt_rand(0, 1); }
    if (chance(3)) { $r['on-audit'] = (bool) mt_rand(0, 1); }

    return $r;
}

function ignoreMap(array $names, bool $packages): array
{
    global $reasons;
    $m = [];
    $n = mt_rand(0, 4);
    for ($i = 0; $i < $n; $i++) {
        $name = pick($names);
        switch (mt_rand(0, $packages ? 6 : 4)) {
            case 0: $m[$name] = null; break;
            case 1: $m[$name] = pick($reasons); break;
            case 2: $m[] = $name; break;
            case 3: $m[$name] = $packages ? ruleObject() : ['reason' => pick($reasons), 'on-block' => (bool) mt_rand(0, 1), 'on-audit' => (bool) mt_rand(0, 1)]; break;
            case 4: if (chance(12)) { $m[$name] = pick([true, 42, false]); } else { $m[] = $name; } break;
            case 5: $m[$name] = [ruleObject(), ruleObject()]; break;
            case 6: if (chance(12)) { $m[] = pick([null, true, 42, ['package' => 'x']]); } else { $m[$name] = [ruleObject()]; } break;
        }
    }

    return $m;
}

function listConfig(string $name)
{
    global $packages, $ids, $severities;
    if (chance(6)) { return pick([true, false]); }
    $c = [];
    if (mt_rand(0, 1)) { $c['block'] = (bool) mt_rand(0, 1); }
    if (mt_rand(0, 1)) { $c['audit'] = pick(['ignore', 'report', 'fail']); }
    if (mt_rand(0, 1)) { $c['ignore'] = ignoreMap($packages, true); }
    if ($name === 'advisories') {
        if (mt_rand(0, 1)) { $c['ignore-id'] = ignoreMap($ids, false); }
        if (mt_rand(0, 1)) { $c['ignore-severity'] = ignoreMap($severities, false); }
    }
    if ($name === 'malware') {
        if (mt_rand(0, 1)) { $c['block-scope'] = pick(['all', 'update', 'install']); }
        if (mt_rand(0, 1)) { $c['ignore-source'] = array_slice(['untrusted', 'other'], 0, mt_rand(0, 2)); }
    }
    if (!in_array($name, ['advisories', 'malware', 'abandoned'], true) && mt_rand(0, 1)) {
        $c['sources'] = [];
        $n = mt_rand(0, 2);
        for ($i = 0; $i < $n; $i++) {
            $c['sources'][] = mt_rand(0, 5) ? pick([
                ['type' => 'url', 'url' => 'https://example.com/list.json'],
                ['type' => 'url', 'url' => 'https://example.org/'],
                'not-an-array',
            ]) : pick([
                ['type' => 'url', 'url' => 'http://example.com/list.json'],
                ['type' => 'url'],
                ['type' => 'git', 'url' => 'https://x'],
                ['url' => 'https://x'],
                'not-an-array',
            ]);
        }
    }

    return $c;
}

function legacyIgnore(array $names, bool $withApply): array
{
    global $reasons;
    $m = [];
    $n = mt_rand(0, 4);
    for ($i = 0; $i < $n; $i++) {
        $name = pick($names);
        switch (mt_rand(0, 4)) {
            case 0: $m[] = $name; break;
            case 1: $m[$name] = pick($reasons); break;
            case 2:
                $v = [];
                if (mt_rand(0, 2)) { $v['apply'] = pick(['audit', 'block', 'all', 'all', 'audit', chance(5) ? 'invalid' : 'block']); }
                if (mt_rand(0, 1)) { $v['reason'] = pick($reasons); }
                $m[$name] = $v;
                break;
            case 3: $m[$name] = null; break;
            case 4: $m[$name] = ['apply' => pick(['audit', 'block'])]; break;
        }
    }

    return $m;
}

function policyRaw()
{
    if (chance(8)) { return pick([true, false]); }
    $p = [];
    foreach (['advisories', 'malware', 'abandoned'] as $name) {
        if (mt_rand(0, 2) === 0) { $p[$name] = listConfig($name); }
    }
    $n = mt_rand(0, 2);
    for ($i = 0; $i < $n; $i++) {
        $name = chance(20) ? pick(['ignore-foo', 'package', 'security', 'licences', 'ignoremalware']) : pick(['company-policy', 'internal', 'x']);
        $p[$name] = listConfig($name);
    }
    if (chance(3)) {
        $p['ignore-unreachable'] = pick([true, false, ['audit'], ['update', 'install'], [], ['bogus']]);
    }

    return $p;
}

function auditRaw(): array
{
    global $packages, $ids, $severities;
    $a = [];
    if (mt_rand(0, 1)) { $a['ignore'] = legacyIgnore(array_merge($packages, $ids), true); }
    if (chance(3)) { $a['ignore-severity'] = legacyIgnore($severities, true); }
    if (chance(3)) { $a['ignore-abandoned'] = legacyIgnore($packages, true); }
    if (chance(3)) { $a['block-insecure'] = (bool) mt_rand(0, 1); }
    if (chance(3)) { $a['block-abandoned'] = (bool) mt_rand(0, 1); }
    if (chance(3)) { $a['abandoned'] = pick(['ignore', 'report', 'fail']); }
    if (chance(4)) { $a['ignore-unreachable'] = (bool) mt_rand(0, 1); }

    return $a;
}

$envVars = [
    'COMPOSER_POLICY' => ['0', '1', ''],
    'COMPOSER_POLICY_ADVISORIES_BLOCK' => ['0', '1', 'true', 'off', '', '1', '0', 'bogus'],
    'COMPOSER_POLICY_MALWARE_BLOCK' => ['0', '1', 'on', 'false'],
    'COMPOSER_POLICY_ABANDONED_BLOCK' => ['0', '1', ''],
    'COMPOSER_SECURITY_BLOCKING_ABANDONED' => ['0', '1', 'bogus'],
    'COMPOSER_AUDIT_ABANDONED' => ['report', 'fail', 'ignore', 'bogus'],
];

function serRules(array $m): array
{
    $out = [];
    foreach ($m as $k => $rules) {
        $out[] = [(string) $k, array_map(static function ($r) {
            return [$r->packageName, $r->constraint->getPrettyString(), (string) $r->constraint, $r->reason, $r->onBlock, $r->onAudit, $r->packageNameRegex];
        }, $rules)];
    }

    return $out;
}

function serSimple(array $m): array
{
    $out = [];
    foreach ($m as $k => $r) {
        $out[] = [(string) $k, [$r->id ?? $r->severity, $r->reason, $r->onBlock, $r->onAudit]];
    }

    return $out;
}

function serReasons(array $m): array
{
    $out = [];
    foreach ($m as $k => $v) {
        $out[] = [(string) $k, $v];
    }

    return $out;
}

function serList(ListPolicyConfig $l): array
{
    $out = [
        'name' => $l->name,
        'block' => $l->block,
        'audit' => $l->audit,
        'ignore' => serRules($l->ignore),
        'shouldBlock' => [$l->shouldBlock('update'), $l->shouldBlock('install'), $l->shouldBlock('all')],
        'flatAudit' => serReasons($l->getFlatIgnoreForOperation('audit')),
        'flatBlock' => serReasons($l->getFlatIgnoreForOperation('block')),
        'ignoreAudit' => serRules($l->getIgnoreForOperation('audit')),
        'ignoreBlock' => serRules($l->getIgnoreForOperation('block')),
        'class' => (new \ReflectionClass($l))->getShortName(),
    ];
    if ($l instanceof AdvisoriesPolicyConfig) {
        $out['ignoreId'] = serSimple($l->ignoreId);
        $out['ignoreSeverity'] = serSimple($l->ignoreSeverity);
        $out['ignoreIdAudit'] = serReasons($l->getIgnoreIdForOperation('audit'));
        $out['ignoreIdBlock'] = serReasons($l->getIgnoreIdForOperation('block'));
        $out['ignoreListAudit'] = serReasons($l->getIgnoreListForOperation('audit'));
        $out['ignoreListBlock'] = serReasons($l->getIgnoreListForOperation('block'));
        $out['ignoreSeverityAudit'] = serReasons($l->getIgnoreSeverityForOperation('audit'));
        $out['ignoreSeverityBlock'] = serReasons($l->getIgnoreSeverityForOperation('block'));
    } elseif ($l instanceof MalwarePolicyConfig) {
        $out['blockScope'] = $l->blockScope;
        $out['ignoreSource'] = array_values($l->ignoreSource);
    } elseif ($l instanceof CustomListPolicyConfig) {
        $out['sources'] = array_map(static function ($s) { return [$s->listName, $s->url]; }, $l->sources);
    }

    return $out;
}

function serPolicy(PolicyConfig $p): array
{
    $custom = [];
    foreach ($p->customLists as $name => $l) {
        $custom[] = [(string) $name, serList($l)];
    }

    return [
        'enabled' => $p->enabled,
        'advisories' => serList($p->advisories),
        'malware' => serList($p->malware),
        'abandoned' => serList($p->abandoned),
        'custom' => $custom,
        'ignoreUnreachable' => [$p->ignoreUnreachable->audit, $p->ignoreUnreachable->install, $p->ignoreUnreachable->update],
        'forBlockScope' => [$p->ignoreUnreachable->forBlockScope('install'), $p->ignoreUnreachable->forBlockScope('update')],
        'allLists' => array_map('strval', array_keys($p->getAllLists())),
        'activeAudit' => array_map('strval', $p->getActiveAuditFilterListNames()),
        'activeBlockUpdate' => array_map('strval', $p->getActiveBlockFilterListNames('update')),
        'activeBlockInstall' => array_map('strval', $p->getActiveBlockFilterListNames('install')),
        'withSources' => array_map('strval', array_keys($p->getCustomListsWithSources())),
    ];
}

$cases = [];
while (count($cases) < 1200) {
    $env = [];
    foreach ($envVars as $name => $values) {
        if (chance(6)) {
            $env[$name] = pick($values);
            Platform::putEnv($name, $env[$name]);
        } else {
            Platform::clearEnv($name);
        }
    }

    $config = new Config(true);
    $layers = mt_rand(1, 2);
    for ($i = 0; $i < $layers; $i++) {
        $layer = [];
        if (mt_rand(0, 3)) { $layer['policy'] = policyRaw(); }
        if (mt_rand(0, 2) === 0) { $layer['audit'] = auditRaw(); }
        $config->merge(['config' => $layer]);
    }

    try {
        $policy = $config->get('policy');
        $audit = $config->get('audit');
    } catch (\Throwable $e) {
        continue;
    }

    $case = ['policy' => encode($policy), 'audit' => encode($audit), 'env' => (object) $env];
    try {
        $p = PolicyConfig::fromConfig($config);
        $out = serPolicy($p);
        $out['noBlocking'] = serPolicy($p->withBlockingDisabled());
        $out['withAuditReport'] = $p->withAudit('report')->abandoned->audit;
        $out['withAuditNull'] = $p->withAudit(null)->abandoned->audit;
        $out['withSeverity'] = serSimple($p->withIgnoreSeverity(['low', 'critical'])->advisories->ignoreSeverity);
        $u = $p->withIgnoreUnreachable('audit')->ignoreUnreachable;
        $out['withUnreachable'] = [$u->audit, $u->install, $u->update];
        $case['result'] = $out;
    } catch (\TypeError $e) {
        continue;
    } catch (\Exception $e) {
        $case['error'] = [(new \ReflectionClass($e))->getShortName(), $e->getMessage()];
    }
    $cases[] = $case;
}

foreach (array_keys($envVars) as $name) {
    Platform::clearEnv($name);
}

$out = dirname(__DIR__, 3).'/internal/policy/testdata/oracle/policy.json.gz';
file_put_contents($out, gzencode(json_encode($cases, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE)."\n", 9));
$errors = count(array_filter($cases, static function ($c) { return isset($c['error']); }));
fwrite(STDERR, count($cases)." cases ($errors errors) written to $out\n");
