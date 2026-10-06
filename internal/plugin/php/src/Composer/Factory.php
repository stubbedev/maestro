<?php

/*
 * maestro's plugin shim: Composer\Factory (docs/PLUGINS.md §4.2, §5.11).
 * The file names honour maestro's environment (factory.*), which PHP's
 * putenv() changes reach through the sync engine; the Composer instances
 * and configs are maestro's, created re-entrantly (their plugins load in
 * this same process). The helpers Factory builds a Composer with are
 * Composer's, on maestro's services; maestro's own Factory builds the
 * Composer instances, so a subclass's overrides of them are not used by
 * createComposer().
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer;

class Factory
{
    protected function addLocalRepository(\Composer\IO\IOInterface $io, \Composer\Repository\RepositoryManager $rm, string $vendorDir, \Composer\Package\RootPackageInterface $rootPackage, ?\Composer\Util\ProcessExecutor $process = null): void
    {
        $fs = null;
        if ($process) {
            $fs = new \Composer\Util\Filesystem($process);
        }

        $rm->setLocalRepository(new \Composer\Repository\InstalledFilesystemRepository(new \Composer\Json\JsonFile($vendorDir.'/composer/installed.json', null, $io), true, $rootPackage, $fs));
    }

    public static function create(\Composer\IO\IOInterface $io, $config = null, $disablePlugins = false, bool $disableScripts = false): \Composer\Composer
    {
        return \Maestro\Shim\Rpc::call('factory.create', [$io, $config, $disablePlugins, $disableScripts]);
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
        // maestro's ArchiveManager with its archivers.
        return \Maestro\Shim\Rpc::call('factory.createArchiveManager', [$config, $dm, $loop]);
    }

    public function createComposer(\Composer\IO\IOInterface $io, $localConfig = null, $disablePlugins = false, ?string $cwd = null, bool $fullLoad = true, bool $disableScripts = false)
    {
        return \Maestro\Shim\Rpc::call('factory.createComposer', [$io, $localConfig, $disablePlugins, $cwd, $fullLoad, $disableScripts]);
    }

    public static function createConfig(?\Composer\IO\IOInterface $io = null, ?string $cwd = null): \Composer\Config
    {
        return \Maestro\Shim\Rpc::call('factory.createConfig', [$io, $cwd]);
    }

    protected function createDefaultInstallers(\Composer\Installer\InstallationManager $im, \Composer\PartialComposer $composer, \Composer\IO\IOInterface $io, ?\Composer\Util\ProcessExecutor $process = null): void
    {
        $fs = new \Composer\Util\Filesystem($process);
        $binaryInstaller = new \Composer\Installer\BinaryInstaller($io, rtrim($composer->getConfig()->get('bin-dir'), '/'), $composer->getConfig()->get('bin-compat'), $fs, rtrim($composer->getConfig()->get('vendor-dir'), '/'));

        $im->addInstaller(new \Composer\Installer\LibraryInstaller($io, $composer, null, $fs, $binaryInstaller));
        $im->addInstaller(new \Composer\Installer\PluginInstaller($io, $composer, $fs, $binaryInstaller));
        $im->addInstaller(new \Composer\Installer\MetapackageInstaller($io));
    }

    public function createDownloadManager(\Composer\IO\IOInterface $io, \Composer\Config $config, \Composer\Util\HttpDownloader $httpDownloader, \Composer\Util\ProcessExecutor $process, ?\Composer\EventDispatcher\EventDispatcher $eventDispatcher = null): \Composer\Downloader\DownloadManager
    {
        // maestro's DownloadManager with its downloaders, set up as
        // Factory sets one up.
        return \Maestro\Shim\Rpc::call('factory.createDownloadManager', [$io, $config, $httpDownloader, $process, $eventDispatcher]);
    }

    public static function createGlobal(\Composer\IO\IOInterface $io, bool $disablePlugins = false, bool $disableScripts = false): ?\Composer\Composer
    {
        return \Maestro\Shim\Rpc::call('factory.createGlobal', [$io, $disablePlugins, $disableScripts]);
    }

    protected function createGlobalComposer(\Composer\IO\IOInterface $io, \Composer\Config $config, $disablePlugins, bool $disableScripts, bool $fullLoad = false): ?\Composer\PartialComposer
    {
        // make sure if disable plugins was 'local' it is now turned off
        $disablePlugins = $disablePlugins === 'global' || $disablePlugins === true;

        $composer = null;
        try {
            $composer = $this->createComposer($io, $config->get('home') . '/composer.json', $disablePlugins, $config->get('home'), $fullLoad, $disableScripts);
        } catch (\Exception $e) {
            $io->writeError('Failed to initialize global composer: '.$e->getMessage(), true, \Composer\IO\IOInterface::DEBUG);
        }

        return $composer;
    }

    public static function createHttpDownloader(\Composer\IO\IOInterface $io, \Composer\Config $config, array $options = []): \Composer\Util\HttpDownloader
    {
        return \Maestro\Shim\Rpc::call('factory.createHttpDownloader', [$io, $config, $options]);
    }

    public function createInstallationManager(\Composer\Util\Loop $loop, \Composer\IO\IOInterface $io, ?\Composer\EventDispatcher\EventDispatcher $eventDispatcher = null): \Composer\Installer\InstallationManager
    {
        return new \Composer\Installer\InstallationManager($loop, $io, $eventDispatcher);
    }

    public static function createOutput(): \Symfony\Component\Console\Output\ConsoleOutput
    {
        $styles = self::createAdditionalStyles();
        $formatter = new \Symfony\Component\Console\Formatter\OutputFormatter(false, $styles);

        return new \Symfony\Component\Console\Output\ConsoleOutput(\Symfony\Component\Console\Output\ConsoleOutput::VERBOSITY_NORMAL, null, $formatter);
    }

    protected function createPluginManager(\Composer\IO\IOInterface $io, \Composer\Composer $composer, ?\Composer\PartialComposer $globalComposer = null, $disablePlugins = false): \Composer\Plugin\PluginManager
    {
        return new \Composer\Plugin\PluginManager($io, $composer, $globalComposer, $disablePlugins);
    }

    protected static function getCacheDir(string $home): string
    {
        return \Maestro\Shim\Rpc::call('factory.getCacheDir', [$home]);
    }

    public static function getComposerFile(): string
    {
        return \Maestro\Shim\Rpc::call('factory.getComposerFile', []);
    }

    protected static function getDataDir(string $home): string
    {
        return \Maestro\Shim\Rpc::call('factory.getDataDir', [$home]);
    }

    protected static function getHomeDir(): string
    {
        return \Maestro\Shim\Rpc::call('factory.getHomeDir', []);
    }

    public static function getLockFile(string $composerFile): string
    {
        return \Maestro\Shim\Rpc::call('factory.getLockFile', [$composerFile]);
    }

    protected function loadRootPackage(\Composer\Repository\RepositoryManager $rm, \Composer\Config $config, \Composer\Package\Version\VersionParser $parser, \Composer\Package\Version\VersionGuesser $guesser, \Composer\IO\IOInterface $io): \Composer\Package\Loader\RootPackageLoader
    {
        return new \Composer\Package\Loader\RootPackageLoader($rm, $config, $parser, $guesser, $io);
    }

    protected function purgePackages(\Composer\Repository\InstalledRepositoryInterface $repo, \Composer\Installer\InstallationManager $im): void
    {
        foreach ($repo->getPackages() as $package) {
            if (!$im->isPackageInstalled($repo, $package)) {
                $repo->removePackage($package);
            }
        }
    }
}
