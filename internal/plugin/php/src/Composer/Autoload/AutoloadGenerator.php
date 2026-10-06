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
        // maestro's generator (ag.new), whose proxy this object is from
        // now on (magento/magento-composer-installer's
        // new AutoloadGenerator(new EventDispatcher($composer, $io))).
        \Maestro\Shim\Rpc::call('ag.new', [$this, $eventDispatcher, $io]);
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
        // maestro writes the autoloader (ag.dump); the result is
        // Composer's ClassMap of what it found.
        $d = \Maestro\Shim\Rpc::call('ag.dump', [$this, $config, $localRepo, $rootPackage, $installationManager, $targetDir, $scanPsrPackages, $suffix, $locker, $strictAmbiguous]);
        $classMap = new \Composer\ClassMapGenerator\ClassMap();
        foreach ($d['map'] as $class => $path) {
            $classMap->addClass((string) $class, $path);
        }
        foreach ($d['ambiguous'] as $class => $paths) {
            foreach ($paths as $path) {
                $classMap->addAmbiguousClass((string) $class, $path);
            }
        }
        foreach ($d['psrViolations'] as $path => $violations) {
            foreach ($violations as $violation) {
                $classMap->addPsrViolation($violation['warning'], $violation['className'], (string) $path);
            }
        }

        return $classMap;
    }

    // The protected methods are Composer's code here (on the given
    // Filesystem, the package getters), or maestro's generator's (ag.*):
    // what a subclass calls on itself. maestro's dump() does not call a
    // subclass's overrides of them.

    protected function filterPackageMap(array $packageMap, \Composer\Package\RootPackageInterface $rootPackage)
    {
        $packages = [];
        $include = [];
        $replacedBy = [];

        foreach ($packageMap as $item) {
            $package = $item[0];
            $name = $package->getName();
            $packages[$name] = $package;
            foreach ($package->getReplaces() as $replace) {
                $replacedBy[$replace->getTarget()] = $name;
            }
        }

        $add = static function (\Composer\Package\PackageInterface $package) use (&$add, $packages, &$include, $replacedBy): void {
            foreach ($package->getRequires() as $link) {
                $target = $link->getTarget();
                if (isset($replacedBy[$target])) {
                    $target = $replacedBy[$target];
                }
                if (!isset($include[$target])) {
                    $include[$target] = true;
                    if (isset($packages[$target])) {
                        $add($packages[$target]);
                    }
                }
            }
        };
        $add($rootPackage);

        return array_filter(
            $packageMap,
            static function ($item) use ($include): bool {
                $package = $item[0];
                foreach ($package->getNames() as $name) {
                    if (isset($include[$name])) {
                        return true;
                    }
                }

                return false;
            }
        );
    }

    protected function getAutoloadFile(string $vendorPathToTargetDirCode, string $suffix)
    {
        return \Maestro\Shim\Rpc::call('ag.getAutoloadFile', [$this, $vendorPathToTargetDirCode, $suffix]);
    }

    protected function getAutoloadRealFile(bool $useClassMap, bool $useIncludePath, ?string $targetDirLoader, bool $useIncludeFiles, string $vendorPathCode, string $appBaseDirCode, string $suffix, bool $useGlobalIncludePath, string $prependAutoloader, bool $checkPlatform)
    {
        return \Maestro\Shim\Rpc::call('ag.getAutoloadRealFile', [$this, $useClassMap, $useIncludePath, $targetDirLoader, $useIncludeFiles, $vendorPathCode, $appBaseDirCode, $suffix, $useGlobalIncludePath, $prependAutoloader, $checkPlatform]);
    }

    protected function getFileIdentifier(\Composer\Package\PackageInterface $package, string $path)
    {
        return hash('md5', $package->getName() . ':' . $path);
    }

    protected function getIncludeFilesFile(array $files, \Composer\Util\Filesystem $filesystem, string $basePath, string $vendorPath, string $vendorPathCode, string $appBaseDirCode)
    {
        // Get the path to each file, and make sure these paths are unique.
        $files = array_map(
            function (string $functionFile) use ($filesystem, $basePath, $vendorPath): string {
                return $this->getPathCode($filesystem, $basePath, $vendorPath, $functionFile);
            },
            $files
        );
        $uniqueFiles = array_unique($files);
        if (count($uniqueFiles) < count($files)) {
            // Composer's $this->io: maestro's generator's.
            $io = \Maestro\Shim\Rpc::call('ag.getIO', [$this]);
            $io->writeError('<warning>The following "files" autoload rules are included multiple times, this may cause issues and should be resolved:</warning>');
            foreach (array_unique(array_diff_assoc($files, $uniqueFiles)) as $duplicateFile) {
                $io->writeError('<warning> - '.$duplicateFile.'</warning>');
            }
        }
        unset($uniqueFiles);

        $filesCode = '';

        foreach ($files as $fileIdentifier => $functionFile) {
            $filesCode .= '    ' . var_export($fileIdentifier, true) . ' => ' . $functionFile . ",\n";
        }

        if (!$filesCode) {
            return null;
        }

        return <<<EOF
<?php

// autoload_files.php @generated by Composer

\$vendorDir = $vendorPathCode;
\$baseDir = $appBaseDirCode;

return array(
$filesCode);

EOF;
    }

    protected function getIncludePathsFile(array $packageMap, \Composer\Util\Filesystem $filesystem, string $basePath, string $vendorPath, string $vendorPathCode, string $appBaseDirCode)
    {
        $includePaths = [];

        foreach ($packageMap as $item) {
            [$package, $installPath] = $item;

            // packages that are not installed cannot autoload anything
            if (null === $installPath) {
                continue;
            }

            if (null !== $package->getTargetDir() && strlen($package->getTargetDir()) > 0) {
                $installPath = substr($installPath, 0, -strlen('/'.$package->getTargetDir()));
            }

            foreach ($package->getIncludePaths() as $includePath) {
                $includePath = trim($includePath, '/');
                $includePaths[] = $installPath === '' ? $includePath : $installPath.'/'.$includePath;
            }
        }

        if (\count($includePaths) === 0) {
            return null;
        }

        $includePathsCode = '';
        foreach ($includePaths as $path) {
            $includePathsCode .= "    " . $this->getPathCode($filesystem, $basePath, $vendorPath, $path) . ",\n";
        }

        return <<<EOF
<?php

// include_paths.php @generated by Composer

\$vendorDir = $vendorPathCode;
\$baseDir = $appBaseDirCode;

return array(
$includePathsCode);

EOF;
    }

    protected function getPathCode(\Composer\Util\Filesystem $filesystem, string $basePath, string $vendorPath, string $path)
    {
        if (!$filesystem->isAbsolutePath($path)) {
            $path = $basePath . '/' . $path;
        }
        $path = $filesystem->normalizePath($path);

        $baseDir = '';
        if (strpos($path.'/', $vendorPath.'/') === 0) {
            $path = (string) substr($path, strlen($vendorPath));
            $baseDir = '$vendorDir . ';
        } else {
            $path = $filesystem->normalizePath($filesystem->findShortestPath($basePath, $path, true));
            if (!$filesystem->isAbsolutePath($path)) {
                $baseDir = '$baseDir . ';
                $path = '/' . $path;
            }
        }

        if (\Composer\Pcre\Preg::isMatch('{\.phar([\\\\/]|$)}', $path)) {
            $baseDir = "'phar://' . " . $baseDir;
        }

        return $baseDir . var_export($path, true);
    }

    protected function getPlatformCheck(array $packageMap, $checkPlatform, array $devPackageNames)
    {
        return \Maestro\Shim\Rpc::call('ag.getPlatformCheck', [$this, $packageMap, $checkPlatform, array_values($devPackageNames)]);
    }

    protected function getStaticFile(string $suffix, string $targetDir, string $vendorPath, string $basePath)
    {
        $file = <<<HEADER
<?php

// autoload_static.php @generated by Composer

namespace Composer\Autoload;

class ComposerStaticInit$suffix
{

HEADER;

        $loader = new ClassLoader();

        $map = require $targetDir . '/autoload_namespaces.php';
        foreach ($map as $namespace => $path) {
            $loader->set($namespace, $path);
        }

        $map = require $targetDir . '/autoload_psr4.php';
        foreach ($map as $namespace => $path) {
            $loader->setPsr4($namespace, $path);
        }

        /**
         * @var string $vendorDir
         * @var string $baseDir
         */
        $classMap = require $targetDir . '/autoload_classmap.php';
        if ($classMap) {
            $loader->addClassMap($classMap);
        }

        $filesystem = new \Composer\Util\Filesystem();

        $vendorPathCode = ' => ' . $filesystem->findShortestPathCode(realpath($targetDir), $vendorPath, true, true) . " . '/";
        $vendorPharPathCode = ' => \'phar://\' . ' . $filesystem->findShortestPathCode(realpath($targetDir), $vendorPath, true, true) . " . '/";
        $appBaseDirCode = ' => ' . $filesystem->findShortestPathCode(realpath($targetDir), $basePath, true, true) . " . '/";
        $appBaseDirPharCode = ' => \'phar://\' . ' . $filesystem->findShortestPathCode(realpath($targetDir), $basePath, true, true) . " . '/";

        $absoluteVendorPathCode = ' => ' . substr(var_export(rtrim($vendorDir, '\\/') . '/', true), 0, -1);
        $absoluteVendorPharPathCode = ' => ' . substr(var_export(rtrim('phar://' . $vendorDir, '\\/') . '/', true), 0, -1);
        $absoluteAppBaseDirCode = ' => ' . substr(var_export(rtrim($baseDir, '\\/') . '/', true), 0, -1);
        $absoluteAppBaseDirPharCode = ' => ' . substr(var_export(rtrim('phar://' . $baseDir, '\\/') . '/', true), 0, -1);

        $initializer = '';
        $prefix = "\0Composer\Autoload\ClassLoader\0";
        $prefixLen = strlen($prefix);
        if (file_exists($targetDir . '/autoload_files.php')) {
            $maps = ['files' => require $targetDir . '/autoload_files.php'];
        } else {
            $maps = [];
        }

        foreach ((array) $loader as $prop => $value) {
            if (!is_array($value) || \count($value) === 0 || !str_starts_with($prop, $prefix)) {
                continue;
            }
            $maps[substr($prop, $prefixLen)] = $value;
        }

        foreach ($maps as $prop => $value) {
            $value = strtr(
                var_export($value, true),
                [
                    $absoluteVendorPathCode => $vendorPathCode,
                    $absoluteVendorPharPathCode => $vendorPharPathCode,
                    $absoluteAppBaseDirCode => $appBaseDirCode,
                    $absoluteAppBaseDirPharCode => $appBaseDirPharCode,
                ]
            );
            $value = ltrim(\Composer\Pcre\Preg::replace('/^ */m', '    $0$0', $value));
            $value = \Composer\Pcre\Preg::replace('/ +$/m', '', $value);

            $file .= sprintf("    public static $%s = %s;\n\n", $prop, $value);
            if ('files' !== $prop) {
                $initializer .= "            \$loader->$prop = ComposerStaticInit$suffix::\$$prop;\n";
            }
        }

        return $file . <<<INITIALIZER
    public static function getInitializer(ClassLoader \$loader)
    {
        return \Closure::bind(function () use (\$loader) {
$initializer
        }, null, ClassLoader::class);
    }
}

INITIALIZER;
    }

    public function parseAutoloads(array $packageMap, \Composer\Package\PackageInterface $rootPackage, $filteredDevPackages = false)
    {
        return \Maestro\Shim\Rpc::call('ag.parseAutoloads', [$this, $packageMap, $rootPackage, $filteredDevPackages]);
    }

    protected function parseAutoloadsType(array $packageMap, string $type, \Composer\Package\RootPackageInterface $rootPackage)
    {
        return \Maestro\Shim\Rpc::call('ag.parseAutoloadsType', [$this, $packageMap, $type, $rootPackage]);
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
        trigger_error('AutoloadGenerator::setIgnorePlatformRequirements is deprecated since Composer 2.2, use setPlatformRequirementFilter instead.', E_USER_DEPRECATED);

        $this->setPlatformRequirementFilter(\Composer\Filter\PlatformRequirementFilter\PlatformRequirementFilterFactory::fromBoolOrList($ignorePlatformReqs));
    }

    public function setPlatformRequirementFilter(\Composer\Filter\PlatformRequirementFilter\PlatformRequirementFilterInterface $platformRequirementFilter)
    {
        \Maestro\Shim\Rpc::call('ag.setPlatformRequirementFilter', [$this, ["\0filter" => \Maestro\Shim\Adapter\ServiceAdapter::describeFilter($platformRequirementFilter)]]);
    }

    public function setRunScripts(bool $runScripts = true)
    {
        \Maestro\Shim\Rpc::call('ag.setRunScripts', [$this, $runScripts]);
    }

    protected function sortPackageMap(array $packageMap)
    {
        // PackageSorter::sortPackages() is maestro's.
        return \Maestro\Shim\Rpc::call('ag.sortPackageMap', [$this, $packageMap]);
    }

    protected function validatePackage(\Composer\Package\PackageInterface $package)
    {
        $autoload = $package->getAutoload();
        if (!empty($autoload['psr-4']) && null !== $package->getTargetDir()) {
            $name = $package->getName();
            $package->getTargetDir();
            throw new \InvalidArgumentException("PSR-4 autoloading is incompatible with the target-dir property, remove the target-dir in package '$name'.");
        }
        if (!empty($autoload['psr-4'])) {
            foreach ($autoload['psr-4'] as $namespace => $dirs) {
                if ($namespace !== '' && '\\' !== substr($namespace, -1)) {
                    throw new \InvalidArgumentException("psr-4 namespaces must end with a namespace separator, '$namespace' does not, use '$namespace\\'.");
                }
            }
        }
    }
}

function composerRequire(string $fileIdentifier, string $file): void
{
    if (empty($GLOBALS['__composer_autoload_files'][$fileIdentifier])) {
        $GLOBALS['__composer_autoload_files'][$fileIdentifier] = true;

        require $file;
    }
}
