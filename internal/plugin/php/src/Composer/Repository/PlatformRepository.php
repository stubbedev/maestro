<?php

/*
 * maestro's plugin shim: Composer\Repository\PlatformRepository
 * (docs/PLUGINS.md §4.6). maestro's platform repositories are proxies of
 * maestro's (see ArrayRepository); creating one in PHP is not supported yet.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Repository;

class PlatformRepository extends \Composer\Repository\ArrayRepository
{
    public const PLATFORM_PACKAGE_REGEX = '{^(?:php(?:-64bit|-ipv6|-zts|-debug)?|hhvm|(?:ext|lib)-[a-z0-9](?:[_.-]?[a-z0-9]+)*|composer(?:-(?:plugin|runtime)-api)?)$}iD';

    public function __construct(array $packages = [], array $overrides = [], ?\Composer\Platform\Runtime $runtime = null, ?\Composer\Platform\HhvmDetector $hhvmDetector = null)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Repository\\PlatformRepository::__construct() in plugins yet');
    }

    public function addPackage(\Composer\Package\PackageInterface $package): void
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Repository\\PlatformRepository::addPackage() in plugins yet');
    }

    public function getDisabledPackages(): array
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Repository\\PlatformRepository::getDisabledPackages() in plugins yet');
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
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Repository\\PlatformRepository::initialize() in plugins yet');
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
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Repository\\PlatformRepository::isPlatformPackageDisabled() in plugins yet');
    }

    public function search(string $query, int $mode = 0, ?string $type = null): array
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            return \Maestro\Shim\Rpc::call('repo.search', [$this, $query, $mode, $type]);
        }
        \Maestro\Shim\Remote::unsupported(self::class, 'search');
    }
}
