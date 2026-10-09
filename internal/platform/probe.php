<?php
// maestro's platform probe: run once with the user's php (fed on its
// standard input, or as a temporary script file on Windows, where it
// ends waiting for its standard input) to collect, in one invocation,
// every fact about the running PHP that Composer reads in-process. See
// doc.go.
//
// Output: "\n" MARKER JSON, after anything PHP printed at startup. Values
// that JSON cannot carry are wrapped in single-key arrays whose key starts
// with a NUL byte (maestro_probe_enc here, unwrap in snapshot.go). The syntax
// stays PHP 5.6 compatible so that an old php still reports its version.

function maestro_probe_enc($v)
{
    if (is_string($v)) {
        return preg_match('//u', $v) ? $v : array("\0s" => base64_encode($v));
    }
    if (is_float($v)) {
        if (is_nan($v)) {
            return array("\0f" => 'NAN');
        }
        if (is_infinite($v)) {
            return array("\0f" => $v > 0 ? 'INF' : '-INF');
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
            $pairs = array();
            foreach ($v as $k => $x) {
                $pairs[] = array(maestro_probe_enc($k), maestro_probe_enc($x));
            }

            return array("\0p" => $pairs);
        }
        if (array() === $v) {
            return $v;
        }
        $out = array();
        foreach ($v as $k => $x) {
            $out[$k] = maestro_probe_enc($x);
        }
        // a list would encode as a JSON array, a map as an object: either
        // way php's json_decode($json, true) restores the keys.
        return $out;
    }
    if (is_object($v)) {
        return array("\0o" => get_class($v));
    }
    if (is_resource($v)) {
        return array("\0r" => get_resource_type($v));
    }

    return $v;
}

// maestro_probe_call records $fn(...$args): its value, or the class and
// message of what it threw. Methods named in $methods are then called on
// an object result in the same way.
function maestro_probe_call($callable, $args, $methods = array())
{
    $entry = array('callable' => $callable, 'args' => $args);
    $isNew = is_string($callable) && strpos($callable, 'new ') === 0;
    if (!$isNew && !is_callable($callable)) {
        // Runtime::invoke(callable $callable, ...)
        $entry['error'] = array('TypeError', 'Composer\\Platform\\Runtime::invoke(): Argument #1 ($callable) must be of type callable, '.(is_string($callable) ? 'string' : 'array').' given');

        return $entry;
    }
    try {
        $value = $isNew
            ? maestro_probe_new(substr($callable, 4), $args)
            : $callable(...$args);
        $entry['value'] = $value;
        if (is_object($value)) {
            $entry['methods'] = array();
            foreach ($methods as $m) {
                $entry['methods'][] = maestro_probe_call(array($value, $m[0]), $m[1]);
                $entry['methods'][count($entry['methods']) - 1]['callable'] = $m[0];
            }
        }
    } catch (Exception $e) {
        $entry['error'] = array(get_class($e), $e->getMessage());
    } catch (Throwable $e) {
        $entry['error'] = array(get_class($e), $e->getMessage());
    }

    return $entry;
}

function maestro_probe_new($class, $args)
{
    if (count($args) === 0) {
        return new $class;
    }
    $refl = new ReflectionClass($class);

    return $refl->newInstanceArgs($args);
}

// The ini settings come first, before this script changes any of them.
$maestroProbe = array('format' => 1, 'ini' => function_exists('ini_get_all') ? ini_get_all(null, false) : null);

// What bin/composer does before anything else.
setlocale(LC_ALL, 'C');
ob_start();

// Runtime::getExtensions, getExtensionVersion and getExtensionInfo for
// every loaded extension (as the CLI SAPI prints it).
$maestroProbe['extensions'] = array();
foreach (get_loaded_extensions() as $maestroName) {
    $maestroInfo = null;
    if (class_exists('ReflectionExtension', false)) {
        try {
            $maestroReflector = new ReflectionExtension($maestroName);
            ob_start();
            $maestroReflector->info();
            $maestroInfo = (string) ob_get_clean();
        } catch (Exception $e) {
        }
    }
    $maestroProbe['extensions'][] = array($maestroName, phpversion($maestroName), $maestroInfo);
}

// The rest may change settings, which the extensions' info (Core's
// lists local values) must not show.
error_reporting(0);
@ini_set('display_errors', '0');
@ini_set('serialize_precision', '-1');

// The CLI defines STDIN, STDOUT and STDERR for a script file, as Composer
// is, but not for a script read from standard input, as this one is.
if (PHP_SAPI === 'cli' && !defined('STDIN')) {
    define('STDIN', fopen('php://stdin', 'r'));
    define('STDOUT', fopen('php://stdout', 'w'));
    define('STDERR', fopen('php://stderr', 'w'));
}
$maestroProbe['constants'] = get_defined_constants();
// The constants of the classes Composer reads class constants of.
$maestroProbe['class_constants'] = array();
foreach (array('ZipArchive') as $maestroName) {
    if (class_exists($maestroName, false)) {
        $maestroReflector = new ReflectionClass($maestroName);
        $maestroProbe['class_constants'][$maestroName] = $maestroReflector->getConstants();
    }
}
$maestroFunctions = get_defined_functions();
$maestroProbe['functions'] = $maestroFunctions['internal'];
$maestroProbe['classes'] = get_declared_classes();
$maestroProbe['ini_files'] = array(
    function_exists('php_ini_loaded_file') ? php_ini_loaded_file() : false,
    function_exists('php_ini_scanned_files') ? php_ini_scanned_files() : false,
);
$maestroProbe['zend_extensions'] = get_loaded_extensions(true);

// DiagnoseCommand::checkPlatform's "Configure Command" of
// phpinfo(INFO_GENERAL), matched with its pattern (null when it does not
// match, as when PHP was built without it; phpinfo may be disabled, which
// Composer would not survive).
$maestroProbe['configure_command'] = null;
if (function_exists('phpinfo')) {
    ob_start();
    phpinfo(INFO_GENERAL);
    $maestroInfo = ob_get_clean();
    if (is_string($maestroInfo) && preg_match('{Configure Command(?: *</td><td class="v">| *=> *)(.*?)(?:</td>|$)}m', $maestroInfo, $maestroMatch)) {
        $maestroProbe['configure_command'] = $maestroMatch[1];
    }
}

// Every Runtime::invoke and Runtime::construct call in Composer, plus the
// functions maestro asks about elsewhere (listed in doc.go).
$maestroProbe['calls'] = array(
    maestro_probe_call('inet_pton', array('::')),
    maestro_probe_call('curl_version', array()),
    maestro_probe_call(array('ResourceBundle', 'create'), array('root', 'ICUDATA', false), array(array('get', array('Version')))),
    maestro_probe_call(array('IntlChar', 'getUnicodeVersion'), array()),
    maestro_probe_call('new Imagick', array(), array(array('getVersion', array()))),
    maestro_probe_call('php_uname', array('s')),
    maestro_probe_call('php_uname', array('n')),
    maestro_probe_call('php_uname', array('r')),
    maestro_probe_call('php_uname', array('v')),
    maestro_probe_call('php_uname', array('m')),
    maestro_probe_call('xdebug_info', array('mode')),
    maestro_probe_call('ioncube_loader_iversion', array()),
    maestro_probe_call('sys_get_temp_dir', array()),
);

// XdebugHandler::setXdebugDetails, which reads the environment.
$maestroXdebug = array('loaded' => extension_loaded('xdebug'), 'version' => null, 'mode' => null, 'active' => false);
if ($maestroXdebug['loaded']) {
    $maestroVersion = phpversion('xdebug');
    $maestroXdebug['version'] = $maestroVersion !== false ? $maestroVersion : 'unknown';
    if (version_compare($maestroXdebug['version'], '3.1', '>=')) {
        $maestroModes = xdebug_info('mode');
        $maestroXdebug['mode'] = count($maestroModes) === 0 ? 'off' : implode(',', $maestroModes);
    } else {
        $maestroIniMode = ini_get('xdebug.mode');
        if ($maestroIniMode !== false) {
            $maestroEnvMode = (string) getenv('XDEBUG_MODE');
            if ($maestroEnvMode !== '') {
                $maestroXdebug['mode'] = $maestroEnvMode;
            } else {
                $maestroXdebug['mode'] = $maestroIniMode !== '' ? $maestroIniMode : 'off';
            }
            if (preg_match('/^,+$/', str_replace(' ', '', $maestroXdebug['mode']))) {
                $maestroXdebug['mode'] = 'off';
            }
        }
    }
    $maestroXdebug['active'] = $maestroXdebug['mode'] !== 'off';
}
// What the restart without xdebug that bin/composer does would remove.
if ($maestroXdebug['loaded']) {
    $maestroXdebug['functions'] = get_extension_funcs('xdebug') ?: array();
    $maestroConstants = get_defined_constants(true);
    $maestroXdebug['constants'] = isset($maestroConstants['xdebug']) ? array_keys($maestroConstants['xdebug']) : array();
    $maestroReflector = new ReflectionExtension('xdebug');
    $maestroXdebug['classes'] = $maestroReflector->getClassNames();
    $maestroXdebug['ini'] = array_keys($maestroReflector->getINIEntries());
}
$maestroProbe['xdebug'] = $maestroXdebug;

// The files this process maps (its binary, libraries and extensions), which
// maestro checks before reusing a cached copy of this result. Linux only:
// on Windows maestro lists the loaded modules of the still-running process
// itself (probe_windows.go).
$maestroProbe['mapped_files'] = null;
$maestroMaps = @file_get_contents('/proc/self/maps');
if (is_string($maestroMaps) && preg_match_all('{^\S+ \S+ \S+ \S+ \S+\s+(/.*)$}m', $maestroMaps, $maestroMatch)) {
    $maestroProbe['mapped_files'] = array_values(array_unique($maestroMatch[1]));
}

ob_end_clean();
// The result ends with a newline (json_encode escapes newlines inside
// strings, so the first one after the marker ends the result): what the
// probe reads until on Windows, and harmless whitespace to the decoder
// elsewhere.
echo "\n\0maestro-probe\0", json_encode(maestro_probe_enc($maestroProbe), defined('JSON_PRESERVE_ZERO_FRACTION') ? JSON_PRESERVE_ZERO_FRACTION : 0), "\n";

// Windows: maestro runs this script as a file and lists the process's
// loaded modules for its probe cache while the script waits here for its
// standard input to end (probe_windows.go). Elsewhere the script comes on
// standard input, which is at its end once the script was read, so this
// returns at once.
if (defined('PHP_OS_FAMILY') ? PHP_OS_FAMILY === 'Windows' : 0 === strncmp(PHP_OS, 'WIN', 3)) {
    while (false !== fgets(STDIN)) {
    }
}
