<?php
// Generates internal/config/testdata/oracle/factory.json: the configuration
// half of Composer\Factory (getHomeDir, getCacheDir, getDataDir,
// createConfig, getComposerFile, getLockFile) over generated environments,
// directory layouts and global config.json/auth.json/COMPOSER_AUTH contents.
// Paths are recorded relative to a scratch directory, as <BASE>.
//
// Run: php tools/oracle/config/factory.php
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Composer\Factory;
use Composer\IO\NullIO;
use Composer\Util\Platform;

set_error_handler(static function (int $level, string $message, string $file, int $line): bool {
    if ($level === E_DEPRECATED || $level === E_USER_DEPRECATED || 0 === (error_reporting() & $level)) {
        return true;
    }
    throw new \ErrorException($message, 0, $level, $file, $line);
});

mt_srand(20261005);

class RecordingIO extends NullIO
{
    /** @var list<string> */
    public $errors = [];

    public function writeError($messages, bool $newline = true, int $verbosity = self::NORMAL): void
    {
        $this->errors[] = $verbosity.'|'.$messages;
    }
}

function pick(array $a) { return $a[mt_rand(0, count($a) - 1)]; }
function chance(int $percent): bool { return mt_rand(1, 100) <= $percent; }

function rrmdir(string $dir): void
{
    if (!is_dir($dir) || is_link($dir)) {
        @unlink($dir);

        return;
    }
    foreach (scandir($dir) as $f) {
        if ($f !== '.' && $f !== '..') {
            rrmdir($dir.'/'.$f);
        }
    }
    rmdir($dir);
}

const MANAGED_ENV = ['COMPOSER_HOME', 'COMPOSER_CACHE_DIR', 'HOME', 'XDG_CONFIG_HOME', 'XDG_CACHE_HOME', 'XDG_DATA_HOME', 'XDG_RUNTIME_DIR', 'APPDATA', 'LOCALAPPDATA', 'COMPOSER', 'COMPOSER_AUTH', 'COMPOSER_HTACCESS_PROTECT'];

// clear every inherited XDG_ variable, as useXdg() looks at all of them
foreach (array_keys($_SERVER) as $key) {
    if (strpos((string) $key, 'XDG_') === 0) {
        Platform::clearEnv((string) $key);
    }
}
foreach (array_keys(getenv()) as $key) {
    if (strpos((string) $key, 'XDG_') === 0) {
        Platform::clearEnv((string) $key);
    }
}

$base = sys_get_temp_dir().'/maestro-factory-oracle';
rrmdir($base);

$call = static function (string $method, ...$args) {
    return Closure::bind(static function () use ($method, $args) {
        return Factory::$method(...$args);
    }, null, Factory::class)();
};

function rel(string $base, $v)
{
    return is_string($v) ? str_replace($base, '<BASE>', $v) : $v;
}

function outcome(string $base, callable $fn): array
{
    try {
        $v = $fn();

        return ['v' => is_string($v) ? rel($base, $v) : var_export($v, true)];
    } catch (\Throwable $e) {
        return ['e' => [get_class($e), rel($base, preg_replace('{, called in .* on line \d+$}', '', $e->getMessage()))]];
    }
}

$envValues = [
    'COMPOSER_HOME' => ['<BASE>/chome', '<BASE>/chome/', '', '0', 'rel/home'],
    'COMPOSER_CACHE_DIR' => ['<BASE>/ccache', '', '0'],
    'HOME' => ['<BASE>/home', '<BASE>/home/', '<BASE>\\home\\', '', '0'],
    'XDG_CONFIG_HOME' => ['<BASE>/xdgc', '', '0'],
    'XDG_CACHE_HOME' => ['<BASE>/xdgcache', ''],
    'XDG_DATA_HOME' => ['<BASE>/xdgdata', '', '0'],
    'XDG_RUNTIME_DIR' => ['/run/user/1000', ''],
    'APPDATA' => ['C:\\Users\\x\\AppData\\Roaming\\', ''],
    'LOCALAPPDATA' => ['C:\\Users\\x\\AppData\\Local', ''],
];
$layouts = ['home', 'home/.composer', 'home/.composer/cache', 'home/.config/composer', 'xdgc/composer', 'chome', 'home/.cache'];

$cases = [];
$caseNo = 0;

// directories
for ($n = 0; $n < 800; $n++) {
    rrmdir($base);
    mkdir($base, 0777, true);
    foreach (MANAGED_ENV as $var) {
        Platform::clearEnv($var);
    }
    $env = [];
    foreach ($envValues as $var => $values) {
        if (chance($var === 'HOME' ? 85 : 25)) {
            $env[$var] = pick($values);
            Platform::putEnv($var, str_replace('<BASE>', $base, $env[$var]));
        }
    }
    $dirs = [];
    foreach ($layouts as $dir) {
        if (chance(35)) {
            $dirs[] = $dir;
            @mkdir($base.'/'.$dir, 0777, true);
        }
    }
    $home = outcome($base, static function () use ($call) { return $call('getHomeDir'); });
    $case = ['kind' => 'dirs', 'env' => (object) $env, 'dirs' => $dirs, 'home' => $home];
    $homeArg = isset($home['v']) ? str_replace('<BASE>', $base, $home['v']) : $base.'/fallback';
    $case['homeArg'] = rel($base, $homeArg);
    $case['cache'] = outcome($base, static function () use ($call, $homeArg) { return $call('getCacheDir', $homeArg); });
    $case['data'] = outcome($base, static function () use ($call, $homeArg) { return $call('getDataDir', $homeArg); });
    $cases[] = $case;
}

// createConfig
$configJsons = [
    null,
    '{}',
    '{"config": {"vendor-dir": "lib", "github-oauth": {"github.com": "tok"}}, "repositories": [{"type": "vcs", "url": "https://x.org/r.git"}]}',
    '{"config": {"htaccess-protect": false}}',
    '{"config": {"process-timeout": "abc"}}',
    '{"config": {"preferred-install": {"foo/*": "source"}}, "repositories": {"packagist.org": false}}',
    '{"config": {"unknown-setting": 1}, "foo": "bar"}',
    '{"config": {"cache-dir": "{$home}/c2", "data-dir": "/abs/data"}}',
    '{"config": ',
    'null',
    '"str"',
    '[]',
    "{\n    \"config\": {\n        \"allow-plugins\": {\"a/b\": true}\n    }\n}\n",
];
$authJsons = [
    null,
    '{}',
    '{"github-oauth": {"github.com": "abc"}, "http-basic": {"x.org": {"username": "u", "password": "p"}}}',
    '{"github-oauth": "foo"}',
    '{"bearer": {"x.org": 5}}',
    '{"gitlab-token": {"gitlab.com": {"username": "u", "token": "t"}}}',
    '{bad',
];
$authEnvs = [
    null, '', '{"github-oauth": {"github.com": "envtok"}}', '{"github-oauth": "foo"}', 'null', '{bad', '[]', '{"http-basic": {"y.org": {"username": "a", "password": "b"}}}',
];
for ($n = 0; $n < 400; $n++) {
    rrmdir($base);
    mkdir($base.'/project', 0777, true);
    foreach (MANAGED_ENV as $var) {
        Platform::clearEnv($var);
    }
    Platform::putEnv('HOME', $base.'/home');
    $env = ['HOME' => '<BASE>/home', 'COMPOSER_HOME' => '<BASE>/chome'];
    Platform::putEnv('COMPOSER_HOME', $base.'/chome');
    if (chance(20)) {
        $env['COMPOSER_CACHE_DIR'] = '<BASE>/ccache';
        Platform::putEnv('COMPOSER_CACHE_DIR', $base.'/ccache');
    }
    if (chance(15)) {
        $env['COMPOSER_HTACCESS_PROTECT'] = pick(['0', '1']);
        Platform::putEnv('COMPOSER_HTACCESS_PROTECT', $env['COMPOSER_HTACCESS_PROTECT']);
    }
    $authEnv = pick($authEnvs);
    if ($authEnv !== null) {
        $env['COMPOSER_AUTH'] = $authEnv;
        Platform::putEnv('COMPOSER_AUTH', $authEnv);
    }
    $configJson = pick($configJsons);
    $authJson = pick($authJsons);
    $existing = chance(30);
    if ($configJson !== null || $authJson !== null || $existing) {
        @mkdir($base.'/chome', 0777, true);
    }
    if ($configJson !== null) {
        file_put_contents($base.'/chome/config.json', $configJson);
    }
    if ($authJson !== null) {
        file_put_contents($base.'/chome/auth.json', $authJson);
    }
    $withIO = chance(60);
    $io = $withIO ? new RecordingIO() : null;
    $cwd = pick([null, '<BASE>/project']);
    $case = ['kind' => 'create', 'env' => (object) $env, 'configJson' => $configJson, 'authJson' => $authJson, 'mkHome' => $existing, 'io' => $withIO, 'cwd' => $cwd];
    $case['r'] = outcome($base, static function () use ($io, $cwd, $base) {
        $config = Factory::createConfig($io, $cwd === null ? null : str_replace('<BASE>', $base, $cwd));

        return var_export([$config->raw(), $config->getConfigSource()->getName(), $config->getAuthConfigSource()->getName()], true);
    });
    if (isset($case['r']['v'])) {
        $case['r']['v'] = rel($base, $case['r']['v']);
    }
    $case['warnings'] = $io ? array_map(static function ($m) use ($base) { return rel($base, $m); }, $io->errors) : [];
    $files = [];
    foreach (['chome/.htaccess', 'ccache/.htaccess', 'chome/cache/.htaccess'] as $f) {
        if (file_exists($base.'/'.$f)) {
            $files[] = $f.':'.file_get_contents($base.'/'.$f);
        }
    }
    $case['files'] = $files;
    $cases[] = $case;
}

// getComposerFile, getLockFile
foreach ([null, '', '   ', ' foo.json ', '<BASE>', '<BASE>/x.json', 'composer.jsonc', "\tbar.json\n"] as $value) {
    rrmdir($base);
    mkdir($base, 0777, true);
    Platform::clearEnv('COMPOSER');
    if ($value !== null) {
        Platform::putEnv('COMPOSER', str_replace('<BASE>', $base, $value));
    }
    $cases[] = ['kind' => 'composerFile', 'env' => $value, 'r' => outcome($base, static function () { return Factory::getComposerFile(); })];
}
foreach (['composer.json', './composer.json', 'foo.json', 'composer.jsonc', 'dir.json/composer', '/a/b/project.JSON', 'no-extension', '/path/with.dot/c.json', '.json', 'json', 'a.b.json', 'x/.json', '', 'c.json/', 'C:\\x\\c.json', 'c.json.', 'c..json'] as $file) {
    $cases[] = ['kind' => 'lockFile', 'file' => $file, 'r' => ['v' => Factory::getLockFile($file)]];
}

rrmdir($base);
$out = dirname(__DIR__, 3).'/internal/config/testdata/oracle/factory.json';
file_put_contents($out, json_encode(['etcXdg' => is_dir('/etc/xdg'), 'cases' => $cases], JSON_UNESCAPED_SLASHES | JSON_THROW_ON_ERROR));
echo count($cases), " cases\n";
