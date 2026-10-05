// Package repository ports Composer\Repository (src/Composer/Repository)
// without ComposerRepository and VcsRepository, which live in the
// composerrepo and vcs subpackages and register themselves through
// RepositoryManager's type registry (see docs/PORTING.md).
//
// # Class hierarchy
//
// PHP's repository classes map to Go types as follows:
//
//	RepositoryInterface            RepositoryInterface
//	WritableRepositoryInterface    WritableRepository
//	InstalledRepositoryInterface   InstalledRepositoryInterface
//	ArrayRepository                *ArrayRepository
//	WritableArrayRepository        *WritableArrayRepository (embeds ArrayRepository)
//	InstalledArrayRepository       *InstalledArrayRepository (embeds WritableArrayRepository)
//	FilesystemRepository           *FilesystemRepository (embeds WritableArrayRepository)
//	InstalledFilesystemRepository  *InstalledFilesystemRepository (embeds FilesystemRepository)
//	LockArrayRepository            *LockArrayRepository (embeds ArrayRepository)
//	RootPackageRepository          *RootPackageRepository (embeds ArrayRepository)
//	PackageRepository, PathRepository, ArtifactRepository, PlatformRepository
//	                               embed ArrayRepository
//	CompositeRepository            *CompositeRepository
//	InstalledRepository            *InstalledRepository (embeds CompositeRepository)
//	FilterRepository               *FilterRepository
//
// `instanceof` a concrete class is a type assertion to its pointer type;
// `instanceof CompositeRepository`, which InstalledRepository also
// satisfies, is AsComposite. Class returns the PHP class name, for the
// plugin shim's mirrors.
//
// ArrayRepository's overridable initialize() and addPackage() are
// dispatched to the embedding type, so that PlatformRepository's override
// of addPackage applies to the packages its constructor receives, as in
// PHP. Packages get the outermost repository as their repository.
//
// # Errors
//
// Every repository operation may fail (a remote repository's network
// request, a broken installed.json), so the methods of RepositoryInterface
// return errors where PHP throws. RepoName does not: when listing the
// packages fails, it counts the packages loaded so far.
//
// # State
//
// Repositories are not safe for concurrent use. Nothing is shared between
// repositories beyond what the caller passes in; PlatformRepository's
// process-wide $lastSeenPlatformPhp is a per-repository value here
// (PlatformPhpVersion).
package repository
