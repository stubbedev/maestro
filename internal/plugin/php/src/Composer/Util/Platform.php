<?php

/*
 * maestro's plugin shim: Composer\Util\Platform, reimplemented with
 * Composer 2.10.3's behaviour (docs/PLUGINS.md §4.8). It works on PHP's own
 * environment and working directory, which the sync engine keeps in step
 * with maestro's.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Util;

class Platform
{
    private static $isVirtualBoxGuest = null;
    private static $isWindowsSubsystemForLinux = null;
    private static $isDocker = null;

    private static function isVirtualBoxGuest(): bool
    {
        if (null === self::$isVirtualBoxGuest) {
            self::$isVirtualBoxGuest = false;
            if (self::isWindows()) {
                return self::$isVirtualBoxGuest;
            }

            if (function_exists('posix_getpwuid') && function_exists('posix_geteuid')) {
                $processUser = posix_getpwuid(posix_geteuid());
                if (is_array($processUser) && $processUser['name'] === 'vagrant') {
                    return self::$isVirtualBoxGuest = true;
                }
            }

            if (self::getEnv('COMPOSER_RUNTIME_ENV') === 'virtualbox') {
                return self::$isVirtualBoxGuest = true;
            }

            if (defined('PHP_OS_FAMILY') && PHP_OS_FAMILY === 'Linux') {
                $process = new ProcessExecutor();
                try {
                    if (0 === $process->execute(['lsmod'], $output) && str_contains($output, 'vboxguest')) {
                        return self::$isVirtualBoxGuest = true;
                    }
                } catch (\Exception $e) {
                    // noop
                }
            }
        }

        return self::$isVirtualBoxGuest;
    }

    public static function assertPharMetadataSafe(): void
    {
        if (\PHP_VERSION_ID >= 80000) {
            return;
        }

        if (self::getBoolEnv('COMPOSER_ALLOW_UNSAFE_PHAR_METADATA', false)) {
            return;
        }

        throw new \RuntimeException(
            'Refusing to parse a tar/phar archive on PHP < 8.0 because it is not safe to process untrusted archives on that PHP version. '
            .'Upgrade to PHP 8.0+ to remove this risk, or set COMPOSER_ALLOW_UNSAFE_PHAR_METADATA=1 to override (not recommended).'
        );
    }

    public static function clearEnv(string $name): void
    {
        putenv($name);
        unset($_SERVER[$name], $_ENV[$name]);
    }

    public static function expandPath(string $path): string
    {
        if (\Composer\Pcre\Preg::isMatch('#^~[\\/]#', $path)) {
            return self::getUserDirectory() . substr($path, 1);
        }

        return \Composer\Pcre\Preg::replaceCallback('#^(\$|(?P<percent>%))(?P<var>\w++)(?(percent)%)(?P<path>.*)#', static function ($matches): string {
            // Treat HOME as an alias for USERPROFILE on Windows for legacy reasons
            if (Platform::isWindows() && $matches['var'] === 'HOME') {
                if ((bool) Platform::getEnv('HOME')) {
                    return Platform::getEnv('HOME') . $matches['path'];
                }

                return Platform::getEnv('USERPROFILE') . $matches['path'];
            }

            return Platform::getEnv($matches['var']) . $matches['path'];
        }, $path);
    }

    public static function getBoolEnv(string $name, ?bool $default = null): ?bool
    {
        $value = self::getEnv($name);
        if (false === $value || '' === $value) {
            return $default;
        }

        if (!in_array($value, ['0', '1', 'false', 'true', 'off', 'on'], true)) {
            throw new \RuntimeException(
                "Invalid value for {$name}: {$value}. Expected 0, 1, false, true, off, or on."
            );
        }

        return in_array($value, ['1', 'true', 'on', ], true);
    }

    public static function getCwd(bool $allowEmpty = false): string
    {
        $cwd = getcwd();

        // fallback to realpath('') just in case this works but odds are it would break as well if we are in a case where getcwd fails
        if (false === $cwd) {
            $cwd = realpath('');
        }

        // crappy state, assume '' and hopefully relative paths allow things to continue
        if (false === $cwd) {
            if ($allowEmpty) {
                return '';
            }

            throw new \RuntimeException('Could not determine the current working directory');
        }

        return $cwd;
    }

    public static function getDevNull(): string
    {
        if (self::isWindows()) {
            return 'NUL';
        }

        return '/dev/null';
    }

    public static function getEnv(string $name)
    {
        if (array_key_exists($name, $_SERVER)) {
            return (string) $_SERVER[$name];
        }
        if (array_key_exists($name, $_ENV)) {
            return (string) $_ENV[$name];
        }

        return getenv($name);
    }

    public static function getUserDirectory(): string
    {
        if (false !== ($home = self::getEnv('HOME'))) {
            return $home;
        }

        if (self::isWindows() && false !== ($home = self::getEnv('USERPROFILE'))) {
            return $home;
        }

        if (\function_exists('posix_getuid') && \function_exists('posix_getpwuid')) {
            $info = posix_getpwuid(posix_getuid());

            if (is_array($info)) {
                return $info['dir'];
            }
        }

        throw new \RuntimeException('Could not determine user directory');
    }

    public static function isDocker(): bool
    {
        if (null !== self::$isDocker) {
            return self::$isDocker;
        }

        // cannot check so assume no
        if ((bool) ini_get('open_basedir')) {
            return self::$isDocker = false;
        }

        // .dockerenv and .containerenv are present in some cases but not reliably
        if (file_exists('/.dockerenv') || file_exists('/run/.containerenv') || file_exists('/var/run/.containerenv')) {
            return self::$isDocker = true;
        }

        // see https://www.baeldung.com/linux/is-process-running-inside-container
        $cgroups = [
            '/proc/self/mountinfo', // cgroup v2
            '/proc/1/cgroup', // cgroup v1
        ];
        foreach ($cgroups as $cgroup) {
            if (!is_readable($cgroup)) {
                continue;
            }
            // suppress errors as some environments have these files as readable but system restrictions prevent the read from succeeding
            // see https://github.com/composer/composer/issues/12095
            try {
                $data = @file_get_contents($cgroup);
            } catch (\Throwable $e) {
                break;
            }
            if (!is_string($data)) {
                continue;
            }
            // detect default mount points created by Docker/containerd
            if (str_contains($data, '/var/lib/docker/') || str_contains($data, '/io.containerd.snapshotter')) {
                return self::$isDocker = true;
            }
        }

        return self::$isDocker = false;
    }

    public static function isInputCompletionProcess(): bool
    {
        return '_complete' === ($_SERVER['argv'][1] ?? null);
    }

    public static function isTty($fd = null): bool
    {
        if ($fd === null) {
            $fd = defined('STDOUT') ? STDOUT : fopen('php://stdout', 'w');
            if ($fd === false) {
                return false;
            }
        }

        // detect msysgit/mingw and assume this is a tty because detection
        // does not work correctly, see https://github.com/composer/composer/issues/9690
        if (in_array(strtoupper((string) self::getEnv('MSYSTEM')), ['MINGW32', 'MINGW64'], true)) {
            return true;
        }

        // modern cross-platform function, includes the fstat
        // fallback so if it is present we trust it
        if (function_exists('stream_isatty')) {
            return stream_isatty($fd);
        }

        // only trusting this if it is positive, otherwise prefer fstat fallback
        if (function_exists('posix_isatty') && posix_isatty($fd)) {
            return true;
        }

        $stat = @fstat($fd);
        if ($stat === false) {
            return false;
        }

        // Check if formatted mode is S_IFCHR
        return 0020000 === ($stat['mode'] & 0170000);
    }

    public static function isWindows(): bool
    {
        return \defined('PHP_WINDOWS_VERSION_BUILD');
    }

    public static function isWindowsSubsystemForLinux(): bool
    {
        if (null === self::$isWindowsSubsystemForLinux) {
            self::$isWindowsSubsystemForLinux = false;

            // while WSL will be hosted within windows, WSL itself cannot be windows based itself.
            if (self::isWindows()) {
                return self::$isWindowsSubsystemForLinux = false;
            }

            if (
                !(bool) ini_get('open_basedir')
                && is_readable('/proc/version')
                && false !== stripos((string) Silencer::call('file_get_contents', '/proc/version'), 'microsoft')
                && !self::isDocker() // Docker and Podman running inside WSL should not be seen as WSL
            ) {
                return self::$isWindowsSubsystemForLinux = true;
            }
        }

        return self::$isWindowsSubsystemForLinux;
    }

    public static function putEnv(string $name, string $value): void
    {
        putenv($name . '=' . $value);
        $_SERVER[$name] = $_ENV[$name] = $value;
    }

    public static function realpath(string $path): string
    {
        $realPath = realpath($path);
        if ($realPath === false) {
            return $path;
        }

        return $realPath;
    }

    public static function strlen(string $str): int
    {
        static $useMbString = null;
        if (null === $useMbString) {
            $useMbString = \function_exists('mb_strlen') && (bool) ini_get('mbstring.func_overload');
        }

        if ($useMbString) {
            return mb_strlen($str, '8bit');
        }

        return \strlen($str);
    }

    public static function workaroundFilesystemIssues(): void
    {
        if (self::isVirtualBoxGuest()) {
            usleep(200000);
        }
    }
}
