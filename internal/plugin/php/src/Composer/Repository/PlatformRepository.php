<?php

/*
 * maestro's plugin shim: Composer\Repository\PlatformRepository
 * (docs/PLUGINS.md §4.6). maestro's platform repositories are proxies of
 * maestro's (see ArrayRepository); one created in PHP is maestro's too
 * (repo.newPlatform detects the platform, as Composer's constructor does).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Repository;

class PlatformRepository extends \Composer\Repository\ArrayRepository
{
    public const PLATFORM_PACKAGE_REGEX = '{^(?:php(?:-64bit|-ipv6|-zts|-debug)?|hhvm|(?:ext|lib)-[a-z0-9](?:[_.-]?[a-z0-9]+)*|composer(?:-(?:plugin|runtime)-api)?)$}iD';

    public function __construct(array $packages = [], array $overrides = [], ?\Composer\Platform\Runtime $runtime = null, ?\Composer\Platform\HhvmDetector $hhvmDetector = null)
    {
        \Maestro\Shim\Rpc::call('repo.newPlatform', [$this, $packages, $overrides]);
    }

    public function addPackage(\Composer\Package\PackageInterface $package): void
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Rpc::call('repo.addPackage', [$this, $package]);

            return;
        }
        \Maestro\Shim\Remote::unsupported(self::class, 'addPackage');
    }

    public function getDisabledPackages(): array
    {
        return \Maestro\Shim\Rpc::call('repo.getDisabledPackages', [$this]);
    }

    public static function getPlatformPhpVersion(): ?string
    {
        return \Maestro\Shim\Rpc::call('repo.getPlatformPhpVersion', []);
    }

    public function getRepoName(): string
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            return \Maestro\Shim\Rpc::call('repo.getRepoName', [$this]);
        }
        \Maestro\Shim\Remote::unsupported(self::class, 'getRepoName');
    }

    protected function initialize(): void
    {
        if (!\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Remote::unsupported(self::class, 'initialize');
        }
    }

    public static function isPlatformPackage(string $name): bool
    {
        static $cache = [];

        if (isset($cache[$name])) {
            return $cache[$name];
        }

        return $cache[$name] = \Composer\Pcre\Preg::isMatch(PlatformRepository::PLATFORM_PACKAGE_REGEX, $name);
    }

    public function isPlatformPackageDisabled(string $name): bool
    {
        return \Maestro\Shim\Rpc::call('repo.isPlatformPackageDisabled', [$this, $name]);
    }

    public function search(string $query, int $mode = 0, ?string $type = null): array
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            return \Maestro\Shim\Rpc::call('repo.search', [$this, $query, $mode, $type]);
        }
        \Maestro\Shim\Remote::unsupported(self::class, 'search');
    }
}
