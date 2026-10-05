<?php
// Generates internal/config/testdata/oracle/config.json: Composer\Config
// over thousands of generated merge sequences and environments, recording
// get() for every key (with and without RELATIVE_PATHS), getRepositories(),
// raw(), all(), getSourceOfValue() and has(), or the exception thrown.
//
// Values are recorded as var_export() strings; merge inputs as JSON, which
// both sides decode with json_decode($json, true). PHP warnings become
// ErrorExceptions, as Composer's ErrorHandler makes them.
//
// Run: php -d memory_limit=-1 tools/oracle/config/config.php
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Composer\Config;
use Composer\Util\Platform;

set_error_handler(static function (int $level, string $message, string $file, int $line): bool {
    if ($level === E_DEPRECATED || $level === E_USER_DEPRECATED || 0 === (error_reporting() & $level)) {
        return true;
    }
    throw new \ErrorException($message, 0, $level, $file, $line);
});

mt_srand(20261005);

const ENV_VARS = [
    'COMPOSER_VENDOR_DIR', 'COMPOSER_BIN_DIR', 'COMPOSER_PROCESS_TIMEOUT', 'COMPOSER_DATA_DIR', 'COMPOSER_CACHE_DIR',
    'COMPOSER_CACHE_FILES_DIR', 'COMPOSER_CACHE_REPO_DIR', 'COMPOSER_CACHE_VCS_DIR', 'COMPOSER_CAFILE', 'COMPOSER_CAPATH',
    'COMPOSER_CACHE_READ_ONLY', 'COMPOSER_HTACCESS_PROTECT', 'COMPOSER_BIN_COMPAT', 'COMPOSER_DISCARD_CHANGES',
    'COMPOSER_AUDIT_ABANDONED', 'COMPOSER_SECURITY_BLOCKING_ABANDONED', 'COMPOSER_POLICY', 'ORACLE_VAR',
];

// A value for a key may only reference ({$ref}) keys of a lower rank, so
// that no generated configuration recurses forever.
const RANK = [
    'a' => 0, 'home' => 1, 'vendor-dir' => 2, 'data-dir' => 2, 'cache-dir' => 2, 'bin-dir' => 3,
    'cache-files-dir' => 3, 'cache-repo-dir' => 3, 'cache-vcs-dir' => 3, 'c' => 3,
];

function pick(array $a) { return $a[mt_rand(0, count($a) - 1)]; }
function chance(int $percent): bool { return mt_rand(1, 100) <= $percent; }
function rank(string $key): int { return RANK[$key] ?? 4; }

function refTo(string $key): string
{
    $lower = array_keys(array_filter(RANK, static function ($r) use ($key) { return $r < rank($key); }));
    if ($lower === []) {
        return '';
    }

    return '{$'.pick($lower).'}';
}

function pathString(string $key): string
{
    $ref = refTo($key);
    $choices = [
        'vendor', 'vendor/', 'libs\\', '/abs/path/', '/abs//', 'rel/./dir', '~/foo', '~/', '~', '~user/x', '$HOME/x', '$HOME',
        '$ORACLE_VAR/y', '$UNSET_ORACLE/y', '%ORACLE_VAR%/z', '%HOME%', 's3://bucket/', 'phar://x.phar/y', 'C:\\x\\',
        'c:/y', '\\\\server\\share', '', '0', '/', '//', 'x{$unknown}y', '{$a}/{$b}', 'pre{$}post', '{$ }',
    ];
    if ($ref !== '') {
        $choices[] = $ref;
        $choices[] = $ref.'/sub';
        $choices[] = 'x'.$ref.'/';
        $choices[] = $ref.$ref;
        $choices[] = '~/'.$ref;
    }

    return pick($choices);
}

function scalar(string $key)
{
    switch (mt_rand(0, 12)) {
        case 0: return pick([true, false]);
        case 1: return null;
        case 2: return pick([0, 1, -5, 100, 300, 15552000, PHP_INT_MAX]);
        case 3: return pick([1.5, 0.0, -2.25, 1e20, 300.0]);
        case 4: return pick(['false', 'true', '0', '1', '', 'off', 'yes']);
        case 5: return pick(['stash', 'auto', 'full', 'proxy', 'symlink', 'bogus', 'AUTO', ' auto']);
        case 6: return pick(['300', '12abc', '1.5', ' 10 ', '-3', '0x1A', '1e3', 'abc']);
        case 7: return pick(['300MiB', ' 10 MiB ', '1g', '2KB', '1.5m', '1.2.3k', 'k', '5 gb', '7Mb', '9 kib', '', '12', '0.5G', 'xMB']);
        case 8: return pick(['https', 'ssh', 'git', 'http', 'dist', 'source', 'report', 'ignore', 'fail', 'prompt', 'php-only']);
        default: return pathString($key);
    }
}

function arrayValue(string $key)
{
    switch (mt_rand(0, 9)) {
        case 0: return [];
        case 1: return pick([['https', 'git'], ['git', 'https'], ['http', 'ssh'], ['https'], ['ssh', 'git', 'https'], ['git']]);
        case 2: return pick([['foo/*' => 'source'], ['*' => 'dist'], ['*' => 'source', 'bar/*' => 'dist'], ['a/b' => 'auto']]);
        case 3: return pick([['github.com' => 'tok'], ['example.org' => ['username' => 'u', 'password' => 'p']], ['x.org' => 'y', 'github.com' => 'other']]);
        case 4: return pick([['some/plugin' => true], ['other/*' => false, 'some/plugin' => false], ['a/b' => true, 'c/d' => true]]);
        case 5: return pick([['github.com', 'example.org'], ['gitlab.com', 'gitlab.example.com', 'gitlab.com'], ['codeberg.org']]);
        case 6: return [mt_rand(0, 5) => 'x', 'k' => [1, 2]];
        case 7: return pick([['A', 'B'], ['A', 'C'], [1, 2], [null], [[]]]);
        default: return [scalar($key), scalar($key)];
    }
}

function auditValue()
{
    $v = [];
    if (chance(60)) {
        $v['ignore'] = chance(85) ? pick([[], ['CVE-1'], ['CVE-1' => 'reason'], ['GHSA-x', 'acme/pkg'], ['a' => ['apply' => 'audit']]]) : pick(['str', 5, null]);
    }
    if (chance(40)) {
        $v['abandoned'] = pick(['ignore', 'report', 'fail', 'bogus']);
    }
    if (chance(20)) {
        $v['block-abandoned'] = pick([true, false]);
    }
    if (chance(10)) {
        $v['ignore-unreachable'] = true;
    }

    return chance(95) ? $v : pick(['str', 5, true, null]);
}

function policyList()
{
    switch (mt_rand(0, 6)) {
        case 0: return true;
        case 1: return false;
        case 2: return [];
        case 3: return pick(['str', 5, null]);
        default:
            $v = [];
            foreach (['block' => [true, false], 'audit' => ['report', 'fail', 'ignore'], 'block-scope' => ['update', 'all']] as $k => $vals) {
                if (chance(40)) {
                    $v[$k] = pick($vals);
                }
            }
            foreach (['ignore', 'ignore-id', 'ignore-severity', 'ignore-source'] as $k) {
                if (chance(30)) {
                    $v[$k] = chance(85) ? pick([[], ['vendor/a'], ['vendor/b', 'vendor/c'], ['vendor/d' => 'why'], ['low']]) : 'str';
                }
            }

            return $v;
    }
}

function policyValue()
{
    switch (mt_rand(0, 5)) {
        case 0: return true;
        case 1: return false;
        case 2: return pick(['str', 5, null]);
        default:
            $v = [];
            foreach (['advisories', 'malware', 'abandoned', 'custom-list', 'ignore-unreachable', 0] as $name) {
                if (chance(35)) {
                    $v[$name] = $name === 'ignore-unreachable' ? pick([true, false, ['audit']]) : policyList();
                }
            }

            return $v;
    }
}

const CONFIG_KEYS = [
    'process-timeout', 'use-include-path', 'allow-plugins', 'use-parent-dir', 'preferred-install', 'audit', 'policy',
    'notify-on-install', 'github-protocols', 'gitlab-protocol', 'vendor-dir', 'bin-dir', 'cache-dir', 'data-dir',
    'cache-files-dir', 'cache-repo-dir', 'cache-vcs-dir', 'cache-ttl', 'cache-files-ttl', 'cache-files-maxsize',
    'cache-read-only', 'bin-compat', 'discard-changes', 'autoloader-suffix', 'sort-packages', 'optimize-autoloader',
    'classmap-authoritative', 'apcu-autoloader', 'prepend-autoloader', 'update-with-minimal-changes', 'github-domains',
    'bitbucket-expose-hostname', 'disable-tls', 'secure-http', 'secure-svn-domains', 'cafile', 'capath',
    'github-expose-hostname', 'gitlab-domains', 'store-auths', 'platform', 'archive-format', 'archive-dir',
    'htaccess-protect', 'use-github-api', 'lock', 'platform-check', 'bitbucket-oauth', 'github-oauth', 'gitlab-oauth',
    'gitlab-token', 'http-basic', 'bearer', 'custom-headers', 'bump-after-update', 'allow-missing-requirements',
    'client-certificate', 'forgejo-domains', 'forgejo-token', 'source-fallback',
];
const EXTRA_KEYS = ['home', 'a', 'c', 'foo', 'nonexistent', 'UPPER', ''];
const MAP_KEYS = ['allow-plugins', 'bitbucket-oauth', 'github-oauth', 'gitlab-oauth', 'gitlab-token', 'http-basic', 'bearer', 'custom-headers', 'client-certificate', 'forgejo-token', 'platform', 'preferred-install'];

function configValue(string $key)
{
    if ($key === 'audit') {
        return auditValue();
    }
    if ($key === 'policy') {
        return policyValue();
    }
    if (in_array($key, MAP_KEYS, true) || in_array($key, ['github-protocols', 'github-domains', 'gitlab-domains', 'secure-svn-domains', 'forgejo-domains'], true)) {
        return chance(80) ? arrayValue($key) : scalar($key);
    }

    return chance(88) ? scalar($key) : arrayValue($key);
}

function repoEntry()
{
    switch (mt_rand(0, 9)) {
        case 0: return false;
        case 1: return [pick(['packagist.org', 'packagist', 'example.com', 'foo', '0']) => false];
        case 2: return ['type' => 'composer', 'url' => pick(['https://repo.packagist.org', 'http://packagist.org', 'https://packagist.org/', 'https://repo.packagist.org.evil.com', 'https://example.com', 'HTTPS://PACKAGIST.ORG'])];
        case 3: return ['type' => pick(['vcs', 'git', 'path', 'package']), 'url' => pick(['git://github.com/composer/composer.git', '../path', 'https://x.org/r.git'])];
        case 4: return ['type' => 'composer', 'url' => pick([5, null, ['x']])];
        case 5: return ['type' => 'composer'];
        case 6: return pick(['str', 5, null, true]);
        case 7: return ['name' => 'named', 'type' => 'vcs', 'url' => 'https://x.org/n.git'];
        default: return ['type' => 'composer', 'url' => 'https://repo'.mt_rand(1, 4).'.example.org'];
    }
}

function repositories()
{
    $repos = [];
    $n = mt_rand(0, 4);
    $list = chance(50);
    for ($i = 0; $i < $n; $i++) {
        if ($list) {
            $repos[] = repoEntry();
        } else {
            $repos[pick(['packagist.org', 'packagist', 'example.com', 'foo', 'bar', 0, 1, 7, '3'])] = repoEntry();
        }
    }

    return $repos;
}

function mergeData(): array
{
    $data = [];
    if (chance(85)) {
        $config = [];
        $n = mt_rand(0, 6);
        for ($i = 0; $i < $n; $i++) {
            $key = chance(85) ? pick(CONFIG_KEYS) : pick(['home', 'home', 'a', 'c', 'foo', 'UPPER', 0, '1']);
            $config[$key] = configValue((string) $key);
        }
        if (chance(15)) {
            $config['home'] = pick(['/home/oracle/.composer', '~/.composer/', '$HOME/c', '{$a}/h', '/x/']);
        }
        $data['config'] = chance(95) ? $config : pick(['str', null, []]);
    }
    if (chance(35)) {
        $data['repositories'] = chance(95) ? repositories() : pick(['str', null, false]);
    }

    return $data;
}

function envValue(string $name)
{
    switch ($name) {
        case 'COMPOSER_PROCESS_TIMEOUT': return pick(['0', '10', '-1', 'abc', '1.9', '']);
        case 'COMPOSER_CACHE_READ_ONLY':
        case 'COMPOSER_HTACCESS_PROTECT': return pick(['0', '1', 'false', 'true', '', 'no']);
        case 'COMPOSER_BIN_COMPAT': return pick(['auto', 'full', 'proxy', 'symlink', 'bogus', '', '0']);
        case 'COMPOSER_DISCARD_CHANGES': return pick(['stash', 'true', 'false', '1', '0', 'yes', '']);
        case 'COMPOSER_AUDIT_ABANDONED': return pick(['ignore', 'report', 'fail', 'bogus', '']);
        case 'COMPOSER_SECURITY_BLOCKING_ABANDONED':
        case 'COMPOSER_POLICY': return pick(['0', '1', 'true', 'false', 'on', 'off', '', 'maybe']);
        case 'ORACLE_VAR': return pick(['/oracle', 'rel', '']);
        default:
            $key = strtolower(strtr(substr($name, 9), '_', '-'));

            return pathString($key);
    }
}

function result(callable $fn): array
{
    try {
        return ['v' => var_export($fn(), true)];
    } catch (\Throwable $e) {
        // PHP appends where a user function was called from to TypeErrors
        return ['e' => [get_class($e), preg_replace('{, called in .* on line \d+$}', '', $e->getMessage())]];
    }
}

$cases = [];
for ($n = 0; $n < 1000; $n++) {
    foreach (ENV_VARS as $var) {
        Platform::clearEnv($var);
    }
    Platform::putEnv('HOME', '/home/oracle');
    $env = [];
    foreach (ENV_VARS as $var) {
        if (chance(12)) {
            $env[$var] = envValue($var);
            Platform::putEnv($var, $env[$var]);
        }
    }

    $useEnv = chance(70);
    $baseDir = pick([null, '', '/foo/bar', '/foo/bar/', 'rel/dir', 'C:/x']);
    $config = new Config($useEnv, $baseDir);
    $ops = [];
    if (chance(90)) {
        // most real configurations know their home
        $data = json_encode(['config' => ['home' => pick(['/home/oracle/.composer', '/h/', '~/.c', '$HOME/c'])]]);
        $ops[] = ['op' => 'merge', 'data' => $data, 'source' => 'default', 'r' => result(static function () use ($config, $data) { $config->merge(json_decode($data, true), 'default'); return null; })];
    }
    $merges = mt_rand(0, 4);
    for ($i = 0; $i < $merges; $i++) {
        $data = json_encode(mergeData(), JSON_PRESERVE_ZERO_FRACTION);
        $source = pick(['unknown', 'default', 'command', '/path/composer.json', 'COMPOSER_AUTH']);
        $ops[] = ['op' => 'merge', 'data' => $data, 'source' => $source, 'r' => result(static function () use ($config, $data, $source) { $config->merge(json_decode($data, true), $source); return null; })];
    }
    foreach (array_merge(CONFIG_KEYS, EXTRA_KEYS) as $key) {
        $flagSet = in_array($key, ['vendor-dir', 'bin-dir', 'data-dir', 'cache-dir', 'cache-files-dir', 'cache-repo-dir', 'cache-vcs-dir', 'c', 'home'], true) ? [0, Config::RELATIVE_PATHS] : [0];
        foreach ($flagSet as $flags) {
            $ops[] = ['op' => 'get', 'key' => $key, 'flags' => $flags, 'r' => result(static function () use ($config, $key, $flags) { return $config->get($key, $flags); })];
        }
        if (chance(10)) {
            $ops[] = ['op' => 'source', 'key' => $key, 'r' => result(static function () use ($config, $key) { return $config->getSourceOfValue($key); })];
        }
        if (chance(10)) {
            $ops[] = ['op' => 'has', 'key' => $key, 'r' => result(static function () use ($config, $key) { return $config->has($key); })];
        }
    }
    foreach (['repositories.packagist.org', 'repositories.0', 'repositories.foo', 'github-oauth.github.com', 'preferred-install*', 'audit.ignore.0', 'policy.advisories'] as $path) {
        $ops[] = ['op' => 'source', 'key' => $path, 'r' => result(static function () use ($config, $path) { return $config->getSourceOfValue($path); })];
    }
    $ops[] = ['op' => 'repos', 'r' => result(static function () use ($config) { return $config->getRepositories(); })];
    if (chance(30)) {
        $ops[] = ['op' => 'raw', 'r' => result(static function () use ($config) { return $config->raw(); })];
    }
    if (chance(15)) {
        $flags = mt_rand(0, 1);
        $ops[] = ['op' => 'all', 'flags' => $flags, 'r' => result(static function () use ($config, $flags) { return $config->all($flags); })];
    }

    $cases[] = ['env' => (object) $env, 'useEnv' => $useEnv, 'baseDir' => $baseDir, 'ops' => $ops];
}

$out = dirname(__DIR__, 3).'/internal/config/testdata/oracle/config.json';
@mkdir(dirname($out), 0777, true);
file_put_contents($out, json_encode(['cases' => $cases], JSON_UNESCAPED_SLASHES | JSON_PRESERVE_ZERO_FRACTION | JSON_THROW_ON_ERROR | JSON_INVALID_UTF8_SUBSTITUTE));
echo count($cases), " cases\n";
