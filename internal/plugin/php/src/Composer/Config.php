<?php

/*
 * maestro's plugin shim: Composer\Config (docs/PLUGINS.md §4.2). maestro's
 * Config is a service proxy: every call is maestro's (config.*), without
 * caching, since the configuration can change (merge()).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer;

use Composer\Config\ConfigSourceInterface;
use Composer\IO\IOInterface;
use Composer\Util\ProcessExecutor;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class Config
{
    public const SOURCE_DEFAULT = 'default';
    public const SOURCE_COMMAND = 'command';
    public const SOURCE_UNKNOWN = 'unknown';

    public const RELATIVE_PATHS = 1;

    public static $defaultConfig = ['process-timeout' => 300, 'use-include-path' => false, 'allow-plugins' => [], 'use-parent-dir' => 'prompt', 'preferred-install' => 'dist', 'audit' => ['ignore' => [], 'abandoned' => 'fail'], 'policy' => true, 'notify-on-install' => true, 'github-protocols' => ['https', 'ssh', 'git'], 'gitlab-protocol' => null, 'vendor-dir' => 'vendor', 'bin-dir' => '{$vendor-dir}/bin', 'cache-dir' => '{$home}/cache', 'data-dir' => '{$home}', 'cache-files-dir' => '{$cache-dir}/files', 'cache-repo-dir' => '{$cache-dir}/repo', 'cache-vcs-dir' => '{$cache-dir}/vcs', 'cache-ttl' => 15552000, 'cache-files-ttl' => null, 'cache-files-maxsize' => '300MiB', 'cache-read-only' => false, 'bin-compat' => 'auto', 'discard-changes' => false, 'autoloader-suffix' => null, 'sort-packages' => false, 'optimize-autoloader' => false, 'classmap-authoritative' => false, 'apcu-autoloader' => false, 'prepend-autoloader' => true, 'update-with-minimal-changes' => false, 'github-domains' => ['github.com'], 'bitbucket-expose-hostname' => true, 'disable-tls' => false, 'secure-http' => true, 'secure-svn-domains' => [], 'cafile' => null, 'capath' => null, 'github-expose-hostname' => true, 'gitlab-domains' => ['gitlab.com'], 'store-auths' => 'prompt', 'platform' => [], 'archive-format' => 'tar', 'archive-dir' => '.', 'htaccess-protect' => true, 'use-github-api' => true, 'lock' => true, 'platform-check' => 'php-only', 'bitbucket-oauth' => [], 'github-oauth' => [], 'gitlab-oauth' => [], 'gitlab-token' => [], 'http-basic' => [], 'bearer' => [], 'custom-headers' => [], 'bump-after-update' => false, 'allow-missing-requirements' => false, 'client-certificate' => [], 'forgejo-domains' => ['codeberg.org'], 'forgejo-token' => [], 'source-fallback' => false];

    public static $defaultRepositories = ['packagist.org' => ['type' => 'composer', 'url' => 'https://repo.packagist.org']];

    private $config;
    private $baseDir;
    private $repositories;
    private $configSource;
    private $authConfigSource;
    private $localAuthConfigSource = null;
    private $useEnvironment;
    private $warnedHosts = [];

    public function __construct(bool $useEnvironment = true, ?string $baseDir = null)
    {
        Remote::unsupported(self::class, '__construct');
    }

    public function setConfigSource(ConfigSourceInterface $source): void
    {
        Rpc::call('config.setConfigSource', [$this, $source]);
    }

    public function getConfigSource(): ConfigSourceInterface
    {
        return Rpc::call('config.getConfigSource', [$this]);
    }

    public function setAuthConfigSource(ConfigSourceInterface $source): void
    {
        Rpc::call('config.setAuthConfigSource', [$this, $source]);
    }

    public function getAuthConfigSource(): ConfigSourceInterface
    {
        return Rpc::call('config.getAuthConfigSource', [$this]);
    }

    public function setLocalAuthConfigSource(ConfigSourceInterface $source): void
    {
        Rpc::call('config.setLocalAuthConfigSource', [$this, $source]);
    }

    public function getLocalAuthConfigSource(): ?ConfigSourceInterface
    {
        return Rpc::call('config.getLocalAuthConfigSource', [$this]);
    }

    public function merge(array $config, string $source = self::SOURCE_UNKNOWN): void
    {
        Rpc::call('config.merge', [$this, $config, $source]);
    }

    public function getRepositories(): array
    {
        return Rpc::call('config.getRepositories', [$this]);
    }

    public function get(string $key, int $flags = 0)
    {
        return Rpc::call('config.get', [$this, $key, $flags]);
    }

    public function all(int $flags = 0): array
    {
        return Rpc::call('config.all', [$this, $flags]);
    }

    public function getSourceOfValue(string $key): string
    {
        return Rpc::call('config.getSourceOfValue', [$this, $key]);
    }

    public function raw(): array
    {
        return Rpc::call('config.raw', [$this]);
    }

    public function has(string $key): bool
    {
        return Rpc::call('config.has', [$this, $key]);
    }

    public function prohibitUrlByConfig(string $url, ?IOInterface $io = null, array $repoOptions = []): void
    {
        Rpc::call('config.prohibitUrlByConfig', [$this, $url, $io, $repoOptions]);
    }

    public function setBaseDir(?string $baseDir): void
    {
        Rpc::call('config.setBaseDir', [$this, $baseDir]);
    }

    public static function disableProcessTimeout(): void
    {
        // Override global timeout set earlier by environment or config
        ProcessExecutor::setTimeout(0);
    }
}
