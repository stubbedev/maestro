// Ports src/Composer/Repository/RepositoryInterface.php,
// WritableRepositoryInterface.php, InstalledRepositoryInterface.php,
// ConfigurableRepositoryInterface.php, AdvisoryProviderInterface.php,
// FilterListProviderInterface.php and VersionCacheInterface.php.

package repository

import (
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
)

// Search modes: RepositoryInterface::SEARCH_*.
const (
	SearchFulltext = 0
	SearchName     = 1
	SearchVendor   = 2
)

// SearchResult is an entry of RepositoryInterface::search's result:
// ['name' => ..., 'description' => ..., 'abandoned' => ..., 'url' => ...].
type SearchResult struct {
	Name        string
	Description pkg.NullString
	// Abandoned is nil when the key is unset, else true or the name of the
	// replacement package.
	Abandoned any
	// URL is null when the key is unset.
	URL pkg.NullString
}

// ProviderInfo is a value of RepositoryInterface::getProviders' result:
// ['name' => ..., 'description' => ..., 'type' => ...].
type ProviderInfo struct {
	Name        string
	Description pkg.NullString
	Type        string
}

// LoadResult is RepositoryInterface::loadPackages' result.
type LoadResult struct {
	// NamesFound are the names this repository canonically holds: lower
	// priority repositories must not be asked for them.
	NamesFound []string
	// Packages are the packages that matched, without duplicates.
	Packages []pkg.PackageInterface
}

// AlreadyLoaded is loadPackages' $alreadyLoaded: package name => version
// => package.
type AlreadyLoaded map[string]map[string]pkg.PackageInterface

// RepositoryInterface ports Composer\Repository\RepositoryInterface.
//
// Constraints are nil for PHP's null (findPackages) and match every
// version in FindPackage; PHP's string constraints are parsed by the
// caller (ParseConstraint).
type RepositoryInterface interface {
	// RepoName ports getRepoName: a name representing the repository to
	// the user.
	RepoName() string
	// Class returns the PHP class name, for the plugin shim's mirrors.
	Class() string
	// HasPackage ports hasPackage.
	HasPackage(p pkg.PackageInterface) (bool, error)
	// FindPackage ports findPackage: the first package with the name
	// whose version matches, or nil.
	FindPackage(name string, constraint semver.ConstraintInterface) (pkg.PackageInterface, error)
	// FindPackages ports findPackages.
	FindPackages(name string, constraint semver.ConstraintInterface) ([]pkg.PackageInterface, error)
	// Packages ports getPackages.
	Packages() ([]pkg.PackageInterface, error)
	// LoadPackages ports loadPackages: the packages of the names in
	// packageNameMap that match their constraint (nil: any), the
	// acceptable stabilities (stability name => BasePackage::STABILITY_*)
	// and the stability flags (package name => BasePackage::STABILITY_*),
	// and that alreadyLoaded does not hold.
	LoadPackages(packageNameMap *ConstraintMap, acceptableStabilities, stabilityFlags *php.Array, alreadyLoaded AlreadyLoaded) (LoadResult, error)
	// Search ports search; typ "" is null (any type). For SearchName and
	// SearchVendor the query is a regular expression: callers preg_quote
	// user input.
	Search(query string, mode int, typ string) ([]SearchResult, error)
	// Providers ports getProviders: the packages providing packageName,
	// keyed (and so unique) by name, in order.
	Providers(packageName string) ([]ProviderInfo, error)
	// Count ports count().
	Count() (int, error)
}

// InstallationManager is the part of Composer\Installer\InstallationManager
// the repositories and the Locker use.
type InstallationManager interface {
	// InstallPath ports getInstallPath; ok false is null.
	InstallPath(p pkg.PackageInterface) (path string, ok bool, err error)
}

// WritableRepository ports Composer\Repository\WritableRepositoryInterface.
type WritableRepository interface {
	RepositoryInterface
	// Write ports write: devMode tells whether dev requirements were
	// installed.
	Write(devMode bool, im InstallationManager) error
	// AddPackage ports addPackage.
	AddPackage(p pkg.PackageInterface) error
	// RemovePackage ports removePackage.
	RemovePackage(p pkg.PackageInterface) error
	// CanonicalPackages ports getCanonicalPackages: at most one package of
	// each name, aliases resolved.
	CanonicalPackages() ([]pkg.PackageInterface, error)
	// Reload ports reload.
	Reload() error
	// SetDevPackageNames ports setDevPackageNames.
	SetDevPackageNames(names []string)
	// DevPackageNames ports getDevPackageNames.
	DevPackageNames() []string
	// Rev changes whenever the package list changes (add, remove,
	// reload), for the plugin shim's mirrors.
	Rev() uint64
}

// InstalledRepositoryInterface ports
// Composer\Repository\InstalledRepositoryInterface.
type InstalledRepositoryInterface interface {
	WritableRepository
	// DevMode ports getDevMode: whether dev requirements were installed;
	// ok false is null (not known yet).
	DevMode() (devMode, ok bool)
	// IsFresh ports isFresh: whether packages were never installed in
	// this repository.
	IsFresh() (bool, error)
}

// ConfigurableRepository ports
// Composer\Repository\ConfigurableRepositoryInterface.
type ConfigurableRepository interface {
	// RepoConfig ports getRepoConfig.
	RepoConfig() *php.Array
}

// AdvisoryResult is AdvisoryProviderInterface::getSecurityAdvisories'
// result.
type AdvisoryResult struct {
	NamesFound []string
	// Advisories maps package names to their advisories.
	Advisories *NameMap[[]Advisory]
}

// AdvisoryProvider ports Composer\Repository\AdvisoryProviderInterface.
type AdvisoryProvider interface {
	HasSecurityAdvisories() (bool, error)
	// SecurityAdvisories ports getSecurityAdvisories: packageConstraintMap
	// maps package names to the constraint advisories must affect
	// (MatchAllConstraint for all of them). Without allowPartial every
	// advisory is a *SecurityAdvisory.
	SecurityAdvisories(packageConstraintMap *ConstraintMap, allowPartial bool) (AdvisoryResult, error)
}

// FilterListProvider ports Composer\Repository\FilterListProviderInterface.
type FilterListProvider interface {
	HasFilter() (bool, error)
	// Filter ports getFilter: list name => entries for the packages of
	// packageConstraintMap; configuredLists are the lists the caller is
	// interested in.
	Filter(packageConstraintMap *ConstraintMap, configuredLists []string) (*NameMap[[]*FilterListEntry], error)
	// FilterLists ports getFilterLists.
	FilterLists() ([]string, error)
}

// VersionCache ports Composer\Repository\VersionCacheInterface.
type VersionCache interface {
	// VersionPackage ports getVersionPackage: the package data, false when
	// the identifier is known to have no package, nil for an unknown one.
	VersionPackage(version, identifier string) any
}

// JSONFile is the part of Composer\Json\JsonFile the filesystem
// repositories and the Locker use; *json.File implements it.
type JSONFile interface {
	Path() string
	Exists() bool
	Read() (any, error)
	Write(hash any, options php.JSONFlag) error
}

// EventDispatcher is the Composer\EventDispatcher\EventDispatcher handed to
// repository constructors. This package does not use it; the
// repositories of the subpackages assert the interface they need.
type EventDispatcher any
