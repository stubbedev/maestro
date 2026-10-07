// Ports src/Composer/Installer.php (its properties, as the plugin runtime
// shows them to PHP).

package composer

import (
	"slices"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/autoload"
	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/installer"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
)

// InstallerState is what an Installer holds: Composer\Installer's
// properties, which plugins read from the object on Composer's stack
// (docs/PLUGINS.md §5.12: php-http/discovery and symfony/flex clone the
// running Installer and read its protected platformRequirementFilter).
// Collaborators maestro adapted are given as their own objects: a nil
// field is PHP's null.
type InstallerState struct {
	IO                  io.IO
	Config              ConfigReader
	Package             pkg.RootPackageInterface
	FixedRootPackage    pkg.RootPackageInterface
	DownloadManager     *downloader.DownloadManager
	RepositoryManager   *repository.RepositoryManager
	Locker              *locker.Locker
	InstallationManager InstallationManager
	EventDispatcher     EventDispatcher
	AutoloadGenerator   *autoload.Generator

	PreferSource, PreferDist, OptimizeAutoloader, ClassMapAuthoritative bool
	ApcuAutoloader                                                      bool
	ApcuAutoloaderPrefix                                                *string
	DevMode, DryRun, DownloadOnly, Verbose, Update, Install             bool
	DumpAutoloader, RunScripts, PreferStable, PreferLowest              bool
	MinimalUpdate, WriteLock, ExecuteOperations, Audit, ErrorOnAudit    bool
	StrictPsrAutoloader                                                 bool
	AuditFormat                                                         string
	IgnoredTypes                                                        []string
	// AllowedTypes is ?array: null allows every type, [] none.
	AllowedTypes  php.Nullable[[]string]
	UpdateMirrors bool
	// UpdateAllowList is nil for PHP's null.
	UpdateAllowList                   []string
	UpdateAllowTransitiveDependencies int
	SuggestedPackagesReporter         *installer.SuggestedPackagesReporter
	PlatformRequirementFilter         version.PlatformRequirementFilter
	AdditionalFixedRepository         repository.RepositoryInterface
	TemporaryConstraints              *repository.ConstraintMap
	AuditConfig                       *advisory.AuditConfig
	PolicyConfig                      *policy.PolicyConfig
	LockTransaction                   *resolver.LockTransaction
}

// State returns the installer's properties.
func (i *Installer) State() InstallerState {
	s := InstallerState{
		IO:                                i.io,
		Config:                            i.config,
		Package:                           i.pkg,
		FixedRootPackage:                  i.fixedRootPackage,
		RepositoryManager:                 i.repositoryManager,
		Locker:                            i.locker,
		InstallationManager:               i.installationManager,
		EventDispatcher:                   i.eventDispatcher,
		PreferSource:                      i.preferSource,
		PreferDist:                        i.preferDist,
		OptimizeAutoloader:                i.optimizeAutoloader,
		ClassMapAuthoritative:             i.classMapAuthoritative,
		ApcuAutoloader:                    i.apcuAutoloader,
		ApcuAutoloaderPrefix:              i.apcuAutoloaderPrefix,
		DevMode:                           i.devMode,
		DryRun:                            i.dryRun,
		DownloadOnly:                      i.downloadOnly,
		Verbose:                           i.verbose,
		Update:                            i.update,
		Install:                           i.install,
		DumpAutoloader:                    i.dumpAutoloader,
		RunScripts:                        i.runScripts,
		PreferStable:                      i.preferStable,
		PreferLowest:                      i.preferLowest,
		MinimalUpdate:                     i.minimalUpdate,
		WriteLock:                         i.writeLock,
		ExecuteOperations:                 i.executeOperations,
		Audit:                             i.audit,
		ErrorOnAudit:                      i.errorOnAudit,
		StrictPsrAutoloader:               i.strictPsrAutoloader,
		AuditFormat:                       i.auditFormat,
		IgnoredTypes:                      slices.Clone(i.ignoredTypes),
		AllowedTypes:                      cloneNullableStrings(i.allowedTypes),
		UpdateMirrors:                     i.updateMirrors,
		UpdateAllowList:                   slices.Clone(i.updateAllowList),
		UpdateAllowTransitiveDependencies: i.updateAllowTransitiveDependencies,
		SuggestedPackagesReporter:         i.suggestedPackagesReporter,
		PlatformRequirementFilter:         i.platformRequirementFilter,
		AdditionalFixedRepository:         i.additionalFixedRepository,
		TemporaryConstraints:              i.temporaryConstraints,
		AuditConfig:                       i.auditConfig,
		PolicyConfig:                      i.policyConfig,
		LockTransaction:                   i.lockTransaction,
	}
	if a, ok := i.downloadManager.(DownloadManagerAdapter); ok {
		s.DownloadManager = a.DownloadManager
	}
	if g, ok := i.autoloadGenerator.(GeneratorAdapter); ok {
		s.AutoloadGenerator = g.Generator
	}

	return s
}

// cloneNullableStrings copies a ?array of strings, keeping null null and
// [] an empty array.
func cloneNullableStrings(n php.Nullable[[]string]) php.Nullable[[]string] {
	if v, ok := n.Get(); ok {
		return php.Some(slices.Clone(v))
	}

	return n
}
