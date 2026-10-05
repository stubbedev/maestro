package composer

import (
	"slices"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/filter"
	"github.com/stubbedev/maestro/internal/installer"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
	"github.com/stubbedev/maestro/internal/util"
)

// The Installer's setters, ported from Installer.php. They return the
// installer, as PHP's fluent setters do.

// SetIgnoredTypes ports setIgnoredTypes: packages of those types are
// ignored (php-ext and php-ext-zend by default).
func (i *Installer) SetIgnoredTypes(types []string) *Installer {
	i.ignoredTypes = types

	return i
}

// SetAllowedTypes ports setAllowedTypes: only packages of those types are
// allowed when non-nil.
func (i *Installer) SetAllowedTypes(types []string) *Installer {
	i.allowedTypes = types

	return i
}

// SetAdditionalFixedRepository ports setAdditionalFixedRepository.
func (i *Installer) SetAdditionalFixedRepository(repo repository.RepositoryInterface) *Installer {
	i.additionalFixedRepository = repo

	return i
}

// SetTemporaryConstraints ports setTemporaryConstraints.
func (i *Installer) SetTemporaryConstraints(constraints *repository.ConstraintMap) *Installer {
	if constraints == nil {
		constraints = &repository.ConstraintMap{}
	}
	i.temporaryConstraints = constraints

	return i
}

// SetDryRun ports setDryRun.
func (i *Installer) SetDryRun(dryRun bool) *Installer {
	i.dryRun = dryRun

	return i
}

// IsDryRun ports isDryRun.
func (i *Installer) IsDryRun() bool { return i.dryRun }

// SetDownloadOnly ports setDownloadOnly.
func (i *Installer) SetDownloadOnly(downloadOnly bool) *Installer {
	i.downloadOnly = downloadOnly

	return i
}

// SetPreferSource ports setPreferSource.
func (i *Installer) SetPreferSource(preferSource bool) *Installer {
	i.preferSource = preferSource

	return i
}

// SetPreferDist ports setPreferDist.
func (i *Installer) SetPreferDist(preferDist bool) *Installer {
	i.preferDist = preferDist

	return i
}

// SetOptimizeAutoloader ports setOptimizeAutoloader: turning it off also
// turns off the authoritative class map.
func (i *Installer) SetOptimizeAutoloader(optimizeAutoloader bool) *Installer {
	i.optimizeAutoloader = optimizeAutoloader
	if !i.optimizeAutoloader {
		// Force classMapAuthoritative off when not optimizing the
		// autoloader
		i.SetClassMapAuthoritative(false)
	}

	return i
}

// SetClassMapAuthoritative ports setClassMapAuthoritative: turning it on
// also optimizes the autoloader.
func (i *Installer) SetClassMapAuthoritative(classMapAuthoritative bool) *Installer {
	i.classMapAuthoritative = classMapAuthoritative
	if i.classMapAuthoritative {
		// Force optimizeAutoloader when classmap is authoritative
		i.SetOptimizeAutoloader(true)
	}

	return i
}

// SetApcuAutoloader ports setApcuAutoloader; a nil prefix is null.
func (i *Installer) SetApcuAutoloader(apcuAutoloader bool, apcuAutoloaderPrefix *string) *Installer {
	i.apcuAutoloader = apcuAutoloader
	i.apcuAutoloaderPrefix = apcuAutoloaderPrefix

	return i
}

// SetStrictPsrAutoloader ports setStrictPsrAutoloader.
func (i *Installer) SetStrictPsrAutoloader(strictPsr bool) *Installer {
	i.strictPsrAutoloader = strictPsr

	return i
}

// SetUpdate ports setUpdate.
func (i *Installer) SetUpdate(update bool) *Installer {
	i.update = update

	return i
}

// SetInstall ports setInstall: false skips the install step after an
// update.
func (i *Installer) SetInstall(install bool) *Installer {
	i.install = install

	return i
}

// SetDevMode ports setDevMode.
func (i *Installer) SetDevMode(devMode bool) *Installer {
	i.devMode = devMode

	return i
}

// SetDumpAutoloader ports setDumpAutoloader.
func (i *Installer) SetDumpAutoloader(dumpAutoloader bool) *Installer {
	i.dumpAutoloader = dumpAutoloader

	return i
}

// SetRunScripts ports the deprecated setRunScripts.
func (i *Installer) SetRunScripts(runScripts bool) *Installer {
	i.runScripts = runScripts

	return i
}

// SetConfig ports setConfig.
func (i *Installer) SetConfig(config ConfigReader) *Installer {
	i.config = config

	return i
}

// SetVerbose ports setVerbose.
func (i *Installer) SetVerbose(verbose bool) *Installer {
	i.verbose = verbose

	return i
}

// IsVerbose ports isVerbose.
func (i *Installer) IsVerbose() bool { return i.verbose }

// SetIgnorePlatformRequirements ports the deprecated
// setIgnorePlatformRequirements: true, false or a list of names.
func (i *Installer) SetIgnorePlatformRequirements(ignorePlatformReqs any) (*Installer, error) {
	f, err := filter.FromBoolOrList(ignorePlatformReqs)
	if err != nil {
		return nil, err
	}

	return i.SetPlatformRequirementFilter(f), nil
}

// SetPlatformRequirementFilter ports setPlatformRequirementFilter.
func (i *Installer) SetPlatformRequirementFilter(f version.PlatformRequirementFilter) *Installer {
	i.platformRequirementFilter = f

	return i
}

// PlatformRequirementFilter is the protected $platformRequirementFilter
// (plugins read it through an array cast).
func (i *Installer) PlatformRequirementFilter() version.PlatformRequirementFilter {
	return i.platformRequirementFilter
}

// SetUpdateMirrors ports setUpdateMirrors: update the lock file to the
// exact same versions and references but use current remote metadata like
// URLs and mirror info.
func (i *Installer) SetUpdateMirrors(updateMirrors bool) *Installer {
	i.updateMirrors = updateMirrors

	return i
}

// SetUpdateAllowList ports setUpdateAllowList: restrict the update
// operation to a few packages (names or patterns).
func (i *Installer) SetUpdateAllowList(packages []string) *Installer {
	if len(packages) == 0 {
		i.updateAllowList = nil

		return i
	}

	list := make([]string, 0, len(packages))
	for _, p := range packages {
		p = php.Strtolower(p)
		if !slices.Contains(list, p) {
			list = append(list, p)
		}
	}
	i.updateAllowList = list

	return i
}

// SetUpdateAllowTransitiveDependencies ports
// setUpdateAllowTransitiveDependencies: one of the resolver.Update*
// constants.
func (i *Installer) SetUpdateAllowTransitiveDependencies(updateAllowTransitiveDependencies int) (*Installer, error) {
	switch updateAllowTransitiveDependencies {
	case resolver.UpdateOnlyListed, resolver.UpdateListedWithTransitiveDepsNoRootRequire, resolver.UpdateListedWithTransitiveDeps:
	default:
		return nil, &util.RuntimeError{Site: phperr.At("Installer.php", 1509), Message: "Invalid value for updateAllowTransitiveDependencies supplied"}
	}

	i.updateAllowTransitiveDependencies = updateAllowTransitiveDependencies

	return i, nil
}

// SetPreferStable ports setPreferStable.
func (i *Installer) SetPreferStable(preferStable bool) *Installer {
	i.preferStable = preferStable

	return i
}

// SetPreferLowest ports setPreferLowest.
func (i *Installer) SetPreferLowest(preferLowest bool) *Installer {
	i.preferLowest = preferLowest

	return i
}

// SetMinimalUpdate ports setMinimalUpdate: in partial updates, prefer the
// locked versions of the packages not in the allow list.
func (i *Installer) SetMinimalUpdate(minimalUpdate bool) *Installer {
	i.minimalUpdate = minimalUpdate

	return i
}

// SetWriteLock ports setWriteLock.
func (i *Installer) SetWriteLock(writeLock bool) *Installer {
	i.writeLock = writeLock

	return i
}

// SetExecuteOperations ports setExecuteOperations.
func (i *Installer) SetExecuteOperations(executeOperations bool) *Installer {
	i.executeOperations = executeOperations

	return i
}

// SetAudit ports the deprecated setAudit.
func (i *Installer) SetAudit(audit bool) *Installer {
	i.audit = audit
	i.auditConfig = nil // Invalidate cached config

	return i
}

// SetErrorOnAudit ports setErrorOnAudit: exit with ErrorAuditFailed when
// the audit finds problems.
func (i *Installer) SetErrorOnAudit(errorOnAudit bool) *Installer {
	i.errorOnAudit = errorOnAudit

	return i
}

// SetAuditFormat ports the deprecated setAuditFormat.
func (i *Installer) SetAuditFormat(auditFormat string) *Installer {
	i.auditFormat = auditFormat
	i.auditConfig = nil // Invalidate cached config

	return i
}

// SetAuditConfig ports setAuditConfig.
func (i *Installer) SetAuditConfig(auditConfig advisory.AuditConfig) *Installer {
	i.auditConfig = &auditConfig

	return i
}

// SetPolicyConfig ports setPolicyConfig.
func (i *Installer) SetPolicyConfig(policyConfig *policy.PolicyConfig) *Installer {
	i.policyConfig = policyConfig

	return i
}

// DisablePlugins ports disablePlugins.
func (i *Installer) DisablePlugins() (*Installer, error) {
	return i, i.installationManager.DisablePlugins()
}

// SetSuggestedPackagesReporter ports setSuggestedPackagesReporter.
func (i *Installer) SetSuggestedPackagesReporter(reporter *installer.SuggestedPackagesReporter) *Installer {
	i.suggestedPackagesReporter = reporter

	return i
}

// LockTransaction ports getLockTransaction; nil is null.
func (i *Installer) LockTransaction() *resolver.LockTransaction { return i.lockTransaction }
