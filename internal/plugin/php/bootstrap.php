<?php

/*
 * The entry point of maestro's plugin shim (docs/PLUGINS.md §5.2): the php
 * child maestro starts runs this file. It does what Composer's bin/composer
 * does before running Composer, opens the channel to maestro and serves
 * maestro's calls until the process ends.
 *
 * Plain syntax only up to the version check, so an old PHP reaches
 * Composer's message instead of a parse error.
 */

if (PHP_SAPI !== 'cli' && PHP_SAPI !== 'phpdbg') {
    echo 'Warning: Composer should be invoked via the CLI version of PHP, not the '.PHP_SAPI.' SAPI'.PHP_EOL;
}

// 1. bin/composer's version check.
if (PHP_VERSION_ID < 70205) {
    echo 'Composer 2.3.0 dropped support for PHP <7.2.5 and you are running '.PHP_VERSION.', please upgrade PHP or use Composer 2.2 LTS via "composer self-update --2.2". Aborting.'.PHP_EOL;
    exit(1);
}

// 2. The channel's settings leave the environment before any plugin code
// runs (the channel opens once the shim's classes can load).
$maestroIpc = (string) getenv('MAESTRO_IPC');
$maestroToken = getenv('MAESTRO_IPC_TOKEN');
foreach (array('MAESTRO_IPC', 'MAESTRO_IPC_TOKEN') as $maestroVar) {
    putenv($maestroVar);
    unset($_SERVER[$maestroVar], $_ENV[$maestroVar]);
}
unset($maestroVar);

if (ini_get('opcache.enable_cli') && ini_get('opcache.preload')) {
    register_shutdown_function(static function () {
        $e = error_get_last();
        if ($e !== null
            && (
                strpos($e['message'], 'Cannot redeclare class') !== false
                || (strpos($e['message'], 'Declaration of') !== false && strpos($e['message'], 'must be compatible with') !== false)
            )
        ) {
            echo 'Warning: The error above might be caused by the fact opcache.enable_cli is enabled and opcache.preload is set in php.ini. We recommend disabling opcache.enable_cli if you really need preloading on this machine.'.PHP_EOL;
        }
    });
}

// 3.
setlocale(LC_ALL, 'C');
error_reporting(-1);

// bin/composer restarts without Xdebug here; maestro has started this
// process the way XdebugHandler restarts (internal/plugin/xdebug.go).

if (defined('HHVM_VERSION') && version_compare(HHVM_VERSION, '4.0', '>=')) {
    echo 'HHVM 4.0 has dropped support for Composer, please use PHP instead. Aborting.'.PHP_EOL;
    exit(1);
}
if (!extension_loaded('iconv') && !extension_loaded('mbstring')) {
    echo 'The iconv OR mbstring extension is required and both are missing.'
        .PHP_EOL.'Install either of them or recompile php without --disable-iconv.'
        .PHP_EOL.'Aborting.'.PHP_EOL;
    exit(1);
}

// 4. and 5.
if (function_exists('ini_set')) {
    // check if error logging is on, but to an empty destination - for the CLI SAPI, that means stderr
    $logsToSapiDefault = ('' === ini_get('error_log') && (bool) ini_get('log_errors'));
    // on the CLI SAPI, ensure errors are displayed on stderr, either via display_errors or via error_log
    if (PHP_SAPI === 'cli') {
        @ini_set('display_errors', $logsToSapiDefault ? '0' : 'stderr');
    }
    unset($logsToSapiDefault);

    // Set user defined memory limit
    if ($memoryLimit = getenv('COMPOSER_MEMORY_LIMIT')) {
        @ini_set('memory_limit', $memoryLimit);
    } else {
        $memoryInBytes = function ($value) {
            $unit = strtolower(substr($value, -1, 1));
            $value = (int) $value;
            switch($unit) {
                case 'g':
                    $value *= 1024;
                    // no break (cumulative multiplier)
                case 'm':
                    $value *= 1024;
                    // no break (cumulative multiplier)
                case 'k':
                    $value *= 1024;
            }

            return $value;
        };

        $memoryLimit = trim(ini_get('memory_limit'));
        // Increase memory_limit if it is lower than 1.5GB
        if ($memoryLimit != -1 && $memoryInBytes($memoryLimit) < 1024 * 1024 * 1536) {
            @ini_set('memory_limit', '1536M');
        }
        unset($memoryInBytes);
    }
    unset($memoryLimit);
}

// Workaround PHP bug on Windows where env vars containing Unicode chars are mangled in $_SERVER
// see https://github.com/php/php-src/issues/7896
if ((PHP_VERSION_ID < 80016 || (PHP_VERSION_ID >= 80100 && PHP_VERSION_ID < 80103)) && defined('PHP_WINDOWS_VERSION_BUILD')) {
    foreach ($_SERVER as $serverVar => $serverVal) {
        if (($serverVal = getenv($serverVar)) !== false) {
            $_SERVER[$serverVar] = $serverVal;
        }
    }
    unset($serverVar, $serverVal);
}

// 6. The shim's class loader (the phar's classes win over a project's) and
// the vendored libraries' autoload files.
require __DIR__.'/src/Maestro/Shim/Autoloader.php';
\Maestro\Shim\Autoloader::register(__DIR__);

// bin/composer registers the error handler without an IO first.
\Composer\Util\ErrorHandler::register();

// What Composer's Application::__construct does for the application
// maestro runs, once per process: xdebug's ini, the default timezone and
// the out-of-memory hint.
if (function_exists('ini_set') && extension_loaded('xdebug')) {
    ini_set('xdebug.show_exception_trace', '0');
    ini_set('xdebug.scream', '0');
}
if (function_exists('date_default_timezone_set') && function_exists('date_default_timezone_get')) {
    date_default_timezone_set(\Composer\Util\Silencer::call('date_default_timezone_get'));
}
register_shutdown_function(static function () {
    $lastError = error_get_last();

    if ($lastError && $lastError['message'] &&
       (strpos($lastError['message'], 'Allowed memory') !== false /*Zend PHP out of memory error*/ ||
        strpos($lastError['message'], 'exceeded memory') !== false /*HHVM out of memory errors*/)) {
        echo "\n". 'Check https://getcomposer.org/doc/articles/troubleshooting.md#memory-limit-errors for more info on how to handle out of memory errors.';
    }
});

// The Composer API's mirror families, value tags and methods, before the
// first message (boot's IO is a mirror).
\Maestro\Shim\Api::register();

// 7. to 9.: handshake, `boot` (which registers the error handler with the
// IO), then maestro's calls.
\Maestro\Shim\Rpc::connect($maestroIpc);
\Maestro\Shim\Sync::init();
\Maestro\Shim\Rpc::hello($maestroToken === false ? null : $maestroToken);
unset($maestroIpc, $maestroToken);
\Maestro\Shim\Rpc::serve();
