<?php
// Generates internal/platform/testdata/oracle/<variant>.json: what
// internal/platform/probe.php prints when run by this php, next to what
// Composer's own Composer\Platform\Runtime (and the other in-process facts
// maestro takes from the probe) answer in this php. The Go tests parse the
// recorded probe output and check that the snapshot's Runtime gives the
// same answers.
//
// Run with the php to record (tools/oracle/platform/generate.sh runs the
// variants): php tools/oracle/platform/runtime.php <variant>, or "-" to
// print the JSON instead.
// Any environment (PHP_INI_SCAN_DIR, PHPRC, XDEBUG_MODE) applies to both.
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Composer\Platform\Runtime;
use Composer\Repository\PlatformRepository;
use Composer\Util\IniHelper;
use Composer\XdebugHandler\XdebugHandler;

// The ini settings, before this script or Composer's code changes any.
$ini = [];
foreach (ini_get_all(null, false) as $name => $value) {
    $ini[$name] = ini_get($name);
}

$variant = $argv[1] ?? 'default';
$root = dirname(__DIR__, 3);

// enc is probe.php's maestro_probe_enc.
function enc($v)
{
    if (is_string($v)) {
        return preg_match('//u', $v) ? $v : ["\0s" => base64_encode($v)];
    }
    if (is_float($v)) {
        if (is_nan($v)) {
            return ["\0f" => 'NAN'];
        }
        if (is_infinite($v)) {
            return ["\0f" => $v > 0 ? 'INF' : '-INF'];
        }

        return $v;
    }
    if (is_array($v)) {
        $plain = true;
        foreach ($v as $k => $x) {
            if (is_string($k) && ($k === '' || $k[0] === "\0" || !preg_match('//u', $k))) {
                $plain = false;
                break;
            }
        }
        if (!$plain) {
            $pairs = [];
            foreach ($v as $k => $x) {
                $pairs[] = [enc($k), enc($x)];
            }

            return ["\0p" => $pairs];
        }

        return array_map('enc', $v);
    }
    if (is_object($v)) {
        return ["\0o" => get_class($v)];
    }
    if (is_resource($v)) {
        return ["\0r" => get_resource_type($v)];
    }

    return $v;
}

// message is the exception's message without where PHP says the failing
// call was made from, which a probe cannot know.
function message(\Throwable $e): string
{
    return preg_replace('/, called in .* on line \d+$/', '', $e->getMessage());
}

// outcome runs $fn and records its value or what it threw.
function outcome(callable $fn): array
{
    try {
        return ['value' => $fn()];
    } catch (\Throwable $e) {
        return ['error' => [get_class($e), message($e)]];
    }
}

// The probe, run by this php.
$proc = proc_open([PHP_BINARY], [0 => ['file', $root.'/internal/platform/probe.php', 'r'], 1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes);
$probe = stream_get_contents($pipes[1]);
fclose($pipes[1]);
fclose($pipes[2]);
if (proc_close($proc) !== 0) {
    fwrite(STDERR, "probe failed\n");
    exit(1);
}

$runtime = new Runtime();
$out = ['php_version' => PHP_VERSION, 'probe' => $probe];

$extensions = $runtime->getExtensions();
$out['getExtensions'] = $extensions;
foreach (array_merge($extensions, ['nope', strtoupper($extensions[0])]) as $ext) {
    $out['getExtensionVersion'][] = [$ext, $runtime->getExtensionVersion($ext)];
    $out['getExtensionInfo'][] = [$ext, outcome(static function () use ($runtime, $ext) { return $runtime->getExtensionInfo($ext); })];
}

// Every constant PlatformRepository, Composer and maestro read, and some
// that exercise the edges.
$constants = [
    ['PHP_VERSION', null], ['PHP_VERSION_ID', null], ['PHP_MAJOR_VERSION', null], ['PHP_MINOR_VERSION', null],
    ['PHP_RELEASE_VERSION', null], ['PHP_EXTRA_VERSION', null], ['PHP_DEBUG', null], ['PHP_ZTS', null],
    ['PHP_INT_SIZE', null], ['PHP_INT_MAX', null], ['PHP_OS', null], ['PHP_OS_FAMILY', null], ['PHP_BINARY', null],
    ['PHP_SAPI', null], ['PHP_EOL', null], ['PHP_WINDOWS_VERSION_BUILD', null], ['HHVM_VERSION', null],
    ['AF_INET6', null], ['GD_VERSION', null], ['GMP_VERSION', null], ['ICONV_VERSION', null],
    ['INTL_ICU_VERSION', null], ['LIBXML_DOTTED_VERSION', null], ['MB_ONIGURUMA_VERSION', null],
    ['OPENSSL_VERSION_TEXT', null], ['OPENSSL_VERSION_NUMBER', null], ['OPENSSL_ALGO_SHA384', null],
    ['PCRE_VERSION', null], ['PGSQL_LIBPQ_VERSION', null], ['RD_KAFKA_VERSION', null],
    ['SODIUM_LIBRARY_VERSION', null], ['LIBXSLT_DOTTED_VERSION', null], ['ZLIB_VERSION', null],
    ['CURL_VERSION_HTTP2', null], ['CURL_VERSION_HTTP3', null], ['CURL_VERSION_ZSTD', null],
    ['CURL_VERSION_LIBZ', null], ['CURL_VERSION_HTTPS_PROXY', null], ['CURL_HTTP_VERSION_2_0', null],
    ['CURL_HTTP_VERSION_3', null], ['CURLMOPT_MAX_HOST_CONNECTIONS', null], ['GLOB_BRACE', null],
    ['SIGINT', null], ['STDIN', null], ['INF', null], ['NAN', null], ['M_PI', null], ['E_ALL', null],
    ['XDEBUG_TRACE_APPEND', null], ['true', null], ['TRUE', null], ['Null', null], ['NOPE', null],
    ['php_version', null], ['\\PHP_VERSION', null],
    ['LIBZIP_VERSION', 'ZipArchive'], ['LIBZIP_VERSION', 'ziparchive'], ['NOPE', 'ZipArchive'],
    ['LIBZIP_VERSION', 'NopeClass'],
];
// Composer's vendor code (the symfony/polyfill-* packages) defines
// constants and functions of its own, which the probe cannot see.
$definedConstants = get_defined_constants(true);
$polyfillConstants = $definedConstants['user'] ?? [];
$definedFunctions = get_defined_functions();
$polyfillFunctions = array_flip($definedFunctions['user']);
foreach ($constants as [$name, $class]) {
    if ($class === null && array_key_exists(ltrim($name, '\\'), $polyfillConstants)) {
        continue;
    }
    $out['constants'][] = [
        $name, $class, $runtime->hasConstant($name, $class),
        outcome(static function () use ($runtime, $name, $class) { return @$runtime->getConstant($name, $class); }),
    ];
}

foreach (['proc_open', 'exec', 'inet_pton', 'curl_version', 'php_uname', 'posix_getuid', 'disk_free_space',
    'symlink', 'json_decode', 'mb_strlen', 'iconv', 'gzcompress', 'bzcompress', 'xdebug_info',
    'uopz_allow_exit', 'STRLEN', '\\strlen', 'nope'] as $fn) {
    if (isset($polyfillFunctions[strtolower(ltrim($fn, '\\'))])) {
        continue;
    }
    $out['hasFunction'][] = [$fn, $runtime->hasFunction($fn)];
}

foreach (['ResourceBundle', 'IntlChar', 'Imagick', 'ZipArchive', 'Phar', 'ReflectionExtension',
    'Traversable', 'resourcebundle', '\\Exception', 'NopeClass'] as $class) {
    $out['hasClass'][] = [$class, $runtime->hasClass($class)];
}

// Runtime::invoke and Runtime::construct as Composer calls them; objects
// are recorded with the results of the methods Composer calls on them.
$calls = [
    ['inet_pton', ['::'], []],
    ['curl_version', [], []],
    [['ResourceBundle', 'create'], ['root', 'ICUDATA', false], [['get', ['Version']]]],
    [['IntlChar', 'getUnicodeVersion'], [], []],
    ['php_uname', ['s'], []],
    ['php_uname', ['n'], []],
    ['php_uname', ['r'], []],
    ['php_uname', ['v'], []],
    ['php_uname', ['m'], []],
    ['xdebug_info', ['mode'], []],
    ['ioncube_loader_iversion', [], []],
    ['sys_get_temp_dir', [], []],
    ['nope_function', [], []],
    [['NopeClass', 'create'], [], []],
];
foreach ($calls as [$callable, $args, $methods]) {
    $result = outcome(static function () use ($runtime, $callable, $args) { return @$runtime->invoke($callable, $args); });
    if (isset($result['value']) && is_object($result['value'])) {
        foreach ($methods as [$m, $margs]) {
            $object = $result['value'];
            $result['methods'][] = [$m, $margs, outcome(static function () use ($object, $m, $margs) { return $object->$m(...$margs); })];
        }
    }
    $out['invoke'][] = [$callable, $args, $result];
}
foreach (['Imagick', 'NopeClass'] as $class) {
    $result = outcome(static function () use ($runtime, $class) { return $runtime->construct($class); });
    if (isset($result['value']) && is_object($result['value'])) {
        $object = $result['value'];
        $result['methods'][] = ['getVersion', [], outcome(static function () use ($object) { return $object->getVersion(); })];
    }
    $out['construct'][] = [$class, $result];
}

$out['ini_get'] = $ini;
$out['ini_get']['nope.setting'] = ini_get('nope.setting');
putenv('COMPOSER_ORIGINAL_INIS');
$out['IniHelper::getAll'] = IniHelper::getAll();
$out['php_ini_loaded_file'] = php_ini_loaded_file();
$out['zend_extensions'] = get_loaded_extensions(true);

// DiagnoseCommand::checkPlatform's Configure Command (null without a match).
ob_start();
phpinfo(INFO_GENERAL);
$phpinfo = ob_get_clean();
$out['configure_command'] = null;
if (is_string($phpinfo) && \Composer\Pcre\Preg::isMatchStrictGroups('{Configure Command(?: *</td><td class="v">| *=> *)(.*?)(?:</td>|$)}m', $phpinfo, $match)) {
    $out['configure_command'] = $match[1];
}

$xdebugActive = XdebugHandler::isXdebugActive();
$prop = static function (string $name) {
    $p = new ReflectionProperty(XdebugHandler::class, $name);
    $p->setAccessible(true);

    return $p->getValue();
};
$out['xdebug'] = ['active' => $xdebugActive, 'version' => $prop('xdebugVersion'), 'mode' => $prop('xdebugMode')];

// The platform packages Composer derives from all this (PlatformRepository
// without overrides), for the PlatformRepository port.
$repo = new PlatformRepository([], [], $runtime);
try {
    $packages = $repo->getPackages();
} catch (\Throwable $e) {
    $out['platform_packages_error'] = [get_class($e), message($e)];
    $packages = [];
}
foreach ($packages as $package) {
    $links = static function (array $links): array {
        $out = [];
        foreach ($links as $key => $link) {
            $out[$key] = [$link->getSource(), $link->getTarget(), $link->getDescription(), $link->getPrettyConstraint()];
        }

        return $out;
    };
    $out['platform_packages'][] = [
        'name' => $package->getName(),
        'version' => $package->getVersion(),
        'pretty_version' => $package->getPrettyVersion(),
        'description' => $package->getDescription(),
        'type' => $package->getType(),
        'replaces' => $links($package->getReplaces()),
        'provides' => $links($package->getProvides()),
    ];
}

// html_entity_decode (Runtime::parseHtmlExtensionInfo's decoder) and
// parseHtmlExtensionInfo itself.
$html = ['', 'plain', '&amp;', '&amp', 'a&amp;b', '&lt;&gt;&quot;&#39;&apos;', '&#65;&#x42;&#X43;', '&#x0x41;',
    '&#xZ;', '&#;', '&#1114111;', '&#1114112;', '&#99999999999999999999;', '&#0;', '&#127;', '&#128;', '&#160;',
    '&#xD800;', '&#xFFFE;', '&#xFDD0;', '&#x1FFFF;', '&#9;&#10;&#13;&#11;', '&eacute;&Eacute;&euro;&hellip;',
    '&nbsp;x', '&nope;', '&;', '&&amp;', 'x&', 'x&a', 'x&ab', '&ab;', '&#38;amp;', "&am\xC3\xA9p;", '&copy2;',
    '&#x41', '&#65 ', '&AMP;', '&thetasym;&upsih;&piv;', '&lang;&rang;'];
foreach ($html as $h) {
    $out['html_entity_decode'][] = [$h, html_entity_decode($h)];
}
$infos = [
    "<h2><a name=\"module_x\">x &amp; y</a></h2>\n<table>\n<tr><td class=\"e\">A &lt;b&gt; </td><td class=\"v\"><i>1.0</i> </td></tr>\n</table>",
    "<h2>  <a href=\"#\">curl</a>  </h2><tr><td class=\"e\">SSL Version</td><td class=\"v\">OpenSSL/3.0</td></tr><TR><TD class=\"e\">x</TD><TD class=\"v\">y</TD></TR>",
    "<table><tr><td class=\"e\">only</td><td class=\"v\">rows</td></tr></table>",
    "no html at all",
];
foreach ($infos as $h) {
    $out['parseHtmlExtensionInfo'][] = [$h, Runtime::parseHtmlExtensionInfo($h)];
}

// Keep the checkout's location out of the goldens (the custom-ini
// variant's php.ini lives in it).
array_walk_recursive($out, static function (&$v) use ($root) {
    if (is_string($v)) {
        $v = str_replace([$root, str_replace('/', '\\/', $root)], '<root>', $v);
    }
});

$json = json_encode(enc($out), JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_PRESERVE_ZERO_FRACTION)."\n";
if ($variant === '-') { // the live test in internal/platform reads it
    echo $json;
    exit;
}
$dir = $root.'/internal/platform/testdata/oracle';
@mkdir($dir, 0777, true);
file_put_contents($dir.'/'.$variant.'.json', $json);
