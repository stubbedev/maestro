// Ports src/Composer/Installer.php.

package composer

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/filter"
	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/installer"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json/jsonlint"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/platform"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/script"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/ui"
	"github.com/stubbedev/maestro/internal/util"
)

// The Installer::ERROR_* exit codes.
const (
	ErrorNone                       = 0
	ErrorGenericFailure             = 1
	ErrorDependencyResolutionFailed = resolver.ErrorDependencyResolutionFailed
	ErrorNoLockFileForPartialUpdate = 3
	ErrorLockFileInvalid            = 4
	ErrorAuditFailed                = 5
	ErrorPsrAutoloadViolation       = 6
	ErrorTransportException         = 100
	installerUpdateFallbackMessage  = "Updating dependencies to latest instead of installing from lock file. See https://getcomposer.org/install for more information."
)

// Installer ports Composer\Installer: install and update runs. It holds no
// package-level state, so nested runs (plugins calling
// Installer::create()->run()) are independent; Clone copies one.
type Installer struct {
	io                  io.IO
	config              ConfigReader
	pkg                 pkg.RootPackageInterface
	fixedRootPackage    pkg.RootPackageInterface
	downloadManager     *downloader.DownloadManager
	repositoryManager   *repository.RepositoryManager
	locker              *locker.Locker
	installationManager InstallationManager
	eventDispatcher     EventDispatcher
	autoloadGenerator   AutoloadGenerator
	runtime             *Runtime
	process             *util.ProcessExecutor

	installPreference     downloader.InstallPreference
	optimizeAutoloader    bool
	classMapAuthoritative bool
	strictPsrAutoloader   bool
	apcuAutoloader        bool
	apcuAutoloaderPrefix  *string
	devMode               bool
	dryRun                bool
	downloadOnly          bool
	verbose               bool
	update                bool
	install               bool
	dumpAutoloader        bool
	runScripts            bool
	preferStable          bool
	preferLowest          bool
	minimalUpdate         bool
	writeLock             bool
	executeOperations     bool
	audit                 bool
	errorOnAudit          bool
	auditFormat           string
	auditConfig           *advisory.AuditConfig
	policyConfig          *policy.PolicyConfig
	ignoredTypes          []string
	allowedTypes          php.Nullable[[]string] // ?array: null allows every type, [] none

	updateMirrors                     bool
	updateAllowList                   []string
	updateAllowTransitiveDependencies int

	suggestedPackagesReporter *installer.SuggestedPackagesReporter
	platformRequirementFilter version.PlatformRequirementFilter
	additionalFixedRepository repository.RepositoryInterface
	temporaryConstraints      *repository.ConstraintMap
	lockTransaction           *resolver.LockTransaction

	// executedOperations is set once the install ran package operations.
	executedOperations bool
}

// InstallerDeps are the collaborators of new Installer(...).
type InstallerDeps struct {
	IO                  io.IO
	Config              ConfigReader
	Package             pkg.RootPackageInterface
	DownloadManager     *downloader.DownloadManager
	RepositoryManager   *repository.RepositoryManager
	Locker              *locker.Locker
	InstallationManager InstallationManager
	EventDispatcher     EventDispatcher
	AutoloadGenerator   AutoloadGenerator
	// Runtime is the process the installer runs in (statics, platform).
	Runtime *Runtime
	// Process runs the HHVM detection of the platform repositories; nil
	// leaves HHVM undetected.
	Process *util.ProcessExecutor
	// SuggestedPackagesReporter is the initial reporter (new
	// SuggestedPackagesReporter($io)).
	SuggestedPackagesReporter *installer.SuggestedPackagesReporter
}

// NewInstaller ports new Installer($io, $config, $package, $downloadManager,
// $repositoryManager, $locker, $installationManager, $eventDispatcher,
// $autoloadGenerator).
func NewInstaller(d InstallerDeps) (*Installer, error) {
	writeLock, err := d.Config.Get("lock", 0)
	if err != nil {
		return nil, err
	}
	reporter := d.SuggestedPackagesReporter
	if reporter == nil {
		reporter = installer.NewSuggestedPackagesReporter(d.IO)
	}

	return &Installer{
		io:                        d.IO,
		config:                    d.Config,
		pkg:                       d.Package,
		downloadManager:           d.DownloadManager,
		repositoryManager:         d.RepositoryManager,
		locker:                    d.Locker,
		installationManager:       d.InstallationManager,
		eventDispatcher:           d.EventDispatcher,
		autoloadGenerator:         d.AutoloadGenerator,
		runtime:                   d.Runtime,
		process:                   d.Process,
		install:                   true,
		dumpAutoloader:            true,
		runScripts:                true,
		executeOperations:         true,
		audit:                     true,
		auditFormat:               advisory.FormatSummary,
		ignoredTypes:              []string{"php-ext", "php-ext-zend"},
		suggestedPackagesReporter: reporter,
		platformRequirementFilter: filter.IgnoreNothingFilter(),
		writeLock:                 php.ToBool(writeLock),
		temporaryConstraints:      &repository.ConstraintMap{},
	}, nil
}

// CreateInstaller ports Installer::create($io, $composer).
func CreateInstaller(out io.IO, c *Composer) (*Installer, error) {
	deps := InstallerDeps{
		IO:                  out,
		Config:              c.Config(),
		Package:             c.Package(),
		RepositoryManager:   c.RepositoryManager(),
		Locker:              c.Locker(),
		InstallationManager: c.InstallationManager(),
		Runtime:             c.runtime,
		Process:             c.process,
	}
	deps.DownloadManager = c.DownloadManager()
	if ed := c.EventDispatcher(); ed != nil {
		deps.EventDispatcher = ed
	}
	if g := c.AutoloadGenerator(); g != nil {
		deps.AutoloadGenerator = GeneratorAdapter{g}
	}

	return NewInstaller(deps)
}

// Clone returns a copy of the installer (PHP's clone $installer).
func (i *Installer) Clone() *Installer {
	c := *i
	c.ignoredTypes = slices.Clone(i.ignoredTypes)
	c.allowedTypes = cloneNullableStrings(i.allowedTypes)
	c.updateAllowList = slices.Clone(i.updateAllowList)
	if i.temporaryConstraints != nil {
		c.temporaryConstraints = i.temporaryConstraints.Clone()
	}

	return &c
}

// configBool reads a config setting as PHP truthiness.
func (i *Installer) configBool(key string) (bool, error) {
	v, err := i.config.Get(key, 0)

	return php.ToBool(v), err
}

// configArray reads `$this->config->get($key) ?: []`.
func (i *Installer) configArray(key string) (*php.Array, error) {
	v, err := i.config.Get(key, 0)
	if err != nil {
		return nil, err
	}
	if a, ok := v.(*php.Array); ok && a.Len() > 0 {
		return a, nil
	}

	return php.NewArray(), nil
}

// Run ports run: an install (or update), returning one of the Error*
// exit codes.
func (i *Installer) Run() (int, error) {
	if i.runtime != nil {
		i.runtime.PushFrame(`Composer\Installer->run`, i)
		defer i.runtime.PopFrame()
	}

	if i.updateAllowList != nil && i.updateMirrors {
		return 0, &util.RuntimeError{Message: "The installer options updateMirrors and updateAllowList are mutually exclusive."}
	}
	// the package store's upkeep, once everything else is done
	defer i.maintainStore()
	i.prefetchFilterSummaries()

	isFreshInstall, err := i.repositoryManager.LocalRepository().IsFresh()
	if err != nil {
		return 0, err
	}

	// Force update if there is no lock file present
	if !i.update {
		locked, err := i.locker.IsLocked()
		if err != nil {
			return 0, err
		}
		if !locked {
			if !i.writeLock {
				// Print more specific warning message when creating the lock file is disabled in config, but still notify the user that the update command is used
				i.io.WriteError(fmt.Sprintf("<warning>Lockfile creation is disabled in config. %s</warning>", installerUpdateFallbackMessage), true, io.Normal)
			} else {
				i.io.WriteError(fmt.Sprintf("<warning>No composer.lock file present. %s</warning>", installerUpdateFallbackMessage), true, io.Normal)
			}
			i.update = true
		}
	}

	// surface the actual install/update operation in telemetry, even when the outer command is a
	// plugin/script or a command like require/remove that runs the installer (see StreamContextFactory)
	if i.runtime != nil {
		if i.update {
			i.runtime.SetRunningOperation("update", true)
		} else {
			i.runtime.SetRunningOperation("install", true)
		}
	}

	if i.dryRun {
		i.verbose = true
		i.runScripts = false
		i.executeOperations = false
		i.writeLock = false
		i.dumpAutoloader = false
		if err := mockLocalRepositories(i.repositoryManager); err != nil {
			return 0, err
		}
	}

	if i.downloadOnly {
		i.dumpAutoloader = false
	}

	if i.update && !i.install {
		i.dumpAutoloader = false
	}

	if i.runScripts {
		util.PutEnv("COMPOSER_DEV_MODE", boolString(i.devMode))

		// dispatch pre event
		// should we treat this more strictly as running an update and then running an install, triggering events multiple times?
		eventName := script.PreInstallCmd
		if i.update {
			eventName = script.PreUpdateCmd
		}
		if _, err := i.eventDispatcher.DispatchScript(eventName, i.devMode, nil, nil); err != nil {
			return 0, err
		}
	}

	if i.downloadManager != nil {
		i.downloadManager.SetInstallPreference(i.installPreference)
	}

	// the autoload dump below scans the files of the packages installed:
	// those the package store inserts are parsed as they are inserted
	// (deliberate deviation 3)
	if a, ok := i.autoloadGenerator.(autoloadParser); ok && i.dumpAutoloader && i.executeOperations {
		a.ParseAhead(i.optimizeAutoloader || i.classMapAuthoritative)
	}

	localRepo := i.repositoryManager.LocalRepository()

	var notified func()
	defer func() {
		if notified != nil {
			notified()
		}
	}()

	notifyOnInstall := func() error {
		if !i.executeOperations || !i.install {
			return nil
		}
		notify, err := i.configBool("notify-on-install")
		if err != nil || !notify {
			return err
		}

		// below -vvv nothing shows when the notifications complete:
		// they are waited for when the run ends (deliberate deviation 3);
		// an IO created in PHP sees every call, so it gets Composer's
		// (and no isDebug() of maestro's)
		if a, ok := i.installationManager.(asyncNotifier); ok && !io.IsForeign(i.io) && !i.io.IsDebug() {
			notified = a.NotifyInstallsAsync(i.io)

			return nil
		}

		i.installationManager.NotifyInstalls(i.io)

		return nil
	}

	var res int
	if i.update {
		res, err = i.doUpdate(localRepo, i.install)
	} else {
		// the scan doInstall hands over to the dump below goes when Run
		// returns before the dump takes it
		defer i.discardAutoloadSpeculation()
		res, err = i.doInstall(localRepo, false)
	}
	if err != nil {
		if nerr := notifyOnInstall(); nerr != nil {
			return 0, nerr
		}

		return 0, err
	}
	if res != 0 {
		return res, nil
	}
	// the autoload dump below scans the installed files: they are parsed
	// while the install notifications are sent, when they are waited for
	// (deliberate deviation 3)
	if w, ok := i.autoloadGenerator.(autoloadWarmer); ok && i.dumpAutoloader && i.io.IsDebug() {
		w.WarmAutoloads(i.config, localRepo, i.pkg, i.installationManager, i.optimizeAutoloader || i.classMapAuthoritative)
	}
	if err := notifyOnInstall(); err != nil {
		return 0, err
	}

	if i.update {
		lockedRepo, err := i.locker.LockedRepository(i.devMode)
		if err != nil {
			return 0, err
		}
		platformRepo, err := i.createPlatformRepo(false)
		if err != nil {
			return 0, err
		}
		rootRepo, err := repository.NewRootPackageRepository(cloneRoot(i.pkg))
		if err != nil {
			return 0, err
		}
		installedRepo, err := repository.NewInstalledRepository([]repository.RepositoryInterface{lockedRepo, platformRepo, rootRepo})
		if err != nil {
			return 0, err
		}
		if isFreshInstall {
			i.suggestedPackagesReporter.AddSuggestionsFromPackage(i.pkg)
		}
		if err := i.suggestedPackagesReporter.OutputMinimalistic(installedRepo, nil); err != nil {
			return 0, err
		}
	}

	// Find abandoned packages and warn user
	lockedRepository, err := i.locker.LockedRepository(true)
	if err != nil {
		return 0, err
	}
	lockedPackages, err := lockedRepository.Packages()
	if err != nil {
		return 0, err
	}
	for _, p := range lockedPackages {
		complete, ok := pkg.AsCompletePackage(p)
		if !ok || !complete.IsAbandoned() {
			continue
		}

		replacement := "No replacement was suggested"
		if r := complete.ReplacementPackage(); r.Valid {
			replacement = "Use " + r.S + " instead"
		}

		i.io.WriteError(fmt.Sprintf("<warning>Package %s is abandoned, you should avoid using it. %s.</warning>", p.PrettyName(), replacement), true, io.Normal)
	}

	if i.dumpAutoloader {
		// write autoloader
		if i.optimizeAutoloader {
			i.io.WriteError(ui.RoleMuted.Wrap("Generating optimized autoload files"), true, io.Normal)
		} else {
			i.io.WriteError(ui.RoleMuted.Wrap("Generating autoload files"), true, io.Normal)
		}

		i.autoloadGenerator.SetClassMapAuthoritative(i.classMapAuthoritative)
		i.autoloadGenerator.SetApcu(i.apcuAutoloader, i.apcuAutoloaderPrefix)
		i.autoloadGenerator.SetRunScripts(i.runScripts)
		i.autoloadGenerator.SetPlatformRequirementFilter(i.platformRequirementFilter)
		classMap, err := i.autoloadGenerator.DumpAutoloads(i.config, localRepo, i.pkg, i.installationManager, "composer", i.optimizeAutoloader, "", i.locker)
		if err != nil {
			return 0, err
		}

		if i.strictPsrAutoloader && classMap != nil && len(classMap.PsrViolations()) > 0 {
			return ErrorPsrAutoloadViolation, nil
		}
	}

	if i.install && i.executeOperations {
		// force binaries re-generation in case they are missing
		packages, err := localRepo.Packages()
		if err != nil {
			return 0, err
		}
		for _, p := range packages {
			if err := i.installationManager.EnsureBinariesPresence(p); err != nil {
				return 0, err
			}
		}
	}

	showFunding := true
	if fundEnv, ok := util.GetEnv("COMPOSER_FUND"); ok && php.IsNumeric(fundEnv) {
		showFunding = php.ToInt(fundEnv) != 0
	}

	if showFunding {
		packages, err := localRepo.Packages()
		if err != nil {
			return 0, err
		}
		fundingCount := 0
		for _, p := range packages {
			if _, isAlias := p.(pkg.Alias); isAlias {
				continue
			}
			if complete, ok := p.(pkg.CompletePackageInterface); ok && php.ToBool(complete.Funding()) {
				fundingCount++
			}
		}
		if fundingCount > 0 {
			s, verb := "s", "are"
			if fundingCount == 1 {
				s, verb = "", "is"
			}
			i.io.WriteErrorMessages([]string{
				ui.RoleMuted.Wrap(fmt.Sprintf("%d package%s you are using %s looking for funding.", fundingCount, s, verb)),
				ui.RoleMuted.Wrap("Use the `composer fund` command to find out more!"),
			}, true, io.Normal)
		}
	}

	if i.runScripts {
		// dispatch post event
		eventName := script.PostInstallCmd
		if i.update {
			eventName = script.PostUpdateCmd
		}
		if _, err := i.eventDispatcher.DispatchScript(eventName, i.devMode, nil, nil); err != nil {
			return 0, err
		}
	}

	return i.runAudit(lockedRepository, localRepo)
}

// runAudit is the audit at the end of run().
func (i *Installer) runAudit(lockedRepository *repository.LockArrayRepository, localRepo repository.InstalledRepositoryInterface) (int, error) {
	auditConfig := i.getAuditConfig()
	if !auditConfig.Audit {
		return 0, nil
	}

	var packages []pkg.PackageInterface
	var target string
	var err error
	if i.update && !i.install {
		packages, err = lockedRepository.CanonicalPackages()
		target = "locked"
	} else {
		packages, err = localRepo.CanonicalPackages()
		target = "installed"
	}
	if err != nil {
		return 0, err
	}
	if len(packages) == 0 {
		i.io.WriteError("No "+target+" packages - skipping audit.", true, io.Normal)

		return 0, nil
	}

	result, err := i.runAuditor(packages, auditConfig)
	if err == nil {
		return result, nil
	}
	if !errors.As(err, new(*util.TransportError)) {
		return 0, err
	}
	i.io.Error("Failed to audit "+target+" packages.", nil)
	if i.io.IsVerbose() {
		i.io.Error("[Composer\\Downloader\\TransportException] "+err.Error(), nil)
	}

	return 0, nil
}

func (i *Installer) runAuditor(packages []pkg.PackageInterface, auditConfig advisory.AuditConfig) (int, error) {
	repoSet, err := repository.NewRepositorySet("stable", nil, nil, nil, nil, nil)
	if err != nil {
		return 0, err
	}
	for _, repo := range i.repositoryManager.Repositories() {
		if err := repoSet.AddRepository(repo); err != nil {
			return 0, err
		}
	}
	policyConfig, err := i.getPolicyConfig()
	if err != nil {
		return 0, err
	}
	var providerSet filterlist.ProviderSet
	if policyConfig.Enabled {
		set, err := filterlist.CreateFilterListProviderSet(policyConfig, i.repositoryManager.Repositories(), getter(i.repositoryManager))
		if err != nil {
			return 0, err
		}
		providerSet = set
	}

	status, err := advisory.Auditor{}.Audit(i.io, repoSet, policyConfig, packages, auditConfig.AuditFormat, true, providerSet)
	if err != nil {
		return 0, err
	}
	if status > 0 && i.errorOnAudit {
		return ErrorAuditFailed, nil
	}

	return 0, nil
}

// solverProblem reports a SolverProblemsException the way doUpdate,
// extractDevPackages and doInstall do.
func (i *Installer) prettyProblem(e *resolver.SolverProblemsError, repositorySet *repository.RepositorySet, request *resolver.Request, pool *resolver.Pool, isDevExtraction bool) (string, error) {
	var env resolver.Environment
	if i.runtime != nil {
		env = i.runtime.Environment()
	}

	return e.PrettyString(repositorySet, request, pool, i.io.IsVerbose(), isDevExtraction, env)
}

func (i *Installer) emitGithubActionError(message string) {
	console.NewGithubActionError(func(m string) { i.io.Write(m, true, io.Normal) }).Emit(message, "", 0)
}

func (i *Installer) doUpdate(localRepo repository.InstalledRepositoryInterface, doInstall bool) (int, error) {
	if i.runtime != nil {
		i.runtime.PushFrame(`Composer\Installer->doUpdate`, i, localRepo, doInstall)
		defer i.runtime.PopFrame()
	}
	platformRepo, err := i.createPlatformRepo(true)
	if err != nil {
		return 0, err
	}
	aliases, err := i.getRootAliases(true)
	if err != nil {
		return 0, err
	}

	var lockedRepository *repository.LockArrayRepository
	locked, err := i.locker.IsLocked()
	if err == nil && locked {
		lockedRepository, err = i.locker.LockedRepository(true)
	}
	if err != nil {
		if !isParsingException(err) || i.updateAllowList != nil || i.updateMirrors {
			// in case we are doing a partial update or updating mirrors, the lock file is needed so we error
			return 0, err
		}
		// otherwise, ignoring parse errors as the lock file will be regenerated from scratch when
		// doing a full update
		lockedRepository = nil
	}

	if (i.updateAllowList != nil || i.updateMirrors) && lockedRepository == nil {
		what := "only a partial set of packages"
		if i.updateMirrors {
			what = "lock file information"
		}
		i.io.WriteError("<error>Cannot update "+what+" without a lock file present. Run `composer update` to generate a lock file.</error>", true, io.Quiet)

		return ErrorNoLockFileForPartialUpdate, nil
	}

	i.io.WriteError("<info>Loading composer repositories with package information</info>", true, io.Normal)

	// creating repository set
	policy, err := i.createPolicy(true, lockedRepository)
	if err != nil {
		return 0, err
	}
	repositorySet, err := i.createRepositorySet(true, platformRepo, aliases, nil)
	if err != nil {
		return 0, err
	}
	for _, repo := range i.repositoryManager.Repositories() {
		if err := repositorySet.AddRepository(repo); err != nil {
			return 0, err
		}
	}
	if lockedRepository != nil {
		if err := repositorySet.AddRepository(lockedRepository); err != nil {
			return 0, err
		}
	}
	i.prefetchMetadata(repositorySet, lockedRepository)

	request, err := i.createRequest(i.fixedRootPackage, platformRepo, lockedRepository)
	if err != nil {
		return 0, err
	}
	if err := i.requirePackagesForUpdate(request, lockedRepository, true); err != nil {
		return 0, err
	}

	// pass the allow list into the request, so the pool builder can apply it
	if i.updateAllowList != nil {
		request.SetUpdateAllowList(i.updateAllowList, i.updateAllowTransitiveDependencies)
	}

	securityFilter, err := i.createSecurityAuditPoolFilter()
	if err != nil {
		return 0, err
	}
	filterListFilter, err := i.createFilterListPoolFilter(policyBlockScopeUpdate)
	if err != nil {
		return 0, err
	}
	pool, err := resolver.CreatePool(repositorySet, request, i.io, resolver.CreatePoolOptions{
		EventDispatcher:            i.poolEventDispatcher(),
		PoolOptimizer:              i.createPoolOptimizer(policy),
		IgnoredTypes:               i.ignoredTypes,
		AllowedTypes:               i.allowedTypes,
		SecurityAdvisoryPoolFilter: securityFilter,
		FilterListPoolFilter:       filterListFilter,
	})
	if err != nil {
		return 0, err
	}

	i.io.WriteError("<info>Updating dependencies</info>", true, io.Normal)

	// solve dependencies
	solver := resolver.NewSolver(policy, pool, i.io)
	i.lockTransaction, err = solver.Solve(request, i.platformRequirementFilter)
	if problems, ok := errors.AsType[*resolver.SolverProblemsError](err); ok {
		msg := "Your requirements could not be resolved to an installable set of packages."
		prettyProblem, err := i.prettyProblem(problems, repositorySet, request, pool, false)
		if err != nil {
			return 0, err
		}

		i.io.WriteError("<error>"+msg+"</error>", true, io.Quiet)
		i.io.WriteError(prettyProblem, true, io.Normal)
		if !i.devMode {
			i.io.WriteError("<warning>Running update with --no-dev does not mean require-dev is ignored, it just means the packages will not be installed. If dev requirements are blocking the update you have to resolve those problems.</warning>", true, io.Quiet)
		}

		i.emitGithubActionError(msg + "\n" + prettyProblem)

		return max(ErrorGenericFailure, problems.Code()), nil
	}
	if err != nil {
		return 0, err
	}
	ruleSetSize := solver.RuleSetSize()

	i.io.WriteError("Analyzed "+strconv.Itoa(pool.Count())+" packages to resolve dependencies", true, io.Verbose)
	i.io.WriteError("Analyzed "+strconv.Itoa(ruleSetSize)+" rules to resolve dependencies", true, io.Verbose)

	if len(i.lockTransaction.Operations()) == 0 {
		i.io.WriteError(ui.RoleMuted.Wrap("Nothing to modify in lock file"), true, io.Normal)

		if i.minimalUpdate && i.updateAllowList == nil {
			fresh, err := i.locker.IsFresh()
			if err != nil {
				return 0, err
			}
			if fresh {
				i.io.WriteError("<warning>The --minimal-changes option should be used with package arguments or after modifying composer.json requirements, otherwise it will likely not yield any dependency changes.</warning>", true, io.Normal)
			}
		}
	}

	exitCode, err := i.extractDevPackages(i.lockTransaction, platformRepo, aliases, policy, lockedRepository)
	if err != nil || exitCode != 0 {
		return exitCode, err
	}

	semver.CompilingMatcher.Clear()

	// write lock
	platformReqs := extractPlatformRequirements(i.pkg.Requires())
	platformDevReqs := extractPlatformRequirements(i.pkg.DevRequires())

	var installsUpdates, uninstalls []operation.Operation
	if operations := i.lockTransaction.Operations(); len(operations) > 0 {
		var installNames, updateNames, uninstallNames []string
		for _, op := range operations {
			switch op := op.(type) {
			case *operation.InstallOperation:
				installsUpdates = append(installsUpdates, op)
				installNames = append(installNames, op.Package().PrettyName()+":"+op.Package().FullPrettyVersion(true, pkg.DisplaySourceRefIfDev))
			case *operation.UpdateOperation:
				// when mirrors/metadata from a package gets updated we do not want to list it as an
				// update in the output as it is only an internal lock file metadata update
				if i.updateMirrors &&
					op.InitialPackage().Name() == op.TargetPackage().Name() &&
					op.InitialPackage().Version() == op.TargetPackage().Version() {
					continue
				}

				installsUpdates = append(installsUpdates, op)
				updateNames = append(updateNames, op.TargetPackage().PrettyName()+":"+op.TargetPackage().FullPrettyVersion(true, pkg.DisplaySourceRefIfDev))
			case *operation.UninstallOperation:
				uninstalls = append(uninstalls, op)
				uninstallNames = append(uninstallNames, op.Package().PrettyName())
			}
		}

		lock, err := i.configBool("lock")
		if err != nil {
			return 0, err
		}
		if lock {
			i.io.WriteError(operationTally("Lock file operations", len(installNames), len(updateNames), len(uninstalls)), true, io.Normal)
			if len(installNames) > 0 {
				i.io.WriteError("Installs: "+strings.Join(installNames, ", "), true, io.Verbose)
			}
			if len(updateNames) > 0 {
				i.io.WriteError("Updates: "+strings.Join(updateNames, ", "), true, io.Verbose)
			}
			if len(uninstalls) > 0 {
				i.io.WriteError("Removals: "+strings.Join(uninstallNames, ", "), true, io.Verbose)
			}
		}
	}

	php.SortSlice(uninstalls, compareOperationsByName)
	php.SortSlice(installsUpdates, compareOperationsByName)

	lock, err := i.configBool("lock")
	if err != nil {
		return 0, err
	}
	for _, op := range slices.Concat(uninstalls, installsUpdates) {
		// collect suggestions
		if install, ok := op.(*operation.InstallOperation); ok {
			i.suggestedPackagesReporter.AddSuggestionsFromPackage(install.Package())
		}

		// output op if lock file is enabled, but alias op only in debug verbosity
		isAlias := op.OperationType().IsAlias()
		if lock && (!isAlias || i.io.IsDebug()) {
			sourceRepo := ""
			if i.io.IsVeryVerbose() && !isAlias {
				if repo := operationPackage(op).Repository(); repo != nil {
					sourceRepo = " from " + repo.RepoName()
				}
			}
			shown, err := operation.Item(op, true)
			if err != nil {
				return 0, err
			}
			i.io.WriteError(shown+sourceRepo, true, io.Normal)
		}
	}

	if err := i.setLockData(platformReqs, platformDevReqs, aliases); err != nil {
		return 0, err
	}

	if doInstall {
		// TODO ensure lock is used from locker as-is, since it may not have been written to disk in case of executeOperations == false
		res, err := i.doInstall(localRepo, true)

		return res, err
	}

	return 0, nil
}

func (i *Installer) setLockData(platformReqs, platformDevReqs, aliases *php.Array) error {
	packages, err := i.lockTransaction.NewLockPackages(false, i.updateMirrors)
	if err != nil {
		return err
	}
	devPackages, err := i.lockTransaction.NewLockPackages(true, i.updateMirrors)
	if err != nil {
		return err
	}
	platformOverrides, err := i.configArray("platform")
	if err != nil {
		return err
	}
	write := i.writeLock && i.executeOperations
	updatedLock, err := i.locker.SetLockData(locker.LockDataInput{
		Packages:          packages,
		DevPackages:       php.Some(devPackages), // an array, empty when there are none: never null
		PlatformReqs:      platformReqs,
		PlatformDevReqs:   platformDevReqs,
		Aliases:           i.lockTransaction.Aliases(aliases),
		MinimumStability:  i.pkg.MinimumStability(),
		StabilityFlags:    i.pkg.StabilityFlags(),
		PreferStable:      i.preferStable || i.pkg.PreferStable(),
		PreferLowest:      i.preferLowest,
		PlatformOverrides: platformOverrides,
	}, write)
	if err != nil {
		return err
	}
	if updatedLock && write {
		i.io.WriteError("<info>Writing lock file</info>", true, io.Normal)
	}

	return nil
}

// operationTally is Composer's "<info>label: N installs, N updates, N
// removals</info>", with zero counts muted when decorated (ui.Tally).
func operationTally(label string, installs, updates, removals int) string {
	return ui.Tally(label, ui.Count{N: installs, Noun: "install"}, ui.Count{N: updates, Noun: "update"}, ui.Count{N: removals, Noun: "removal"})
}

func boolString(b bool) string {
	if b {
		return "1"
	}

	return "0"
}

// operationPackage is `$operation instanceof UpdateOperation ?
// $operation->getTargetPackage() : $operation->getPackage()`.
func operationPackage(op operation.Operation) pkg.PackageInterface {
	switch op := op.(type) {
	case *operation.UpdateOperation:
		return op.TargetPackage()
	case *operation.InstallOperation:
		return op.Package()
	case *operation.UninstallOperation:
		return op.Package()
	case *operation.MarkAliasInstalledOperation:
		return op.Package()
	case *operation.MarkAliasUninstalledOperation:
		return op.Package()
	}

	return nil
}

// compareOperationsByName is doUpdate's $sortByName.
func compareOperationsByName(a, b operation.Operation) int {
	return strings.Compare(operationPackage(a).Name(), operationPackage(b).Name())
}

// isParsingException reports a Seld\JsonLint\ParsingException.
func isParsingException(err error) bool {
	return errors.As(err, new(*jsonlint.ParsingError))
}

// extractDevPackages runs the solver a second time on top of the existing
// update result with only the current result set in the pool and see what
// packages would get removed if we only had the non-dev packages in the
// solver request.
func (i *Installer) extractDevPackages(lockTransaction *resolver.LockTransaction, platformRepo *repository.PlatformRepository, aliases *php.Array, policy resolver.Policy, lockedRepository *repository.LockArrayRepository) (int, error) {
	if i.pkg.DevRequires().Len() == 0 {
		return 0, nil
	}

	resultRepo, err := repository.NewArrayRepository(nil)
	if err != nil {
		return 0, err
	}
	arrayLoader := loader.NewArrayLoader(nil, true)
	arrayDumper := dumper.ArrayDumper{}
	lockPackages, err := lockTransaction.NewLockPackages(false, false)
	if err != nil {
		return 0, err
	}
	for _, p := range lockPackages {
		dumped, err := arrayDumper.Dump(p)
		if err != nil {
			return 0, err
		}
		loaded, err := arrayLoader.Load(dumped, pkg.ClassCompletePackage)
		if err != nil {
			return 0, err
		}
		if err := resultRepo.AddPackage(loaded); err != nil {
			return 0, err
		}
	}

	repositorySet, err := i.createRepositorySet(true, platformRepo, aliases, nil)
	if err != nil {
		return 0, err
	}
	if err := repositorySet.AddRepository(resultRepo); err != nil {
		return 0, err
	}

	request, err := i.createRequest(i.fixedRootPackage, platformRepo, nil)
	if err != nil {
		return 0, err
	}
	if err := i.requirePackagesForUpdate(request, lockedRepository, false); err != nil {
		return 0, err
	}

	pool, err := resolver.CreatePoolWithAllPackages(repositorySet)
	if err != nil {
		return 0, err
	}

	solver := resolver.NewSolver(policy, pool, i.io)
	nonDevLockTransaction, err := solver.Solve(request, i.platformRequirementFilter)
	if problems, ok := errors.AsType[*resolver.SolverProblemsError](err); ok {
		msg := "Unable to find a compatible set of packages based on your non-dev requirements alone."
		prettyProblem, err := i.prettyProblem(problems, repositorySet, request, pool, true)
		if err != nil {
			return 0, err
		}

		i.io.WriteError("<error>"+msg+"</error>", true, io.Quiet)
		i.io.WriteError("Your requirements can be resolved successfully when require-dev packages are present.", true, io.Normal)
		i.io.WriteError("You may need to move packages from require-dev or some of their dependencies to require.", true, io.Normal)
		i.io.WriteError(prettyProblem, true, io.Normal)

		i.emitGithubActionError(msg + "\n" + prettyProblem)

		return problems.Code(), nil
	}
	if err != nil {
		return 0, err
	}

	return 0, lockTransaction.SetNonDevPackages(nonDevLockTransaction)
}

func (i *Installer) doInstall(localRepo repository.InstalledRepositoryInterface, alreadySolved bool) (int, error) {
	if i.runtime != nil {
		// run() calls doInstall($localRepo), doUpdate() doInstall($localRepo, true)
		args := []any{localRepo}
		if alreadySolved {
			args = append(args, true)
		}
		i.runtime.PushFrame(`Composer\Installer->doInstall`, i, args...)
		defer i.runtime.PopFrame()
	}
	lock, err := i.configBool("lock")
	if err != nil {
		return 0, err
	}
	if lock {
		dev := ""
		if i.devMode {
			dev = " (including require-dev)"
		}
		i.io.WriteError("<info>Installing dependencies from lock file"+dev+"</info>", true, io.Normal)
	}

	lockedRepository, err := i.locker.LockedRepository(i.devMode)
	if err != nil {
		return 0, err
	}

	// verify that the lock file works with the current platform repository
	// we can skip this part if we're doing this as the second step after an update
	var ahead *aheadWork
	var early *localRepoTransaction
	if !alreadySolved {
		// the work started while the lock is verified ends with doInstall,
		// whichever way it returns, but the scan a no-op install hands over
		early = newLocalRepoTransaction(lockedRepository, localRepo)
		ahead = i.startAhead(localRepo, early.transaction)
		defer ahead.Discard()
		if code, err := i.verifyLock(lockedRepository); err != nil || code != 0 {
			return code, err
		}
	}

	// TODO in how far do we need to do anything here to ensure dev packages being updated to latest in lock without version change are treated correctly?
	var localRepoTransaction *resolver.Transaction
	if early != nil {
		localRepoTransaction, err = early.take()
	} else {
		localRepoTransaction, err = resolver.NewLocalRepoTransaction(lockedRepository, localRepo)
	}
	if err != nil {
		return 0, err
	}
	if len(localRepoTransaction.Operations()) != 0 {
		ahead.discardNoOperations()
	}
	if _, err := i.eventDispatcher.DispatchInstallerEvent(installerPreOperationsExec, i.devMode, i.executeOperations, localRepoTransaction); err != nil {
		return 0, err
	}

	operations := localRepoTransaction.Operations()
	var installs, updates, uninstalls []string
	for _, op := range operations {
		switch op := op.(type) {
		case *operation.InstallOperation:
			installs = append(installs, op.Package().PrettyName()+":"+op.Package().FullPrettyVersion(true, pkg.DisplaySourceRefIfDev))
		case *operation.UpdateOperation:
			updates = append(updates, op.TargetPackage().PrettyName()+":"+op.TargetPackage().FullPrettyVersion(true, pkg.DisplaySourceRefIfDev))
		case *operation.UninstallOperation:
			uninstalls = append(uninstalls, op.Package().PrettyName())
		}
	}

	if len(installs) == 0 && len(updates) == 0 && len(uninstalls) == 0 {
		i.io.WriteError(ui.RoleMuted.Wrap("Nothing to install, update or remove"), true, io.Normal)
	} else {
		i.io.WriteError(operationTally("Package operations", len(installs), len(updates), len(uninstalls)), true, io.Normal)
		if len(installs) > 0 {
			i.io.WriteError("Installs: "+strings.Join(installs, ", "), true, io.Verbose)
		}
		if len(updates) > 0 {
			i.io.WriteError("Updates: "+strings.Join(updates, ", "), true, io.Verbose)
		}
		if len(uninstalls) > 0 {
			i.io.WriteError("Removals: "+strings.Join(uninstalls, ", "), true, io.Verbose)
		}
	}

	if !i.executeOperations {
		for _, op := range operations {
			// output op, but alias op only in debug verbosity
			if !op.OperationType().IsAlias() || i.io.IsDebug() {
				shown, err := operation.Item(op, false)
				if err != nil {
					return 0, err
				}
				i.io.WriteError(shown, true, io.Normal)
			}
		}

		return 0, nil
	}

	devPackageNames, err := i.locker.DevPackageNames()
	if err != nil {
		return 0, err
	}
	localRepo.SetDevPackageNames(devPackageNames)
	if err := i.installationManager.Execute(localRepo, operations, i.devMode, i.runScripts, i.downloadOnly); err != nil {
		return 0, err
	}
	i.executedOperations = i.executedOperations || len(operations) > 0

	// see https://github.com/composer/composer/issues/2764
	if len(operations) > 0 {
		vendorDir, err := i.config.Get("vendor-dir", 0)
		if err != nil {
			return 0, err
		}
		dir := php.ToString(vendorDir)
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			// suppress errors as this fails sometimes on OSX for no apparent reason
			// see https://github.com/composer/composer/issues/4070#issuecomment-129792748
			now := time.Now()
			_ = os.Chtimes(dir, now, now)
		}
	}
	// the dump that follows takes the scan a no-op install started
	ahead.keepAutoloads()

	return 0, nil
}

// maintainStore prunes the package store when it is due, after an install
// that ran package operations (DownloadManager.MaintainStore).
func (i *Installer) maintainStore() {
	if i.executedOperations && i.downloadManager != nil {
		i.downloadManager.MaintainStore()
	}
}

// verifyLock is doInstall's check that the lock file works with the
// current platform repository.
func (i *Installer) verifyLock(lockedRepository *repository.LockArrayRepository) (int, error) {
	i.io.WriteError("<info>Verifying lock file contents can be installed on current platform.</info>", true, io.Normal)

	platformRepo, err := i.createPlatformRepo(false)
	if err != nil {
		return 0, err
	}
	// creating repository set
	policy, err := i.createPolicy(false, nil)
	if err != nil {
		return 0, err
	}
	// use aliases from lock file only, so empty root aliases here
	repositorySet, err := i.createRepositorySet(false, platformRepo, nil, lockedRepository)
	if err != nil {
		return 0, err
	}
	if err := repositorySet.AddRepository(lockedRepository); err != nil {
		return 0, err
	}

	// creating requirements request
	request, err := i.createRequest(i.fixedRootPackage, platformRepo, lockedRepository)
	if err != nil {
		return 0, err
	}

	fresh, err := i.locker.IsFresh()
	if err != nil {
		return 0, err
	}
	if !fresh {
		i.io.WriteError("<warning>Warning: The lock file is not up to date with the latest changes in composer.json. You may be getting outdated dependencies. It is recommended that you run `composer update` or `composer update <package name>`.</warning>", true, io.Quiet)
	}

	missingRequirementInfo, err := i.locker.MissingRequirementInfo(i.pkg, i.devMode)
	if err != nil {
		return 0, err
	}
	if len(missingRequirementInfo) > 0 {
		i.io.WriteErrorMessages(missingRequirementInfo, true, io.Normal)

		allowMissing, err := i.configBool("allow-missing-requirements")
		if err != nil {
			return 0, err
		}
		if !allowMissing {
			return ErrorLockFileInvalid, nil
		}
	}

	lockedPackages, err := lockedRepository.Packages()
	if err != nil {
		return 0, err
	}
	for _, p := range lockedPackages {
		request.FixLockedPackage(p)
	}

	rootRequires := i.pkg.Requires()
	if i.devMode {
		rootRequires = repository.MergeLinks(rootRequires, i.pkg.DevRequires())
	}
	for _, link := range rootRequires.All() {
		if pkg.IsPlatformPackage(link.Target()) {
			if err := request.RequireName(link.Target(), link.Constraint()); err != nil {
				return 0, err
			}
		}
	}

	platformRequirements, err := i.locker.PlatformRequirements(i.devMode)
	if err != nil {
		return 0, err
	}
	for _, link := range platformRequirements.All() {
		if !rootRequires.Has(link.Target()) {
			if err := request.RequireName(link.Target(), link.Constraint()); err != nil {
				return 0, err
			}
		}
	}

	filterListFilter, err := i.createFilterListPoolFilter(policyBlockScopeInstall)
	if err != nil {
		return 0, err
	}
	pool, err := resolver.CreatePool(repositorySet, request, i.io, resolver.CreatePoolOptions{
		EventDispatcher:      i.poolEventDispatcher(),
		IgnoredTypes:         i.ignoredTypes,
		AllowedTypes:         i.allowedTypes,
		FilterListPoolFilter: filterListFilter,
	})
	if err != nil {
		return 0, err
	}

	// solve dependencies
	lockTransaction, err := resolver.NewSolver(policy, pool, i.io).Solve(request, i.platformRequirementFilter)
	if problems, ok := errors.AsType[*resolver.SolverProblemsError](err); ok {
		msg := "Your lock file does not contain a compatible set of packages. Please run composer update."
		prettyProblem, err := i.prettyProblem(problems, repositorySet, request, pool, false)
		if err != nil {
			return 0, err
		}

		i.io.WriteError("<error>"+msg+"</error>", true, io.Quiet)
		i.io.WriteError(prettyProblem, true, io.Normal)

		i.emitGithubActionError(msg + "\n" + prettyProblem)

		return max(ErrorGenericFailure, problems.Code()), nil
	}
	if err != nil {
		return 0, err
	}

	// installing the locked packages on this platform resulted in lock modifying operations, there wasn't a conflict, but the lock file as-is seems to not work on this system
	if len(lockTransaction.Operations()) != 0 {
		i.io.WriteError("<error>Your lock file cannot be installed on this system without changes. Please run composer update.</error>", true, io.Quiet)

		return ErrorLockFileInvalid, nil
	}

	return 0, nil
}

// poolEventDispatcher returns the dispatcher for createPool, never a
// typed nil.
func (i *Installer) poolEventDispatcher() resolver.EventDispatcher {
	if i.eventDispatcher == nil {
		return nil
	}

	return i.eventDispatcher
}

func (i *Installer) createPlatformRepo(forUpdate bool) (*repository.PlatformRepository, error) {
	var platformOverrides *php.Array
	var err error
	if forUpdate {
		platformOverrides, err = i.configArray("platform")
	} else {
		platformOverrides, err = i.locker.PlatformOverrides()
	}
	if err != nil {
		return nil, err
	}

	var opts repository.PlatformOptions
	if i.runtime != nil {
		var process platform.HhvmExecutor
		if i.process != nil {
			process = i.process
		}
		if opts, err = i.runtime.PlatformOptions(process); err != nil {
			return nil, err
		}
	}

	return repository.NewPlatformRepository(nil, platformOverrides, opts)
}

func (i *Installer) createRepositorySet(forUpdate bool, platformRepo *repository.PlatformRepository, rootAliases *php.Array, lockedRepository repository.RepositoryInterface) (*repository.RepositorySet, error) {
	var minimumStability string
	var stabilityFlags *php.Array
	var requires []struct {
		name       string
		constraint semver.ConstraintInterface
	}
	if forUpdate {
		minimumStability = i.pkg.MinimumStability()
		stabilityFlags = i.pkg.StabilityFlags()

		for name, link := range repository.MergeLinks(i.pkg.Requires(), i.pkg.DevRequires()).All() {
			requires = append(requires, struct {
				name       string
				constraint semver.ConstraintInterface
			}{name, link.Constraint()})
		}
	} else {
		var err error
		if minimumStability, err = i.locker.MinimumStability(); err != nil {
			return nil, err
		}
		if stabilityFlags, err = i.locker.StabilityFlags(); err != nil {
			return nil, err
		}

		packages, err := lockedRepository.Packages()
		if err != nil {
			return nil, err
		}
		byName := &repository.ConstraintMap{}
		for _, p := range packages {
			constraint := semver.NewConstraintOp(semver.OpEQ, p.Version())
			constraint.SetPrettyString(p.PrettyVersion())
			byName.Set(p.Name(), constraint)
		}
		for name, constraint := range byName.All() {
			requires = append(requires, struct {
				name       string
				constraint semver.ConstraintInterface
			}{name, constraint})
		}
	}

	ignoreList, _ := i.platformRequirementFilter.(version.IgnoreListPlatformRequirementFilter)
	rootRequires := &repository.ConstraintMap{}
	for _, req := range requires {
		constraint := req.constraint
		// skip platform requirements from the root package to avoid filtering out existing platform packages
		if i.platformRequirementFilter.IsIgnored(req.name) {
			continue
		} else if ignoreList != nil {
			constraint = ignoreList.FilterConstraint(req.name, constraint)
		}
		rootRequires.Set(req.name, constraint)
	}

	fixed := cloneRoot(i.pkg)
	fixed.SetRequires(pkg.Links{})
	fixed.SetDevRequires(pkg.Links{})
	i.fixedRootPackage = fixed

	if stabilityFlags == nil {
		stabilityFlags = php.NewArray()
	} else {
		stabilityFlags = stabilityFlags.Clone()
	}
	stability, _ := pkg.StabilityValue(semver.ParseStability(i.pkg.Version()))
	stabilityFlags.Set(i.pkg.Name(), int64(stability))

	var aliases []repository.RootAlias
	if rootAliases != nil {
		aliases = repository.RootAliasesFromArray(rootAliases)
	}
	repositorySet, err := repository.NewRepositorySet(minimumStability, stabilityFlags, aliases, i.pkg.References(), rootRequires, i.temporaryConstraints)
	if err != nil {
		return nil, err
	}
	rootRepo, err := repository.NewRootPackageRepository(fixed)
	if err != nil {
		return nil, err
	}
	if err := repositorySet.AddRepository(rootRepo); err != nil {
		return nil, err
	}
	if err := repositorySet.AddRepository(platformRepo); err != nil {
		return nil, err
	}
	if i.additionalFixedRepository != nil {
		// allow using installed repos if needed to avoid warnings about installed repositories being used in the RepositorySet
		// see https://github.com/composer/composer/pull/9574
		additional := []repository.RepositoryInterface{i.additionalFixedRepository}
		if composite, ok := repository.AsComposite(i.additionalFixedRepository); ok {
			additional = composite.Repositories()
		}
		for _, repo := range additional {
			_, isInstalled := repo.(*repository.InstalledRepository)
			_, isInstalledInterface := repo.(repository.InstalledRepositoryInterface)
			if isInstalled || isInstalledInterface {
				repositorySet.AllowInstalledRepositories(true)

				break
			}
		}

		if err := repositorySet.AddRepository(i.additionalFixedRepository); err != nil {
			return nil, err
		}
	}

	return repositorySet, nil
}

func (i *Installer) createPolicy(forUpdate bool, lockedRepo *repository.LockArrayRepository) (*resolver.DefaultPolicy, error) {
	var preferStable, preferLowest, stableOK, lowestOK bool
	if !forUpdate {
		var err error
		if preferStable, stableOK, err = i.locker.PreferStable(); err != nil {
			return nil, err
		}
		if preferLowest, lowestOK, err = i.locker.PreferLowest(); err != nil {
			return nil, err
		}
	}
	// old lock file without prefer stable/lowest will return null
	// so in this case we use the composer.json info
	if !stableOK {
		preferStable = i.preferStable || i.pkg.PreferStable()
	}
	if !lowestOK {
		preferLowest = i.preferLowest
	}

	var preferredVersions map[string]string
	if forUpdate && i.minimalUpdate && lockedRepo != nil {
		preferredVersions = map[string]string{}
		packages, err := lockedRepo.Packages()
		if err != nil {
			return nil, err
		}
		for _, p := range packages {
			if _, isAlias := p.(pkg.Alias); isAlias || (i.updateAllowList != nil && slices.Contains(i.updateAllowList, p.Name())) {
				continue
			}
			preferredVersions[p.Name()] = p.Version()
		}
	}

	return resolver.NewDefaultPolicy(preferStable, preferLowest, preferredVersions), nil
}

func (i *Installer) createRequest(rootPackage pkg.RootPackageInterface, platformRepo *repository.PlatformRepository, lockedRepository *repository.LockArrayRepository) (*resolver.Request, error) {
	request := resolver.NewRequest(lockedRepository)

	request.FixPackage(rootPackage)
	if alias, ok := rootPackage.(*pkg.RootAliasPackage); ok {
		request.FixPackage(alias.AliasOf())
	}

	fixedPackages, err := platformRepo.Packages()
	if err != nil {
		return nil, err
	}
	if i.runtime != nil {
		v, _ := platformRepo.PlatformPhpVersion()
		i.runtime.SetPlatformPHPVersion(v)
	}
	if i.additionalFixedRepository != nil {
		additional, err := i.additionalFixedRepository.Packages()
		if err != nil {
			return nil, err
		}
		fixedPackages = slices.Concat(fixedPackages, additional)
	}

	// fix the version of all platform packages + additionally installed packages
	// to prevent the solver trying to remove or update those
	// TODO why not replaces?
	provided := rootPackage.Provides()
	for _, p := range fixedPackages {
		// skip platform packages that are provided by the root package
		link, isProvided := provided.Get(p.Name())
		if p.Repository() != pkg.Repository(platformRepo) || !isProvided || !link.Constraint().Matches(semver.NewConstraintOp(semver.OpEQ, p.Version())) {
			request.FixPackage(p)
		}
	}

	return request, nil
}

func (i *Installer) requirePackagesForUpdate(request *resolver.Request, lockedRepository *repository.LockArrayRepository, includeDevRequires bool) error {
	// if we're updating mirrors we want to keep exactly the same versions installed which are in the lock file, but we want current remote metadata
	if i.updateMirrors {
		excludedPackages := map[string]bool{}
		if !includeDevRequires {
			names, err := i.locker.DevPackageNames()
			if err != nil {
				return err
			}
			for _, name := range names {
				excludedPackages[name] = true
			}
		}

		packages, err := lockedRepository.Packages()
		if err != nil {
			return err
		}
		for _, lockedPackage := range packages {
			// exclude alias packages here as for root aliases, both alias and aliased are
			// present in the lock repo and we only want to require the aliased version
			if _, isAlias := lockedPackage.(pkg.Alias); !isAlias && !excludedPackages[lockedPackage.Name()] {
				if err := request.RequireName(lockedPackage.Name(), semver.NewConstraintOp(semver.OpEQ, lockedPackage.Version())); err != nil {
					return err
				}
			}
		}

		return nil
	}

	links := i.pkg.Requires()
	if includeDevRequires {
		links = repository.MergeLinks(links, i.pkg.DevRequires())
	}
	for _, link := range links.All() {
		if err := request.RequireName(link.Target(), link.Constraint()); err != nil {
			return err
		}
	}

	return nil
}

func (i *Installer) getRootAliases(forUpdate bool) (*php.Array, error) {
	if forUpdate {
		return i.pkg.Aliases(), nil
	}

	return i.locker.Aliases()
}

// cloneRoot is `clone $rootPackage`.
func cloneRoot(p pkg.RootPackageInterface) pkg.RootPackageInterface {
	if root, ok := pkg.Clone(p).(pkg.RootPackageInterface); ok {
		return root
	}

	return p
}

// extractPlatformRequirements ports extractPlatformRequirements: platform
// package name => pretty constraint.
func extractPlatformRequirements(links pkg.Links) *php.Array {
	platformReqs := php.NewArray()
	for _, link := range links.All() {
		if pkg.IsPlatformPackage(link.Target()) {
			pretty, _ := link.PrettyConstraint()
			platformReqs.Set(link.Target(), pretty)
		}
	}

	return platformReqs
}

// mockLocalRepositories replaces the local repository with an
// InstalledArrayRepository, to prevent any accidental modification of the
// existing repos on disk.
func mockLocalRepositories(rm *repository.RepositoryManager) error {
	localPackages, err := rm.LocalRepository().Packages()
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(localPackages))
	packages := map[string]pkg.PackageInterface{}
	for _, p := range localPackages {
		key := p.String()
		if _, ok := packages[key]; !ok {
			keys = append(keys, key)
		}
		packages[key] = pkg.Clone(p)
	}
	for _, key := range keys {
		alias, ok := packages[key].(pkg.Alias)
		if !ok {
			continue
		}
		aliasOf := packages[alias.AliasOf().String()]
		p := packages[key]
		complete, isComplete := aliasOf.(pkg.CompletePackageInterface)
		root, isRoot := aliasOf.(pkg.RootPackageInterface)
		switch p.(type) {
		case *pkg.CompleteAliasPackage:
			if isComplete {
				packages[key] = pkg.NewCompleteAliasPackage(complete, p.Version(), p.PrettyVersion())

				continue
			}
		case *pkg.RootAliasPackage:
			if isRoot {
				packages[key] = pkg.NewRootAliasPackage(root, p.Version(), p.PrettyVersion())

				continue
			}
		}
		packages[key] = pkg.NewAliasPackage(aliasOf, p.Version(), p.PrettyVersion())
	}
	list := make([]pkg.PackageInterface, len(keys))
	for n, key := range keys {
		list[n] = packages[key]
	}
	repo, err := repository.NewInstalledArrayRepository(list)
	if err != nil {
		return err
	}
	rm.SetLocalRepository(repo)

	return nil
}

func (i *Installer) createPoolOptimizer(policy resolver.Policy) *resolver.PoolOptimizer {
	// Not the best architectural decision here, would need to be able
	// to configure from the outside of Installer but this is only
	// a debugging tool and should never be required in any other use case
	if v, ok := util.GetEnv("COMPOSER_POOL_OPTIMIZER"); ok && v == "0" {
		i.io.Write("Pool Optimizer was disabled for debugging purposes.", true, io.Debug)

		return nil
	}

	return resolver.NewPoolOptimizer(policy)
}

func (i *Installer) getPolicyConfig() (*policy.PolicyConfig, error) {
	if i.policyConfig == nil {
		cfg, err := policy.FromConfig(i.config)
		if err != nil {
			return nil, err
		}
		i.policyConfig = cfg
	}

	return i.policyConfig, nil
}

func (i *Installer) getAuditConfig() advisory.AuditConfig {
	if i.auditConfig == nil {
		i.auditConfig = &advisory.AuditConfig{Audit: i.audit, AuditFormat: i.auditFormat}
	}

	return *i.auditConfig
}

func (i *Installer) createSecurityAuditPoolFilter() (*resolver.SecurityAdvisoryPoolFilter, error) {
	policyConfig, err := i.getPolicyConfig()
	if err != nil {
		return nil, err
	}

	if policyConfig.Enabled && policyConfig.Advisories.ShouldBlock("update") && !i.updateMirrors {
		return resolver.NewSecurityAdvisoryPoolFilter(advisory.Auditor{}, policyConfig, i.io), nil
	}

	return nil, nil
}

const (
	policyBlockScopeUpdate     = policy.BlockScopeUpdate
	policyBlockScopeInstall    = policy.BlockScopeInstall
	installerPreOperationsExec = "pre-operations-exec"
)

func (i *Installer) createFilterListPoolFilter(blockScope string) (*resolver.FilterListPoolFilter, error) {
	policyConfig, err := i.getPolicyConfig()
	if err != nil {
		return nil, err
	}

	if !policyConfig.Enabled {
		return nil, nil
	}

	hasUpdateLists := policyConfig.ActiveBlockFilterLists(policy.BlockScopeUpdate).Len() > 0
	hasInstallLists := policyConfig.ActiveBlockFilterLists(policy.BlockScopeInstall).Len() > 0

	if blockScope == policy.BlockScopeInstall && !hasInstallLists {
		return nil, nil
	}

	// For UPDATE scope we may also need install-scope lists for locked
	// packages (so a malware-flagged dependency in the lock file is still
	// blocked during a partial update / `update mirrors`).
	if blockScope == policy.BlockScopeUpdate && !hasUpdateLists && !hasInstallLists {
		return nil, nil
	}

	return resolver.NewFilterListPoolFilter(policyConfig, filterlist.FilterListAuditor{}, getter(i.repositoryManager), blockScope, i.repositoryManager.Repositories(), i.io), nil
}

// autoloadWarmer is an AutoloadGenerator that can parse ahead
// (GeneratorAdapter).
type autoloadWarmer interface {
	WarmAutoloads(config ConfigReader, localRepo repository.InstalledRepositoryInterface, root pkg.RootPackageInterface, im InstallationManager, scanPsrPackages bool)
}

// autoloadParser is an AutoloadGenerator that can parse the files of
// packages as the package store inserts them (GeneratorAdapter).
type autoloadParser interface {
	ParseAhead(scanPsrPackages bool)
}

// asyncNotifier is an InstallationManager that can send the install
// notifications without waiting for them (installer.Manager).
type asyncNotifier interface {
	NotifyInstallsAsync(out io.IO) (waitFor func())
}

// prefetchMetadata starts, ahead of the pool builder's waves of requests,
// the metadata requests of the packages an update will almost certainly
// load: the root's requirements and the locked packages (deliberate
// deviation 3: the pool builder still asks for them in Composer's order
// and prints what Composer prints; it just finds them answered).
func (i *Installer) prefetchMetadata(set *repository.RepositorySet, locked *repository.LockArrayRepository) {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		if name = php.Strtolower(name); !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	// the root package's: the fixed root package has none
	for _, links := range []pkg.Links{i.pkg.Requires(), i.pkg.DevRequires()} {
		for name := range links.All() {
			add(name)
		}
	}
	if locked != nil {
		if packages, err := locked.Packages(); err == nil {
			for _, p := range packages {
				add(p.Name())
			}
		}
	}

	repository.PrefetchPackages(set.Repositories(), names, set.AcceptableStabilities(), set.StabilityFlags())
}

// filterSummaryPrefetcher is a repository that can start its filter list
// summary request ahead (composerrepo.ComposerRepository).
type filterSummaryPrefetcher interface {
	PrefetchFilterSummary()
}

// prefetchFilterSummaries starts the filter list summary requests the
// filter list pool filter will make, when it will run (deliberate
// deviation 3: an install from the lock otherwise waits on that round trip
// after reading the lock).
func (i *Installer) prefetchFilterSummaries() {
	policyConfig, err := i.getPolicyConfig()
	if err != nil || !policyConfig.Enabled {
		return
	}
	if policyConfig.ActiveBlockFilterLists(policy.BlockScopeInstall).Len() == 0 && (!i.update || policyConfig.ActiveBlockFilterLists(policy.BlockScopeUpdate).Len() == 0) {
		return
	}

	for _, repo := range i.repositoryManager.Repositories() {
		if p, ok := repo.(filterSummaryPrefetcher); ok {
			p.PrefetchFilterSummary()
		}
	}
}
