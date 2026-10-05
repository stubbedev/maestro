<?php
// Generates internal/config/testdata/oracle/configsource.json.gz:
// Composer\Config\JsonConfigSource over generated composer.json, config.json
// and auth.json files (or none) and sequences of its operations, recording
// after each operation the exception, the file's contents and its mode.
// Documents the JsonManipulator cannot edit exercise the whole-file
// fallbacks.
//
// Run: php tools/oracle/config/configsource.php
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Composer\Config\JsonConfigSource;
use Composer\Json\JsonFile;

set_error_handler(static function (int $level, string $message, string $file, int $line): bool {
    if ($level === E_DEPRECATED || $level === E_USER_DEPRECATED || 0 === (error_reporting() & $level)) {
        return true;
    }
    throw new \ErrorException($message, 0, $level, $file, $line);
});

mt_srand(20261005);
umask(022);

function pick(array $a) { return $a[mt_rand(0, count($a) - 1)]; }
function chance(int $percent): bool { return mt_rand(1, 100) <= $percent; }

$docs = [
    "{\n}\n",
    "{\n    \"config\": {\n    }\n}\n",
    "{\n    \"name\": \"vendor/pkg\",\n    \"require\": {\n        \"php\": \"^8.1\",\n        \"vendor/a\": \"^1.0\"\n    }\n}\n",
    "{\n  \"require\": {\n    \"vendor/a\": \"^1.0\"\n  },\n  \"require-dev\": {},\n  \"config\": {\"sort-packages\": true}\n}\n",
    "{\n\t\"repositories\": [\n\t\t{\"type\": \"vcs\", \"url\": \"https://x.org/a.git\"},\n\t\t{\"packagist.org\": false}\n\t]\n}\n",
    "{\n    \"repositories\": {\n        \"foo\": {\"type\": \"vcs\", \"url\": \"https://x.org/foo.git\"},\n        \"packagist.org\": false\n    }\n}\n",
    "{\n    \"repositories\": [\n        {\"name\": \"bar\", \"type\": \"composer\", \"url\": \"https://bar.org\"}\n    ],\n    \"config\": {\n        \"policy\": {\n            \"advisories\": {\"block\": true}\n        }\n    }\n}\n",
    "{\"config\": [], \"extra\": [], \"scripts\": []}",
    "{\n    \"config\": \"oops\",\n    \"require\": []\n}\n",
    "{\n    \"repositories\": \"str\",\n    \"extra\": {\"foo\": {\"bar\": 1}}\n}\n",
    "{\n    \"extra\": {\"extra\": {\"x\": 1}},\n    \"scripts\": {\"test\": \"phpunit\"}\n}\n",
    "{\n    \"autoload\": {\"psr-4\": {\"App\\\\\": \"src/\"}},\n    \"autoload-dev\": {\"psr-0\": []}\n}\n",
    "{\n    \"config\": {\n        \"platform\": {\"php\": \"8.1\"},\n        \"github-oauth\": {\"github.com\": \"tok\"},\n        \"preferred-install\": {\"*\": \"dist\"}\n    }\n}\n",
    "{\n    \"github-oauth\": {\"github.com\": \"abc\"},\n    \"http-basic\": {}\n}\n",
    "{\"require\":{\"a/b\":\"1.0\"},\"require-dev\":{\"c/d\":\"2.0\"},\"suggest\":{\"e/f\":\"why\"},\"conflict\":{},\"replace\":{},\"provide\":{}}",
    "{\r\n    \"name\": \"crlf/pkg\",\r\n    \"config\": {\r\n        \"vendor-dir\": \"lib\"\r\n    }\r\n}\r\n",
    "{\n    \"repositories\": [\n        {\"type\": \"vcs\", \"url\": \"https://x.org/a.git\"}\n    ],\n    \"repositories\": []\n}\n",
    "[]",
    "{ \"config\": { \"policy\": { \"malware\": { \"ignore\": [\"a/b\"] }, \"custom\": {} } } }",
    "{\n    \"name\": \"x/y\",\n    \"config\": {\"allow-plugins\": {\"a/b\": true}},\n    \"extra\": {\"branch-alias\": {\"dev-main\": \"1.0-dev\"}}\n}\n",
];

function value()
{
    return pick([true, false, null, 0, 5, 'str', 'https://x.org', [], ['a', 'b'], ['k' => 'v'], ['nested' => ['x' => 1]], 1.5, '$1', 'a\\b']);
}

function repoConfig()
{
    return pick([false, ['type' => 'vcs', 'url' => 'https://x.org/r.git'], ['type' => 'composer', 'url' => 'https://repo.org', 'options' => ['ssl' => ['verify_peer' => false]]], ['name' => 'given', 'type' => 'path', 'url' => '../x'], ['type' => 'package', 'package' => ['name' => 'a/b', 'version' => '1.0']], ['url' => 5]]);
}

function operation(): array
{
    $repoNames = ['foo', 'bar', 'packagist.org', 'packagist', '', 'new', '0', 'given'];
    switch (mt_rand(0, 13)) {
        case 0: return ['addRepository', pick($repoNames), repoConfig(), chance(70)];
        case 1: return ['insertRepository', pick($repoNames), repoConfig(), pick($repoNames), mt_rand(0, 1)];
        case 2: return ['setRepositoryUrl', pick($repoNames), pick(['https://new.org', 'file:///x', ''])];
        case 3: return ['removeRepository', pick($repoNames)];
        case 4:
        case 5: return ['addConfigSetting', pick(['vendor-dir', 'sort-packages', 'github-oauth.github.com', 'http-basic.x.org', 'platform.php', 'platform.ext-foo', 'policy.advisories.block', 'policy.custom.sources', 'policy.malware', 'policy', 'preferred-install.foo/*', 'allow-plugins.a/b', 'custom-headers.x.org', 'github-oauth', 'process-timeout', 'audit.abandoned', 'bearer.x.org']), value()];
        case 6: return ['removeConfigSetting', pick(['vendor-dir', 'sort-packages', 'github-oauth.github.com', 'http-basic.x.org', 'platform.php', 'policy.advisories.block', 'policy.malware.ignore', 'policy.custom', 'preferred-install.*', 'allow-plugins.a/b', 'github-oauth', 'nope'])];
        case 7: return ['addProperty', pick(['name', 'description', 'extra.foo', 'extra.foo.bar', 'extra.extra.x', 'scripts.test', 'scripts.foo.bar', 'suggest.a/b', 'minimum-stability', 'type']), value()];
        case 8: return ['removeProperty', pick(['name', 'extra.foo', 'extra.foo.bar', 'extra.extra.x', 'scripts.test', 'autoload.psr-4.App\\', 'AUTOLOAD.psr-4', 'autoload-dev.psr-0', 'suggest.e/f', 'require', 'nope'])];
        case 9:
        case 10: return ['addLink', pick(['require', 'require-dev', 'suggest', 'conflict', 'provide', 'replace', 'config', 'extra']), pick(['a/b', 'vendor/a', 'new/pkg', 'php', 'ext-json', 'A/B']), pick(['^1.0', '*', 'dev-main as 1.0', 'why not'])];
        default: return ['removeLink', pick(['require', 'require-dev', 'suggest', 'conflict', 'provide', 'replace', 'nope']), pick(['a/b', 'vendor/a', 'php', 'c/d', 'e/f', 'A/B'])];
    }
}

$dir = sys_get_temp_dir().'/maestro-configsource-oracle';
@mkdir($dir, 0777, true);
$path = $dir.'/composer.json';

$cases = [];
for ($n = 0; $n < 1500; $n++) {
    @unlink($path);
    $doc = chance(90) ? pick($docs) : null;
    if ($doc !== null) {
        file_put_contents($path, $doc);
        chmod($path, 0644);
    }
    $auth = chance(25);
    $ops = [];
    $source = new JsonConfigSource(new JsonFile($path), $auth);
    $count = mt_rand(1, 3);
    for ($i = 0; $i < $count; $i++) {
        $op = operation();
        $method = array_shift($op);
        $step = ['method' => $method, 'args' => json_encode($op, JSON_PRESERVE_ZERO_FRACTION)];
        try {
            $source->$method(...$op);
        } catch (\Throwable $e) {
            $step['e'] = [get_class($e), str_replace($dir, '<DIR>', preg_replace('{, called in .* on line \d+$}', '', $e->getMessage()))];
        }
        clearstatcache();
        $step['contents'] = file_exists($path) ? file_get_contents($path) : null;
        $step['mode'] = file_exists($path) ? decoct(fileperms($path) & 0777) : null;
        $ops[] = $step;
    }
    $cases[] = ['doc' => $doc, 'auth' => $auth, 'ops' => $ops];
}
@unlink($path);
@rmdir($dir);

$out = dirname(__DIR__, 3).'/internal/config/testdata/oracle/configsource.json.gz';
file_put_contents($out, gzencode(json_encode($cases, JSON_UNESCAPED_SLASHES | JSON_THROW_ON_ERROR), 9));
echo count($cases), " cases\n";
