<?php

/*
 * maestro's plugin shim: Composer\Autoload\AutoloadGenerator
 * (docs/PLUGINS.md §4.9), a service proxy of maestro's (ag.*). createLoader()
 * builds a real ClassLoader here from the autoloads, as Composer does; the
 * class map is scanned by maestro.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Autoload;

class AutoloadGenerator
{
    public function __construct(\Composer\EventDispatcher\EventDispatcher $eventDispatcher, ?\Composer\IO\IOInterface $io = null)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Autoload\\AutoloadGenerator::__construct() in plugins yet');
    }

    public function buildPackageMap(\Composer\Installer\InstallationManager $installationManager, \Composer\Package\PackageInterface $rootPackage, array $packages)
    {
        return \Maestro\Shim\Rpc::call('ag.buildPackageMap', [$this, $installationManager, $rootPackage, $packages]);
    }

    public function createLoader(array $autoloads, ?string $vendorDir = null)
    {
        $loader = new ClassLoader($vendorDir);

        if (isset($autoloads['psr-0'])) {
            foreach ($autoloads['psr-0'] as $namespace => $path) {
                $loader->add($namespace, $path);
            }
        }

        if (isset($autoloads['psr-4'])) {
            foreach ($autoloads['psr-4'] as $namespace => $path) {
                $loader->addPsr4($namespace, $path);
            }
        }

        if (isset($autoloads['classmap'])) {
            $excluded = [];
            if (!empty($autoloads['exclude-from-classmap'])) {
                $excluded = $autoloads['exclude-from-classmap'];
            }

            // maestro scans the class map (writing the warnings Composer does).
            $loader->addClassMap(\Maestro\Shim\Rpc::call('ag.classMap', [$this, $autoloads['classmap'], $excluded]));
        }

        return $loader;
    }

    public function dump(\Composer\Config $config, \Composer\Repository\InstalledRepositoryInterface $localRepo, \Composer\Package\RootPackageInterface $rootPackage, \Composer\Installer\InstallationManager $installationManager, string $targetDir, bool $scanPsrPackages = false, ?string $suffix = null, ?\Composer\Package\Locker $locker = null, bool $strictAmbiguous = false)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Autoload\\AutoloadGenerator::dump() in plugins yet');
    }

    protected function filterPackageMap(array $packageMap, \Composer\Package\RootPackageInterface $rootPackage)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Autoload\\AutoloadGenerator::filterPackageMap() in plugins yet');
    }

    protected function getAutoloadFile(string $vendorPathToTargetDirCode, string $suffix)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Autoload\\AutoloadGenerator::getAutoloadFile() in plugins yet');
    }

    protected function getAutoloadRealFile(bool $useClassMap, bool $useIncludePath, ?string $targetDirLoader, bool $useIncludeFiles, string $vendorPathCode, string $appBaseDirCode, string $suffix, bool $useGlobalIncludePath, string $prependAutoloader, bool $checkPlatform)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Autoload\\AutoloadGenerator::getAutoloadRealFile() in plugins yet');
    }

    protected function getFileIdentifier(\Composer\Package\PackageInterface $package, string $path)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Autoload\\AutoloadGenerator::getFileIdentifier() in plugins yet');
    }

    protected function getIncludeFilesFile(array $files, \Composer\Util\Filesystem $filesystem, string $basePath, string $vendorPath, string $vendorPathCode, string $appBaseDirCode)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Autoload\\AutoloadGenerator::getIncludeFilesFile() in plugins yet');
    }

    protected function getIncludePathsFile(array $packageMap, \Composer\Util\Filesystem $filesystem, string $basePath, string $vendorPath, string $vendorPathCode, string $appBaseDirCode)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Autoload\\AutoloadGenerator::getIncludePathsFile() in plugins yet');
    }

    protected function getPathCode(\Composer\Util\Filesystem $filesystem, string $basePath, string $vendorPath, string $path)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Autoload\\AutoloadGenerator::getPathCode() in plugins yet');
    }

    protected function getPlatformCheck(array $packageMap, $checkPlatform, array $devPackageNames)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Autoload\\AutoloadGenerator::getPlatformCheck() in plugins yet');
    }

    protected function getStaticFile(string $suffix, string $targetDir, string $vendorPath, string $basePath)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Autoload\\AutoloadGenerator::getStaticFile() in plugins yet');
    }

    public function parseAutoloads(array $packageMap, \Composer\Package\PackageInterface $rootPackage, $filteredDevPackages = false)
    {
        return \Maestro\Shim\Rpc::call('ag.parseAutoloads', [$this, $packageMap, $rootPackage, $filteredDevPackages]);
    }

    protected function parseAutoloadsType(array $packageMap, string $type, \Composer\Package\RootPackageInterface $rootPackage)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Autoload\\AutoloadGenerator::parseAutoloadsType() in plugins yet');
    }

    public function setApcu(bool $apcu, ?string $apcuPrefix = null)
    {
        \Maestro\Shim\Rpc::call('ag.setApcu', [$this, $apcu, $apcuPrefix]);
    }

    public function setClassMapAuthoritative(bool $classMapAuthoritative)
    {
        \Maestro\Shim\Rpc::call('ag.setClassMapAuthoritative', [$this, $classMapAuthoritative]);
    }

    public function setDevMode(bool $devMode = true)
    {
        \Maestro\Shim\Rpc::call('ag.setDevMode', [$this, $devMode]);
    }

    public function setDryRun(bool $dryRun = true): void
    {
        \Maestro\Shim\Rpc::call('ag.setDryRun', [$this, $dryRun]);
    }

    public function setIgnorePlatformRequirements($ignorePlatformReqs)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Autoload\\AutoloadGenerator::setIgnorePlatformRequirements() in plugins yet');
    }

    public function setPlatformRequirementFilter(\Composer\Filter\PlatformRequirementFilter\PlatformRequirementFilterInterface $platformRequirementFilter)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Autoload\\AutoloadGenerator::setPlatformRequirementFilter() in plugins yet');
    }

    public function setRunScripts(bool $runScripts = true)
    {
        \Maestro\Shim\Rpc::call('ag.setRunScripts', [$this, $runScripts]);
    }

    protected function sortPackageMap(array $packageMap)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Autoload\\AutoloadGenerator::sortPackageMap() in plugins yet');
    }

    protected function validatePackage(\Composer\Package\PackageInterface $package)
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Autoload\\AutoloadGenerator::validatePackage() in plugins yet');
    }
}

function composerRequire(string $fileIdentifier, string $file): void
{
    if (empty($GLOBALS['__composer_autoload_files'][$fileIdentifier])) {
        $GLOBALS['__composer_autoload_files'][$fileIdentifier] = true;

        require $file;
    }
}
