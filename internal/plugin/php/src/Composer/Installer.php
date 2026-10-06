<?php

/*
 * maestro's plugin shim: Composer\Installer (docs/PLUGINS.md §4.12, §5.11).
 * The object records its settings in Composer's properties, as Composer's
 * does; run() runs maestro's Installer with them (installer.run),
 * re-entrantly: a plugin can run an install or update from inside an
 * event, as merge-plugin's first install does.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer;

use Composer\Advisory\AuditConfig;
use Composer\Autoload\AutoloadGenerator;
use Composer\DependencyResolver\LockTransaction;
use Composer\DependencyResolver\Request;
use Composer\Downloader\DownloadManager;
use Composer\EventDispatcher\EventDispatcher;
use Composer\Filter\PlatformRequirementFilter\PlatformRequirementFilterFactory;
use Composer\Filter\PlatformRequirementFilter\PlatformRequirementFilterInterface;
use Composer\Installer\InstallationManager;
use Composer\Installer\SuggestedPackagesReporter;
use Composer\IO\IOInterface;
use Composer\Package\Locker;
use Composer\Package\RootPackageInterface;
use Composer\Policy\PolicyConfig;
use Composer\Repository\RepositoryInterface;
use Composer\Repository\RepositoryManager;
use Maestro\Shim\Adapter\ServiceAdapter;
use Maestro\Shim\Mirrors;
use Maestro\Shim\Rpc;

class Installer
{
    public const ERROR_NONE = 0;
    public const ERROR_GENERIC_FAILURE = 1;
    public const ERROR_NO_LOCK_FILE_FOR_PARTIAL_UPDATE = 3;
    public const ERROR_LOCK_FILE_INVALID = 4;
    public const ERROR_AUDIT_FAILED = 5;
    public const ERROR_PSR_AUTOLOAD_VIOLATION = 6;
    public const ERROR_DEPENDENCY_RESOLUTION_FAILED = 2;
    public const ERROR_TRANSPORT_EXCEPTION = 100;

    protected $io;
    protected $config;
    protected $package;
    protected $fixedRootPackage;
    protected $downloadManager;
    protected $repositoryManager;
    protected $locker;
    protected $installationManager;
    protected $eventDispatcher;
    protected $autoloadGenerator;
    protected $preferSource = false;
    protected $preferDist = false;
    protected $optimizeAutoloader = false;
    protected $classMapAuthoritative = false;
    protected $apcuAutoloader = false;
    protected $apcuAutoloaderPrefix;
    protected $devMode = false;
    protected $dryRun = false;
    protected $downloadOnly = false;
    protected $verbose = false;
    protected $update = false;
    protected $install = true;
    protected $dumpAutoloader = true;
    protected $runScripts = true;
    protected $preferStable = false;
    protected $preferLowest = false;
    protected $minimalUpdate = false;
    protected $writeLock;
    protected $executeOperations = true;
    protected $audit = true;
    protected $errorOnAudit = false;
    protected $auditFormat = 'summary';
    private $ignoredTypes = ['php-ext', 'php-ext-zend'];
    private $allowedTypes = null;
    protected $updateMirrors = false;
    protected $updateAllowList = null;
    protected $updateAllowTransitiveDependencies = Request::UPDATE_ONLY_LISTED;
    protected $suggestedPackagesReporter;
    protected $platformRequirementFilter;
    protected $additionalFixedRepository;
    protected $temporaryConstraints = [];
    protected $strictPsrAutoloader = false;
    private $auditConfig = null;
    private $policyConfig = null;
    protected $lockTransaction;

    public function __construct(IOInterface $io, Config $config, RootPackageInterface $package, DownloadManager $downloadManager, RepositoryManager $repositoryManager, Locker $locker, InstallationManager $installationManager, EventDispatcher $eventDispatcher, AutoloadGenerator $autoloadGenerator)
    {
        $this->io = $io;
        $this->config = $config;
        $this->package = $package;
        $this->downloadManager = $downloadManager;
        $this->repositoryManager = $repositoryManager;
        $this->locker = $locker;
        $this->installationManager = $installationManager;
        $this->eventDispatcher = $eventDispatcher;
        $this->autoloadGenerator = $autoloadGenerator;
        $this->suggestedPackagesReporter = new SuggestedPackagesReporter($this->io);
        $this->platformRequirementFilter = PlatformRequirementFilterFactory::ignoreNothing();

        $this->writeLock = $config->get('lock');
    }

    public function run(): int
    {
        if ($this->updateAllowList !== null && $this->updateMirrors) {
            throw new \RuntimeException("The installer options updateMirrors and updateAllowList are mutually exclusive.");
        }

        $settings = [];
        foreach (get_object_vars($this) as $name => $value) {
            $settings[$name] = $value;
        }
        $settings['platformRequirementFilter'] = ServiceAdapter::describeFilter($this->platformRequirementFilter);
        $result = Rpc::call('installer.run', [$settings]);
        $this->lockTransaction = $result['lockTransaction'];

        return $result['code'];
    }

    protected function doUpdate(\Composer\Repository\InstalledRepositoryInterface $localRepo, bool $doInstall): int
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Installer::doUpdate() in plugins yet');
    }

    protected function doInstall(\Composer\Repository\InstalledRepositoryInterface $localRepo, bool $alreadySolved = false): int
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Installer::doInstall() in plugins yet');
    }

    protected function extractDevPackages(LockTransaction $lockTransaction, \Composer\Repository\PlatformRepository $platformRepo, array $aliases, \Composer\DependencyResolver\PolicyInterface $policy, ?\Composer\Repository\LockArrayRepository $lockedRepository = null): int
    {
        throw new \Maestro\Shim\UnsupportedApiException('maestro does not support Composer\\Installer::extractDevPackages() in plugins yet');
    }

    protected function createPlatformRepo(bool $forUpdate): \Composer\Repository\PlatformRepository
    {
        if ($forUpdate) {
            $platformOverrides = $this->config->get('platform') ?: [];
        } else {
            $platformOverrides = $this->locker->getPlatformOverrides();
        }

        return new \Composer\Repository\PlatformRepository([], $platformOverrides);
    }

    public static function create(IOInterface $io, Composer $composer): self
    {
        return new static(
            $io,
            $composer->getConfig(),
            $composer->getPackage(),
            $composer->getDownloadManager(),
            $composer->getRepositoryManager(),
            $composer->getLocker(),
            $composer->getInstallationManager(),
            $composer->getEventDispatcher(),
            $composer->getAutoloadGenerator()
        );
    }

    public function setIgnoredTypes(array $types): self
    {
        $this->ignoredTypes = $types;
        Mirrors::touch($this, 'ignoredTypes');

        return $this;
    }

    public function setAllowedTypes(?array $types): self
    {
        $this->allowedTypes = $types;
        Mirrors::touch($this, 'allowedTypes');

        return $this;
    }

    public function setAdditionalFixedRepository(RepositoryInterface $additionalFixedRepository): self
    {
        $this->additionalFixedRepository = $additionalFixedRepository;
        Mirrors::touch($this, 'additionalFixedRepository');

        return $this;
    }

    public function setTemporaryConstraints(array $constraints): self
    {
        $this->temporaryConstraints = $constraints;
        Mirrors::touch($this, 'temporaryConstraints');

        return $this;
    }

    public function setDryRun(bool $dryRun = true): self
    {
        $this->dryRun = $dryRun;
        Mirrors::touch($this, 'dryRun');

        return $this;
    }

    public function isDryRun(): bool
    {
        return $this->dryRun;
    }

    public function setDownloadOnly(bool $downloadOnly = true): self
    {
        $this->downloadOnly = $downloadOnly;
        Mirrors::touch($this, 'downloadOnly');

        return $this;
    }

    public function setPreferSource(bool $preferSource = true): self
    {
        $this->preferSource = $preferSource;
        Mirrors::touch($this, 'preferSource');

        return $this;
    }

    public function setPreferDist(bool $preferDist = true): self
    {
        $this->preferDist = $preferDist;
        Mirrors::touch($this, 'preferDist');

        return $this;
    }

    public function setOptimizeAutoloader(bool $optimizeAutoloader): self
    {
        $this->optimizeAutoloader = $optimizeAutoloader;
        Mirrors::touch($this, 'optimizeAutoloader');
        if (!$this->optimizeAutoloader) {
            $this->setClassMapAuthoritative(false);
        }

        return $this;
    }

    public function setClassMapAuthoritative(bool $classMapAuthoritative): self
    {
        $this->classMapAuthoritative = $classMapAuthoritative;
        Mirrors::touch($this, 'classMapAuthoritative');
        if ($this->classMapAuthoritative) {
            $this->setOptimizeAutoloader(true);
        }

        return $this;
    }

    public function setApcuAutoloader(bool $apcuAutoloader, ?string $apcuAutoloaderPrefix = null): self
    {
        $this->apcuAutoloader = $apcuAutoloader;
        Mirrors::touch($this, 'apcuAutoloader');
        $this->apcuAutoloaderPrefix = $apcuAutoloaderPrefix;
        Mirrors::touch($this, 'apcuAutoloaderPrefix');

        return $this;
    }

    public function setStrictPsrAutoloader(bool $strictPsr): self
    {
        $this->strictPsrAutoloader = $strictPsr;
        Mirrors::touch($this, 'strictPsrAutoloader');

        return $this;
    }

    public function setUpdate(bool $update): self
    {
        $this->update = $update;
        Mirrors::touch($this, 'update');

        return $this;
    }

    public function setInstall(bool $install): self
    {
        $this->install = $install;
        Mirrors::touch($this, 'install');

        return $this;
    }

    public function setDevMode(bool $devMode = true): self
    {
        $this->devMode = $devMode;
        Mirrors::touch($this, 'devMode');

        return $this;
    }

    public function setDumpAutoloader(bool $dumpAutoloader = true): self
    {
        $this->dumpAutoloader = $dumpAutoloader;
        Mirrors::touch($this, 'dumpAutoloader');

        return $this;
    }

    public function setRunScripts(bool $runScripts = true): self
    {
        $this->runScripts = $runScripts;
        Mirrors::touch($this, 'runScripts');

        return $this;
    }

    public function setConfig(Config $config): self
    {
        $this->config = $config;
        Mirrors::touch($this, 'config');

        return $this;
    }

    public function setVerbose(bool $verbose = true): self
    {
        $this->verbose = $verbose;
        Mirrors::touch($this, 'verbose');

        return $this;
    }

    public function isVerbose(): bool
    {
        return $this->verbose;
    }

    public function setIgnorePlatformRequirements($ignorePlatformReqs): self
    {
        trigger_error('Installer::setIgnorePlatformRequirements is deprecated since Composer 2.2, use setPlatformRequirementFilter instead.', E_USER_DEPRECATED);

        return $this->setPlatformRequirementFilter(PlatformRequirementFilterFactory::fromBoolOrList($ignorePlatformReqs));
    }

    public function setPlatformRequirementFilter(PlatformRequirementFilterInterface $platformRequirementFilter): self
    {
        $this->platformRequirementFilter = $platformRequirementFilter;
        Mirrors::touch($this, 'platformRequirementFilter');

        return $this;
    }

    public function setUpdateMirrors(bool $updateMirrors): self
    {
        $this->updateMirrors = $updateMirrors;
        Mirrors::touch($this, 'updateMirrors');

        return $this;
    }

    public function setUpdateAllowList(array $packages): self
    {
        if (count($packages) === 0) {
            $this->updateAllowList = null;
        } else {
            $this->updateAllowList = array_values(array_unique(array_map('strtolower', $packages)));
        }

        return $this;
    }

    public function setUpdateAllowTransitiveDependencies(int $updateAllowTransitiveDependencies): self
    {
        if (!in_array($updateAllowTransitiveDependencies, [Request::UPDATE_ONLY_LISTED, Request::UPDATE_LISTED_WITH_TRANSITIVE_DEPS_NO_ROOT_REQUIRE, Request::UPDATE_LISTED_WITH_TRANSITIVE_DEPS], true)) {
            throw new \RuntimeException("Invalid value for updateAllowTransitiveDependencies supplied");
        }

        $this->updateAllowTransitiveDependencies = $updateAllowTransitiveDependencies;
        Mirrors::touch($this, 'updateAllowTransitiveDependencies');

        return $this;
    }

    public function setPreferStable(bool $preferStable = true): self
    {
        $this->preferStable = $preferStable;
        Mirrors::touch($this, 'preferStable');

        return $this;
    }

    public function setPreferLowest(bool $preferLowest = true): self
    {
        $this->preferLowest = $preferLowest;
        Mirrors::touch($this, 'preferLowest');

        return $this;
    }

    public function setMinimalUpdate(bool $minimalUpdate = true): self
    {
        $this->minimalUpdate = $minimalUpdate;
        Mirrors::touch($this, 'minimalUpdate');

        return $this;
    }

    public function setWriteLock(bool $writeLock = true): self
    {
        $this->writeLock = $writeLock;
        Mirrors::touch($this, 'writeLock');

        return $this;
    }

    public function setExecuteOperations(bool $executeOperations = true): self
    {
        $this->executeOperations = $executeOperations;
        Mirrors::touch($this, 'executeOperations');

        return $this;
    }

    public function setAudit(bool $audit): self
    {
        $this->audit = $audit;
        Mirrors::touch($this, 'audit');
        $this->auditConfig = null;
        Mirrors::touch($this, 'auditConfig');

        return $this;
    }

    public function setErrorOnAudit(bool $errorOnAudit): self
    {
        $this->errorOnAudit = $errorOnAudit;
        Mirrors::touch($this, 'errorOnAudit');

        return $this;
    }

    public function setAuditFormat(string $auditFormat): self
    {
        $this->auditFormat = $auditFormat;
        Mirrors::touch($this, 'auditFormat');
        $this->auditConfig = null;
        Mirrors::touch($this, 'auditConfig');

        return $this;
    }

    public function setAuditConfig(AuditConfig $auditConfig): self
    {
        $this->auditConfig = $auditConfig;
        Mirrors::touch($this, 'auditConfig');

        return $this;
    }

    public function setPolicyConfig(PolicyConfig $policyConfig): self
    {
        $this->policyConfig = $policyConfig;
        Mirrors::touch($this, 'policyConfig');

        return $this;
    }

    public function disablePlugins(): self
    {
        $this->installationManager->disablePlugins();

        return $this;
    }

    public function setSuggestedPackagesReporter(SuggestedPackagesReporter $suggestedPackagesReporter): self
    {
        $this->suggestedPackagesReporter = $suggestedPackagesReporter;
        Mirrors::touch($this, 'suggestedPackagesReporter');

        return $this;
    }

    public function getLockTransaction(): ?LockTransaction
    {
        return $this->lockTransaction;
    }
}
