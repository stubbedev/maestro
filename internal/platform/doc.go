// Package platform ports Composer\Platform (Runtime, Version,
// HhvmDetector) and answers Composer's in-process questions about PHP for
// a program that is not PHP.
//
// # The probe
//
// Composer asks the PHP it runs on directly (constants, loaded extensions
// and their phpinfo sections, functions, classes, ini settings).
// maestro runs probe.php once with the user's php instead and keeps the
// answers in a Snapshot: every constant (get_defined_constants()), the
// constants of the classes Composer reads (ZipArchive), every internal
// function and class, ini_get_all(), the loaded ini files, every loaded
// extension with its version and ReflectionExtension::info() output, the
// zend extensions, XdebugHandler's view of xdebug, and the outcome of each
// call Composer makes through Runtime::invoke and Runtime::construct:
//
//	inet_pton('::')                                   PlatformRepository (php-ipv6)
//	curl_version()                                    PlatformRepository (lib-curl)
//	ResourceBundle::create('root', 'ICUDATA', false)  PlatformRepository (lib-icu-cldr)
//	  ->get('Version')
//	IntlChar::getUnicodeVersion()                     PlatformRepository (lib-icu-unicode)
//	new Imagick() ->getVersion()                      PlatformRepository (lib-imagick-imagemagick)
//
// plus php_uname('s'|'n'|'r'|'v'|'m') (User-Agent, diagnose), xdebug_info
// ('mode'), ioncube_loader_iversion() and sys_get_temp_dir(). NewRuntime
// answers Runtime from a Snapshot; anything the probe does not record is
// a *NotProbedError, so a new call site fails loudly in tests. Errors PHP
// would throw come back as *PHPError with PHP 8's class and message (minus
// the "called in" location of a TypeError).
//
// The snapshot describes php itself: the symbols that Composer's bundled
// symfony/polyfill-* packages define in its process are not in it.
// Composer's code asks Runtime about none of them.
//
// The script is fed to php on its standard input, so it runs like
// bin/composer as a script file (auto_prepend_file applies), and it
// defines STDIN, STDOUT and STDERR, which PHP only defines for a script
// file. It records the extensions' info before changing any setting, as
// Core's info lists the local values.
//
// # Which php
//
// Composer runs on the php its `#!/usr/bin/env php` shebang finds, and
// has no setting (no COMPOSER_PHP, no config key) that makes it run on
// another; PHP_BINARY, PHP_PATH and PHP_PEAR_PHP_BIN only steer
// PhpExecutableFinder, which Composer uses to start PHP scripts
// (util.PhpExecutableFinder). maestro does the same: FindPHP is php on the
// PATH, and the plugin runtime uses the same binary.
//
// # No disk cache
//
// Probing costs about what starting php costs: 22 ms with the 58
// extensions of the dev shell's php 8.4, against 17 ms for `php -r 1` (4
// ms with -n), plus 1.5 ms to parse its 150 kB result. A cache keyed on
// the php binary, PHPRC, PHP_INI_SCAN_DIR and the ini files' mtimes would
// save those ~20 ms, but goes stale whenever a shared library (libcurl,
// ICU, OpenSSL, libxml) or an extension's .so is upgraded on its own,
// which changes lib-* versions and so dependency resolution, and no cheap
// key covers those. So maestro probes once per process: a Detector runs
// the probe at most once, and Start lets it run in the background while
// composer.json, the lock file and the repositories load.
//
// # Without php
//
// Composer cannot run without PHP, but maestro can do much without it.
// Only callers that need a value fail, with a *PHPNotFoundError
// ("maestro: platform detection requires PHP but no php binary was found
// in PATH"): PlatformRepository needs the php version unless
// config.platform.php overrides it, and needs the extensions unless
// platform requirements are ignored (--ignore-platform-reqs), so it can
// work with only the overridden packages; `diagnose`, `@php` scripts,
// plugins and `exec` need php. Elsewhere the absence is reported the way
// Composer reports an unknown value: "PHP unknown" in the User-Agent.
//
// # Composer's view
//
// bin/composer restarts php without xdebug unless COMPOSER_ALLOW_XDEBUG
// allows it, then raises memory_limit and points display_errors at
// stderr. Snapshot.ComposerView applies all that, so Composer's Runtime
// has no xdebug and PlatformRepository adds ext-xdebug with
// XdebugHandler::getSkippedVersion() after the other extensions, as in
// Composer, and ini_get answers what it answers in Composer's process.
// CheckBinComposer ports bin/composer's aborts (PHP < 7.2.5, HHVM 4,
// neither iconv nor mbstring).
package platform
