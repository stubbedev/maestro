<?php

/*
 * maestro's plugin shim: Composer\Factory (docs/PLUGINS.md §4.2). The file
 * names honour maestro's environment (factory.*), which PHP's putenv()
 * changes reach through the sync engine.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer;

class Factory
{
    protected function addLocalRepository(\Composer\IO\IOInterface $io, \Composer\Repository\RepositoryManager $rm, string $vendorDir, \Composer\Package\RootPackageInterface $rootPackage, ?\Composer\Util\ProcessExecutor $process = null): void
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Factory::addLocalRepository() in plugins yet');
    }

    public static function create(\Composer\IO\IOInterface $io, $config = null, $disablePlugins = false, bool $disableScripts = false): \Composer\Composer
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Factory::create() in plugins yet');
    }

    public static function createAdditionalStyles(): array
    {
        return [
            'highlight' => new \Symfony\Component\Console\Formatter\OutputFormatterStyle('red'),
            'warning' => new \Symfony\Component\Console\Formatter\OutputFormatterStyle('black', 'yellow'),
        ];
    }

    public function createArchiveManager(\Composer\Config $config, \Composer\Downloader\DownloadManager $dm, \Composer\Util\Loop $loop)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Factory::createArchiveManager() in plugins yet');
    }

    public function createComposer(\Composer\IO\IOInterface $io, $localConfig = null, $disablePlugins = false, ?string $cwd = null, bool $fullLoad = true, bool $disableScripts = false)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Factory::createComposer() in plugins yet');
    }

    public static function createConfig(?\Composer\IO\IOInterface $io = null, ?string $cwd = null): \Composer\Config
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Factory::createConfig() in plugins yet');
    }

    protected function createDefaultInstallers(\Composer\Installer\InstallationManager $im, \Composer\PartialComposer $composer, \Composer\IO\IOInterface $io, ?\Composer\Util\ProcessExecutor $process = null): void
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Factory::createDefaultInstallers() in plugins yet');
    }

    public function createDownloadManager(\Composer\IO\IOInterface $io, \Composer\Config $config, \Composer\Util\HttpDownloader $httpDownloader, \Composer\Util\ProcessExecutor $process, ?\Composer\EventDispatcher\EventDispatcher $eventDispatcher = null): \Composer\Downloader\DownloadManager
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Factory::createDownloadManager() in plugins yet');
    }

    public static function createGlobal(\Composer\IO\IOInterface $io, bool $disablePlugins = false, bool $disableScripts = false): ?\Composer\Composer
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Factory::createGlobal() in plugins yet');
    }

    protected function createGlobalComposer(\Composer\IO\IOInterface $io, \Composer\Config $config, $disablePlugins, bool $disableScripts, bool $fullLoad = false): ?\Composer\PartialComposer
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Factory::createGlobalComposer() in plugins yet');
    }

    public static function createHttpDownloader(\Composer\IO\IOInterface $io, \Composer\Config $config, array $options = []): \Composer\Util\HttpDownloader
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Factory::createHttpDownloader() in plugins yet');
    }

    public function createInstallationManager(\Composer\Util\Loop $loop, \Composer\IO\IOInterface $io, ?\Composer\EventDispatcher\EventDispatcher $eventDispatcher = null): \Composer\Installer\InstallationManager
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Factory::createInstallationManager() in plugins yet');
    }

    public static function createOutput(): \Symfony\Component\Console\Output\ConsoleOutput
    {
        $styles = self::createAdditionalStyles();
        $formatter = new \Symfony\Component\Console\Formatter\OutputFormatter(false, $styles);

        return new \Symfony\Component\Console\Output\ConsoleOutput(\Symfony\Component\Console\Output\ConsoleOutput::VERBOSITY_NORMAL, null, $formatter);
    }

    protected function createPluginManager(\Composer\IO\IOInterface $io, \Composer\Composer $composer, ?\Composer\PartialComposer $globalComposer = null, $disablePlugins = false): \Composer\Plugin\PluginManager
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Factory::createPluginManager() in plugins yet');
    }

    protected static function getCacheDir(string $home): string
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Factory::getCacheDir() in plugins yet');
    }

    public static function getComposerFile(): string
    {
        return \Maestro\Shim\Rpc::call('factory.getComposerFile', []);
    }

    protected static function getDataDir(string $home): string
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Factory::getDataDir() in plugins yet');
    }

    protected static function getHomeDir(): string
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Factory::getHomeDir() in plugins yet');
    }

    public static function getLockFile(string $composerFile): string
    {
        return \Maestro\Shim\Rpc::call('factory.getLockFile', [$composerFile]);
    }

    protected function loadRootPackage(\Composer\Repository\RepositoryManager $rm, \Composer\Config $config, \Composer\Package\Version\VersionParser $parser, \Composer\Package\Version\VersionGuesser $guesser, \Composer\IO\IOInterface $io): \Composer\Package\Loader\RootPackageLoader
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Factory::loadRootPackage() in plugins yet');
    }

    protected function purgePackages(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\Installer\InstallationManager $im): void
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Factory::purgePackages() in plugins yet');
    }
}
