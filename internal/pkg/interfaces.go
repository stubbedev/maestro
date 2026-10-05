// Ports src/Composer/Package/PackageInterface.php,
// src/Composer/Package/CompletePackageInterface.php and
// src/Composer/Package/RootPackageInterface.php.

package pkg

import (
	"time"

	"github.com/stubbedev/maestro/internal/php"
)

// DisplayMode is one of PackageInterface::DISPLAY_*.
type DisplayMode int

// PackageInterface::DISPLAY_* constants.
const (
	DisplaySourceRefIfDev DisplayMode = 0
	DisplaySourceRef      DisplayMode = 1
	DisplayDistRef        DisplayMode = 2
)

// Concrete PHP class names, as Class returns them.
const (
	ClassPackage              = `Composer\Package\Package`
	ClassCompletePackage      = `Composer\Package\CompletePackage`
	ClassRootPackage          = `Composer\Package\RootPackage`
	ClassAliasPackage         = `Composer\Package\AliasPackage`
	ClassCompleteAliasPackage = `Composer\Package\CompleteAliasPackage`
	ClassRootAliasPackage     = `Composer\Package\RootAliasPackage`
)

// Repository is the part of Composer\Repository\RepositoryInterface the
// package classes use.
type Repository interface {
	RepoName() string
}

// PlatformRepositoryMarker is implemented by Composer\Repository\PlatformRepository
// (returning true); IsPlatform tests the package's repository for it, as
// `instanceof PlatformRepository` does.
type PlatformRepositoryMarker interface {
	IsPlatformRepository() bool
}

// PackageInterface ports Composer\Package\PackageInterface, plus the
// BasePackage methods every package has.
type PackageInterface interface {
	Name() string
	PrettyName() string
	Names(provides bool) []string
	SetID(id int)
	ID() int
	IsDev() bool
	Type() string
	TargetDir() NullString
	Extra() *php.Array
	SetInstallationSource(typ NullString)
	InstallationSource() NullString
	SourceType() NullString
	SourceURL() NullString
	SourceURLs() []string
	SourceReference() NullString
	SourceMirrors() *php.Array
	SetSourceMirrors(mirrors *php.Array)
	DistType() NullString
	DistURL() NullString
	DistURLs() []string
	DistReference() NullString
	DistSha1Checksum() NullString
	DistMirrors() *php.Array
	SetDistMirrors(mirrors *php.Array)
	Version() string
	PrettyVersion() string
	FullPrettyVersion(truncate bool, displayMode DisplayMode) string
	ReleaseDate() (time.Time, bool)
	Stability() string
	Requires() Links
	Conflicts() Links
	Provides() Links
	Replaces() Links
	DevRequires() Links
	Suggests() *php.Array
	Autoload() *php.Array
	DevAutoload() *php.Array
	IncludePaths() *php.Array
	PhpExt() *php.Array
	SetRepository(repository Repository) error
	Repository() Repository
	Binaries() *php.Array
	UniqueName() string
	NotificationURL() NullString
	String() string
	PrettyString() string
	IsDefaultBranch() bool
	TransportOptions() *php.Array
	SetTransportOptions(options *php.Array)
	SetSourceReference(reference NullString)
	SetDistURL(url NullString)
	SetDistType(typ NullString)
	SetDistReference(reference NullString)
	SetSourceDistReferences(reference string)

	// BasePackage methods.
	IsPlatform() bool
	Equals(other PackageInterface) bool
	StabilityPriority() int

	// Rev changes whenever a setter changes the package (or, for an
	// alias, the aliased package).
	Rev() uint64
	// Class returns the concrete PHP class name (Class* constants).
	Class() string

	base() *basePackage
}

// CompletePackageInterface ports Composer\Package\CompletePackageInterface.
type CompletePackageInterface interface {
	PackageInterface
	Scripts() *php.Array
	SetScripts(scripts *php.Array)
	Repositories() *php.Array
	SetRepositories(repositories *php.Array)
	License() *php.Array
	SetLicense(license *php.Array)
	Keywords() *php.Array
	SetKeywords(keywords *php.Array)
	Description() NullString
	SetDescription(description NullString)
	Homepage() NullString
	SetHomepage(homepage NullString)
	Authors() *php.Array
	SetAuthors(authors *php.Array)
	Support() *php.Array
	SetSupport(support *php.Array)
	Funding() *php.Array
	SetFunding(funding *php.Array)
	IsAbandoned() bool
	ReplacementPackage() NullString
	Abandoned() any
	SetAbandoned(abandoned any)
	ArchiveName() NullString
	SetArchiveName(name NullString)
	ArchiveExcludes() *php.Array
	SetArchiveExcludes(excludes *php.Array)
}

// RootPackageInterface ports Composer\Package\RootPackageInterface.
type RootPackageInterface interface {
	CompletePackageInterface
	Aliases() *php.Array
	MinimumStability() string
	StabilityFlags() *php.Array
	References() *php.Array
	PreferStable() bool
	Config() *php.Array
	SetRequires(requires Links)
	SetDevRequires(devRequires Links)
	SetConflicts(conflicts Links)
	SetProvides(provides Links)
	SetReplaces(replaces Links)
	SetAutoload(autoload *php.Array)
	SetDevAutoload(devAutoload *php.Array)
	SetStabilityFlags(stabilityFlags *php.Array)
	SetMinimumStability(minimumStability string)
	SetPreferStable(preferStable bool)
	SetConfig(config *php.Array)
	SetReferences(references *php.Array)
	SetAliases(aliases *php.Array)
	SetSuggests(suggests *php.Array)
	SetExtra(extra *php.Array)
}

// Alias is implemented by AliasPackage and its subclasses: a type
// assertion to Alias is `instanceof AliasPackage`.
type Alias interface {
	PackageInterface
	AliasOf() PackageInterface
	SetRootPackageAlias(value bool)
	IsRootPackageAlias() bool
	HasSelfVersionRequires() bool
}

// AsPackage reports `$p instanceof Package` and returns the Package part
// of p (for a CompletePackage or RootPackage, the embedded Package).
func AsPackage(p PackageInterface) (*Package, bool) {
	switch p := p.(type) {
	case *Package:
		return p, true
	case *CompletePackage:
		return &p.Package, true
	case *RootPackage:
		return &p.Package, true
	}

	return nil, false
}

// AsCompletePackage reports `$p instanceof CompletePackage` and returns
// its CompletePackage part.
func AsCompletePackage(p PackageInterface) (*CompletePackage, bool) {
	switch p := p.(type) {
	case *CompletePackage:
		return p, true
	case *RootPackage:
		return &p.CompletePackage, true
	}

	return nil, false
}

// Clone ports `clone $package`: a shallow copy without repository and
// with id -1. A RootAliasPackage also clones the package it aliases.
func Clone(p PackageInterface) PackageInterface {
	switch p := p.(type) {
	case *Package:
		return p.Clone()
	case *CompletePackage:
		return p.Clone()
	case *RootPackage:
		return p.Clone()
	case *AliasPackage:
		return p.Clone()
	case *CompleteAliasPackage:
		return p.Clone()
	case *RootAliasPackage:
		return p.Clone()
	}

	panic("pkg: Clone of unknown package type " + p.Class())
}
