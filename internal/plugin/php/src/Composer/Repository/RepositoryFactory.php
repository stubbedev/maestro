<?php

/*
 * maestro's plugin shim: Composer\Repository\RepositoryFactory
 * (docs/PLUGINS.md §4.6), reimplemented with Composer 2.10.3's behaviour:
 * the repositories are maestro's (RepositoryManager::createRepository),
 * and so is the manager it creates (repofactory.manager) and the parsing
 * of a repository string (repofactory.configFromString).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Repository;

use Composer\Config;
use Composer\EventDispatcher\EventDispatcher;
use Composer\Factory;
use Composer\IO\IOInterface;
use Composer\Json\JsonFile;
use Composer\Pcre\Preg;
use Composer\Util\HttpDownloader;
use Composer\Util\ProcessExecutor;
use Maestro\Shim\Rpc;

class RepositoryFactory
{
    public static function configFromString(IOInterface $io, Config $config, string $repository, bool $allowFilesystem = false)
    {
        $repoConfig = Rpc::call('repofactory.configFromString', [$io, $config, $repository, $allowFilesystem]);
        if (is_array($repoConfig) && isset($repoConfig['type']) && $repoConfig['type'] === 'filesystem') {
            $repoConfig['json'] = new JsonFile($repoConfig['json'], Factory::createHttpDownloader($io, $config));
        }

        return $repoConfig;
    }

    public static function fromString(IOInterface $io, Config $config, string $repository, bool $allowFilesystem = false, ?RepositoryManager $rm = null): RepositoryInterface
    {
        $repoConfig = static::configFromString($io, $config, $repository, $allowFilesystem);

        return static::createRepo($io, $config, $repoConfig, $rm);
    }

    public static function createRepo(IOInterface $io, Config $config, array $repoConfig, ?RepositoryManager $rm = null): RepositoryInterface
    {
        if (!$rm) {
            @trigger_error('Not passing a repository manager when calling createRepo is deprecated since Composer 2.3.6', E_USER_DEPRECATED); // @line 70
            $rm = static::manager($io, $config);
        }
        $repos = self::createRepos($rm, [$repoConfig]);

        return reset($repos);
    }

    public static function defaultRepos(?IOInterface $io = null, ?Config $config = null, ?RepositoryManager $rm = null): array
    {
        if (null === $rm) {
            @trigger_error('Not passing a repository manager when calling defaultRepos is deprecated since Composer 2.3.6, use defaultReposWithDefaultManager() instead if you cannot get a manager.', E_USER_DEPRECATED); // @line 84
        }

        if (null === $config) {
            $config = Factory::createConfig($io);
        }
        if (null !== $io) {
            $io->loadConfiguration($config);
        }
        if (null === $rm) {
            if (null === $io) {
                throw new \InvalidArgumentException('This function requires either an IOInterface or a RepositoryManager');
            }
            $rm = static::manager($io, $config, Factory::createHttpDownloader($io, $config));
        }

        return self::createRepos($rm, $config->getRepositories());
    }

    public static function manager(IOInterface $io, Config $config, ?HttpDownloader $httpDownloader = null, ?EventDispatcher $eventDispatcher = null, ?ProcessExecutor $process = null): RepositoryManager
    {
        if ($httpDownloader === null) {
            $httpDownloader = Factory::createHttpDownloader($io, $config);
        }

        return Rpc::call('repofactory.manager', [$io, $config, $httpDownloader, $eventDispatcher]);
    }

    public static function defaultReposWithDefaultManager(IOInterface $io): array
    {
        $manager = RepositoryFactory::manager($io, $config = Factory::createConfig($io));
        $io->loadConfiguration($config);

        return RepositoryFactory::defaultRepos($io, $config, $manager);
    }

    /**
     * @param array<int|string, mixed> $repoConfigs
     * @return RepositoryInterface[]
     */
    private static function createRepos(RepositoryManager $rm, array $repoConfigs): array
    {
        $repos = [];

        foreach ($repoConfigs as $index => $repo) {
            if (is_string($repo)) {
                throw new \UnexpectedValueException('"repositories" should be an array of repository definitions, only a single repository was given');
            }
            if (!is_array($repo)) {
                throw new \UnexpectedValueException('Repository "'.$index.'" ('.json_encode($repo).') should be an array, '.get_debug_type($repo).' given');
            }
            if (!isset($repo['type'])) {
                throw new \UnexpectedValueException('Repository "'.$index.'" ('.json_encode($repo).') must have a type defined');
            }

            $name = self::generateRepositoryName($index, $repo, $repos);
            if ($repo['type'] === 'filesystem') {
                $repos[$name] = new FilesystemRepository($repo['json']);
            } else {
                $repos[$name] = $rm->createRepository($repo['type'], $repo, (string) $index);
            }
        }

        return $repos;
    }

    public static function generateRepositoryName($index, array $repo, array $existingRepos): string
    {
        $name = is_int($index) && isset($repo['url']) ? Preg::replace('{^https?://}i', '', $repo['url']) : (string) $index;
        while (isset($existingRepos[$name])) {
            $name .= '2';
        }

        return $name;
    }
}
